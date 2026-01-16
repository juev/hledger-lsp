package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/cli"
	"github.com/juev/hledger-lsp/internal/formatter"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/lsputil"
	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/juev/hledger-lsp/internal/workspace"
)

type Server struct {
	client                protocol.Client
	documents             sync.Map
	analyzer              *analyzer.Analyzer
	loader                *include.Loader
	resolved              sync.Map
	cliClient             *cli.Client
	rootURI               string
	workspace             *workspace.Workspace
	settings              serverSettings
	settingsMu            sync.RWMutex
	supportsConfiguration bool
	snippetSupport        bool
}

func NewServer() *Server {
	srv := &Server{
		analyzer:  analyzer.New(),
		loader:    include.NewLoader(),
		cliClient: cli.NewClient("hledger", 30*time.Second),
	}
	srv.setSettings(defaultServerSettings())
	return srv
}

func (s *Server) SetClient(client protocol.Client) {
	s.client = client
}

func (s *Server) Initialize(ctx context.Context, params *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	if params != nil && params.Capabilities.Workspace != nil {
		s.supportsConfiguration = params.Capabilities.Workspace.Configuration
	}
	if params != nil && params.Capabilities.TextDocument != nil &&
		params.Capabilities.TextDocument.Completion != nil &&
		params.Capabilities.TextDocument.Completion.CompletionItem != nil {
		s.snippetSupport = params.Capabilities.TextDocument.Completion.CompletionItem.SnippetSupport
	}
	if params != nil {
		settings := parseSettingsFromRaw(s.getSettings(), params.InitializationOptions)
		s.setSettings(settings)
	}
	if len(params.WorkspaceFolders) > 0 {
		s.rootURI = strings.TrimPrefix(params.WorkspaceFolders[0].URI, "file://")
	} else {
		rootURI := params.RootURI //nolint:staticcheck // keep for backward compatibility
		if rootURI != "" {
			s.rootURI = strings.TrimPrefix(string(rootURI), "file://")
		}
	}

	if s.rootURI != "" {
		s.workspace = workspace.NewWorkspace(s.rootURI, s.loader)
	}

	return &protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{
			TextDocumentSync: protocol.TextDocumentSyncOptions{
				OpenClose: true,
				Change:    protocol.TextDocumentSyncKindIncremental,
				Save: &protocol.SaveOptions{
					IncludeText: false,
				},
			},
			CompletionProvider: &protocol.CompletionOptions{
				TriggerCharacters: []string{":", "@", "="},
				ResolveProvider:   true,
			},
			HoverProvider:              true,
			DocumentFormattingProvider: true,
			DocumentSymbolProvider:     true,
			DefinitionProvider:         true,
			ReferencesProvider:         true,
			RenameProvider: &protocol.RenameOptions{
				PrepareProvider: true,
			},
			SemanticTokensProvider: true,
			CodeActionProvider: &protocol.CodeActionOptions{
				CodeActionKinds: []protocol.CodeActionKind{
					"source.hledger",
				},
			},
			ExecuteCommandProvider: &protocol.ExecuteCommandOptions{
				Commands: []string{"hledger.run"},
			},
		},
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
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return nil
}

func (s *Server) Exit(ctx context.Context) error {
	return nil
}

func (s *Server) DidOpen(ctx context.Context, params *protocol.DidOpenTextDocumentParams) error {
	s.documents.Store(params.TextDocument.URI, params.TextDocument.Text)
	go s.publishDiagnostics(ctx, params.TextDocument.URI, params.TextDocument.Text)
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
				content = change.Text
			} else {
				content = applyChange(content, change.Range, change.Text)
			}
		}
		s.documents.Store(params.TextDocument.URI, content)
		if s.workspace != nil {
			if path := uriToPath(params.TextDocument.URI); path != "" {
				s.workspace.UpdateFile(path, content)
				s.loader.InvalidateFile(path)
			}
		}
		go s.publishDiagnostics(ctx, params.TextDocument.URI, content)
	}
	return nil
}

func isFullChange(r protocol.Range) bool {
	return r.Start.Line == 0 && r.Start.Character == 0 &&
		r.End.Line == 0 && r.End.Character == 0
}

