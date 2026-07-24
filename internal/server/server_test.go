package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/analyzer"
)

type mockClient struct {
	protocol.UnimplementedClient
	mu            sync.Mutex
	diagnostics   []protocol.PublishDiagnosticsParams
	registrations []protocol.Registration
}

func (m *mockClient) PublishDiagnostics(ctx context.Context, params *protocol.PublishDiagnosticsParams) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.diagnostics = append(m.diagnostics, *params)
	return nil
}

func (m *mockClient) RegisterCapability(ctx context.Context, params *protocol.RegistrationParams) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registrations = append(m.registrations, params.Registrations...)
	return nil
}

func (m *mockClient) getDiagnostics() []protocol.PublishDiagnosticsParams {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]protocol.PublishDiagnosticsParams, len(m.diagnostics))
	copy(result, m.diagnostics)
	return result
}

func (m *mockClient) getRegistrations() []protocol.Registration {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]protocol.Registration, len(m.registrations))
	copy(result, m.registrations)
	return result
}

type slowMockClient struct {
	mockClient
	delay time.Duration
}

func (m *slowMockClient) Configuration(_ context.Context, _ *protocol.ConfigurationParams) ([]protocol.LSPAny, error) {
	time.Sleep(m.delay)
	return nil, nil
}

func (m *slowMockClient) RegisterCapability(_ context.Context, _ *protocol.RegistrationParams) error {
	time.Sleep(m.delay)
	return nil
}

// TestServer_Initialized_NonBlocking verifies that Initialized returns immediately
// without blocking on client.Configuration(). Uses 200ms client delay with 50ms
// timeout (4x safety margin) to avoid flakiness on loaded CI systems.
func TestServer_Initialized_NonBlocking(t *testing.T) {
	srv := NewServer()
	client := &slowMockClient{delay: 200 * time.Millisecond}
	srv.SetClient(client)
	srv.supportsConfiguration = true

	done := make(chan struct{})
	go func() {
		err := srv.Initialized(context.Background(), &protocol.InitializedParams{})
		assert.NoError(t, err)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Initialized blocked on Configuration call - should return immediately")
	}
}

func TestServer_RegisterFileWatchers_IncludesPricesPattern(t *testing.T) {
	srv := NewServer()
	srv.diagDebounce = 0
	client := &mockClient{}
	srv.SetClient(client)

	srv.registerFileWatchers()

	registrations := client.getRegistrations()
	require.Len(t, registrations, 1)

	watchedOptions, err := unmarshalLSPAny[protocol.DidChangeWatchedFilesRegistrationOptions](registrations[0].RegisterOptions)
	require.NoError(t, err)

	assert.Len(t, watchedOptions.Watchers, 6)

	gotPricesPattern := false
	for _, watcher := range watchedOptions.Watchers {
		pattern, ok := watcher.GlobPattern.(protocol.Pattern)
		if ok && pattern == "**/*.prices" {
			gotPricesPattern = true
		}
	}
	assert.True(t, gotPricesPattern, "expected .prices watcher pattern to be registered")
}

func TestServer_Initialized_MinimalClientDoesNotRegisterFileWatchers(t *testing.T) {
	srv := NewServer()
	client := &mockClient{}
	srv.SetClient(client)

	_, err := srv.Initialize(context.Background(), &protocol.InitializeParams{})
	require.NoError(t, err)
	require.NoError(t, srv.Initialized(context.Background(), &protocol.InitializedParams{}))

	assert.Never(t, func() bool {
		return len(client.getRegistrations()) > 0
	}, 100*time.Millisecond, 10*time.Millisecond)
}

