package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/cli"
	"github.com/juev/hledger-lsp/internal/document"
	"github.com/juev/hledger-lsp/internal/filetype"
	"github.com/juev/hledger-lsp/internal/formatter"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/lsputil"
	"github.com/juev/hledger-lsp/internal/rules"
	"github.com/juev/hledger-lsp/internal/textutil"
	"github.com/juev/hledger-lsp/internal/workspace"
)

// cliRunner is the subset of the hledger CLI client the server depends on.
// It is an interface so tests can substitute a fake without the real binary.
type cliRunner interface {
	Available() bool
	Run(ctx context.Context, file string, args ...string) (string, error)
}

// diagEntry tracks the pending debounced diagnostics computation for one URI.
type diagEntry struct {
	timer  *time.Timer
	cancel context.CancelFunc
}

type Server struct {
	protocol.UnimplementedServer

	version               string
	client                protocol.Client
	documents             sync.Map
	analyzer              *analyzer.Analyzer
	loader                *include.Loader
	rulesLoader           *rules.Loader
	resolved              sync.Map
	cliClient             cliRunner
	rootURI               string
	workspace             *workspace.Workspace
	settings              serverSettings
	settingsMu            sync.RWMutex
	clientCapabilities    clientCapabilities
	supportsConfiguration bool
	payeeTemplatesCache   sync.Map // map[uri.URI]map[string][]analyzer.PostingTemplate
	alignmentCache        sync.Map // map[uri.URI]int
	tokenCache            *semanticTokensCache
	parseCache            sync.Map // map[uri.URI]*cachedDoc
	docTexts              sync.Map // map[uri.URI]*document.Text
	docTextMu             sync.Mutex

	diagDebounce time.Duration
	diagMu       sync.Mutex
	diagEntries  map[uri.URI]*diagEntry
}

func NewServer() *Server {
	return NewServerWithVersion("dev")
}

func NewServerWithVersion(version string) *Server {
	srv := &Server{
		version:      version,
		analyzer:     analyzer.New(),
		loader:       include.NewLoader(),
		rulesLoader:  rules.NewLoader(),
		diagDebounce: 100 * time.Millisecond,
		diagEntries:  make(map[uri.URI]*diagEntry),
		tokenCache:   newSemanticTokensCache(),
	}
	// Honour unsaved editor content for .rules files that are pulled in via
	// transitive `include` from a currently-open rules file. The loader first
	// consults this getter before reading from disk.
	srv.rulesLoader.SetContentGetter(srv.rulesLoaderContentGetter)
	defaults := defaultServerSettings()
	srv.cliClient = cli.NewClient(defaults.CLI.Path, defaults.CLI.Timeout)
	srv.setSettings(defaults)
	return srv
}

// rulesLoaderContentGetter looks up the given filesystem path in the open
// documents map and returns the editor content if found. When the file is not
// open in the editor, it returns found=false so the rules Loader falls back to
// os.ReadFile.
func (s *Server) rulesLoaderContentGetter(path string) (string, bool, error) {
	docURI := pathToURI(path)
	if content, ok := s.GetDocument(docURI); ok {
		return content, true, nil
	}
	return "", false, nil
}

func (s *Server) reinitCLI(cfg cliSettings) {
	s.cliClient = cli.NewClient(cfg.Path, cfg.Timeout)
}

func (s *Server) SetClient(client protocol.Client) {
	s.client = client
}

// StoreDocument records document content, normalizing line endings the same
// way the didOpen/didChange handlers do. Everything downstream — AST offsets,
// PositionMapper, the rope in internal/document — assumes LF-only text.
func (s *Server) StoreDocument(uri uri.URI, content string) {
	s.documents.Store(uri, textutil.NormalizeLineEndings(content))
}

