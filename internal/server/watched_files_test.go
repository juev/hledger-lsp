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
