package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/include"
)

// TestHover_ChildDecimalMark_DoesNotAffectParent verifies that a child's
// decimal-mark directive does not leak into the parent's hover formatting.
// The root has no D directive; the child sets decimal-mark to comma. Hovering
// over a root amount must not show the comma decimal mark.
func TestHover_ChildDecimalMark_DoesNotAffectParent(t *testing.T) {
	tmpDir := t.TempDir()

	childContent := "decimal-mark ,\n\n2024-01-01 child tx\n    expenses:child  10,50\n    assets:cash\n"
	rootContent := "include child.journal\n\n2024-01-02 root tx\n    expenses:food  50.00\n    assets:cash\n"

	childPath := filepath.Join(tmpDir, "child.journal")
	rootPath := filepath.Join(tmpDir, "root.journal")
	require.NoError(t, os.WriteFile(childPath, []byte(childContent), 0644))
	require.NoError(t, os.WriteFile(rootPath, []byte(rootContent), 0644))

	ts := newTestServer()
	rootURI := protocol.DocumentURI(fmt.Sprintf("file://%s", rootPath))
	_, err := ts.openAndWait(rootURI, rootContent)
	require.NoError(t, err)

	// Hover over the amount "50.00" in the root transaction (line 3, "    expenses:food  50.00")
	hover, err := ts.Hover(context.Background(), &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: rootURI},
			Position:     protocol.Position{Line: 3, Character: 22},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, hover)

	content := hover.Contents.Value
	// Root must not inherit child's comma decimal mark
	assert.NotContains(t, content, "50,00", "root hover must not use child's comma decimal mark")
}

// TestReferences_RepeatedInclude_Dedup verifies that references to an account
// in a file included twice returns each (URI, range) exactly once.
func TestReferences_RepeatedInclude_Dedup(t *testing.T) {
	tmpDir := t.TempDir()

	childContent := "2024-01-01 child tx\n    expenses:child  $10.00\n    assets:cash\n"
	rootContent := "include child.journal\n\ninclude child.journal\n\n2024-01-02 root tx\n    expenses:child  $20.00\n    assets:cash\n"

	childPath := filepath.Join(tmpDir, "child.journal")
	rootPath := filepath.Join(tmpDir, "root.journal")
	require.NoError(t, os.WriteFile(childPath, []byte(childContent), 0644))
	require.NoError(t, os.WriteFile(rootPath, []byte(rootContent), 0644))

	ts := newTestServer()
	rootURI := protocol.DocumentURI(fmt.Sprintf("file://%s", rootPath))
	_, err := ts.openAndWait(rootURI, rootContent)
	require.NoError(t, err)

	// References to "expenses:child" from the root transaction (line 5)
	refs, err := ts.references(rootURI, 5, 6, true)
	require.NoError(t, err)

	// Count references in child.journal — must be exactly 1 (deduped)
	childURI := protocol.DocumentURI(fmt.Sprintf("file://%s", childPath))
	childRefCount := 0
	for _, ref := range refs {
		if ref.URI == childURI {
			childRefCount++
		}
	}
	assert.Equal(t, 1, childRefCount,
		"child.journal reference must appear exactly once despite double include")
}

// TestRename_RepeatedInclude_NoDuplicateEdits verifies that rename does not
// produce duplicate text edits for the same source location when a file is
// included twice.
func TestRename_RepeatedInclude_NoDuplicateEdits(t *testing.T) {
	tmpDir := t.TempDir()

	childContent := "2024-01-01 child tx\n    expenses:child  $10.00\n    assets:cash\n"
	rootContent := "include child.journal\n\ninclude child.journal\n\n2024-01-02 root tx\n    expenses:child  $20.00\n    assets:cash\n"

	childPath := filepath.Join(tmpDir, "child.journal")
	rootPath := filepath.Join(tmpDir, "root.journal")
	require.NoError(t, os.WriteFile(childPath, []byte(childContent), 0644))
	require.NoError(t, os.WriteFile(rootPath, []byte(rootContent), 0644))

	ts := newTestServer()
	rootURI := protocol.DocumentURI(fmt.Sprintf("file://%s", rootPath))
	_, err := ts.openAndWait(rootURI, rootContent)
	require.NoError(t, err)

	result, err := ts.Rename(context.Background(), &protocol.RenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: rootURI},
			Position:     protocol.Position{Line: 5, Character: 6},
		},
		NewName: "expenses:renamed",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	childURI := protocol.DocumentURI(fmt.Sprintf("file://%s", childPath))
	childEdits := result.Changes[childURI]
	// child.journal has one posting with expenses:child — must be exactly 1 edit
	assert.Len(t, childEdits, 1,
		"rename must produce exactly one edit per source location in child.journal")
}

// TestResolvedWithoutTransaction_PreservesOccurrences verifies that the
// completion filter preserves the occurrence structure so AnalyzeResolved
// counts each occurrence of a repeated include.
func TestResolvedWithoutTransaction_PreservesOccurrences(t *testing.T) {
	tmpDir := t.TempDir()

	childContent := "2024-01-01 child tx\n    expenses:child  $10.00\n    assets:cash\n"
	rootContent := "include child.journal\n\ninclude child.journal\n\n2024-01-02 root tx\n    expenses:food  $20.00\n    assets:cash\n"

	childPath := filepath.Join(tmpDir, "child.journal")
	rootPath := filepath.Join(tmpDir, "root.journal")
	require.NoError(t, os.WriteFile(childPath, []byte(childContent), 0644))
	require.NoError(t, os.WriteFile(rootPath, []byte(rootContent), 0644))

	loader := include.NewLoader()
	resolved, errs := loader.Load(rootPath)
	require.Empty(t, errs)
	require.NotNil(t, resolved)
	require.NotEmpty(t, resolved.Occurrences, "loader must populate occurrences")

	rootURI := protocol.DocumentURI(fmt.Sprintf("file://%s", rootPath))
	// Cursor inside the root transaction (line 5)
	filtered := resolvedWithoutTransaction(resolved, 5, rootURI)

	assert.NotNil(t, filtered.Occurrences,
		"resolvedWithoutTransaction must preserve occurrences for AnalyzeResolved")
	assert.NotNil(t, filtered.Items,
		"resolvedWithoutTransaction must preserve items for AnalyzeResolved")
}
