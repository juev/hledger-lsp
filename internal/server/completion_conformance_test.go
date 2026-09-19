package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// complete stores content and asks for completions at a position, returning the
// item labels (nil when the server declines to offer completions at all).
func complete(t *testing.T, ts *testServer, docURI uri.URI, content string, line, character uint32) []string {
	t.Helper()

	ts.StoreDocument(docURI, content)
	list, err := ts.completion(docURI, line, character)
	require.NoError(t, err)
	if list == nil {
		return nil
	}
	return extractCompletionLabels(list.Items)
}

func TestCompletion_AccountAfterStatusMark(t *testing.T) {
	// hledger writes cleared postings as "* ACCOUNT AMOUNT". The marker must be
	// skipped when building the query, and the replaced range must not swallow it.
	ts := newTestServer()
	docURI := uri.URI("file:///status.journal")
	content := "2024-01-15 shop\n    expenses:food  $50.00\n    assets:cash\n\n2024-01-16 shop\n    * exp\n"

	labels := complete(t, ts, docURI, content, 5, 9)
	require.Equal(t, []string{"expenses:food"}, labels)

	ts.StoreDocument(docURI, content)
	list, err := ts.completion(docURI, 5, 9)
	require.NoError(t, err)
	require.Len(t, list.Items, 1)

	edit, ok := list.Items[0].TextEdit.(*protocol.TextEdit)
	require.True(t, ok, "completion items must replace only the account name")
	assert.Equal(t, uint32(6), edit.Range.Start.Character, "range starts after '* '")
	assert.Equal(t, uint32(9), edit.Range.End.Character)
}

func TestCompletion_AccountAfterVirtualBracket(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///virtual.journal")
	content := "2024-01-15 shop\n    expenses:food  $50.00\n    assets:cash\n\n2024-01-16 shop\n    (exp\n"

	labels := complete(t, ts, docURI, content, 5, 8)
	require.Equal(t, []string{"expenses:food"}, labels)
}

func TestCompletion_SubaccountAfterStatusMark(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///status-sub.journal")
	content := "2024-01-15 shop\n    expenses:food:lunch  $50.00\n    assets:cash\n\n2024-01-16 shop\n    * expenses:\n"

	labels := complete(t, ts, docURI, content, 5, 15)
	require.Contains(t, labels, "expenses:food:lunch")
}

func TestCompletion_CommodityAfterPriceAndAssertionMarkers(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///markers.journal")

	cases := []struct {
		name        string
		content     string
		line, char  uint32
		wantAtLeast bool
	}{
		{
			name:        "after @",
			content:     "commodity $\ncommodity EUR\n\n2024-01-15 shop\n    expenses:food  $50.00 @ $\n    assets:cash\n",
			line:        4,
			char:        29,
			wantAtLeast: true,
		},
		{
			name:        "after @@",
			content:     "commodity $\ncommodity EUR\n\n2024-01-15 shop\n    expenses:food  $50.00 @@ 100 \n    assets:cash\n",
			line:        4,
			char:        34,
			wantAtLeast: true,
		},
		{
			name:        "after =",
			content:     "commodity $\ncommodity EUR\n\n2024-01-15 shop\n    expenses:food  $50.00 = $\n    assets:cash\n",
			line:        4,
			char:        31,
			wantAtLeast: true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			labels := complete(t, ts, docURI, tt.content, tt.line, tt.char)
			require.NotEmpty(t, labels, "a commodity position must offer commodities")
			assert.Contains(t, labels, "$")
		})
	}
}

func TestCompletion_HeaderTriggerCharactersAreIgnored(t *testing.T) {
	// '=', ':' and '@' are legal inside a transaction header (secondary dates,
	// payee text, e-mail addresses) and must not open a completion list.
	ts := newTestServer()
	docURI := uri.URI("file:///header.journal")
	content := "commodity $\n\n2024-01-15 shop = 2024-01-20 | note\n    expenses:food  $50.00\n    assets:cash\n"

	for _, position := range []uint32{24, 25, 26} {
		list, err := func() (*protocol.CompletionList, error) {
			ts.StoreDocument(docURI, content)
			return ts.completion(docURI, 2, position)
		}()
		require.NoError(t, err)
		if list == nil {
			continue
		}
		for _, item := range list.Items {
			assert.NotEqual(t, "Commodity", completionDetailKind(item),
				"a header line must not offer commodities at position %d", position)
		}
	}
}

// completionDetailKind extracts the item kind word from a completion detail.
func completionDetailKind(item protocol.CompletionItem) string {
	detail := completionDetail(item)
	if detail == "" {
		return ""
	}
	for i := 0; i < len(detail); i++ {
		if detail[i] == ' ' {
			return detail[:i]
		}
	}
	return detail
}