func (s *Server) Initialize(ctx context.Context, params *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	if client, ok := protocol.ClientFromContext(ctx); ok {
		s.SetClient(client)
	}

	if params != nil {
		s.clientCapabilities = newClientCapabilities(params.Capabilities)
		s.supportsConfiguration = s.clientCapabilities.supportsConfiguration
		settings := parseSettingsFromLSPAny(s.getSettings(), params.InitializationOptions)
		s.setSettings(settings)

		if folders, ok := params.WorkspaceFolders.Get(); ok && len(folders) > 0 {
			s.rootURI = uriToPath(folders[0].URI)
		} else {
			rootURI := params.RootURI //nolint:staticcheck
			if rootURI != nil {
				s.rootURI = uriToPath(*rootURI)
			}
		}
	}

	if s.rootURI != "" {
		s.workspace = workspace.NewWorkspace(s.rootURI, s.loader)
	}

	settings := s.getSettings()

	// Feature flags gate capability registration at Initialize only.
	// LSP capabilities are static after the Initialize handshake; toggling
	// a feature via DidChangeConfiguration requires a server restart to
	// take effect. This is a protocol limitation, not a bug.
	caps := protocol.ServerCapabilities{
		TextDocumentSync: &protocol.TextDocumentSyncOptions{
			OpenClose: boolPtr(true),
			Change:    syncKindPtr(protocol.TextDocumentSyncKindIncremental),
			Save: &protocol.SaveOptions{
				IncludeText: boolPtr(false),
			},
			WillSaveWaitUntil: boolPtr(false),
		},
		DocumentSymbolProvider:    protocol.Boolean(true),
		DocumentHighlightProvider: protocol.Boolean(true),
		SelectionRangeProvider:    protocol.Boolean(true),
		DefinitionProvider:        protocol.Boolean(true),
		ReferencesProvider:        protocol.Boolean(true),
		RenameProvider:            protocol.Boolean(true),
		TypeHierarchyProvider:     protocol.Boolean(true),
	}
	if s.clientCapabilities.supportsRenamePrepare {
		caps.RenameProvider = &protocol.RenameOptions{PrepareProvider: boolPtr(true)}
	}

	if settings.Features.Completion {
		caps.CompletionProvider = &protocol.CompletionOptions{
			TriggerCharacters: []string{":", "@", "=", "0", "1", "2", "3", "4", "5", "6", "7", "8", "9"},
			ResolveProvider:   boolPtr(true),
		}
	}
	if settings.Features.Hover {
		caps.HoverProvider = protocol.Boolean(true)
	}
	if settings.Features.Formatting {
		caps.DocumentFormattingProvider = protocol.Boolean(true)
		caps.DocumentRangeFormattingProvider = protocol.Boolean(true)
		caps.DocumentOnTypeFormattingProvider = protocol.DocumentOnTypeFormattingOptions{
			FirstTriggerCharacter: "\n",
			MoreTriggerCharacter:  []string{"\t"},
		}
	}
	if settings.Features.SemanticTokens {
		caps.SemanticTokensProvider = &protocol.SemanticTokensOptions{
			Legend: GetSemanticTokensLegend(),
			Range:  protocol.Boolean(true),
			Full:   protocol.Boolean(true),
		}
	}
	if settings.Features.FoldingRanges {
		caps.FoldingRangeProvider = protocol.Boolean(true)
	}
	if settings.Features.DocumentLinks {
		caps.DocumentLinkProvider = &protocol.DocumentLinkOptions{}
	}
	if settings.Features.WorkspaceSymbol {
		caps.WorkspaceSymbolProvider = protocol.Boolean(true)
	}
	if settings.Features.CodeActions {
		caps.CodeActionProvider = protocol.Boolean(true)
		if s.clientCapabilities.supportsCodeActionLiterals {
			caps.CodeActionProvider = &protocol.CodeActionOptions{
				CodeActionKinds: []protocol.CodeActionKind{
					protocol.CodeActionKindQuickFix,
					insertInferredAmountKind,
					"source.hledger",
				},
			}
		}
	}

	if settings.Features.CodeLens {
		caps.CodeLensProvider = &protocol.CodeLensOptions{}
	}
	if settings.Features.InlayHints {
		caps.InlayHintProvider = protocol.Boolean(true)
	}
	commands := make([]string, 0, 2)
	if settings.Features.CodeActions {
		commands = append(commands, "hledger.run")
	}
	if settings.Features.CodeLens {
		commands = append(commands, "hledger.fixUnbalanced")
	}
	if len(commands) > 0 {
		caps.ExecuteCommandProvider = protocol.ExecuteCommandOptions{Commands: commands}
	}
	if settings.Features.InlineCompletion {
		caps.InlineCompletionProvider = &protocol.InlineCompletionOptions{}
	}

	return &protocol.InitializeResult{
		Capabilities: caps,
		ServerInfo: protocol.ServerInfo{
			Name:    "hledger-lsp",
			Version: protocol.NewOptional(s.version),
		},
	}, nil
}