func TestServer_Initialized_DynamicFileWatcherClientRegistersExistingPatternsOnce(t *testing.T) {
	trueValue := true
	srv := NewServer()
	client := &mockClient{}
	srv.SetClient(client)

	_, err := srv.Initialize(context.Background(), &protocol.InitializeParams{
		Capabilities: protocol.ClientCapabilities{
			Workspace: &protocol.WorkspaceClientCapabilities{
				DidChangeWatchedFiles: &protocol.DidChangeWatchedFilesClientCapabilities{
					DynamicRegistration: &trueValue,
				},
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, srv.Initialized(context.Background(), &protocol.InitializedParams{}))

	require.Eventually(t, func() bool {
		return len(client.getRegistrations()) == 1
	}, time.Second, 10*time.Millisecond)

	registrations := client.getRegistrations()
	require.Len(t, registrations, 1)
	assert.Equal(t, "workspace/didChangeWatchedFiles", registrations[0].Method)

	watchedOptions, err := unmarshalLSPAny[protocol.DidChangeWatchedFilesRegistrationOptions](registrations[0].RegisterOptions)
	require.NoError(t, err)

	assert.Equal(t, []protocol.FileSystemWatcher{
		{GlobPattern: protocol.Pattern("**/*.journal")},
		{GlobPattern: protocol.Pattern("**/*.hledger")},
		{GlobPattern: protocol.Pattern("**/*.j")},
		{GlobPattern: protocol.Pattern("**/*.ledger")},
		{GlobPattern: protocol.Pattern("**/*.prices")},
		{GlobPattern: protocol.Pattern("**/*.rules")},
	}, watchedOptions.Watchers)
}

// TestServer_DidChangeConfiguration_NonBlocking verifies that DidChangeConfiguration
// returns immediately without blocking. Uses same timing assumptions as above.
func TestServer_DidChangeConfiguration_NonBlocking(t *testing.T) {
	srv := NewServer()
	client := &slowMockClient{delay: 200 * time.Millisecond}
	srv.SetClient(client)
	srv.supportsConfiguration = true

	done := make(chan struct{})
	go func() {
		err := srv.DidChangeConfiguration(context.Background(), &protocol.DidChangeConfigurationParams{})
		assert.NoError(t, err)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("DidChangeConfiguration blocked on Configuration call - should return immediately")
	}
}

func TestServer_Initialize(t *testing.T) {
	srv := NewServer()

	params := &protocol.InitializeParams{}
	result, err := srv.Initialize(context.Background(), params)

	require.NoError(t, err)
	require.NotNil(t, result)

	caps := result.Capabilities
	syncOptions, ok := caps.TextDocumentSync.(*protocol.TextDocumentSyncOptions)
	require.True(t, ok)
	require.NotNil(t, syncOptions.OpenClose)
	assert.True(t, *syncOptions.OpenClose)
	require.NotNil(t, syncOptions.Change)
	assert.Equal(t, protocol.TextDocumentSyncKindIncremental, *syncOptions.Change)
	assert.NotNil(t, caps.CompletionProvider)
	assert.Equal(t, []string{":", "@", "=", "0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}, caps.CompletionProvider.TriggerCharacters)
	hoverEnabled, ok := caps.HoverProvider.(protocol.Boolean)
	require.True(t, ok)
	assert.True(t, bool(hoverEnabled))
	formatEnabled, ok := caps.DocumentFormattingProvider.(protocol.Boolean)
	require.True(t, ok)
	assert.True(t, bool(formatEnabled))
	rangeFormatEnabled, ok := caps.DocumentRangeFormattingProvider.(protocol.Boolean)
	require.True(t, ok)
	assert.True(t, bool(rangeFormatEnabled))
	assert.NotNil(t, caps.DocumentOnTypeFormattingProvider)
	assert.Equal(t, "\n", caps.DocumentOnTypeFormattingProvider.FirstTriggerCharacter)
	symbolEnabled, ok := caps.DocumentSymbolProvider.(protocol.Boolean)
	require.True(t, ok)
	assert.True(t, bool(symbolEnabled))
	assert.NotNil(t, caps.SemanticTokensProvider)
	assert.NotNil(t, caps.CodeActionProvider)
	assert.NotNil(t, caps.ExecuteCommandProvider)
	assert.Contains(t, caps.ExecuteCommandProvider.Commands, "hledger.run")
	assert.NotContains(t, caps.ExecuteCommandProvider.Commands, "hledger.fixUnbalanced")

	require.NotNil(t, result.ServerInfo)
	assert.Equal(t, "hledger-lsp", result.ServerInfo.Name)
	assert.Equal(t, "dev", optionalString(result.ServerInfo.Version))
}

func TestNewServerWithVersion_InitializeReportsVersion(t *testing.T) {
	srv := NewServerWithVersion("test-version")

	result, err := srv.Initialize(context.Background(), &protocol.InitializeParams{})

	require.NoError(t, err)
	assert.Equal(t, "test-version", optionalString(result.ServerInfo.Version))
}

func TestServer_Initialized(t *testing.T) {
	srv := NewServer()

	err := srv.Initialized(context.Background(), &protocol.InitializedParams{})

	assert.NoError(t, err)
}

func TestServer_Initialize_WithRootURI(t *testing.T) {
	srv := NewServer()

	rootURI := uri.URI("file:///tmp/test-workspace")
	params := &protocol.InitializeParams{
		RootURI: &rootURI,
	}

	_, err := srv.Initialize(context.Background(), params)
	require.NoError(t, err)

	assert.Equal(t, "/tmp/test-workspace", srv.RootURI())
	assert.NotNil(t, srv.Workspace())
}

func TestServer_Initialize_WithWorkspaceFolders(t *testing.T) {
	srv := NewServer()

	params := &protocol.InitializeParams{
		WorkspaceFoldersInitializeParams: protocol.WorkspaceFoldersInitializeParams{WorkspaceFolders: protocol.NewNullable([]protocol.WorkspaceFolder{
			{URI: uri.URI("file:///tmp/folder1"), Name: "folder1"},
			{URI: uri.URI("file:///tmp/folder2"), Name: "folder2"},
		})},
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
	uri := uri.URI("file:///test.journal")
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
	uri := uri.URI("file:///test.journal")
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
			&protocol.TextDocumentContentChangeWholeDocument{Text: newContent},
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
	uri := uri.URI("file:///test.journal")
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	srv.documents.Store(uri, content)

	params := &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri},
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			&protocol.TextDocumentContentChangePartial{
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
	uri := uri.URI("file:///nonexistent.journal")

	params := &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri},
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			&protocol.TextDocumentContentChangeWholeDocument{Text: "new content"},
		},
	}

	err := srv.DidChange(context.Background(), params)

	assert.NoError(t, err)

	_, ok := srv.GetDocument(uri)
	assert.False(t, ok)
}

func TestServer_DidClose(t *testing.T) {
	srv := NewServer()
	uri := uri.URI("file:///test.journal")
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

func TestServer_DidClose_RestoresWorkspaceIndexFromDisk(t *testing.T) {
	tmpDir := t.TempDir()
	journalPath := filepath.Join(tmpDir, "main.journal")
	diskContent := "account assets:disk\n"
	unsavedContent := "account assets:discarded\n"
	require.NoError(t, os.WriteFile(journalPath, []byte(diskContent), 0644))

	srv := NewServer()
	rootURI := uri.File(tmpDir)
	_, err := srv.Initialize(context.Background(), &protocol.InitializeParams{RootURI: &rootURI})
	require.NoError(t, err)
	require.NoError(t, srv.workspace.Initialize())

	journalURI := uri.File(journalPath)
	require.NoError(t, srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: journalURI, Text: unsavedContent},
	}))

	accounts := srv.workspace.GetDeclaredAccountsForFile(journalPath)
	assert.Contains(t, accounts, "assets:discarded")
	assert.NotContains(t, accounts, "assets:disk")

	require.NoError(t, srv.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: journalURI},
	}))

	accounts = srv.workspace.GetDeclaredAccountsForFile(journalPath)
	assert.Contains(t, accounts, "assets:disk")
	assert.NotContains(t, accounts, "assets:discarded")
}

