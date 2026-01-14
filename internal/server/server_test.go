package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/analyzer"
)

type mockClient struct {
	mu          sync.Mutex
	diagnostics []protocol.PublishDiagnosticsParams
}

func (m *mockClient) Progress(ctx context.Context, params *protocol.ProgressParams) error {
	return nil
}

func (m *mockClient) WorkDoneProgressCreate(ctx context.Context, params *protocol.WorkDoneProgressCreateParams) error {
	return nil
}

func (m *mockClient) LogMessage(ctx context.Context, params *protocol.LogMessageParams) error {
	return nil
}

func (m *mockClient) PublishDiagnostics(ctx context.Context, params *protocol.PublishDiagnosticsParams) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.diagnostics = append(m.diagnostics, *params)
	return nil
}

func (m *mockClient) ShowMessage(ctx context.Context, params *protocol.ShowMessageParams) error {
	return nil
}

func (m *mockClient) ShowMessageRequest(ctx context.Context, params *protocol.ShowMessageRequestParams) (*protocol.MessageActionItem, error) {
	return nil, nil
}

func (m *mockClient) Telemetry(ctx context.Context, params interface{}) error {
	return nil
}

func (m *mockClient) RegisterCapability(ctx context.Context, params *protocol.RegistrationParams) error {
	return nil
}

func (m *mockClient) UnregisterCapability(ctx context.Context, params *protocol.UnregistrationParams) error {
	return nil
}

func (m *mockClient) ApplyEdit(ctx context.Context, params *protocol.ApplyWorkspaceEditParams) (bool, error) {
	return false, nil
}

func (m *mockClient) Configuration(ctx context.Context, params *protocol.ConfigurationParams) ([]interface{}, error) {
	return nil, nil
}

func (m *mockClient) WorkspaceFolders(ctx context.Context) ([]protocol.WorkspaceFolder, error) {
	return nil, nil
}

func (m *mockClient) getDiagnostics() []protocol.PublishDiagnosticsParams {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]protocol.PublishDiagnosticsParams, len(m.diagnostics))
	copy(result, m.diagnostics)
	return result
}

func TestServer_Initialize(t *testing.T) {
	srv := NewServer()

	params := &protocol.InitializeParams{}
	result, err := srv.Initialize(context.Background(), params)

	require.NoError(t, err)
	require.NotNil(t, result)

	caps := result.Capabilities
	assert.True(t, caps.TextDocumentSync.(protocol.TextDocumentSyncOptions).OpenClose)
	assert.Equal(t, protocol.TextDocumentSyncKindIncremental, caps.TextDocumentSync.(protocol.TextDocumentSyncOptions).Change)
	assert.NotNil(t, caps.CompletionProvider)
	assert.Equal(t, []string{":", " ", "@", "="}, caps.CompletionProvider.TriggerCharacters)
	assert.True(t, caps.HoverProvider.(bool))
	assert.True(t, caps.DocumentFormattingProvider.(bool))
	assert.True(t, caps.DocumentSymbolProvider.(bool))
	assert.True(t, caps.SemanticTokensProvider.(bool))
	assert.NotNil(t, caps.CodeActionProvider)
	assert.NotNil(t, caps.ExecuteCommandProvider)
	assert.Contains(t, caps.ExecuteCommandProvider.Commands, "hledger.run")

	require.NotNil(t, result.ServerInfo)
	assert.Equal(t, "hledger-lsp", result.ServerInfo.Name)
	assert.Equal(t, "0.1.0", result.ServerInfo.Version)
}

func TestServer_Initialized(t *testing.T) {
	srv := NewServer()

	err := srv.Initialized(context.Background(), &protocol.InitializedParams{})

	assert.NoError(t, err)
}

func TestServer_Shutdown(t *testing.T) {
	srv := NewServer()

	err := srv.Shutdown(context.Background())

	assert.NoError(t, err)
}

func TestServer_Exit(t *testing.T) {
	srv := NewServer()

	err := srv.Exit(context.Background())

	assert.NoError(t, err)
}

func TestServer_DidOpen(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///test.journal")
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	params := &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:  uri,
			Text: content,
		},
	}

	err := srv.DidOpen(context.Background(), params)

	require.NoError(t, err)

	doc, ok := srv.GetDocument(uri)
	assert.True(t, ok)
	assert.Equal(t, content, doc)
}

func TestServer_DidChange_FullDocument(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///test.journal")
	initialContent := `2024-01-15 test
    expenses:food  $50
    assets:cash`
	newContent := `2024-01-16 updated
    expenses:rent  $100
    assets:bank`

	srv.documents.Store(uri, initialContent)

	params := &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri},
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			{
				Range: protocol.Range{
					Start: protocol.Position{Line: 0, Character: 0},
					End:   protocol.Position{Line: 0, Character: 0},
				},
				Text: newContent,
			},
		},
	}

	err := srv.DidChange(context.Background(), params)

	require.NoError(t, err)

	doc, ok := srv.GetDocument(uri)
	assert.True(t, ok)
	assert.Equal(t, newContent, doc)
}