func (s *Server) Initialized(_ context.Context, _ *protocol.InitializedParams) error {
	if s.workspace != nil {
		if err := s.workspace.Initialize(); err != nil && s.client != nil {
			_ = s.client.LogMessage(context.Background(), &protocol.LogMessageParams{
				Type:    protocol.MessageTypeWarning,
				Message: "Workspace initialization failed: " + err.Error(),
			})
		}
	}
	go s.refreshConfiguration(context.Background())
	if s.clientCapabilities.supportsDynamicFileWatchers {
		go s.registerFileWatchers()
	}
	return nil
}

func (s *Server) registerFileWatchers() {
	if s.client == nil {
		return
	}
	watchers := make([]protocol.FileSystemWatcher, 0, 6)
	for _, pattern := range []string{"**/*.journal", "**/*.hledger", "**/*.j", "**/*.ledger", "**/*.prices", "**/*.rules"} {
		watchers = append(watchers, protocol.FileSystemWatcher{
			GlobPattern: protocol.Pattern(pattern),
		})
	}

	options, err := protocol.Marshal(protocol.DidChangeWatchedFilesRegistrationOptions{
		Watchers: watchers,
	})
	if err != nil {
		return
	}
	_ = s.client.RegisterCapability(context.Background(), &protocol.RegistrationParams{
		Registrations: []protocol.Registration{
			{
				ID:              "workspace/didChangeWatchedFiles",
				Method:          "workspace/didChangeWatchedFiles",
				RegisterOptions: protocol.LSPAny(options),
			},
		},
	})
}

func (s *Server) Shutdown(ctx context.Context) error {
	return nil
}

func (s *Server) Exit(ctx context.Context) error {
	return nil
}

func (s *Server) DidOpen(ctx context.Context, params *protocol.DidOpenTextDocumentParams) error {
	content := textutil.NormalizeLineEndings(params.TextDocument.Text)
	s.documents.Store(params.TextDocument.URI, content)
	s.payeeTemplatesCache.Clear()
	s.alignmentCache.Delete(params.TextDocument.URI)
	s.invalidateDocText(params.TextDocument.URI)
	if s.workspace != nil && s.loader.FileSizeError(content) == nil {
		if path := uriToPath(params.TextDocument.URI); filetype.IsJournalPath(path) {
			journal, _ := s.cachedJournal(params.TextDocument.URI, content)
			s.workspace.UpdateFileWithJournal(path, content, journal)
			s.loader.InvalidateFile(path)
		}
	}
	s.scheduleDiagnostics(params.TextDocument.URI, content, uint32(params.TextDocument.Version))
	return nil
}

func (s *Server) DidChange(ctx context.Context, params *protocol.DidChangeTextDocumentParams) error {
	if doc, ok := s.documents.Load(params.TextDocument.URI); ok {
		content, ok := doc.(string)
		if !ok {
			return nil
		}
		for _, change := range params.ContentChanges {
			switch change := change.(type) {
			case *protocol.TextDocumentContentChangeWholeDocument:
				if change == nil {
					continue
				}
				content = textutil.NormalizeLineEndings(change.Text)
				s.invalidateDocText(params.TextDocument.URI)
			case *protocol.TextDocumentContentChangePartial:
				if change == nil {
					continue
				}
				content = s.applyChange(params.TextDocument.URI, content, change.Range, textutil.NormalizeLineEndings(change.Text))
			}
		}
		s.documents.Store(params.TextDocument.URI, content)
		s.payeeTemplatesCache.Clear()
		s.alignmentCache.Delete(params.TextDocument.URI)
		if s.workspace != nil {
			if path := uriToPath(params.TextDocument.URI); path != "" {
				// Parse once and reuse the cached journal for workspace indexing
				// (analyze and handlers reuse the same cache entry).
				journal, _ := s.cachedJournal(params.TextDocument.URI, content)
				s.workspace.UpdateFileWithJournal(path, content, journal)
				s.loader.InvalidateFile(path)
			}
		}
		// Rules loader cache is independent of the journal workspace — a
		// .rules file that changes must be evicted so the next completion
		// re-reads its current content (editor or disk).
		if path := uriToPath(params.TextDocument.URI); path != "" && filetype.IsRules(path) {
			s.rulesLoader.InvalidateFile(path)
		}
		s.scheduleDiagnostics(params.TextDocument.URI, content, uint32(params.TextDocument.Version))
	}
	return nil
}

