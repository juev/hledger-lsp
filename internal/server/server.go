package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/cli"
	"github.com/juev/hledger-lsp/internal/filetype"
	"github.com/juev/hledger-lsp/internal/formatter"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/lsputil"
	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/juev/hledger-lsp/internal/rules"
	"github.com/juev/hledger-lsp/internal/workspace"
)

type Server struct {
	client                protocol.Client
	documents             sync.Map
	analyzer              *analyzer.Analyzer
	loader                *include.Loader
	rulesLoader           *rules.Loader
	resolved              sync.Map
	cliClient             *cli.Client
	rootURI               string
	workspace             *workspace.Workspace
	settings              serverSettings
	settingsMu            sync.RWMutex
	supportsConfiguration bool
	payeeTemplatesCache   sync.Map // map[protocol.DocumentURI]map[string][]analyzer.PostingTemplate
	alignmentCache        sync.Map // map[protocol.DocumentURI]int
}

func NewServer() *Server {
	srv := &Server{
		analyzer:    analyzer.New(),
		loader:      include.NewLoader(),
		rulesLoader: rules.NewLoader(),
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

func (s *Server) StoreDocument(uri protocol.DocumentURI, content string) {
	s.documents.Store(uri, content)
}

func (s *Server) Initialize(ctx context.Context, params *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	if params != nil && params.Capabilities.Workspace != nil {
		s.supportsConfiguration = params.Capabilities.Workspace.Configuration
	}
	if params != nil {
		settings := parseSettingsFromRaw(s.getSettings(), params.InitializationOptions)
		s.setSettings(settings)

		if len(params.WorkspaceFolders) > 0 {
			s.rootURI = uriToPath(protocol.DocumentURI(params.WorkspaceFolders[0].URI))
		} else {
			rootURI := params.RootURI //nolint:staticcheck
			if rootURI != "" {
				s.rootURI = uriToPath(rootURI)
			}
		}
	}

	if s.rootURI != "" {
		s.workspace = workspace.NewWorkspace(s.rootURI, s.loader)
	}

	settings := s.getSettings()

	caps := protocol.ServerCapabilities{
		TextDocumentSync: protocol.TextDocumentSyncOptions{
			OpenClose: true,
			Change:    protocol.TextDocumentSyncKindIncremental,
			Save: &protocol.SaveOptions{
				IncludeText: false,
			},
			WillSaveWaitUntil: false,
		},
		DocumentSymbolProvider:    true,
		DocumentHighlightProvider: true,
		SelectionRangeProvider:    true,
		DefinitionProvider:        true,
		ReferencesProvider:        true,
		RenameProvider: &protocol.RenameOptions{
			PrepareProvider: true,
		},
	}

	if settings.Features.Completion {
		caps.CompletionProvider = &protocol.CompletionOptions{
			TriggerCharacters: []string{":", "@", "=", "0", "1", "2", "3", "4", "5", "6", "7", "8", "9"},
			ResolveProvider:   true,
		}
	}
	if settings.Features.Hover {
		caps.HoverProvider = true
	}
	if settings.Features.Formatting {
		caps.DocumentFormattingProvider = true
		caps.DocumentRangeFormattingProvider = true
		caps.DocumentOnTypeFormattingProvider = &protocol.DocumentOnTypeFormattingOptions{
			FirstTriggerCharacter: "\n",
			MoreTriggerCharacter:  []string{"\t"},
		}
	}
	if settings.Features.SemanticTokens {
		caps.SemanticTokensProvider = GetSemanticTokensCapabilities()
	}
	if settings.Features.FoldingRanges {
		caps.FoldingRangeProvider = true
	}
	if settings.Features.DocumentLinks {
		caps.DocumentLinkProvider = &protocol.DocumentLinkOptions{}
	}
	if settings.Features.WorkspaceSymbol {
		caps.WorkspaceSymbolProvider = true
	}
	if settings.Features.CodeActions {
		caps.CodeActionProvider = &protocol.CodeActionOptions{
			CodeActionKinds: []protocol.CodeActionKind{
				protocol.QuickFix,
				"source.hledger",
			},
		}
		caps.ExecuteCommandProvider = &protocol.ExecuteCommandOptions{
			Commands: []string{"hledger.run"},
		}
	}

	if settings.Features.CodeLens {
		caps.CodeLensProvider = &protocol.CodeLensOptions{}
	}
	if settings.Features.InlineCompletion {
		caps.Experimental = map[string]any{
			"inlineCompletionProvider": true,
		}
	}

	return &protocol.InitializeResult{
		Capabilities: caps,
		ServerInfo: &protocol.ServerInfo{
			Name:    "hledger-lsp",
			Version: "0.1.0",
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
	go s.registerFileWatchers()
	return nil
}

func (s *Server) registerFileWatchers() {
	if s.client == nil {
		return
	}
	watchers := make([]protocol.FileSystemWatcher, 0, 6)
	for _, pattern := range []string{"**/*.journal", "**/*.hledger", "**/*.j", "**/*.ledger", "**/*.prices", "**/*.rules"} {
		watchers = append(watchers, protocol.FileSystemWatcher{
			GlobPattern: pattern,
		})
	}

	_ = s.client.RegisterCapability(context.Background(), &protocol.RegistrationParams{
		Registrations: []protocol.Registration{
			{
				ID:     "workspace/didChangeWatchedFiles",
				Method: "workspace/didChangeWatchedFiles",
				RegisterOptions: protocol.DidChangeWatchedFilesRegistrationOptions{
					Watchers: watchers,
				},
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
	content := normalizeLineEndings(params.TextDocument.Text)
	s.documents.Store(params.TextDocument.URI, content)
	s.alignmentCache.Delete(params.TextDocument.URI)
	go s.publishDiagnostics(ctx, params.TextDocument.URI, content)
	return nil
}

func (s *Server) DidChange(ctx context.Context, params *protocol.DidChangeTextDocumentParams) error {
	if doc, ok := s.documents.Load(params.TextDocument.URI); ok {
		content, ok := doc.(string)
		if !ok {
			return nil
		}
		for _, change := range params.ContentChanges {
			if isFullChange(change.Range) {
				content = normalizeLineEndings(change.Text)
			} else {
				content = applyChange(content, change.Range, normalizeLineEndings(change.Text))
			}
		}
		s.documents.Store(params.TextDocument.URI, content)
		s.alignmentCache.Delete(params.TextDocument.URI)
		if s.workspace != nil {
			if path := uriToPath(params.TextDocument.URI); path != "" {
				s.workspace.UpdateFile(path, content)
				s.loader.InvalidateFile(path)
			}
		}
		// Rules loader cache is independent of the journal workspace — a
		// .rules file that changes must be evicted so the next completion
		// re-reads its current content (editor or disk).
		if path := uriToPath(params.TextDocument.URI); path != "" && filetype.IsRules(path) {
			s.rulesLoader.InvalidateFile(path)
		}
		go s.publishDiagnostics(ctx, params.TextDocument.URI, content)
	}
	return nil
}

func normalizeLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

func isFullChange(r protocol.Range) bool {
	return r.Start.Line == 0 && r.Start.Character == 0 &&
		r.End.Line == 0 && r.End.Character == 0
}

func (s *Server) clearAlignmentCache() {
	s.alignmentCache.Range(func(key, _ any) bool {
		s.alignmentCache.Delete(key)
		return true
	})
}

func (s *Server) DidClose(ctx context.Context, params *protocol.DidCloseTextDocumentParams) error {
	s.documents.Delete(params.TextDocument.URI)
	s.alignmentCache.Delete(params.TextDocument.URI)
	tokenCache.delete(params.TextDocument.URI)
	return nil
}

func (s *Server) DidSave(ctx context.Context, params *protocol.DidSaveTextDocumentParams) error {
	s.payeeTemplatesCache.Delete(params.TextDocument.URI)
	s.alignmentCache.Delete(params.TextDocument.URI)

	if s.workspace != nil {
		if path := uriToPath(params.TextDocument.URI); path != "" {
			if content, ok := s.GetDocument(params.TextDocument.URI); ok {
				s.workspace.UpdateFile(path, content)
			} else if data, err := os.ReadFile(path); err == nil {
				s.workspace.UpdateFile(path, normalizeLineEndings(string(data)))
			}
			s.loader.InvalidateFile(path)
		}
	}
	if path := uriToPath(params.TextDocument.URI); path != "" && filetype.IsRules(path) {
		s.rulesLoader.InvalidateFile(path)
	}
	return nil
}

func (s *Server) publishDiagnostics(ctx context.Context, docURI protocol.DocumentURI, content string) {
	if s.client == nil {
		return
	}

	settings := s.getSettings()
	if !settings.Features.Diagnostics {
		_ = s.client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{
			URI:         docURI,
			Diagnostics: []protocol.Diagnostic{},
		})
		return
	}

	if filetype.IsRules(string(docURI)) {
		_ = s.client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{
			URI:         docURI,
			Diagnostics: s.analyzeRules(content),
		})
		return
	}

	path := uriToPath(docURI)
	if path == "" {
		return
	}
	resolved, loadErrors := s.loader.LoadFromContent(path, content)
	s.resolved.Store(docURI, resolved)

	diagnostics := s.analyze(path, content)

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
			Source:   "hledger-lsp",
			Message:  err.Message,
		})
	}

	_ = s.client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{
		URI:         docURI,
		Diagnostics: diagnostics,
	})
}

func (s *Server) analyze(path, content string) []protocol.Diagnostic {
	journal, parseErrs := parser.Parse(content)

	diagnostics := make([]protocol.Diagnostic, 0, len(parseErrs))
	for _, err := range parseErrs {
		diagnostics = append(diagnostics, protocol.Diagnostic{
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      uint32(max(0, err.Pos.Line-1)),
					Character: uint32(max(0, err.Pos.Column-1)),
				},
				End: protocol.Position{
					Line:      uint32(max(0, err.End.Line-1)),
					Character: uint32(max(0, err.End.Column-1)),
				},
			},
			Severity: protocol.DiagnosticSeverityError,
			Source:   "hledger-lsp",
			Message:  err.Message,
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
			Range:    *astRangeToProtocol(diag.Range),
			Severity: toProtocolSeverity(diag.Severity),
			Source:   "hledger-lsp",
			Message:  diag.Message,
			Code:     diag.Code,
		})
	}

	return diagnostics
}