func TestServer_DidSave(t *testing.T) {
	srv := NewServer()
	uri := uri.URI("file:///test.journal")

	params := &protocol.DidSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}

	err := srv.DidSave(context.Background(), params)

	assert.NoError(t, err)
}

func TestServer_DidSave_InvalidatesAlignmentCache(t *testing.T) {
	srv := NewServer()
	uri := uri.URI("file:///test.journal")

	srv.alignmentCache.Store(uri, 42)

	params := &protocol.DidSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}
	err := srv.DidSave(context.Background(), params)
	assert.NoError(t, err)

	_, loaded := srv.alignmentCache.Load(uri)
	assert.False(t, loaded, "alignmentCache should be invalidated on DidSave")
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

func TestContentChangeV1Arms(t *testing.T) {
	tests := []struct {
		name   string
		change protocol.TextDocumentContentChangeEvent
		whole  bool
	}{
		{
			name:   "whole document",
			change: &protocol.TextDocumentContentChangeWholeDocument{Text: "replacement"},
			whole:  true,
		},
		{
			name: "partial range",
			change: &protocol.TextDocumentContentChangePartial{Range: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 5},
				End:   protocol.Position{Line: 0, Character: 0},
			}, Text: "insert"},
			whole: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, isWhole := tt.change.(*protocol.TextDocumentContentChangeWholeDocument)
			assert.Equal(t, tt.whole, isWhole)
		})
	}
}