func syncKindPtr(value protocol.TextDocumentSyncKind) *protocol.TextDocumentSyncKind { return &value }

func (s *Server) clearAlignmentCache() {
	s.alignmentCache.Range(func(key, _ any) bool {
		s.alignmentCache.Delete(key)
		return true
	})
}

func (s *Server) DidClose(ctx context.Context, params *protocol.DidCloseTextDocumentParams) error {
	s.cancelDiagnostics(params.TextDocument.URI)
	s.documents.Delete(params.TextDocument.URI)
	s.payeeTemplatesCache.Clear()
	s.alignmentCache.Delete(params.TextDocument.URI)
	s.tokenCache.delete(params.TextDocument.URI)
	s.invalidateParseCache(params.TextDocument.URI)
	s.invalidateDocText(params.TextDocument.URI)
	if s.workspace != nil {
		if path := uriToPath(params.TextDocument.URI); filetype.IsJournalPath(path) {
			if data, err := os.ReadFile(path); err == nil {
				s.workspace.UpdateFile(path, textutil.NormalizeLineEndings(string(data)))
				s.loader.InvalidateFile(path)
			}
		}
	}
	return nil
}

func (s *Server) DidSave(ctx context.Context, params *protocol.DidSaveTextDocumentParams) error {
	s.payeeTemplatesCache.Clear()
	s.alignmentCache.Delete(params.TextDocument.URI)

	if s.workspace != nil {
		if path := uriToPath(params.TextDocument.URI); path != "" {
			if content, ok := s.GetDocument(params.TextDocument.URI); ok {
				s.workspace.UpdateFile(path, content)
			} else if data, err := os.ReadFile(path); err == nil {
				s.workspace.UpdateFile(path, textutil.NormalizeLineEndings(string(data)))
			}
			s.loader.InvalidateFile(path)
		}
	}
	if path := uriToPath(params.TextDocument.URI); path != "" && filetype.IsRules(path) {
		s.rulesLoader.InvalidateFile(path)
	}
	return nil
}

