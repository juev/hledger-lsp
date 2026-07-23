package server

import (
	"context"
	"sync"
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

const integrationTestTimeout = 500 * time.Millisecond

type integrationMockClient struct {
	protocol.UnimplementedClient
	mu            sync.Mutex
	diagnostics   []protocol.PublishDiagnosticsParams
	diagnosticsCh chan struct{}

	applyMu   sync.Mutex
	lastApply *protocol.ApplyWorkspaceEditParams
}

func newIntegrationMockClient() *integrationMockClient {
	return &integrationMockClient{
		diagnosticsCh: make(chan struct{}, 100),
	}
}

func (m *integrationMockClient) ApplyEdit(_ context.Context, params *protocol.ApplyWorkspaceEditParams) (*protocol.ApplyWorkspaceEditResult, error) {
	m.applyMu.Lock()
	m.lastApply = params
	m.applyMu.Unlock()
	return &protocol.ApplyWorkspaceEditResult{Applied: true}, nil
}

func (m *integrationMockClient) lastApplyEdit() *protocol.ApplyWorkspaceEditParams {
	m.applyMu.Lock()
	defer m.applyMu.Unlock()
	return m.lastApply
}

func (m *integrationMockClient) PublishDiagnostics(_ context.Context, params *protocol.PublishDiagnosticsParams) error {
	m.mu.Lock()
	m.diagnostics = append(m.diagnostics, *params)
	m.mu.Unlock()

	select {
	case m.diagnosticsCh <- struct{}{}:
	default:
	}
	return nil
}

func (m *integrationMockClient) waitDiagnostics() bool {
	select {
	case <-m.diagnosticsCh:
		return true
	case <-time.After(integrationTestTimeout):
		return false
	}
}

func (m *integrationMockClient) getLastDiagnostics() *protocol.PublishDiagnosticsParams {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.diagnostics) == 0 {
		return nil
	}
	result := m.diagnostics[len(m.diagnostics)-1]
	return &result
}

type testServer struct {
	*Server
	client *integrationMockClient
}

func newTestServer() *testServer {
	srv := NewServer()
	srv.diagDebounce = 0
	client := newIntegrationMockClient()
	srv.SetClient(client)
	return &testServer{
		Server: srv,
		client: client,
	}
}

func (ts *testServer) openDocument(uri uri.URI, content string) error {
	params := &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:  uri,
			Text: content,
		},
	}
	return ts.DidOpen(context.Background(), params)
}

func (ts *testServer) openAndWait(uri uri.URI, content string) ([]protocol.Diagnostic, error) {
	err := ts.openDocument(uri, content)
	if err != nil {
		return nil, err
	}

	if !ts.client.waitDiagnostics() {
		return nil, nil
	}

	last := ts.client.getLastDiagnostics()
	if last == nil {
		return nil, nil
	}
	return last.Diagnostics, nil
}

func (ts *testServer) changeDocument(uri uri.URI, changes []protocol.TextDocumentContentChangeEvent) error {
	params := &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri},
		},
		ContentChanges: changes,
	}
	return ts.DidChange(context.Background(), params)
}

func (ts *testServer) changeAndWait(uri uri.URI, changes []protocol.TextDocumentContentChangeEvent) ([]protocol.Diagnostic, error) {
	err := ts.changeDocument(uri, changes)
	if err != nil {
		return nil, err
	}

	if !ts.client.waitDiagnostics() {
		return nil, nil
	}

	last := ts.client.getLastDiagnostics()
	if last == nil {
		return nil, nil
	}
	return last.Diagnostics, nil
}

//nolint:unparam // test helper keeps the URI explicit at call sites
func (ts *testServer) replaceAndWait(uri uri.URI, newContent string) ([]protocol.Diagnostic, error) {
	return ts.changeAndWait(uri, []protocol.TextDocumentContentChangeEvent{
		&protocol.TextDocumentContentChangeWholeDocument{Text: newContent},
	})
}

func optionalString(value protocol.Optional[string]) string {
	result, _ := value.Get()
	return result
}

func tooltipString(value protocol.InlayHintTooltip) string {
	result, _ := value.(protocol.String)
	return string(result)
}

func diagnosticCodeString(value protocol.ProgressToken) string {
	result, _ := value.(protocol.String)
	return string(result)
}

func completionDetail(item protocol.CompletionItem) string {
	return optionalString(item.Detail)
}

func completionSortText(item protocol.CompletionItem) string {
	return optionalString(item.SortText)
}

func completionTextEdit(item protocol.CompletionItem) *protocol.TextEdit {
	result, _ := item.TextEdit.(*protocol.TextEdit)
	return result
}

func hoverContent(hover *protocol.Hover) string {
	markup, _ := hover.Contents.(*protocol.MarkupContent)
	if markup == nil {
		return ""
	}
	return markup.Value
}

func (ts *testServer) completion(uri uri.URI, line, character uint32) (*protocol.CompletionList, error) {
	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: line, Character: character},
		},
	}
	return ts.Server.completion(context.Background(), params)
}

func (ts *testServer) hover(uri uri.URI, line uint32) (*protocol.Hover, error) {
	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: line, Character: 10},
		},
	}
	return ts.Hover(context.Background(), params)
}

//nolint:unparam // test helper keeps the URI explicit at call sites
func (ts *testServer) format(uri uri.URI) ([]protocol.TextEdit, error) {
	params := &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}
	return ts.Format(context.Background(), params)
}

func (ts *testServer) definition(uri uri.URI, line, character uint32) ([]protocol.Location, error) {
	params := &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: line, Character: character},
		},
	}
	return ts.Server.definition(context.Background(), params)
}

//nolint:unparam // test helper keeps the character explicit at call sites
func (ts *testServer) references(uri uri.URI, line, character uint32, includeDeclaration bool) ([]protocol.Location, error) {
	params := &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: line, Character: character},
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: includeDeclaration},
	}
	return ts.References(context.Background(), params)
}

func extractCompletionLabels(items []protocol.CompletionItem) []string {
	labels := make([]string, len(items))
	for i, item := range items {
		labels[i] = item.Label
	}
	return labels
}

func hasDiagnosticWithSeverity(diagnostics []protocol.Diagnostic, severity protocol.DiagnosticSeverity) bool {
	for _, d := range diagnostics {
		if d.Severity == severity {
			return true
		}
	}
	return false
}
