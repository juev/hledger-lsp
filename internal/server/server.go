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
	"github.com/juev/hledger-lsp/internal/parser"
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
	documents             documentStore
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

	diagDebounce time.Duration
	diagMu       sync.Mutex
	diagEntries  map[uri.URI]*diagEntry

	journalDiagMu sync.Mutex
	journalDiag   *journalDiagEntry
	docVersions   sync.Map // map[uri.URI]uint32
	warnedMu      sync.Mutex
	warned        sync.Map // map[uri.URI]map[string]bool: one log line per condition
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

// documentStore holds one rope per open document. The rope is the source of
// truth: an incremental edit applies to it in O(log n), and the flat string the
// rest of the server expects is materialized on demand and cached inside the
// rope until the next edit. Store and Load keep a string-shaped API so callers
// that only have content do not have to know about the rope.
type documentStore struct {
	m sync.Map // map[uri.URI]*document.Text
}

func (d *documentStore) Store(docURI uri.URI, content string) {
	d.m.Store(docURI, document.NewText(textutil.NormalizeLineEndings(content)))
}

// rope returns the document's rope, for callers that want to edit it or read a
// line without materializing the whole text.
func (d *documentStore) rope(docURI uri.URI) (*document.Text, bool) {
	v, ok := d.m.Load(docURI)
	if !ok {
		return nil, false
	}
	text, ok := v.(*document.Text)
	return text, ok
}

func (d *documentStore) Load(docURI uri.URI) (string, bool) {
	text, ok := d.rope(docURI)
	if !ok {
		return "", false
	}
	return text.String(), true
}

func (d *documentStore) Delete(docURI uri.URI) {
	d.m.Delete(docURI)
}

// Range visits every open document with its rope.
func (d *documentStore) Range(f func(docURI uri.URI, text *document.Text) bool) {
	d.m.Range(func(key, value any) bool {
		docURI, ok := key.(uri.URI)
		if !ok {
			return true
		}
		text, ok := value.(*document.Text)
		if !ok {
			return true
		}
		return f(docURI, text)
	})
}

// StoreDocument records document content, normalizing line endings the same
// way the didOpen/didChange handlers do. Everything downstream — AST offsets,
// PositionMapper, the rope in internal/document — assumes LF-only text.
func (s *Server) StoreDocument(uri uri.URI, content string) {
	s.documents.Store(uri, content)
}

