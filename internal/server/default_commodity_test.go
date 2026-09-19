package server

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// A bare amount under a D directive is that commodity, so hover names it even
// though the document writes no symbol.
func TestHover_BareAmountShowsDefaultCommodity(t *testing.T) {
	srv := NewServer()
	docURI := uri.URI("file:///test.journal")
	content := "D 1.000,00 RUB\n\n2024-01-15 test\n    expenses:food  50,00\n    assets:cash\n"
	srv.documents.Store(docURI, content)

	result, err := srv.Hover(context.Background(), &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 3, Character: 22},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, hoverContent(result), "50,00 RUB")
}

// The inferred-amount quick fix keeps the journal's bare style: the sibling
// amounts are RUB because of the D directive, but they are written without a
// symbol and so is the inserted amount.
func TestCodeAction_InsertInferredAmount_KeepsBareDefaultCommodity(t *testing.T) {
	ts := newTestServer()
	ts.cliClient = nil
	docURI := inferredTestURI
	content := "D 1.000,00 RUB\n\n2026-08-03 Good\n    Expenses:Test  -20,50\n    Expenses:Food\n"
	require.NoError(t, ts.openDocument(docURI, content))

	actions := inferredAmountActions(t, ts, 4)
	require.Len(t, actions, 1)
	assert.Equal(t, "Insert inferred amount (20,50)", actions[0].Title)

	edit := requireSingleEdit(t, actions[0])
	assert.Equal(t, "20,50", strings.TrimSpace(edit.NewText))
}

// The commodity can also reach the elided posting through a cost instead of a
// sibling amount, which is the case the sibling lookup misses.
func TestCodeAction_InsertInferredAmount_KeepsBareCommodityFromCost(t *testing.T) {
	ts := newTestServer()
	ts.cliClient = nil
	docURI := inferredTestURI
	content := "D 1.000,00 RUB\n\n2026-08-03 Buy\n    Assets:Stocks  10 AAPL @ 100,00\n    Assets:Cash\n"
	require.NoError(t, ts.openDocument(docURI, content))

	actions := inferredAmountActions(t, ts, 4)
	require.Len(t, actions, 1)
	assert.Equal(t, "Insert inferred amount (-1.000,00)", actions[0].Title,
		"the cost is RUB by the default commodity, and RUB is never spelled out here")

	edit := requireSingleEdit(t, actions[0])
	assert.NotContains(t, edit.NewText, "RUB")
	assert.Equal(t, "-1.000,00", strings.TrimSpace(edit.NewText))
}

// A bare amount has no commodity text, so it is not an occurrence of the
// declared commodity for document highlights.
func TestDocumentHighlight_BareAmountIsNotACommodityOccurrence(t *testing.T) {
	ts := newTestServer()
	docURI := inferredTestURI
	content := "D 1.000,00 RUB\ncommodity RUB\n\n2024-01-15 x\n    expenses:food  -50,00\n    assets:cash    50,00 RUB\n"
	require.NoError(t, ts.openDocument(docURI, content))

	highlights, err := ts.DocumentHighlight(context.Background(), &protocol.DocumentHighlightParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			// Inside the `commodity RUB` declaration line.
			Position: protocol.Position{Line: 1, Character: 12},
		},
	})
	require.NoError(t, err)

	var lines []uint32
	for _, h := range highlights {
		lines = append(lines, h.Range.Start.Line)
	}
	assert.Equal(t, []uint32{1, 5}, lines,
		"only the declaration and the explicitly written RUB amount are occurrences")
}

// A computed amount names the commodity its bare siblings carry by the default
// commodity directive, the same way hover does.
func TestInlayHint_BareAmountsReportDefaultCommodity(t *testing.T) {
	srv := NewServer()
	docURI := uri.URI("file:///test.journal")
	content := "D 1.000,00 RUB\n\n2024-01-15 groceries\n    expenses:food  50,00\n    assets:cash\n"
	srv.StoreDocument(docURI, content)

	hints, err := srv.InlayHint(context.Background(), &protocol.InlayHintParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
		Range:        protocol.Range{End: protocol.Position{Line: 10}},
	})
	require.NoError(t, err)
	require.Len(t, hints, 1)
	assert.Equal(t, protocol.String("= -50,00 RUB"), hints[0].Label)
}