// scheduleDiagnostics debounces diagnostics computation for the given URI.
// A trailing timer coalesces rapid edits; the previous in-flight computation
// is cancelled so a stale result never overwrites newer diagnostics.
func (s *Server) scheduleDiagnostics(docURI uri.URI, content string, version uint32) {
	s.diagMu.Lock()
	defer s.diagMu.Unlock()

	if entry, ok := s.diagEntries[docURI]; ok {
		entry.cancel()
		entry.timer.Stop()
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.diagEntries[docURI] = &diagEntry{
		cancel: cancel,
		timer: time.AfterFunc(s.diagDebounce, func() {
			select {
			case <-ctx.Done():
				return
			default:
			}
			s.publishDiagnostics(ctx, docURI, content, version)
		}),
	}
}

func (s *Server) cancelDiagnostics(docURI uri.URI) {
	s.diagMu.Lock()
	defer s.diagMu.Unlock()
	if entry, ok := s.diagEntries[docURI]; ok {
		entry.cancel()
		entry.timer.Stop()
		delete(s.diagEntries, docURI)
	}
}

func (s *Server) publishDiagnostics(ctx context.Context, docURI uri.URI, content string, version uint32) {
	if s.client == nil {
		return
	}

	settings := s.getSettings()
	if !settings.Features.Diagnostics {
		_ = s.client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{
			URI:         docURI,
			Version:     protocol.NewOptional(int32(version)),
			Diagnostics: []protocol.Diagnostic{},
		})
		return
	}

	if filetype.IsRules(string(docURI)) {
		path := uriToPath(docURI)
		if path == "" {
			return
		}
		// Use expansion-based loader for source-mapped diagnostics.
		diagsByURI := s.analyzeRulesResolved(path, content)
		// Publish for the current document.
		_ = s.client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{
			URI:         docURI,
			Version:     protocol.NewOptional(int32(version)),
			Diagnostics: diagsByURI[docURI],
		})
		// Fan out diagnostics to included child documents.
		for uri, diags := range diagsByURI {
			if uri == docURI {
				continue
			}
			_ = s.client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{
				URI:         uri,
				Diagnostics: diags,
			})
		}
		return
	}

	path := uriToPath(docURI)
	if path == "" {
		return
	}

	// Guard the analyze path: a file over the size limit is not parsed. Publish a
	// single diagnostic instead of running the lexer/parser on every keystroke.
	if sizeErr := s.loader.FileSizeError(content); sizeErr != nil {
		if ctx.Err() != nil {
			return
		}
		_ = s.client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{
			URI:     docURI,
			Version: protocol.NewOptional(int32(version)),
			Diagnostics: []protocol.Diagnostic{
				{
					Severity: protocol.DiagnosticSeverityError,
					Source:   protocol.NewOptional("hledger-lsp"),
					Message:  protocol.String(sizeErr.Message),
				},
			},
		})
		return
	}

	// Prefer the workspace's already-resolved journal only when it was built
	// from this root buffer. Otherwise LoadFromContent keeps diagnostics in sync
	// with the editor after an included file invalidates the tree snapshot.
	var resolved *include.ResolvedJournal
	var loadErrors []include.LoadError
	if s.workspace != nil {
		resolved, loadErrors = s.workspace.ResolvedForRootContent(path, content)
	}
	if resolved == nil {
		resolved, loadErrors = s.loader.LoadFromContent(path, content)
		s.resolved.Store(docURI, resolved)
	}

	diagnostics := s.analyze(docURI, path, content)

	if ctx.Err() != nil {
		return
	}

	for _, err := range loadErrors {
		if err.Kind == include.ErrorParseError {
			continue
		}
		severity := protocol.DiagnosticSeverityError
		if err.Kind == include.ErrorNotJournal {
			severity = protocol.DiagnosticSeverityWarning
		}
		diagnostics = append(diagnostics, protocol.Diagnostic{
			Range:    *astRangeToProtocol(err.Range),
			Severity: severity,
			Source:   protocol.NewOptional("hledger-lsp"),
			Message:  protocol.String(err.Message),
		})
	}

	_ = s.client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{
		URI:         docURI,
		Version:     protocol.NewOptional(int32(version)),
		Diagnostics: diagnostics,
	})
}

func (s *Server) analyze(docURI uri.URI, path, content string) []protocol.Diagnostic {
	journal, parseErrs := s.cachedJournal(docURI, content)

	// Analysis runs on every keystroke, and indexing the document costs a pass
	// over it, so pay for the mapper only when there is a range to convert.
	var mapper *lsputil.PositionMapper
	positionMapper := func() *lsputil.PositionMapper {
		if mapper == nil {
			mapper = lsputil.NewPositionMapper(content)
		}
		return mapper
	}

	diagnostics := make([]protocol.Diagnostic, 0, len(parseErrs))
	for _, err := range parseErrs {
		diagnostics = append(diagnostics, protocol.Diagnostic{
			Range: protocol.Range{
				Start: positionMapper().ByteToLSP(err.Pos.Offset),
				End:   positionMapper().ByteToLSP(err.End.Offset),
			},
			Severity: protocol.DiagnosticSeverityError,
			Source:   protocol.NewOptional("hledger-lsp"),
			Message:  protocol.String(err.Message),
		})
	}

	external := analyzer.ExternalDeclarations{}
	if s.workspace != nil {
		external.Accounts = s.workspace.GetDeclaredAccountsForFile(path)
		external.Commodities = s.workspace.GetDeclaredCommoditiesForFile(path)
	}

	var result *analyzer.AnalysisResult
	if external.Accounts != nil || external.Commodities != nil {
		result = s.analyzer.AnalyzeWithExternalDeclarations(journal, external)
	} else {
		result = s.analyzer.Analyze(journal)
	}

	settings := s.getSettings()
	for _, diag := range result.Diagnostics {
		if !s.shouldIncludeDiagnostic(diag.Code, settings.Diagnostics) {
			continue
		}
		diagnostics = append(diagnostics, protocol.Diagnostic{
			Range:    astRangeToLSP(positionMapper(), diag.Range),
			Severity: toProtocolSeverity(diag.Severity),
			Source:   protocol.NewOptional("hledger-lsp"),
			Message:  protocol.String(diag.Message),
			Code:     protocol.String(diag.Code),
		})
	}

	return diagnostics
}