func (s *Server) Initialize(ctx context.Context, params *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	if client, ok := protocol.ClientFromContext(ctx); ok {
		s.SetClient(client)
	}

	if params != nil {
		s.clientCapabilities = newClientCapabilities(params.Capabilities)
		s.supportsConfiguration = s.clientCapabilities.supportsConfiguration
		settings, issues := parseSettingsFromLSPAnyWithIssues(s.getSettings(), params.InitializationOptions)
		for _, issue := range issues {
			s.logMessage(protocol.MessageTypeWarning, "hledger-lsp: "+issue)
		}
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
		// Declare the encoding explicitly. The server only implements UTF-16
		// offsets, which is also LSP's mandatory default, so no negotiation is
		// possible; saying so avoids clients guessing.
		PositionEncoding: protocol.PositionEncodingKindUTF16,
		// Only the first workspace folder is indexed, so advertise that limitation
		// instead of accepting folders the server would ignore.
		Workspace: &protocol.WorkspaceOptions{
			WorkspaceFolders: &protocol.WorkspaceFoldersServerCapabilities{
				Supported:           boolPtr(false),
				ChangeNotifications: protocol.Boolean(false),
			},
		},
	}
	if s.clientCapabilities.supportsRenamePrepare {
		caps.RenameProvider = &protocol.RenameOptions{PrepareProvider: boolPtr(true)}
	}

	if settings.Features.Completion {
		caps.Experimental = protocol.LSPAny(`{"hledgerCompletion":{"accountScope":true}}`)
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
		if err := s.workspace.Initialize(); err != nil {
			s.logMessage(protocol.MessageTypeWarning, "hledger-lsp: workspace initialization failed: "+err.Error())
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
	s.setDocumentVersion(params.TextDocument.URI, uint32(params.TextDocument.Version))
	s.payeeTemplatesCache.Clear()
	s.alignmentCache.Delete(params.TextDocument.URI)
	text, _ := s.documents.rope(params.TextDocument.URI)
	var revision uint64
	if s.workspace != nil && s.loader.FileSizeError(content) == nil {
		if path := uriToPath(params.TextDocument.URI); filetype.IsJournalPath(path) {
			revision = s.workspace.MarkFileDirty(path, text)
			s.loader.InvalidateFile(path)
		}
	}
	s.scheduleDiagnostics(params.TextDocument.URI, text, uint32(params.TextDocument.Version), revision)
	return nil
}

func (s *Server) DidChange(ctx context.Context, params *protocol.DidChangeTextDocumentParams) error {
	text, ok := s.documents.rope(params.TextDocument.URI)
	if !ok {
		return nil
	}
	// The edits apply to the rope and nothing is materialized here: the flat
	// string is produced by the first reader that needs it, and the workspace
	// flattens the rope once per recompute rather than once per keystroke.
	for _, change := range params.ContentChanges {
		switch change := change.(type) {
		case *protocol.TextDocumentContentChangeWholeDocument:
			if change == nil {
				continue
			}
			text.Replace(change.Text)
		case *protocol.TextDocumentContentChangePartial:
			if change == nil {
				continue
			}
			text.ApplyChange(change.Range, textutil.NormalizeLineEndings(change.Text))
		}
	}
	s.setDocumentVersion(params.TextDocument.URI, uint32(params.TextDocument.Version))
	s.payeeTemplatesCache.Clear()
	s.alignmentCache.Delete(params.TextDocument.URI)
	// Record the edit without parsing or re-resolving anything: the whole
	// journal is re-parsed and re-indexed on the first read that needs tree
	// data, so a keystroke stays O(log n) and a burst collapses into one pass.
	var revision uint64
	if s.workspace != nil {
		if path := uriToPath(params.TextDocument.URI); path != "" {
			revision = s.workspace.MarkFileDirty(path, text)
			s.loader.InvalidateFile(path)
		}
	}
	// Rules loader cache is independent of the journal workspace — a
	// .rules file that changes must be evicted so the next completion
	// re-reads its current content (editor or disk).
	if path := uriToPath(params.TextDocument.URI); path != "" && filetype.IsRules(path) {
		s.rulesLoader.InvalidateFile(path)
	}
	s.scheduleDiagnostics(params.TextDocument.URI, text, uint32(params.TextDocument.Version), revision)
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
	// Drop the published diagnostics with the document, so a closed file does not
	// keep stale problems in the client's panel.
	if s.client != nil {
		_ = s.client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{
			URI:         params.TextDocument.URI,
			Diagnostics: []protocol.Diagnostic{},
		})
	}
	s.documents.Delete(params.TextDocument.URI)
	s.docVersions.Delete(params.TextDocument.URI)
	s.forgetWarnings(params.TextDocument.URI)
	s.payeeTemplatesCache.Clear()
	s.alignmentCache.Delete(params.TextDocument.URI)
	s.tokenCache.delete(params.TextDocument.URI)
	s.invalidateParseCache(params.TextDocument.URI)
	if s.workspace != nil {
		if path := uriToPath(params.TextDocument.URI); filetype.IsJournalPath(path) {
			if data, err := os.ReadFile(path); err == nil {
				s.workspace.MarkFileDirty(path, workspace.StaticContent(textutil.NormalizeLineEndings(string(data))))
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
			if text, ok := s.documents.rope(params.TextDocument.URI); ok {
				s.workspace.MarkFileDirty(path, text)
			} else if data, err := os.ReadFile(path); err == nil {
				s.workspace.MarkFileDirty(path, workspace.StaticContent(textutil.NormalizeLineEndings(string(data))))
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
func (s *Server) scheduleDiagnostics(docURI uri.URI, text *document.Text, version uint32, revision uint64) {
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
			s.publishDiagnostics(ctx, docURI, text, version, revision)
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

func (s *Server) publishDiagnostics(ctx context.Context, docURI uri.URI, text *document.Text, version uint32, revision uint64) {
	if s.client == nil {
		return
	}
	if text == nil {
		return
	}
	// The debounce coalesces a burst, so by the time this runs the rope holds
	// the newest content; materializing here is what keeps the keystroke path
	// free of it. A run that lost the race publishes against the current
	// content under its own version and is discarded by the client.
	content := text.Materialize()

	settings := s.getSettings()
	if !settings.Features.Diagnostics {
		s.publishDiagnosticSet(ctx, docURI, []protocol.Diagnostic{}, &version)
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
			Diagnostics: exactDedupDiagnostics(diagsByURI[docURI]),
		})
		// Fan out diagnostics to included child documents.
		for uri, diags := range diagsByURI {
			if uri == docURI {
				continue
			}
			_ = s.client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{
				URI:         uri,
				Diagnostics: exactDedupDiagnostics(diags),
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
		// Tell the user once per document instead of silently ignoring the file.
		s.warnOnce(docURI, "file-too-large", "hledger-lsp: "+sizeErr.Message)
		s.publishDiagnosticSet(ctx, docURI, []protocol.Diagnostic{
			{
				Severity: protocol.DiagnosticSeverityError,
				Source:   protocol.NewOptional("hledger-lsp"),
				Message:  protocol.String(sizeErr.Message),
			},
		}, &version)
		return
	}

	// Prefer the workspace's already-resolved journal only when it was built
	// from this root buffer. Otherwise LoadFromContent keeps diagnostics in sync
	// with the editor after an included file invalidates the tree snapshot. This
	// per-document view is also what handlers that edit the current file (such as
	// the declaration quick fixes) rely on.
	var resolved *include.ResolvedJournal
	var loadErrors []include.LoadError
	if s.workspace != nil {
		resolved, loadErrors = s.workspace.ResolvedForRootContent(path, revision)
	}
	if resolved == nil {
		resolved, loadErrors = s.loader.LoadFromContent(path, content)
		s.resolved.Store(docURI, resolved)
	}

	// Balance rules and assertions depend on every file of the journal, so an
	// included file is evaluated in the context of the root that includes it
	// instead of as a journal of its own; otherwise an assertion in a year file
	// would not see the balances the journal's other files establish. The
	// workspace keeps those trees fresh with the open buffer, and sorts them by
	// root path, so the primary root is chosen deterministically when several
	// journals include the file.
	journalTree := resolved
	journalErrors := loadErrors
	partialContext := false
	if s.workspace != nil {
		trees := s.workspace.GetIncludeTreesForFile(path)
		switch {
		case len(trees) == 0:
			// The workspace does not know this file, so there may be a larger
			// journal behind it that is not open here. The verdict is qualified so
			// the user knows why hledger accepts a file the server flags.
			partialContext = true
		case trees[0].RootPath != path:
			journalTree, journalErrors = trees[0].Resolved, trees[0].LoadErrors
		}
	}

	byURI := s.sourcedDiagnosticsByURI(docURI, path, s.journalDiagnostics(journalTree, partialContext), settings.Diagnostics)

	// The analysed document always gets a publish, even when it has no problems:
	// the empty list is what clears previously reported diagnostics.
	if _, ok := byURI[docURI]; !ok {
		byURI[docURI] = nil
	}

	// Per-buffer diagnostics: parse errors, account declarations and date tags.
	// Balance and assertion codes come from the journal-level pass above, which
	// evaluates the whole include tree in hledger's date order.
	byURI[docURI] = append(byURI[docURI], s.analyzeWithJournalBalance(docURI, path, content, false)...)

	if ctx.Err() != nil {
		return
	}

	// A load failure belongs to the file that contains the failing directive.
	for _, err := range journalErrors {
		if err.Kind != include.ErrorParseError {
			s.warnOnce(docURI, err.ErrorCode()+":"+err.Path, "hledger-lsp: "+err.Message)
		}

		if err.Kind == include.ErrorParseError && (err.SourcePath == "" || err.SourcePath == path) {
			// Already reported by the per-buffer analysis of this document.
			continue
		}

		target := docURI
		if err.SourcePath != "" && err.SourcePath != path {
			target = pathToURI(err.SourcePath)
		}

		diagnostic := loadErrorDiagnostic(err)
		if mapper := s.mapperFor(target); mapper != nil {
			diagnostic.Range = astRangeToLSP(mapper, err.Range)
		}
		byURI[target] = append(byURI[target], diagnostic)
	}

	for target, diagnostics := range byURI {
		if target == docURI {
			s.publishDiagnosticSet(ctx, target, exactDedupDiagnostics(diagnostics), &version)
			continue
		}
		s.publishDiagnosticSet(ctx, target, exactDedupDiagnostics(diagnostics), nil)
	}
}

// analyze returns every per-buffer diagnostic, including the balance codes the
// journal-level pass also produces. Handlers that only need a buffer's own view
// (code actions, semantic tokens) use it directly.
func (s *Server) analyze(docURI uri.URI, path, content string) []protocol.Diagnostic {
	return s.analyzeWithJournalBalance(docURI, path, content, true)
}

// analyzeWithJournalBalance produces per-buffer diagnostics. When includeJournalBalance
// is false the balance and assertion codes are skipped: they belong to
// CheckJournalBalance, which sees the whole include tree in date order.
func (s *Server) analyzeWithJournalBalance(docURI uri.URI, path, content string, includeJournalBalance bool) []protocol.Diagnostic {
	_, parseErrs := s.cachedJournal(docURI, content)

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
		code := err.Code
		if code == "" {
			code = parser.CodeParseError
		}
		diagnostics = append(diagnostics, protocol.Diagnostic{
			Range: protocol.Range{
				Start: positionMapper().ByteToLSP(err.Pos.Offset),
				End:   positionMapper().ByteToLSP(err.End.Offset),
			},
			Severity: protocol.DiagnosticSeverityError,
			Source:   protocol.NewOptional("hledger-lsp"),
			Message:  protocol.String(err.Message),
			Code:     protocol.String(code),
		})
	}

	external := analyzer.ExternalDeclarations{}
	if s.workspace != nil {
		external.Accounts = s.workspace.GetDeclaredAccountsForFile(path)
		external.Commodities = s.workspace.GetDeclaredCommoditiesForFile(path)
	}

	result := s.cachedAnalysis(docURI, content, external)

	settings := s.getSettings()
	for _, diag := range result.Diagnostics {
		if !includeJournalBalance && isJournalBalanceCode(diag.Code) {
			continue
		}
		if !s.shouldIncludeDiagnostic(diag.Code, settings.Diagnostics) {
			continue
		}
		converted := protocol.Diagnostic{
			Range:    astRangeToLSP(positionMapper(), diag.Range),
			Severity: toProtocolSeverity(diag.Severity),
			Source:   protocol.NewOptional("hledger-lsp"),
			Message:  protocol.String(diag.Message),
			Code:     protocol.String(diag.Code),
		}
		if len(diag.Data) > 0 {
			if raw, err := protocol.Marshal(diag.Data); err == nil {
				converted.Data = protocol.LSPAny(raw)
			}
		}
		diagnostics = append(diagnostics, converted)
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

// exactDedupDiagnostics removes duplicate diagnostics by (range, message). The
// code is deliberately not part of the key: the same failure can be reported
// twice (once by the loader, once by the analyzer, and in rules files once with
// a code and once without), and the user must still see it only once. When
// duplicates disagree on severity the most severe one wins.
func exactDedupDiagnostics(diags []protocol.Diagnostic) []protocol.Diagnostic {
	if len(diags) <= 1 {
		return diags
	}
	type diagKey struct {
		line, col       uint32
		endLine, endCol uint32
		message         string
	}
	seen := make(map[diagKey]int, len(diags))
	result := make([]protocol.Diagnostic, 0, len(diags))
	for _, d := range diags {
		key := diagKey{
			line:    d.Range.Start.Line,
			col:     d.Range.Start.Character,
			endLine: d.Range.End.Line,
			endCol:  d.Range.End.Character,
			message: fmt.Sprint(d.Message),
		}
		if index, ok := seen[key]; ok {
			if d.Severity < result[index].Severity {
				result[index] = d
			}
			continue
		}
		seen[key] = len(result)
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
		return settings.AccountCheck != accountCheckOff
	case "UNDECLARED_COMMODITY":
		return settings.UndeclaredCommodities
	case analyzer.CodeUnbalanced, analyzer.CodeMultipleInferred:
		return settings.UnbalancedTransactions
	case analyzer.CodeBalanceAssertionFailed:
		return settings.BalanceAssertions
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
				s.workspace.MarkFileDirty(path, workspace.StaticContent(textutil.NormalizeLineEndings(string(data))))
			}
		}

		// Refreshes the pending edits before answering, so the republish below
		// sees the file as it is on disk now.
		parents := s.workspace.GetIncludedBy(path)
		for _, parentPath := range parents {
			parentURI := pathToURI(parentPath)
			if _, isOpen := s.documents.Load(parentURI); isOpen {
				affected[parentURI] = true
			}
		}
	}

	for docURI := range affected {
		// Revision before content, as in republishDiagnostics: a concurrent edit
		// can only make the pair mismatch, never wrongly match.
		var revision uint64
		if s.workspace != nil {
			revision = s.workspace.ContentRevision(uriToPath(docURI))
		}
		if text, ok := s.documents.rope(docURI); ok {
			s.publishDiagnostics(ctx, docURI, text, s.documentVersion(docURI), revision)
		}
	}

	return nil
}

func (s *Server) GetDocument(uri uri.URI) (string, bool) {
	return s.documents.Load(uri)
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
