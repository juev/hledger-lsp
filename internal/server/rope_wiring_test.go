package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

// TestDidChange_RopeIncrementalAndFullChange verifies the rope-backed applyChange
// wiring: incremental edits accumulate on the persisted rope (including multi-line
// and non-ASCII inserts), and a full-document change resets the rope so the next
// incremental edit starts from the new content.
func TestDidChange_RopeIncrementalAndFullChange(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///rope.journal")
	base := "2024-01-01 * store\n    expenses:food  $50\n    assets:cash\n"
	require.NoError(t, ts.openDocument(uri, base))

	pos := func(line, char uint32) protocol.Position {
		return protocol.Position{Line: line, Character: char}
	}
	apply := func(r protocol.Range, text string) string {
		require.NoError(t, ts.changeDocument(uri, []protocol.TextDocumentContentChangeEvent{{Range: r, Text: text}}))
		got, ok := ts.GetDocument(uri)
		require.True(t, ok)
		return got
	}

	// Incremental single-char insert (non-full range).
	got := apply(protocol.Range{Start: pos(0, 5), End: pos(0, 5)}, "X")
	assert.Equal(t, "2024-X01-01 * store\n    expenses:food  $50\n    assets:cash\n", got)

	// Incremental multi-line + Cyrillic insert; the rope persists across edits.
	got = apply(protocol.Range{Start: pos(1, 4), End: pos(1, 4)}, "расходы:еда  $1\n    ")
	assert.Equal(t, "2024-X01-01 * store\n    расходы:еда  $1\n    expenses:food  $50\n    assets:cash\n", got)

	// Full-document change replaces the content and resets the rope.
	got = apply(protocol.Range{}, "2025-05-05 * fresh\n    assets:bank  $1\n    income:x\n")
	assert.Equal(t, "2025-05-05 * fresh\n    assets:bank  $1\n    income:x\n", got)

	// An incremental edit after a full change builds from the new content.
	got = apply(protocol.Range{Start: pos(0, 4), End: pos(0, 4)}, "-")
	assert.Equal(t, "2025--05-05 * fresh\n    assets:bank  $1\n    income:x\n", got)
}