func TestServer_PublishDiagnostics_ParseError(t *testing.T) {
	srv := NewServer()
	srv.diagDebounce = 0
	client := &mockClient{}
	srv.SetClient(client)

	uri := uri.URI("file:///test.journal")
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

func TestServer_PublishDiagnostics_ParseError_RangeSpansToken(t *testing.T) {
	srv := NewServer()
	srv.diagDebounce = 0
	client := &mockClient{}
	srv.SetClient(client)

	uri := uri.URI("file:///test.journal")
	content := `2024-01-15 test
    12345`

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

	var parseErrDiag *protocol.Diagnostic
	for _, pub := range diagnostics {
		for i, d := range pub.Diagnostics {
			if d.Severity == protocol.DiagnosticSeverityError && optionalString(d.Source) == "hledger-lsp" {
				parseErrDiag = &pub.Diagnostics[i]
				break
			}
		}
	}
	require.NotNil(t, parseErrDiag, "expected a parse error diagnostic")

	assert.True(t,
		parseErrDiag.Range.End.Character > parseErrDiag.Range.Start.Character ||
			parseErrDiag.Range.End.Line > parseErrDiag.Range.Start.Line,
		"diagnostic range should span the token, got Start=%v End=%v",
		parseErrDiag.Range.Start, parseErrDiag.Range.End)
}

func TestServer_PublishDiagnostics_BalanceError(t *testing.T) {
	srv := NewServer()
	srv.diagDebounce = 0
	client := &mockClient{}
	srv.SetClient(client)

	uri := uri.URI("file:///test.journal")
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
	srv.diagDebounce = 0
	client := &mockClient{}
	srv.SetClient(client)

	uri := uri.URI("file:///test.journal")
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
	uri := uri.URI("file:///test.journal")
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
	uri := uri.URI("file:///test.journal")
	content := "test content"

	srv.documents.Store(uri, content)

	doc, ok := srv.GetDocument(uri)

	assert.True(t, ok)
	assert.Equal(t, content, doc)
}

func TestServer_GetDocument_NotFound(t *testing.T) {
	srv := NewServer()
	uri := uri.URI("file:///nonexistent.journal")

	doc, ok := srv.GetDocument(uri)

	assert.False(t, ok)
	assert.Empty(t, doc)
}

func TestServer_GetResolved_Found(t *testing.T) {
	srv := NewServer()
	srv.diagDebounce = 0
	client := &mockClient{}
	srv.SetClient(client)

	uri := uri.URI("file:///test.journal")
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
	uri := uri.URI("file:///nonexistent.journal")

	resolved := srv.GetResolved(uri)

	assert.Nil(t, resolved)
}

func TestServer_Format(t *testing.T) {
	srv := NewServer()
	uri := uri.URI("file:///test.journal")
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
	uri := uri.URI("file:///nonexistent.journal")

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
	srv.diagDebounce = 0
	client := &mockClient{}
	srv.SetClient(client)

	rootURI := uri.File(tmpDir)
	initParams := &protocol.InitializeParams{RootURI: &rootURI}
	_, err = srv.Initialize(context.Background(), initParams)
	require.NoError(t, err)

	err = srv.workspace.Initialize()
	require.NoError(t, err)

	uri := uri.File(txPath)
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
			if diagnosticCodeString(d.Code) == "UNDECLARED_COMMODITY" {
				if strings.Contains(tooltipString(d.Message), "EUR") {
					foundEURWarning = true
				}
				if strings.Contains(tooltipString(d.Message), "RUB") {
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

	rootURI := uri.File(tmpDir)
	initParams := &protocol.InitializeParams{RootURI: &rootURI}
	_, err = srv.Initialize(context.Background(), initParams)
	require.NoError(t, err)

	err = srv.workspace.Initialize()
	require.NoError(t, err)

	uri := uri.File(txPath)
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
		uri      uri.URI
		expected string
	}{
		{
			name:     "file URI",
			uri:      uri.URI("file:///test.journal"),
			expected: "/test.journal",
		},
		{
			name:     "git URI returns empty",
			uri:      uri.URI("git://github.com/user/repo/main/file.journal"),
			expected: "",
		},
		{
			name:     "untitled URI returns empty",
			uri:      uri.URI("untitled:Untitled-1"),
			expected: "",
		},
		{
			name:     "vscode-notebook URI returns empty",
			uri:      uri.URI("vscode-notebook-cell://something"),
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
	srv.diagDebounce = 0
	client := &mockClient{}
	srv.SetClient(client)

	uri := uri.URI("git://github.com/user/repo/main/file.journal")
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
	srv.diagDebounce = 0
	client := &mockClient{}
	srv.SetClient(client)

	rootURI := uri.File(tmpDir)
	_, err = srv.Initialize(context.Background(), &protocol.InitializeParams{RootURI: &rootURI})
	require.NoError(t, err)

	err = srv.Initialized(context.Background(), &protocol.InitializedParams{})
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	txURI := uri.File(txPath)
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

func TestServer_Initialize_FeatureToggles(t *testing.T) {
	tests := []struct {
		name        string
		initOptions map[string]interface{}
		checkCaps   func(t *testing.T, caps protocol.ServerCapabilities)
	}{
		{
			name:        "all features enabled by default",
			initOptions: nil,
			checkCaps: func(t *testing.T, caps protocol.ServerCapabilities) {
				assert.NotNil(t, caps.CompletionProvider)
				hoverEnabled, ok := caps.HoverProvider.(protocol.Boolean)
				require.True(t, ok)
				assert.True(t, bool(hoverEnabled))
				formatEnabled, ok := caps.DocumentFormattingProvider.(protocol.Boolean)
				require.True(t, ok)
				assert.True(t, bool(formatEnabled))
				assert.NotNil(t, caps.SemanticTokensProvider)
				assert.NotNil(t, caps.CodeActionProvider)
				inlayHintsEnabled, ok := caps.InlayHintProvider.(protocol.Boolean)
				require.True(t, ok)
				assert.True(t, bool(inlayHintsEnabled))
			},
		},
		{
			name: "hover disabled",
			initOptions: map[string]interface{}{
				"features": map[string]interface{}{
					"hover": false,
				},
			},
			checkCaps: func(t *testing.T, caps protocol.ServerCapabilities) {
				assert.Nil(t, caps.HoverProvider)
			},
		},
		{
			name: "completion disabled",
			initOptions: map[string]interface{}{
				"features": map[string]interface{}{
					"completion": false,
				},
			},
			checkCaps: func(t *testing.T, caps protocol.ServerCapabilities) {
				assert.Nil(t, caps.CompletionProvider)
			},
		},
		{
			name: "formatting disabled",
			initOptions: map[string]interface{}{
				"features": map[string]interface{}{
					"formatting": false,
				},
			},
			checkCaps: func(t *testing.T, caps protocol.ServerCapabilities) {
				assert.Nil(t, caps.DocumentFormattingProvider)
				assert.Nil(t, caps.DocumentRangeFormattingProvider)
				assert.Zero(t, caps.DocumentOnTypeFormattingProvider)
			},
		},
		{
			name: "semantic tokens disabled",
			initOptions: map[string]interface{}{
				"features": map[string]interface{}{
					"semanticTokens": false,
				},
			},
			checkCaps: func(t *testing.T, caps protocol.ServerCapabilities) {
				assert.Nil(t, caps.SemanticTokensProvider)
			},
		},
		{
			name: "code actions disabled",
			initOptions: map[string]interface{}{
				"features": map[string]interface{}{
					"codeActions": false,
				},
			},
			checkCaps: func(t *testing.T, caps protocol.ServerCapabilities) {
				assert.Nil(t, caps.CodeActionProvider)
				assert.Zero(t, caps.ExecuteCommandProvider)
			},
		},
		{
			name: "code lens command without code actions",
			initOptions: map[string]interface{}{
				"features": map[string]interface{}{
					"codeActions": false,
					"codeLens":    true,
				},
			},
			checkCaps: func(t *testing.T, caps protocol.ServerCapabilities) {
				assert.NotNil(t, caps.CodeLensProvider)
				require.NotEmpty(t, caps.ExecuteCommandProvider.Commands)
				assert.Equal(t, []string{"hledger.fixUnbalanced"}, caps.ExecuteCommandProvider.Commands)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewServer()
			initOptions, err := marshalLSPAny(tt.initOptions)
			require.NoError(t, err)
			params := &protocol.InitializeParams{
				InitializationOptions: initOptions,
			}
			result, err := srv.Initialize(context.Background(), params)
			require.NoError(t, err)
			tt.checkCaps(t, result.Capabilities)
		})
	}
}

func TestServer_DiagnosticsSettings(t *testing.T) {
	t.Run("undeclared accounts disabled", func(t *testing.T) {
		client := &mockClient{}
		srv := NewServer()
		srv.diagDebounce = 0
		srv.SetClient(client)

		settings := srv.getSettings()
		settings.Diagnostics.UndeclaredAccounts = false
		srv.setSettings(settings)

		content := `account assets:cash

2024-01-15 test
    expenses:food  $50
    assets:cash
`
		uri := uri.URI("file:///test.journal")
		err := srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{URI: uri, Text: content},
		})
		require.NoError(t, err)
		time.Sleep(100 * time.Millisecond)

		diagnostics := client.getDiagnostics()
		require.NotEmpty(t, diagnostics)

		for _, pub := range diagnostics {
			for _, d := range pub.Diagnostics {
				assert.NotEqual(t, "UNDECLARED_ACCOUNT", d.Code,
					"undeclared account diagnostics should be filtered out")
			}
		}
	})

	t.Run("undeclared commodities disabled", func(t *testing.T) {
		client := &mockClient{}
		srv := NewServer()
		srv.diagDebounce = 0
		srv.SetClient(client)

		settings := srv.getSettings()
		settings.Diagnostics.UndeclaredCommodities = false
		srv.setSettings(settings)

		content := `commodity $

2024-01-15 test
    expenses:food  50 EUR
    assets:cash
`
		uri := uri.URI("file:///test.journal")
		err := srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{URI: uri, Text: content},
		})
		require.NoError(t, err)
		time.Sleep(100 * time.Millisecond)

		diagnostics := client.getDiagnostics()
		require.NotEmpty(t, diagnostics)

		for _, pub := range diagnostics {
			for _, d := range pub.Diagnostics {
				assert.NotEqual(t, "UNDECLARED_COMMODITY", d.Code,
					"undeclared commodity diagnostics should be filtered out")
			}
		}
	})

	t.Run("unbalanced transactions disabled", func(t *testing.T) {
		client := &mockClient{}
		srv := NewServer()
		srv.diagDebounce = 0
		srv.SetClient(client)

		settings := srv.getSettings()
		settings.Diagnostics.UnbalancedTransactions = false
		srv.setSettings(settings)

		content := `2024-01-15 test
    expenses:food  $50
    assets:cash  $20
`
		uri := uri.URI("file:///test.journal")
		err := srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{URI: uri, Text: content},
		})
		require.NoError(t, err)
		time.Sleep(100 * time.Millisecond)

		diagnostics := client.getDiagnostics()
		require.NotEmpty(t, diagnostics)

		for _, pub := range diagnostics {
			for _, d := range pub.Diagnostics {
				assert.NotEqual(t, "UNBALANCED", d.Code,
					"unbalanced transaction diagnostics should be filtered out")
				assert.NotEqual(t, "MULTIPLE_INFERRED", d.Code,
					"multiple inferred diagnostics should be filtered out")
			}
		}
	})
}

