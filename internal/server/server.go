package server

import (
	"context"
	"sync"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/formatter"
	"github.com/juev/hledger-lsp/internal/parser"
	"go.lsp.dev/protocol"
)

type Server struct {
	client    protocol.Client
	documents sync.Map
	analyzer  *analyzer.Analyzer
}

func NewServer() *Server {
	return &Server{
		analyzer: analyzer.New(),
	}
}

func (s *Server) SetClient(client protocol.Client) {
	s.client = client
}

func (s *Server) Initialize(ctx context.Context, params *protocol.InitializeParams) (*protocol.InitializeResult, error) {
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
				TriggerCharacters: []string{":", " ", "@", "="},
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
		},
		ServerInfo: &protocol.ServerInfo{
			Name:    "hledger-lsp",
			Version: "0.1.0",
		},
	}, nil
}

func (s *Server) Initialized(ctx context.Context, params *protocol.InitializedParams) error {
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
		content := doc.(string)
		for _, change := range params.ContentChanges {
			if isFullChange(change.Range) {
				content = change.Text
			} else {
				content = applyChange(content, change.Range, change.Text)
			}
		}
		s.documents.Store(params.TextDocument.URI, content)
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
	return nil
}

func (s *Server) publishDiagnostics(ctx context.Context, uri protocol.DocumentURI, content string) {
	if s.client == nil {
		return
	}

	diagnostics := s.analyze(content)
	_ = s.client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{
		URI:         uri,
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

	result := s.analyzer.Analyze(journal)
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
		return doc.(string), true
	}
	return "", false
}

func (s *Server) Format(ctx context.Context, params *protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	journal, _ := parser.Parse(doc)
	return formatter.FormatDocument(journal, doc), nil
}

func applyChange(content string, r protocol.Range, text string) string {
	lines := splitLines(content)

	startLine := int(r.Start.Line)
	startChar := int(r.Start.Character)
	endLine := int(r.End.Line)
	endChar := int(r.End.Character)

	if startLine >= len(lines) {
		return content + text
	}

	var result string

	for i := 0; i < startLine; i++ {
		result += lines[i] + "\n"
	}

	if startLine < len(lines) {
		result += lines[startLine][:min(startChar, len(lines[startLine]))]
	}

	result += text

	if endLine < len(lines) {
		result += lines[endLine][min(endChar, len(lines[endLine])):]
	}

	for i := endLine + 1; i < len(lines); i++ {
		result += "\n" + lines[i]
	}

	return result
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