func TestServer_DidChange_Incremental(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///test.journal")
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	srv.documents.Store(uri, content)

	params := &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri},
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			{
				Range: protocol.Range{
					Start: protocol.Position{Line: 0, Character: 11},
					End:   protocol.Position{Line: 0, Character: 15},
				},
				Text: "grocery",
			},
		},
	}

	err := srv.DidChange(context.Background(), params)

	require.NoError(t, err)

	doc, ok := srv.GetDocument(uri)
	assert.True(t, ok)
	assert.Contains(t, doc, "grocery")
	assert.NotContains(t, doc, "test\n")
}

func TestServer_DidChange_DocumentNotFound(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///nonexistent.journal")

	params := &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri},
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			{Text: "new content"},
		},
	}

	err := srv.DidChange(context.Background(), params)

	assert.NoError(t, err)

	_, ok := srv.GetDocument(uri)
	assert.False(t, ok)
}

func TestServer_DidClose(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///test.journal")
	content := "test content"

	srv.documents.Store(uri, content)

	_, ok := srv.GetDocument(uri)
	require.True(t, ok)

	params := &protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}

	err := srv.DidClose(context.Background(), params)

	require.NoError(t, err)

	_, ok = srv.GetDocument(uri)
	assert.False(t, ok)
}

func TestServer_DidSave(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///test.journal")

	params := &protocol.DidSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}

	err := srv.DidSave(context.Background(), params)

	assert.NoError(t, err)
}

func TestApplyChange(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		r        protocol.Range
		text     string
		expected string
	}{
		{
			name:    "single line replacement",
			content: "hello world",
			r: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 6},
				End:   protocol.Position{Line: 0, Character: 11},
			},
			text:     "golang",
			expected: "hello golang",
		},
		{
			name:    "insert at beginning",
			content: "world",
			r: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 0},
				End:   protocol.Position{Line: 0, Character: 0},
			},
			text:     "hello ",
			expected: "hello world",
		},
		{
			name:    "insert at end",
			content: "hello",
			r: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 5},
				End:   protocol.Position{Line: 0, Character: 5},
			},
			text:     " world",
			expected: "hello world",
		},
		{
			name:    "delete text",
			content: "hello world",
			r: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 5},
				End:   protocol.Position{Line: 0, Character: 11},
			},
			text:     "",
			expected: "hello",
		},
		{
			name:    "multiline insert",
			content: "line1\nline2\nline3",
			r: protocol.Range{
				Start: protocol.Position{Line: 1, Character: 0},
				End:   protocol.Position{Line: 1, Character: 5},
			},
			text:     "new line",
			expected: "line1\nnew line\nline3",
		},
		{
			name:    "multiline delete",
			content: "line1\nline2\nline3",
			r: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 5},
				End:   protocol.Position{Line: 2, Character: 0},
			},
			text:     "\n",
			expected: "line1\nline3",
		},
		{
			name:    "out of bounds appends",
			content: "hello",
			r: protocol.Range{
				Start: protocol.Position{Line: 10, Character: 0},
				End:   protocol.Position{Line: 10, Character: 0},
			},
			text:     " appended",
			expected: "hello appended",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := applyChange(tt.content, tt.r, tt.text)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: []string{""},
		},
		{
			name:     "single line",
			input:    "hello",
			expected: []string{"hello"},
		},
		{
			name:     "multiple lines",
			input:    "line1\nline2\nline3",
			expected: []string{"line1", "line2", "line3"},
		},
		{
			name:     "trailing newline",
			input:    "line1\nline2\n",
			expected: []string{"line1", "line2", ""},
		},
		{
			name:     "empty lines",
			input:    "line1\n\nline3",
			expected: []string{"line1", "", "line3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitLines(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsFullChange(t *testing.T) {
	tests := []struct {
		name     string
		r        protocol.Range
		expected bool
	}{
		{
			name: "full change (0,0 to 0,0)",
			r: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 0},
				End:   protocol.Position{Line: 0, Character: 0},
			},
			expected: true,
		},
		{
			name: "partial change start",
			r: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 5},
				End:   protocol.Position{Line: 0, Character: 0},
			},
			expected: false,
		},
		{
			name: "partial change end",
			r: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 0},
				End:   protocol.Position{Line: 1, Character: 0},
			},
			expected: false,
		},
		{
			name: "multiline range",
			r: protocol.Range{
				Start: protocol.Position{Line: 1, Character: 0},
				End:   protocol.Position{Line: 5, Character: 10},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isFullChange(tt.r)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestServer_PublishDiagnostics_ParseError(t *testing.T) {
	srv := NewServer()
	client := &mockClient{}
	srv.SetClient(client)

	uri := protocol.DocumentURI("file:///test.journal")
	content := `2024-01-15 test
    invalid posting without amount or account`

	params := &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:  uri,
			Text: content,
		},
	}

	err := srv.DidOpen(context.Background(), params)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	diagnostics := client.getDiagnostics()
	require.NotEmpty(t, diagnostics)
	assert.Equal(t, uri, diagnostics[0].URI)
}