func TestServer_BalanceTolerance(t *testing.T) {
	t.Run("imbalance within user tolerance is not reported", func(t *testing.T) {
		client := &mockClient{}
		srv := NewServer()
		srv.diagDebounce = 0
		srv.SetClient(client)

		settings := srv.getSettings()
		settings.Diagnostics.BalanceTolerance = 0.01
		srv.setSettings(settings)

		// 3.00 * 0.33510 = 1.0053; balance = 1.0053 - 1.00 = 0.0053
		// Precision-based tolerance: 0.005 → 0.0053 > 0.005 → would be unbalanced
		// User tolerance: 0.01 → 0.0053 < 0.01 → balanced
		content := `2024-01-15 exchange
    assets:foreign  3.00 USD @ 0.33510 EUR
    assets:eur  -1.00 EUR
`
		uri := uri.URI("file:///test.journal")
		err := srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{URI: uri, Text: content},
		})
		require.NoError(t, err)
		time.Sleep(100 * time.Millisecond)

		diagnostics := client.getDiagnostics()
		for _, pub := range diagnostics {
			for _, d := range pub.Diagnostics {
				assert.NotEqual(t, "UNBALANCED", d.Code,
					"imbalance 0.0053 should be within user tolerance 0.01")
			}
		}
	})

	t.Run("imbalance exceeding user tolerance is reported", func(t *testing.T) {
		client := &mockClient{}
		srv := NewServer()
		srv.diagDebounce = 0
		srv.SetClient(client)

		settings := srv.getSettings()
		settings.Diagnostics.BalanceTolerance = 0.001
		srv.setSettings(settings)

		// 3.00 * 0.337 = 1.011; balance = 1.011 - 1.00 = 0.011
		// Precision-based: 0.005, user: 0.001 → max = 0.005
		// 0.011 > 0.005 → unbalanced
		content := `2024-01-15 exchange
    assets:foreign  3.00 USD @ 0.337 EUR
    assets:eur  -1.00 EUR
`
		uri := uri.URI("file:///test.journal")
		err := srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{URI: uri, Text: content},
		})
		require.NoError(t, err)
		time.Sleep(100 * time.Millisecond)

		diagnostics := client.getDiagnostics()
		hasUnbalanced := false
		for _, pub := range diagnostics {
			for _, d := range pub.Diagnostics {
				if diagnosticCodeString(d.Code) == "UNBALANCED" {
					hasUnbalanced = true
				}
			}
		}
		assert.True(t, hasUnbalanced, "imbalance 0.011 should exceed effective tolerance 0.005")
	})
}

