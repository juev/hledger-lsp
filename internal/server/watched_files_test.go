package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/workspace"
)

func TestDidChangeWatchedFiles_RepublishesDiagnostics(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")
	tmpDir := t.TempDir()

	subPath := filepath.Join(tmpDir, "sub.journal")
	subContent := "2024-01-01 test\n    expenses:food  $10\n    assets:cash  $-10\n"
	require.NoError(t, os.WriteFile(subPath, []byte(subContent), 0644))

	mainPath := filepath.Join(tmpDir, "main.journal")
	mainContent := "include sub.journal\n"
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0644))

	ts := newTestServer()
	loader := include.NewLoader()
	ts.workspace = workspace.NewWorkspace(tmpDir, loader)
	ts.loader = loader
	require.NoError(t, ts.workspace.Initialize())

	mainURI := uri.File(mainPath)
	_, err := ts.openAndWait(mainURI, mainContent)
	require.NoError(t, err)

	ts.client.mu.Lock()
	beforeCount := len(ts.client.diagnostics)
	ts.client.mu.Unlock()

	err = ts.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{
			{
				URI:  uri.File(subPath),
				Type: protocol.FileChangeTypeChanged,
			},
		},
	})
	require.NoError(t, err)

	ts.client.waitDiagnostics()

	ts.client.mu.Lock()
	afterCount := len(ts.client.diagnostics)
	ts.client.mu.Unlock()

	assert.Greater(t, afterCount, beforeCount, "diagnostics should be republished")
}

func TestDidChangeWatchedFiles_SkipsOpenDocuments(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")
	tmpDir := t.TempDir()

	subPath := filepath.Join(tmpDir, "sub.journal")
	subContent := "2024-01-01 test\n    expenses:food  $10\n    assets:cash  $-10\n"
	require.NoError(t, os.WriteFile(subPath, []byte(subContent), 0644))

	mainPath := filepath.Join(tmpDir, "main.journal")
	mainContent := "include sub.journal\n"
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0644))

	ts := newTestServer()
	loader := include.NewLoader()
	ts.workspace = workspace.NewWorkspace(tmpDir, loader)
	ts.loader = loader
	require.NoError(t, ts.workspace.Initialize())

	subURI := uri.File(subPath)
	_, err := ts.openAndWait(subURI, subContent)
	require.NoError(t, err)

	ts.client.mu.Lock()
	beforeCount := len(ts.client.diagnostics)
	ts.client.mu.Unlock()

	err = ts.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{
			{
				URI:  subURI,
				Type: protocol.FileChangeTypeChanged,
			},
		},
	})
	require.NoError(t, err)

	ts.client.mu.Lock()
	afterCount := len(ts.client.diagnostics)
	ts.client.mu.Unlock()

	assert.Equal(t, beforeCount, afterCount, "should not republish for open documents")
}

func TestDidChangeWatchedFiles_InvalidatesIncludedTemplateCache(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")
	tmpDir := t.TempDir()

	childPath := filepath.Join(tmpDir, "child.journal")
	childContent1 := `2024-01-01 Grocery
    expenses:food  $10
    assets:cash
`
	require.NoError(t, os.WriteFile(childPath, []byte(childContent1), 0o644))

	mainPath := filepath.Join(tmpDir, "main.journal")
	mainContent := `include child.journal

2024-01-02 Grocery
`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o644))

	ts := newTestServer()
	loader := include.NewLoader()
	ts.workspace = workspace.NewWorkspace(tmpDir, loader)
	ts.loader = loader
	require.NoError(t, ts.workspace.Initialize())

	mainURI := uri.File(mainPath)
	require.NoError(t, ts.openDocument(mainURI, mainContent))
	params := inlineCompletionParams(mainURI, 3, 0)

	result1, err := ts.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, inlineCompletionItems(t, result1), 1)
	assert.Contains(t, inlineCompletionItems(t, result1)[0].InsertText, "assets:cash")

	childContent2 := `2024-01-01 Grocery
    expenses:food  $20
    assets:bank
`
	require.NoError(t, os.WriteFile(childPath, []byte(childContent2), 0o644))
	require.NoError(t, ts.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{{
			URI:  uri.File(childPath),
			Type: protocol.FileChangeTypeChanged,
		}},
	}))

	result2, err := ts.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, inlineCompletionItems(t, result2), 1)
	assert.Contains(t, inlineCompletionItems(t, result2)[0].InsertText, "assets:bank")
}
