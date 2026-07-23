package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/document"
)

// TestDidChange_RebuildsDivergedRope reproduces the corruption where the
// persisted rope diverges from the authoritative document content (as can happen
// under concurrent DidChange interleaving) and an edit then lands in the wrong
// place. applyChange must detect the divergence and rebuild the rope from the
// authoritative content before applying the edit.
func TestDidChange_RebuildsDivergedRope(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///diverge.journal")
	full := "2024-01-01 one\n    expenses:a  $1\n    assets:cash\n\n2024-01-02 two\n    expenses:b  $2\n    assets:cash\n"
	require.NoError(t, ts.openDocument(uri, full))

	// Simulate a diverged rope: it reflects a shorter content than the
	// authoritative document.
	ts.docTexts.Store(uri, document.NewText("2024-01-01 one\n    expenses:a  $1\n    assets:cash\n"))

	// Incremental insert at line 4 (the second transaction) of the FULL document.
	require.NoError(t, ts.changeDocument(uri, []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangePartial{
		Range: protocol.Range{
			Start: protocol.Position{Line: 4, Character: 0},
			End:   protocol.Position{Line: 4, Character: 0},
		},
		Text: "    expenses:inserted  $9\n",
	}}))

	got, ok := ts.GetDocument(uri)
	require.True(t, ok)

	// The second transaction must survive, and the insert must land at line 4 —
	// not be clamped into the first transaction by the diverged shorter rope.
	assert.Contains(t, got, "2024-01-02 two", "second transaction must not be lost")
	assert.Contains(t, got, "2024-01-01 one\n    expenses:a  $1\n    assets:cash", "first transaction must stay intact")
	assert.Contains(t, got, "    expenses:inserted  $9\n2024-01-02 two", "insert must land before the second transaction")
}