func (s *Server) DidClose(ctx context.Context, params *protocol.DidCloseTextDocumentParams) error {
	s.documents.Delete(params.TextDocument.URI)
	return nil
}

func (s *Server) DidSave(ctx context.Context, params *protocol.DidSaveTextDocumentParams) error {
	if s.workspace != nil {
		if path := uriToPath(params.TextDocument.URI); path != "" {
			if content, ok := s.GetDocument(params.TextDocument.URI); ok {
				s.workspace.UpdateFile(path, content)
			} else if data, err := os.ReadFile(path); err == nil {
				s.workspace.UpdateFile(path, string(data))
			}
			s.loader.InvalidateFile(path)
		}
	}
	return nil
}

func (s *Server) publishDiagnostics(ctx context.Context, docURI protocol.DocumentURI, content string) {
	if s.client == nil {
		return
	}

	path := uriToPath(docURI)
	if path == "" {
		return
	}
	resolved, loadErrors := s.loader.LoadFromContent(path, content)
	s.resolved.Store(docURI, resolved)

	diagnostics := s.analyze(content)

	for _, err := range loadErrors {
		severity := protocol.DiagnosticSeverityError
		if err.Kind == include.ErrorParseError {
			continue
		}
		diagnostics = append(diagnostics, protocol.Diagnostic{
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      uint32(max(0, err.Range.Start.Line-1)),
					Character: uint32(max(0, err.Range.Start.Column-1)),
				},
				End: protocol.Position{
					Line:      uint32(max(0, err.Range.End.Line-1)),
					Character: uint32(max(0, err.Range.End.Column-1)),
				},
			},
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

func (s *Server) analyze(content string) []protocol.Diagnostic {
	journal, parseErrs := parser.Parse(content)

	diagnostics := make([]protocol.Diagnostic, 0, len(parseErrs))
	for _, err := range parseErrs {
		diagnostics = append(diagnostics, protocol.Diagnostic{
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      uint32(err.Pos.Line - 1),
					Character: uint32(err.Pos.Column - 1),
				},
				End: protocol.Position{
					Line:      uint32(err.Pos.Line - 1),
					Character: uint32(err.Pos.Column - 1),
				},
			},
			Severity: protocol.DiagnosticSeverityError,
			Source:   "hledger-lsp",
			Message:  err.Message,
		})
	}

	external := analyzer.ExternalDeclarations{}
	if s.workspace != nil {
		external.Accounts = s.workspace.GetDeclaredAccounts()
		external.Commodities = s.workspace.GetDeclaredCommodities()
	}

	var result *analyzer.AnalysisResult
	if external.Accounts != nil || external.Commodities != nil {
		result = s.analyzer.AnalyzeWithExternalDeclarations(journal, external)
	} else {
		result = s.analyzer.Analyze(journal)
	}

	for _, diag := range result.Diagnostics {
		diagnostics = append(diagnostics, protocol.Diagnostic{
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      uint32(diag.Range.Start.Line - 1),
					Character: uint32(diag.Range.Start.Column - 1),
				},
				End: protocol.Position{
					Line:      uint32(diag.Range.End.Line - 1),
					Character: uint32(diag.Range.End.Column - 1),
				},
			},
			Severity: toProtocolSeverity(diag.Severity),
			Source:   "hledger-lsp",
			Message:  diag.Message,
			Code:     diag.Code,
		})
	}

	return diagnostics
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

func (s *Server) GetDocument(uri protocol.DocumentURI) (string, bool) {
	if doc, ok := s.documents.Load(uri); ok {
		if content, ok := doc.(string); ok {
			return content, true
		}
	}
	return "", false
}

func (s *Server) Format(ctx context.Context, params *protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	journal, _ := parser.Parse(doc)

	var commodityFormats map[string]formatter.NumberFormat
	if s.workspace != nil {
		commodityFormats = s.workspace.GetCommodityFormats()
	}

	return formatter.FormatDocumentWithFormats(journal, doc, commodityFormats), nil
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

func (s *Server) RootURI() string {
	return s.rootURI
}

func (s *Server) Workspace() *workspace.Workspace {
	return s.workspace
}