func (s *Server) getJournalDoc(uri uri.URI) (string, bool) {
	if filetype.IsRules(string(uri)) {
		return "", false
	}
	return s.GetDocument(uri)
}

// analyzeRulesResolved produces diagnostics for a rules file using the
// expansion-based loader. Parse diagnostics from included files are remapped
// to original source URIs. Load errors (cycle, depth, etc.) are also included.
func (s *Server) analyzeRulesResolved(path, content string) map[uri.URI][]protocol.Diagnostic {
	result, loadErrors := s.rulesLoader.LoadFromContent(path, content)
	if result == nil {
		return nil
	}

	byURI := make(map[uri.URI][]protocol.Diagnostic)

	// Use the loader's already-parsed Primary for semantic diagnostics.
	// The loader parsed the expanded text and remapped parse errors to
	// original sources via SourceMap. We use result.Primary for semantic
	// diagnostics (DUPLICATE_FIELDS, etc.) which are at expanded-text
	// positions — remap them through the source map.
	if result.Primary != nil {
		_, parseDiags := rules.Parse(result.Expanded)
		semanticDiags := rules.Diagnostics(result.Primary, parseDiags)
		for _, d := range semanticDiags {
			// Remap the diagnostic range from expanded-text coordinates
			// to the original source file.
			mapped := remapRulesDiagRange(d.Range, result.SourceMap, result.LineOffsets)
			uri := pathToURI(mapped.Path)
			if mapped.Path == "" {
				uri = pathToURI(path)
			}
			byURI[uri] = append(byURI[uri], protocol.Diagnostic{
				Range:    *astRangeToProtocol(mapped.Rng),
				Severity: rulesDiagSeverity(d.Severity),
				Source:   protocol.NewOptional("hledger-lsp"),
				Message:  protocol.String(d.Message),
				Code:     protocol.String(d.Code),
			})
		}
	}

	// Load errors include remapped parse errors (from the loader's source
	// map) and structural errors (cycle, depth, file not found, etc.).
	for _, e := range loadErrors {
		targetPath := e.SourcePath
		if targetPath == "" {
			targetPath = path
		}
		uri := pathToURI(targetPath)
		severity := protocol.DiagnosticSeverityError
		if e.Kind == rules.ErrorNotRules {
			severity = protocol.DiagnosticSeverityWarning
		}
		byURI[uri] = append(byURI[uri], protocol.Diagnostic{
			Range:    *astRangeToProtocol(e.Range),
			Severity: severity,
			Source:   protocol.NewOptional("hledger-lsp"),
			Message:  protocol.String(e.Message),
		})
	}

	// Exact-dedup by (range, message, code, severity).
	for uri, diags := range byURI {
		byURI[uri] = exactDedupDiagnostics(diags)
	}

	return byURI
}

// remapRulesDiagRange maps a diagnostic range from expanded-text coordinates
// to the original source file using the rules source map.
func remapRulesDiagRange(rng ast.Range, sourceMap []rules.SourceMapping, lineOffsets map[string][]int) rules.RemappedRange {
	return rules.RemapRange(rng.Start.Offset, rng.End.Offset, sourceMap, lineOffsets)
}