func (s *Server) getJournalDoc(uri protocol.DocumentURI) (string, bool) {
	if filetype.IsRules(string(uri)) {
		return "", false
	}
	return s.GetDocument(uri)
}

func (s *Server) analyzeRules(content string) []protocol.Diagnostic {
	// Rules diagnostics use their own code set; shouldIncludeDiagnostic does not apply here.
	rf, parseDiags := rules.Parse(content)
	ruleDiags := rules.Diagnostics(rf, parseDiags)
	diags := make([]protocol.Diagnostic, 0, len(ruleDiags))
	for _, d := range ruleDiags {
		diags = append(diags, protocol.Diagnostic{
			Range:    *astRangeToProtocol(d.Range),
			Severity: rulesDiagSeverity(d.Severity),
			Source:   "hledger-lsp",
			Message:  d.Message,
			Code:     d.Code,
		})
	}
	return diags
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

	affected := make(map[protocol.DocumentURI]bool)

	for _, change := range params.Changes {
		path := uriToPath(change.URI)
		if path == "" {
			continue
		}

		if _, isOpen := s.documents.Load(change.URI); isOpen {
			continue
		}

		s.loader.InvalidateFile(path)
		if filetype.IsRules(path) {
			s.rulesLoader.InvalidateFile(path)
		}

		if change.Type == protocol.FileChangeTypeChanged || change.Type == protocol.FileChangeTypeCreated {
			if data, err := os.ReadFile(path); err == nil {
				s.workspace.UpdateFile(path, normalizeLineEndings(string(data)))
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
			s.publishDiagnostics(ctx, docURI, content)
		}
	}

	return nil
}

func (s *Server) GetDocument(uri protocol.DocumentURI) (string, bool) {
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

	journal, _ := parser.Parse(doc)

	var commodityFormats map[string]formatter.CommodityFormat
	if s.workspace != nil {
		commodityFormats = s.workspace.GetCommodityFormatsForFile(uriToPath(params.TextDocument.URI))
	}

	settings := s.getSettings()
	opts := formatterOptionsFrom(settings.Formatting)

	return formatter.FormatDocumentWithOptions(journal, doc, commodityFormats, opts), nil
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

func uriToPath(docURI protocol.DocumentURI) string {
	s := string(docURI)
	if !strings.HasPrefix(s, "file://") {
		return ""
	}
	u := uri.URI(docURI) //nolint:unconvert // protocol.DocumentURI and uri.URI are different types
	path := u.Filename()
	if path == "" {
		path = s[7:]
	}
	return filepath.Clean(path)
}

func (s *Server) GetResolved(docURI protocol.DocumentURI) *include.ResolvedJournal {
	if r, ok := s.resolved.Load(docURI); ok {
		if resolved, ok := r.(*include.ResolvedJournal); ok {
			return resolved
		}
	}
	return nil
}

func (s *Server) getWorkspaceResolved(docURI protocol.DocumentURI) *include.ResolvedJournal {
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
