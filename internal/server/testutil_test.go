package server

import (
	"context"
	"sync"
	"time"

	"go.lsp.dev/protocol"
)

const integrationTestTimeout = 500 * time.Millisecond

type integrationMockClient struct {
	mu            sync.Mutex
	diagnostics   []protocol.PublishDiagnosticsParams
	diagnosticsCh chan struct{}
}

func newIntegrationMockClient() *integrationMockClient {
	return &integrationMockClient{
		diagnosticsCh: make(chan struct{}, 100),
	}
}

func (m *integrationMockClient) Progress(_ context.Context, _ *protocol.ProgressParams) error {
	return nil
}

func (m *integrationMockClient) WorkDoneProgressCreate(_ context.Context, _ *protocol.WorkDoneProgressCreateParams) error {
	return nil
}

func (m *integrationMockClient) LogMessage(_ context.Context, _ *protocol.LogMessageParams) error {
	return nil
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

func (m *integrationMockClient) ShowMessage(_ context.Context, _ *protocol.ShowMessageParams) error {
	return nil
}

func (m *integrationMockClient) ShowMessageRequest(_ context.Context, _ *protocol.ShowMessageRequestParams) (*protocol.MessageActionItem, error) {
	return nil, nil
}

func (m *integrationMockClient) Telemetry(_ context.Context, _ interface{}) error {
	return nil
}

func (m *integrationMockClient) RegisterCapability(_ context.Context, _ *protocol.RegistrationParams) error {
	return nil
}

func (m *integrationMockClient) UnregisterCapability(_ context.Context, _ *protocol.UnregistrationParams) error {
	return nil
}

func (m *integrationMockClient) ApplyEdit(_ context.Context, _ *protocol.ApplyWorkspaceEditParams) (bool, error) {
	return false, nil
}

func (m *integrationMockClient) Configuration(_ context.Context, _ *protocol.ConfigurationParams) ([]interface{}, error) {
	return nil, nil
}

func (m *integrationMockClient) WorkspaceFolders(_ context.Context) ([]protocol.WorkspaceFolder, error) {
	return nil, nil
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
	client := newIntegrationMockClient()
	srv.SetClient(client)
	return &testServer{
		Server: srv,
		client: client,
	}
}

func (ts *testServer) openDocument(uri protocol.DocumentURI, content string) error {
	params := &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:  uri,
			Text: content,
		},
	}
	return ts.DidOpen(context.Background(), params)
}

func (ts *testServer) openAndWait(uri protocol.DocumentURI, content string) ([]protocol.Diagnostic, error) {
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

func (ts *testServer) changeDocument(uri protocol.DocumentURI, changes []protocol.TextDocumentContentChangeEvent) error {
	params := &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri},
		},
		ContentChanges: changes,
	}
	return ts.DidChange(context.Background(), params)
}

func (ts *testServer) changeAndWait(uri protocol.DocumentURI, changes []protocol.TextDocumentContentChangeEvent) ([]protocol.Diagnostic, error) {
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

func (ts *testServer) replaceAndWait(uri protocol.DocumentURI, newContent string) ([]protocol.Diagnostic, error) {
	return ts.changeAndWait(uri, []protocol.TextDocumentContentChangeEvent{
		{Text: newContent},
	})
}

func (ts *testServer) completion(uri protocol.DocumentURI, line, character uint32) (*protocol.CompletionList, error) {
	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: line, Character: character},
		},
	}
	return ts.Completion(context.Background(), params)
}

func (ts *testServer) hover(uri protocol.DocumentURI, line uint32) (*protocol.Hover, error) {
	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: line, Character: 10},
		},
	}
	return ts.Hover(context.Background(), params)
}

func (ts *testServer) format(uri protocol.DocumentURI) ([]protocol.TextEdit, error) {
	params := &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}
	return ts.Format(context.Background(), params)
}

func (ts *testServer) definition(uri protocol.DocumentURI, line, character uint32) ([]protocol.Location, error) {
	params := &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: line, Character: character},
		},
	}
	return ts.Definition(context.Background(), params)
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