// exactDedupDiagnostics removes duplicate diagnostics by exact key
// (range, message, code, severity).
func exactDedupDiagnostics(diags []protocol.Diagnostic) []protocol.Diagnostic {
	if len(diags) <= 1 {
		return diags
	}
	type diagKey struct {
		line, col       uint32
		endLine, endCol uint32
		message         string
		code            interface{}
		severity        protocol.DiagnosticSeverity
	}
	seen := make(map[diagKey]bool, len(diags))
	result := make([]protocol.Diagnostic, 0, len(diags))
	for _, d := range diags {
		key := diagKey{
			line:     d.Range.Start.Line,
			col:      d.Range.Start.Character,
			endLine:  d.Range.End.Line,
			endCol:   d.Range.End.Character,
			message:  fmt.Sprint(d.Message),
			code:     d.Code,
			severity: d.Severity,
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, d)
	}
	return result
}

func rulesDiagSeverity(s rules.DiagnosticSeverity) protocol.DiagnosticSeverity {
	if s == rules.SeverityWarning {
		return protocol.DiagnosticSeverityWarning
	}
	return protocol.DiagnosticSeverityError
}

func (s *Server) shouldIncludeDiagnostic(code string, settings diagnosticsSettings) bool {
	switch code {
	case "UNDECLARED_ACCOUNT":
		return settings.UndeclaredAccounts
	case "UNDECLARED_COMMODITY":
		return settings.UndeclaredCommodities
	case "UNBALANCED", "MULTIPLE_INFERRED":
		return settings.UnbalancedTransactions
	default:
		return true
	}
}

func toProtocolSeverity(s analyzer.DiagnosticSeverity) protocol.DiagnosticSeverity {
	switch s {
	case analyzer.SeverityError:
		return protocol.DiagnosticSeverityError
	case analyzer.SeverityWarning:
		return protocol.DiagnosticSeverityWarning
	case analyzer.SeverityInfo:
		return protocol.DiagnosticSeverityInformation
	case analyzer.SeverityHint:
		return protocol.DiagnosticSeverityHint
	default:
		return protocol.DiagnosticSeverityError
	}
}

func (s *Server) DidChangeWatchedFiles(ctx context.Context, params *protocol.DidChangeWatchedFilesParams) error {
	if s.workspace == nil {
		return nil
	}

	affected := make(map[uri.URI]bool)

	for _, change := range params.Changes {
		path := uriToPath(change.URI)
		if path == "" {
			continue
		}

		if _, isOpen := s.documents.Load(change.URI); isOpen {
			continue
		}

		s.loader.InvalidateFile(path)
		if filetype.IsJournalPath(path) {
			s.payeeTemplatesCache.Clear()
		}
		if filetype.IsRules(path) {
			s.rulesLoader.InvalidateFile(path)
		}

		if change.Type == protocol.FileChangeTypeChanged || change.Type == protocol.FileChangeTypeCreated {
			if data, err := os.ReadFile(path); err == nil {
				s.workspace.UpdateFile(path, textutil.NormalizeLineEndings(string(data)))
			}
		}

		parents := s.workspace.GetIncludedBy(path)
		for _, parentPath := range parents {
			parentURI := pathToURI(parentPath)
			if _, isOpen := s.documents.Load(parentURI); isOpen {
				affected[parentURI] = true
			}
		}
	}

	for docURI := range affected {
		if content, ok := s.GetDocument(docURI); ok {
			s.publishDiagnostics(ctx, docURI, content, 0)
		}
	}

	return nil
}

func (s *Server) GetDocument(uri uri.URI) (string, bool) {
	if doc, ok := s.documents.Load(uri); ok {
		if content, ok := doc.(string); ok {
			return content, true
		}
	}
	return "", false
}

// WillSaveWaitUntil is intentionally a no-op: formatting is handled by textDocument/formatting
// to respect the editor's formatOnSave setting.
func (s *Server) WillSaveWaitUntil(_ context.Context, _ *protocol.WillSaveTextDocumentParams) ([]protocol.TextEdit, error) {
	return nil, nil
}