func TestNormalizeLineEndings(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "LF unchanged",
			input:    "line1\nline2\nline3",
			expected: "line1\nline2\nline3",
		},
		{
			name:     "CRLF to LF",
			input:    "line1\r\nline2\r\nline3",
			expected: "line1\nline2\nline3",
		},
		{
			name:     "bare CR to LF",
			input:    "line1\rline2\rline3",
			expected: "line1\nline2\nline3",
		},
		{
			name:     "mixed line endings",
			input:    "line1\r\nline2\nline3\rline4",
			expected: "line1\nline2\nline3\nline4",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "no line endings",
			input:    "hello",
			expected: "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeLineEndings(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestServer_DidOpen_NormalizesCRLF(t *testing.T) {
	srv := NewServer()
	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 test\r\n    expenses:food  $50  ;date:2026-02-21\r\n    assets:cash\r\n"

	params := &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:  uri,
			Text: content,
		},
	}

	err := srv.DidOpen(context.Background(), params)
	require.NoError(t, err)

	doc, ok := srv.GetDocument(uri)
	require.True(t, ok)
	assert.NotContains(t, doc, "\r", "stored document should not contain CR")
	assert.Contains(t, doc, "\n", "stored document should contain LF")
}

func TestServer_DidChange_NormalizesCRLF(t *testing.T) {
	srv := NewServer()
	uri := uri.URI("file:///test.journal")

	srv.documents.Store(uri, "initial")

	params := &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri},
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			&protocol.TextDocumentContentChangePartial{
				Range: protocol.Range{
					Start: protocol.Position{Line: 0, Character: 0},
					End:   protocol.Position{Line: 0, Character: 0},
				},
				Text: "line1\r\nline2\r\n",
			},
		},
	}

	err := srv.DidChange(context.Background(), params)
	require.NoError(t, err)

	doc, ok := srv.GetDocument(uri)
	require.True(t, ok)
	assert.NotContains(t, doc, "\r", "stored document should not contain CR after full change")
}

