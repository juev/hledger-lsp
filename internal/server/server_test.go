package server

import (
	"context"
	"os"
	"strings"
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

func TestServer_Initialize_WithRootURI(t *testing.T) {
	srv := NewServer()

	rootURI := protocol.DocumentURI("file:///tmp/test-workspace")
	params := &protocol.InitializeParams{
		RootURI: rootURI,
	}

	_, err := srv.Initialize(context.Background(), params)
	require.NoError(t, err)

	assert.Equal(t, "/tmp/test-workspace", srv.RootURI())
	assert.NotNil(t, srv.Workspace())
}

func TestServer_Initialize_WithWorkspaceFolders(t *testing.T) {
	srv := NewServer()

	params := &protocol.InitializeParams{
		WorkspaceFolders: []protocol.WorkspaceFolder{
			{URI: "file:///tmp/folder1", Name: "folder1"},
			{URI: "file:///tmp/folder2", Name: "folder2"},
		},
	}

	_, err := srv.Initialize(context.Background(), params)
	require.NoError(t, err)

	assert.Equal(t, "/tmp/folder1", srv.RootURI())
	assert.NotNil(t, srv.Workspace())
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

func TestServer_Diagnostics_WithWorkspaceDeclarations(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := tmpDir + "/main.journal"
	mainContent := `commodity RUB
commodity USD

include transactions.journal`
	err := os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	txPath := tmpDir + "/transactions.journal"
	txContent := `2024-01-15 test
    expenses:food  100 EUR
    assets:cash  100 RUB`
	err = os.WriteFile(txPath, []byte(txContent), 0644)
	require.NoError(t, err)

	srv := NewServer()
	client := &mockClient{}
	srv.SetClient(client)

	initParams := &protocol.InitializeParams{
		RootURI: protocol.DocumentURI("file://" + tmpDir),
	}
	_, err = srv.Initialize(context.Background(), initParams)
	require.NoError(t, err)

	err = srv.workspace.Initialize()
	require.NoError(t, err)

	uri := protocol.DocumentURI("file://" + txPath)
	params := &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:  uri,
			Text: txContent,
		},
	}

	err = srv.DidOpen(context.Background(), params)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	diagnostics := client.getDiagnostics()
	require.NotEmpty(t, diagnostics)

	var foundEURWarning bool
	var foundRUBWarning bool
	for _, pub := range diagnostics {
		for _, d := range pub.Diagnostics {
			if d.Code == "UNDECLARED_COMMODITY" {
				if strings.Contains(d.Message, "EUR") {
					foundEURWarning = true
				}
				if strings.Contains(d.Message, "RUB") {
					foundRUBWarning = true
				}
			}
		}
	}

	assert.True(t, foundEURWarning, "Expected UNDECLARED_COMMODITY warning for EUR (not in workspace declarations)")
	assert.False(t, foundRUBWarning, "RUB should NOT trigger warning (declared in workspace)")
}

func TestServer_Format_WithWorkspaceCommodityFormat(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := tmpDir + "/main.journal"
	mainContent := `commodity RUB
  format 1.000,00 RUB

include transactions.journal`
	err := os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	txPath := tmpDir + "/transactions.journal"
	txContent := `2024-01-15 test
    expenses:food  1000 RUB
    assets:cash`
	err = os.WriteFile(txPath, []byte(txContent), 0644)
	require.NoError(t, err)

	srv := NewServer()

	initParams := &protocol.InitializeParams{
		RootURI: protocol.DocumentURI("file://" + tmpDir),
	}
	_, err = srv.Initialize(context.Background(), initParams)
	require.NoError(t, err)

	err = srv.workspace.Initialize()
	require.NoError(t, err)

	uri := protocol.DocumentURI("file://" + txPath)
	srv.documents.Store(uri, txContent)

	formatParams := &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}

	edits, err := srv.Format(context.Background(), formatParams)
	require.NoError(t, err)
	require.NotEmpty(t, edits)

	foundFormatted := false
	for _, edit := range edits {
		if strings.Contains(edit.NewText, "1.000,00 RUB") {
			foundFormatted = true
			break
		}
	}
	assert.True(t, foundFormatted, "Expected number formatted as 1.000,00 RUB from workspace commodity format, got: %v", edits)
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

func TestServer_Format_UsesCommodityFromSiblingInclude(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainContent := `include common.journal
include 2025.journal`
	mainPath := tmpDir + "/main.journal"
	err := os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	commonContent := `commodity RUB
  format 1 000,00 RUB`
	commonPath := tmpDir + "/common.journal"
	err = os.WriteFile(commonPath, []byte(commonContent), 0644)
	require.NoError(t, err)

	txContent := `2024-01-15 test
    expenses:food  1234,56 RUB
    assets:cash`
	txPath := tmpDir + "/2025.journal"
	err = os.WriteFile(txPath, []byte(txContent), 0644)
	require.NoError(t, err)

	srv := NewServer()
	client := &mockClient{}
	srv.SetClient(client)

	_, err = srv.Initialize(context.Background(), &protocol.InitializeParams{
		RootURI: protocol.DocumentURI("file://" + tmpDir),
	})
	require.NoError(t, err)

	err = srv.Initialized(context.Background(), &protocol.InitializedParams{})
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	txURI := protocol.DocumentURI("file://" + txPath)
	err = srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:  txURI,
			Text: txContent,
		},
	})
	require.NoError(t, err)

	edits, err := srv.Format(context.Background(), &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: txURI},
	})
	require.NoError(t, err)
	require.NotEmpty(t, edits)

	found := false
	for _, edit := range edits {
		if strings.Contains(edit.NewText, "1 234,56 RUB") {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected formatted amount with commodity format from sibling include, got edits: %v", edits)
}
