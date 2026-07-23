package server

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestSelectionRange_AccountSegment(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery
    expenses:food:groceries  $50
    assets:cash`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.SelectionRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Positions:    []protocol.Position{{Line: 1, Character: 15}}, // on "food" segment
	}

	result, err := srv.SelectionRange(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 1)

	// Innermost: "food" segment
	sel := result[0]
	assert.NotNil(t, sel.Parent, "should have parent (full account)")

	// Full account
	account := sel.Parent
	assert.NotNil(t, account.Parent, "should have parent (posting)")

	// Posting
	posting := account.Parent
	assert.NotNil(t, posting.Parent, "should have parent (transaction)")

	// Transaction
	tx := posting.Parent
	assert.NotNil(t, tx.Parent, "should have parent (document)")

	// Document (top)
	doc := tx.Parent
	assert.Nil(t, doc.Parent, "document should be topmost")

	// Verify containment: each parent strictly contains child
	assertContains(t, account.Range, sel.Range)
	assertContains(t, posting.Range, account.Range)
	assertContains(t, tx.Range, posting.Range)
	assertContains(t, doc.Range, tx.Range)
}

func TestSelectionRange_AmountWithCommodity(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery
    expenses:food  $50
    assets:cash`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.SelectionRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Positions:    []protocol.Position{{Line: 1, Character: 20}}, // on "$50"
	}

	result, err := srv.SelectionRange(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 1)

	// Should have parent chain
	sel := result[0]
	assert.NotNil(t, sel.Parent)
}

func TestSelectionRange_Date(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery
    expenses:food  $50
    assets:cash`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.SelectionRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Positions:    []protocol.Position{{Line: 0, Character: 5}}, // on date
	}

	result, err := srv.SelectionRange(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 1)

	// Date → transaction → document
	sel := result[0]
	assert.NotNil(t, sel.Parent, "should have parent (transaction)")
	assert.NotNil(t, sel.Parent.Parent, "should have parent (document)")
}

func TestSelectionRange_AccountDirective(t *testing.T) {
	srv := NewServer()
	content := `account expenses:food

2024-01-15 grocery
    expenses:food  $50
    assets:cash`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.SelectionRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Positions:    []protocol.Position{{Line: 0, Character: 12}}, // on account name in directive
	}

	result, err := srv.SelectionRange(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 1)

	sel := result[0]
	assert.NotNil(t, sel.Parent, "should have parent (directive)")
}

func TestSelectionRange_TopLevelComment(t *testing.T) {
	srv := NewServer()
	content := `; this is a comment
2024-01-15 grocery
    expenses:food  $50
    assets:cash`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.SelectionRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Positions:    []protocol.Position{{Line: 0, Character: 5}}, // on comment text
	}

	result, err := srv.SelectionRange(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 1)

	// Comment → document
	sel := result[0]
	assert.NotNil(t, sel.Parent, "should have parent (document)")
}

func TestSelectionRange_EmptyDocument(t *testing.T) {
	srv := NewServer()
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, "")

	params := &protocol.SelectionRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Positions:    []protocol.Position{{Line: 0, Character: 0}},
	}

	result, err := srv.SelectionRange(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 1)
}

func TestSelectionRange_MultiplePositions(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery
    expenses:food  $50
    assets:cash`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.SelectionRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Positions: []protocol.Position{
			{Line: 0, Character: 5},  // date
			{Line: 1, Character: 10}, // account
		},
	}

	result, err := srv.SelectionRange(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 2)
}

func TestSelectionRange_ParentChainContainment(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery
    expenses:food:groceries  $50
    assets:cash`

	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.SelectionRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Positions:    []protocol.Position{{Line: 1, Character: 15}},
	}

	result, err := srv.SelectionRange(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, result, 1)

	// Walk the parent chain and verify containment
	current := &result[0]
	for current.Parent != nil {
		parent := current.Parent
		assertContains(t, parent.Range, current.Range)
		current = parent
	}
}

func assertContains(t *testing.T, outer, inner protocol.Range) {
	t.Helper()

	outerBefore := outer.Start.Line < inner.Start.Line ||
		(outer.Start.Line == inner.Start.Line && outer.Start.Character <= inner.Start.Character)
	outerAfter := outer.End.Line > inner.End.Line ||
		(outer.End.Line == inner.End.Line && outer.End.Character >= inner.End.Character)

	if !outerBefore || !outerAfter {
		t.Errorf("outer range %v does not contain inner range %v", formatRange(outer), formatRange(inner))
	}
}

func formatRange(r protocol.Range) string {
	return fmt.Sprintf("[%d:%d-%d:%d]", r.Start.Line, r.Start.Character, r.End.Line, r.End.Character)
}