func TestCompletion_DeclaredPayeeAndTagNames(t *testing.T) {
	// hledger's payee/tag directives declare valid names (check payees /
	// check tags), so they must be completable there and indexed for postings.
	ts := newTestServer()
	docURI := uri.URI("file:///directives.journal")
	content := "payee Whole Foods Market\ntag project\n\n2024-01-15 Whole Foods Market\n    expenses:food  $50.00\n    assets:cash\n"

	t.Run("payee directive", func(t *testing.T) {
		labels := complete(t, ts, docURI, content, 0, 12)
		assert.Contains(t, labels, "Whole Foods Market")
	})

	t.Run("tag directive", func(t *testing.T) {
		labels := complete(t, ts, docURI, content, 1, 4)
		assert.Contains(t, labels, "project")
	})
}

func TestCompletion_PriceDirectiveOffersCommodities(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///prices.journal")
	content := "P 2024-01-15 AAPL $1.00\nP 2024-01-16 AAPL €0.90\n\n2024-01-17 shop\n    expenses:food  $50.00\n    assets:cash\n"

	// The two commodities of the last P directive must be offered as the priced
	// commodity and as the price commodity.
	labels := complete(t, ts, docURI, content, 1, 18)
	assert.Contains(t, labels, "AAPL")

	labels = complete(t, ts, docURI, content, 1, 23)
	assert.Contains(t, labels, "€")
}

func TestCompletion_QuotedCommodityKeepsQuotes(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///quoted.journal")
	content := "commodity \"green apples\"\n\n2024-01-15 market\n    expenses:food  3 \"green apples\"\n    assets:cash\n\n2024-01-16 market\n    expenses:food  3 gr\n"

	ts.StoreDocument(docURI, content)
	list, err := ts.completion(docURI, 7, 21)
	require.NoError(t, err)
	require.NotNil(t, list)
	require.Len(t, list.Items, 1)

	item := list.Items[0]
	assert.Equal(t, "\"green apples\"", item.Label)
	edit, ok := item.TextEdit.(*protocol.TextEdit)
	require.True(t, ok)
	assert.Equal(t, "\"green apples\"", edit.NewText,
		"a commodity with spaces must be inserted quoted, or hledger cannot parse the line")
}

func TestCompletion_ApplyAccountInsertsRelativeName(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///apply.journal")
	content := "apply account business\n\n2024-01-15 shop\n    checking  $50.00\n    revenue\n\n2024-01-16 shop\n    ch\n"

	labels := complete(t, ts, docURI, content, 7, 6)
	require.Contains(t, labels, "business:checking")

	ts.StoreDocument(docURI, content)
	list, err := ts.completion(docURI, 7, 6)
	require.NoError(t, err)
	require.NotNil(t, list)

	var found bool
	for _, item := range list.Items {
		if item.Label != "business:checking" {
			continue
		}
		found = true
		edit, ok := item.TextEdit.(*protocol.TextEdit)
		require.True(t, ok)
		assert.Equal(t, "checking", edit.NewText,
			"inserting the resolved name would produce business:business:checking")
	}
	assert.True(t, found, "the account inside the apply block must be offered")
}

func TestCompletion_AccountScopeSettingIncludesZeroBalances(t *testing.T) {
	content := `account assets:closed
account assets:open

2024-01-15 shop
    assets:open  $50.00
    equity:opening

2024-02-15 close
    assets:closed  $10.00
    assets:open  $-10.00

2024-03-15 reopen
    assets:closed  $-10.00
    assets:open  $10.00

2024-04-15 query
    assets:
`

	t.Run("default hides net-zero accounts", func(t *testing.T) {
		ts := newTestServer()
		docURI := uri.URI("file:///scope-default.journal")
		labels := complete(t, ts, docURI, content, 16, 11)
		assert.NotContains(t, labels, "assets:closed",
			"a closed account with a zero balance stays hidden by default")
		assert.Contains(t, labels, "assets:open")
	})

	t.Run("accountScope all keeps them", func(t *testing.T) {
		ts := newTestServer()
		settings := ts.getSettings()
		settings.Completion.AccountScope = "all"
		ts.setSettings(settings)

		docURI := uri.URI("file:///scope-all.journal")
		labels := complete(t, ts, docURI, content, 16, 11)
		assert.Contains(t, labels, "assets:closed",
			"the opt-in scope keeps accounts whose balance is zero")
	})
}

func TestCompletion_ContextIsEmptyForDirectiveArguments(t *testing.T) {
	// '=' inside an email-like payee must not be treated as an assertion marker.
	ts := newTestServer()
	docURI := uri.URI("file:///payee-equals.journal")
	content := "2024-01-15 lunch with a=b@example.com\n    expenses:food  $10.00\n    assets:cash\n"

	list, err := func() (*protocol.CompletionList, error) {
		ts.StoreDocument(docURI, content)
		return ts.completion(docURI, 0, 30)
	}()
	require.NoError(t, err)
	if list == nil {
		return
	}
	for _, item := range list.Items {
		assert.NotEqual(t, "Commodity", completionDetailKind(item))
	}
}