func (s *Server) Format(ctx context.Context, params *protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
	doc, ok := s.getJournalDoc(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	journal, _ := s.cachedJournal(params.TextDocument.URI, doc)
	commodityFormats := s.commodityFormatsForDocument(params.TextDocument.URI)

	settings := s.getSettings()
	opts := formatterOptionsFrom(settings.Formatting)

	return formatter.FormatDocumentWithOptions(journal, doc, commodityFormats, opts), nil
}

// commodityFormatsForDocument returns the commodity formats for the document
// using context-at-position (FormatsAt) when occurrences are available, falling
// back to the workspace tree-wide cache otherwise.
func (s *Server) commodityFormatsForDocument(docURI uri.URI) map[string]formatter.CommodityFormat {
	if resolved := s.getWorkspaceResolved(docURI); resolved != nil && len(resolved.Occurrences) > 0 {
		occID := firstOccurrenceIDForPath(resolved, uriToPath(docURI))
		// Use a large offset to collect all directives and merge-forward
		// commodity declarations for the whole document.
		return resolved.FormatsAt(occID, 1<<30)
	}
	if s.workspace != nil {
		return s.workspace.GetCommodityFormatsForFile(uriToPath(docURI))
	}
	return nil
}

// formatterOptionsFrom builds formatter.Options from the server's formatting
// settings. Single source of truth for FormatDocument, RangeFormatting,
// OnTypeFormatting, and InlineCompletion (ghost text alignment).
func formatterOptionsFrom(f formattingSettings) formatter.Options {
	return formatter.Options{
		IndentSize:            f.IndentSize,
		AlignAmounts:          f.AlignAmounts,
		MinAlignmentColumn:    f.MinAlignmentColumn,
		AmountAlignmentColumn: f.AmountAlignmentColumn,
		AmountAlignmentMode:   f.AmountAlignmentMode,
		AmountAlignmentTarget: f.AmountAlignmentTarget,
	}
}

func applyChange(content string, r protocol.Range, text string) string {
	mapper := lsputil.NewPositionMapper(content)
	return mapper.ApplyChange(r, text)
}

// applyChange applies an incremental LSP edit to the document's cached rope and
// returns the updated content. The rope is persisted per URI so consecutive
// edits do not re-split the whole document: the edit itself is O(log n) in the
// line count, and the returned string is materialized once (O(n)) for the
// string-based consumers downstream. content is the authoritative pre-edit
// document text.
func (s *Server) applyChange(docURI uri.URI, content string, r protocol.Range, text string) string {
	s.docTextMu.Lock()
	defer s.docTextMu.Unlock()
	var dt *document.Text
	if v, ok := s.docTexts.Load(docURI); ok {
		dt = v.(*document.Text)
	}
	// The persisted rope is only trustworthy while it matches the authoritative
	// content. If it diverged (e.g. concurrent DidChange interleaving), rebuild
	// from content so the edit lands at the correct position instead of
	// corrupting the buffer. In the common in-sync case dt.String() is the cached
	// string identical to content, so this check is cheap.
	if dt == nil || dt.String() != content {
		dt = document.NewText(content)
	}
	dt.ApplyChange(r, text)
	updated := dt.String()
	s.docTexts.Store(docURI, dt)
	return updated
}

// invalidateDocText drops the cached rope for a document so the next incremental
// edit rebuilds it from the current content (called on DidOpen, full changes and
// DidClose).
func (s *Server) invalidateDocText(docURI uri.URI) {
	s.docTextMu.Lock()
	s.docTexts.Delete(docURI)
	s.docTextMu.Unlock()
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start <= len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func uriToPath(docURI uri.URI) string {
	s := string(docURI)
	if !strings.HasPrefix(s, "file://") {
		return ""
	}
	return filepath.Clean(docURI.FsPath())
}

func (s *Server) GetResolved(docURI uri.URI) *include.ResolvedJournal {
	if r, ok := s.resolved.Load(docURI); ok {
		if resolved, ok := r.(*include.ResolvedJournal); ok {
			return resolved
		}
	}
	return nil
}

func (s *Server) getWorkspaceResolved(docURI uri.URI) *include.ResolvedJournal {
	if s.workspace != nil {
		path := uriToPath(docURI)
		if resolved := s.workspace.GetResolvedForFile(path); resolved != nil {
			return resolved
		}
	}
	return s.GetResolved(docURI)
}

func (s *Server) RootURI() string {
	return s.rootURI
}

func (s *Server) Workspace() *workspace.Workspace {
	return s.workspace
}