func TestServer_PublishDiagnostics_BalanceError(t *testing.T) {
	srv := NewServer()
	client := &mockClient{}
	srv.SetClient(client)

	uri := protocol.DocumentURI("file:///test.journal")
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash  $30`

	params := &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:  uri,
			Text: content,
		},
	}

	err := srv.DidOpen(context.Background(), params)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	diagnostics := client.getDiagnostics()
	require.NotEmpty(t, diagnostics)
	assert.Equal(t, uri, diagnostics[0].URI)

	hasBalanceError := false
	for _, d := range diagnostics[0].Diagnostics {
		if d.Severity == protocol.DiagnosticSeverityError {
			hasBalanceError = true
			break
		}
	}
	assert.True(t, hasBalanceError)
}

func TestServer_PublishDiagnostics_NoErrors(t *testing.T) {
	srv := NewServer()
	client := &mockClient{}
	srv.SetClient(client)

	uri := protocol.DocumentURI("file:///test.journal")
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	params := &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:  uri,
			Text: content,
		},
	}

	err := srv.DidOpen(context.Background(), params)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	diagnostics := client.getDiagnostics()
	require.NotEmpty(t, diagnostics)
	assert.Equal(t, uri, diagnostics[0].URI)
	assert.Empty(t, diagnostics[0].Diagnostics)
}

func TestServer_PublishDiagnostics_NilClient(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///test.journal")
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	params := &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:  uri,
			Text: content,
		},
	}

	err := srv.DidOpen(context.Background(), params)

	assert.NoError(t, err)
}

func TestServer_GetDocument_Found(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///test.journal")
	content := "test content"

	srv.documents.Store(uri, content)

	doc, ok := srv.GetDocument(uri)

	assert.True(t, ok)
	assert.Equal(t, content, doc)
}

func TestServer_GetDocument_NotFound(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///nonexistent.journal")

	doc, ok := srv.GetDocument(uri)

	assert.False(t, ok)
	assert.Empty(t, doc)
}

func TestServer_GetResolved_Found(t *testing.T) {
	srv := NewServer()
	client := &mockClient{}
	srv.SetClient(client)

	uri := protocol.DocumentURI("file:///test.journal")
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	params := &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:  uri,
			Text: content,
		},
	}

	err := srv.DidOpen(context.Background(), params)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	resolved := srv.GetResolved(uri)
	assert.NotNil(t, resolved)
}

func TestServer_GetResolved_NotFound(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///nonexistent.journal")

	resolved := srv.GetResolved(uri)

	assert.Nil(t, resolved)
}

func TestServer_Format(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///test.journal")
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	srv.documents.Store(uri, content)

	params := &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}

	edits, err := srv.Format(context.Background(), params)

	require.NoError(t, err)
	assert.NotNil(t, edits)
}

func TestServer_Format_DocumentNotFound(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///nonexistent.journal")

	params := &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}

	edits, err := srv.Format(context.Background(), params)

	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestToProtocolSeverity(t *testing.T) {
	tests := []struct {
		name     string
		input    analyzer.DiagnosticSeverity
		expected protocol.DiagnosticSeverity
	}{
		{"error", analyzer.SeverityError, protocol.DiagnosticSeverityError},
		{"warning", analyzer.SeverityWarning, protocol.DiagnosticSeverityWarning},
		{"info", analyzer.SeverityInfo, protocol.DiagnosticSeverityInformation},
		{"hint", analyzer.SeverityHint, protocol.DiagnosticSeverityHint},
		{"unknown defaults to error", analyzer.DiagnosticSeverity(99), protocol.DiagnosticSeverityError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toProtocolSeverity(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUriToPath(t *testing.T) {
	tests := []struct {
		name     string
		uri      protocol.DocumentURI
		expected string
	}{
		{
			name:     "file URI",
			uri:      protocol.DocumentURI("file:///test.journal"),
			expected: "/test.journal",
		},
		{
			name:     "git URI returns empty",
			uri:      protocol.DocumentURI("git://github.com/user/repo/main/file.journal"),
			expected: "",
		},
		{
			name:     "untitled URI returns empty",
			uri:      protocol.DocumentURI("untitled:Untitled-1"),
			expected: "",
		},
		{
			name:     "vscode-notebook URI returns empty",
			uri:      protocol.DocumentURI("vscode-notebook-cell://something"),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := uriToPath(tt.uri)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestServer_DidOpen_NonFileURI(t *testing.T) {
	srv := NewServer()
	client := &mockClient{}
	srv.SetClient(client)

	uri := protocol.DocumentURI("git://github.com/user/repo/main/file.journal")
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	params := &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:  uri,
			Text: content,
		},
	}

	err := srv.DidOpen(context.Background(), params)

	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)
}
