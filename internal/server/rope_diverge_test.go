package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// The rope in the document store is the single source of truth. The workspace
// materializes that same rope when it recomputes a tree, so the content a
// reader is served and the content the index was built from cannot drift apart.
//
// This replaces the test that guarded `applyChange` against a rope diverging
// from a separately stored flat string. With one store that divergence is no
// longer representable, so the invariant worth pinning is the agreement itself:
// an edit lands in the tree the workspace serves, and in GetDocument.
func TestDidChange_WorkspaceAndDocumentAgree(t *testing.T) {
	tmpDir := t.TempDir()
	mainPath := filepath.Join(tmpDir, "main.journal")
	base := "2024-01-01 one\n    expenses:food  $20\n    assets:cash\n"
	require.NoError(t, os.WriteFile(mainPath, []byte(base), 0o644))

	ts := initWorkspaceTestServer(t, tmpDir)
	mainURI := uri.URI("file://" + mainPath)
	require.NoError(t, ts.openDocument(mainURI, base))

	insert := &protocol.TextDocumentContentChangePartial{
		Range: protocol.Range{
			Start: protocol.Position{Line: 3, Character: 0},
			End:   protocol.Position{Line: 3, Character: 0},
		},
		Text: "\n2024-01-02 two\n    expenses:rent  $5\n    assets:cash\n",
	}
	require.NoError(t, ts.changeDocument(mainURI, []protocol.TextDocumentContentChangeEvent{insert}))

	got, ok := ts.GetDocument(mainURI)
	require.True(t, ok)
	require.Contains(t, got, "2024-01-02 two", "the edit must reach the document readers")

	resolved := ts.workspace.GetResolvedForFile(mainPath)
	require.NotNil(t, resolved, "the workspace must own the file it was opened from")

	found := false
	for _, tx := range resolved.AllTransactions() {
		if tx.Description == "two" {
			found = true
			break
		}
	}
	assert.True(t, found,
		"the tree the workspace serves must be built from the same content GetDocument returns")
}
