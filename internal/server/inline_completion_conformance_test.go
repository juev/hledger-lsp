package server

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/uri"
)

// ghostTextFor asks for the ghost text offered at the start of a line.
func ghostTextFor(t *testing.T, ts *testServer, docURI uri.URI, line uint32) string {
	t.Helper()

	result, err := ts.InlineCompletion(context.Background(), inlineCompletionParams(docURI, line, 0))
	require.NoError(t, err)

	items := inlineCompletionItems(t, result)
	require.NotEmpty(t, items)
	return items[0].InsertText
}

func TestInlineCompletion_DeterministicBetweenEqualScoringTemplates(t *testing.T) {
	// "Amazon" and "Amazon Prime" score identically for the prefix "Amazo"; a map
	// iteration used to pick between them at random, so ghost text changed from
	// one keystroke to the next.
	ts := newTestServer()
	docURI := uri.URI("file:///tie.journal")
	content := "2024-01-10 Amazon\n    expenses:shopping  $20.00\n    assets:cash\n\n2024-01-11 Amazon Prime\n    expenses:digital  $9.00\n    assets:cash\n\n2024-01-15 Amazo\n"

	ts.StoreDocument(docURI, content)

	first := ghostTextFor(t, ts, docURI, 9)
	for i := 0; i < 20; i++ {
		assert.Equal(t, first, ghostTextFor(t, ts, docURI, 9), "iteration %d must repeat the same suggestion", i)
	}
}

func TestInlineCompletion_PreservesPostingDetail(t *testing.T) {
	// Ghost text must reproduce the posting the history contains: a dropped cost
	// changes the recorded basis, and a dropped assertion, status or comment
	// silently loses data.
	ts := newTestServer()
	docURI := uri.URI("file:///detail.journal")
	content := "2024-01-10 Broker\n    * assets:brokerage  10 AAPL @ $150.00 = 10 AAPL  ; trade:buy\n    * assets:cash  $-1500.00\n\n2024-01-15 Broker\n"

	ts.StoreDocument(docURI, content)

	ghost := ghostTextFor(t, ts, docURI, 5)
	lines := strings.Split(ghost, "\n")
	require.Len(t, lines, 2)

	assert.Contains(t, lines[0], "* assets:brokerage", "the status mark must survive")
	assert.Contains(t, lines[0], "@ $150.00", "the cost must survive, or the basis changes")
	assert.Contains(t, lines[0], "= 10 AAPL", "the balance assertion must survive")
	assert.Contains(t, lines[0], "; trade:buy", "the comment and its tag must survive")
	assert.Contains(t, lines[1], "* assets:cash")
	assert.Contains(t, lines[1], "$-1500.00")
}

func TestInlineCompletion_QuotedCommodityKeepsQuotes(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///quoted-ghost.journal")
	content := "2024-01-10 Market\n    expenses:food  3 \"green apples\"\n    assets:cash\n\n2024-01-15 Market\n"

	ts.StoreDocument(docURI, content)

	ghost := ghostTextFor(t, ts, docURI, 5)
	assert.Contains(t, ghost, "\"green apples\"",
		"an unquoted multi-word commodity is not valid hledger")
}

func TestCompletion_DetailCountIgnoresPayeeBoost(t *testing.T) {
	// The payee boost exists for ranking only: the usage count a user reads in the
	// popup must stay the real one.
	ts := newTestServer()
	docURI := uri.URI("file:///counts.journal")
	content := "2024-01-10 Grocery Store\n    expenses:food  $10.00\n    assets:cash\n\n2024-01-11 Grocery Store\n    expenses:food  $20.00\n    assets:cash\n\n2024-01-12 Grocery Store\n    exp\n"

	ts.StoreDocument(docURI, content)
	list, err := ts.completion(docURI, 9, 7)
	require.NoError(t, err)
	require.NotNil(t, list)
	require.NotEmpty(t, list.Items)

	found := false
	for _, item := range list.Items {
		if item.Label != "expenses:food" {
			continue
		}
		found = true
		assert.Contains(t, completionDetail(item), "(2)",
			"the popup shows the real usage count")
		assert.NotContains(t, completionDetail(item), "(202)",
			"the payee boost must not leak into the displayed count")
	}
	assert.True(t, found, "expenses:food must be offered")
}
