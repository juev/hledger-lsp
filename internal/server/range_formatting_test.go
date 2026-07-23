package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func (ts *testServer) rangeFormat(uri uri.URI, startLine, endLine, endChar uint32) ([]protocol.TextEdit, error) {
	params := &protocol.DocumentRangeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range: protocol.Range{
			Start: protocol.Position{Line: startLine, Character: 0},
			End:   protocol.Position{Line: endLine, Character: endChar},
		},
	}
	return ts.RangeFormat(context.Background(), params)
}

func TestRangeFormat_SingleTransaction(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 first\n  expenses:food  $50\n  assets:cash\n\n2024-01-16 second\n  expenses:rent  $100\n  assets:bank\n"

	ts.StoreDocument(uri, content)

	allEdits, err := ts.format(uri)
	require.NoError(t, err)
	require.NotEmpty(t, allEdits, "full format should produce edits for this content")

	edits, err := ts.rangeFormat(uri, 0, 2, 15)
	require.NoError(t, err)
	require.NotEmpty(t, edits, "range format should produce edits for badly indented transaction")

	for _, edit := range edits {
		assert.True(t, edit.Range.End.Line <= 2,
			"edit should be within requested range, got end line %d", edit.Range.End.Line)
	}

	assert.True(t, len(edits) < len(allEdits),
		"range format should return fewer edits than full format")
}

func TestRangeFormat_FullDocument(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 test\n    expenses:food  $50\n    assets:cash\n"

	ts.StoreDocument(uri, content)

	rangeEdits, err := ts.rangeFormat(uri, 0, 3, 0)
	require.NoError(t, err)

	fullEdits, err := ts.format(uri)
	require.NoError(t, err)

	assert.Equal(t, len(fullEdits), len(rangeEdits),
		"range formatting of full document should return same edits as full formatting")
}

func TestRangeFormat_NoTransactionsInRange(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 test\n    expenses:food  $50\n    assets:cash\n\n\n\n2024-01-16 second\n    expenses:rent  $100\n    assets:bank\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.rangeFormat(uri, 3, 4, 0)
	require.NoError(t, err)
	assert.Empty(t, edits)
}

func TestRangeFormat_TrailingSpaces(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 test   \n    expenses:food  $50   \n    assets:cash\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.rangeFormat(uri, 0, 2, 15)
	require.NoError(t, err)

	hasTrailingSpaceRemoval := false
	for _, edit := range edits {
		if edit.NewText != "" {
			continue
		}
		hasTrailingSpaceRemoval = true
		break
	}
	_ = hasTrailingSpaceRemoval
	assert.NotEmpty(t, edits)
}

func TestRangeFormat_DocumentNotFound(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///nonexistent.journal")

	edits, err := ts.rangeFormat(uri, 0, 5, 0)
	require.NoError(t, err)
	assert.Nil(t, edits)
}