func TestServer_Format_CRLFDocumentNoBlankLines(t *testing.T) {
	srv := NewServer()
	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 购买基金\r\n    资产:微信wx  $50  ;date:2026-02-21\r\n    资产:待报销费用bx\r\n"

	srv.documents.Store(uri, normalizeLineEndings(content))

	params := &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}

	edits, err := srv.Format(context.Background(), params)
	require.NoError(t, err)

	for _, edit := range edits {
		assert.NotContains(t, edit.NewText, "\r",
			"format edits must not contain CR characters")
	}
}

func TestServer_RulesDiagnostics_PositionConversion(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.rules")

	// "decimal-mark x" triggers INVALID_DECIMAL_MARK at the directive range.
	// The rules lexer uses 1-based positions; analyzeRules must convert to 0-based.
	diags, err := ts.openAndWait(uri, "decimal-mark x")
	require.NoError(t, err)
	require.NotEmpty(t, diags, "expected at least one diagnostic for invalid decimal-mark")

	var found *protocol.Diagnostic
	for i := range diags {
		if diagnosticCodeString(diags[i].Code) == "INVALID_DECIMAL_MARK" {
			found = &diags[i]
			break
		}
	}
	require.NotNil(t, found, "expected INVALID_DECIMAL_MARK diagnostic")

	assert.Equal(t, protocol.DiagnosticSeverityError, found.Severity)
	assert.Equal(t, uint32(0), found.Range.Start.Line, "Start.Line must be 0-based")
	assert.Equal(t, uint32(0), found.Range.Start.Character, "Start.Character must be 0-based")
}

func TestServer_RulesSourceDirective_NoDiagnostics(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.rules")

	content := "source data.csv\nskip 1\nfields date,description,amount"
	diags, err := ts.openAndWait(uri, content)
	require.NoError(t, err)

	// No diagnostics expected for a valid rules file with source directive
	for _, d := range diags {
		if strings.Contains(tooltipString(d.Message), "unexpected content") {
			t.Errorf("unexpected journal parser error in rules file: %s", tooltipString(d.Message))
		}
	}
	assert.Empty(t, diags, "valid rules file with source directive should have no diagnostics")
}

func TestServer_RulesIncludeDirective_NoDiagnostics(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.rules")

	content := "include common.rules\nskip 1\nfields date,description,amount"
	diags, err := ts.openAndWait(uri, content)
	require.NoError(t, err)

	for _, d := range diags {
		if strings.Contains(tooltipString(d.Message), "unexpected content") {
			t.Errorf("unexpected journal parser error in rules file: %s", tooltipString(d.Message))
		}
	}
	// With the expansion-based loader, a missing include produces a
	// file-not-found diagnostic. This is correct behavior — the include
	// target must exist on disk or be provided via overlay.
	for _, d := range diags {
		if d.Severity == protocol.DiagnosticSeverityError && !strings.Contains(tooltipString(d.Message), "cannot read file") {
			t.Errorf("unexpected error diagnostic: %s", tooltipString(d.Message))
		}
	}
}

func TestInitialize_RootURIParsing(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		wantPath string
	}{
		{
			name:     "Unix-style URI",
			uri:      "file:///home/user/workspace",
			wantPath: "/home/user/workspace",
		},
		{
			name:     "URI with workspace folders",
			uri:      "file:///tmp/test-workspace",
			wantPath: "/tmp/test-workspace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewServer()
			_, err := srv.Initialize(context.Background(), &protocol.InitializeParams{WorkspaceFoldersInitializeParams: protocol.WorkspaceFoldersInitializeParams{
				WorkspaceFolders: protocol.NewNullable([]protocol.WorkspaceFolder{{URI: uri.URI(tt.uri), Name: "test"}}),
			}})
			require.NoError(t, err)
			assert.Equal(t, tt.wantPath, srv.rootURI)
		})
	}
}

func TestInitialize_RootURIFromRootURI(t *testing.T) {
	srv := NewServer()
	rootURI := uri.URI("file:///tmp/fallback-workspace")
	_, err := srv.Initialize(context.Background(), &protocol.InitializeParams{RootURI: &rootURI})
	require.NoError(t, err)
	assert.Equal(t, "/tmp/fallback-workspace", srv.rootURI)
}
