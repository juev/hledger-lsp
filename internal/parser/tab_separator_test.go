package parser

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/textutil"
)

// hledger ends an account name at two consecutive whitespace characters, where
// a tab counts as one. A lone tab, like a lone space, belongs to the name.
func TestParser_LoneTabBelongsToAccountName(t *testing.T) {
	cases := []struct {
		name        string
		line        string
		wantAccount string
	}{
		{"before amount", "e:x\t5 USD", "e:x 5 USD"},
		{"before comment", "e:x\t; c", "e:x ; c"},
		{"before assertion", "e:x\t= 5 USD", "e:x = 5 USD"},
		{"inside the name", "e:x\ty", "e:x y"},
		{"Cyrillic", "расходы:еда\t5 ₽", "расходы:еда 5 ₽"},
		{"CJK", "支出:食品\t100 円", "支出:食品 100 円"},
		{"virtual before amount", "(e:x)\t5 USD", "(e:x) 5 USD"},
		{"bracketed before amount", "[e:x]\t5 USD", "[e:x] 5 USD"},
	}
	for _, tc := range cases {
		for _, eol := range []string{"\n", "\r\n"} {
			t.Run(tc.name+map[string]string{"\n": "/LF", "\r\n": "/CRLF"}[eol], func(t *testing.T) {
				input := "2024-01-01 a" + eol + "    " + tc.line + eol + "    a:y  -5 USD" + eol
				journal, errs := Parse(textutil.NormalizeLineEndings(input))
				require.Empty(t, errs)
				require.Len(t, journal.Transactions, 1)
				postings := journal.Transactions[0].Postings
				require.Len(t, postings, 2)
				assert.Equal(t, tc.wantAccount, postings[0].Account.Name)
				assert.Nil(t, postings[0].Amount, "a lone tab is not a separator, so no amount is read")
				assert.Equal(t, "", postings[0].Comment)
			})
		}
	}
}

// Any two consecutive whitespace characters, tabs included, end the account.
func TestParser_WhitespacePairSeparatesAccountFromAmount(t *testing.T) {
	for name, sep := range map[string]string{
		"two spaces":     "  ",
		"two tabs":       "\t\t",
		"tab then space": "\t ",
		"space then tab": " \t",
		"tab, 2 spaces":  "\t  ",
		"2 spaces, tab":  "  \t",
		"three tabs":     "\t\t\t",
	} {
		t.Run(name, func(t *testing.T) {
			input := "2024-01-01 a\n    расходы:еда" + sep + "5 ₽\n    активы:наличные\n"
			journal, errs := Parse(input)
			require.Empty(t, errs)
			posting := journal.Transactions[0].Postings[0]
			assert.Equal(t, "расходы:еда", posting.Account.Name)
			require.NotNil(t, posting.Amount)
			assert.Equal(t, "5", posting.Amount.Quantity.String())
		})
	}
}

func TestParser_WhitespacePairBeforeCommentAndAssertion(t *testing.T) {
	for name, sep := range map[string]string{"tab+space": "\t ", "two tabs": "\t\t", "two spaces": "  "} {
		t.Run(name, func(t *testing.T) {
			journal, errs := Parse("2024-01-01 a\n    e:x" + sep + "; note\n    a:y  -5 USD\n")
			require.Empty(t, errs)
			posting := journal.Transactions[0].Postings[0]
			assert.Equal(t, "e:x", posting.Account.Name)
			assert.Equal(t, "note", strings.TrimSpace(posting.Comment))

			journal, errs = Parse("2024-01-01 a\n    e:x" + sep + "= 5 USD\n    a:y  -5 USD\n")
			require.Empty(t, errs)
			posting = journal.Transactions[0].Postings[0]
			assert.Equal(t, "e:x", posting.Account.Name)
			require.NotNil(t, posting.BalanceAssertion)
		})
	}
}

func TestParser_TabsAroundAccountDoNotChangeIt(t *testing.T) {
	t.Run("tab indentation", func(t *testing.T) {
		journal, errs := Parse("2024-01-01 a\n\te:x  5 USD\n\ta:y\t\n")
		require.Empty(t, errs)
		postings := journal.Transactions[0].Postings
		assert.Equal(t, "e:x", postings[0].Account.Name)
		assert.Equal(t, "a:y", postings[1].Account.Name, "a trailing tab is not part of the name")
	})
	t.Run("tab before amount-side comment", func(t *testing.T) {
		journal, errs := Parse("2024-01-01 a\n    e:x  5 USD\t; c\n    a:y\n")
		require.Empty(t, errs)
		posting := journal.Transactions[0].Postings[0]
		assert.Equal(t, "e:x", posting.Account.Name)
		assert.Equal(t, "c", strings.TrimSpace(posting.Comment))
	})
	t.Run("tab before price and assertion after the amount", func(t *testing.T) {
		journal, errs := Parse("2024-01-01 a\n    e:x  5 USD\t@ 2 EUR\t= 5 USD\n    a:y\n")
		require.Empty(t, errs)
		posting := journal.Transactions[0].Postings[0]
		require.NotNil(t, posting.Amount)
		assert.NotNil(t, posting.Cost)
		assert.NotNil(t, posting.BalanceAssertion)
	})
}

func TestParser_LoneTabInAutoAndPeriodicPostings(t *testing.T) {
	input := "= expenses\n    e:x\t0.1\n    a:y  -0.1\n\n~ monthly\n    q:x\t5 USD\n    q:y  -5 USD\n"
	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.AutoPostingRules, 1)
	assert.Equal(t, "e:x 0.1", journal.AutoPostingRules[0].Postings[0].Account.Name)
	require.Len(t, journal.PeriodicTransactions, 1)
	assert.Equal(t, "q:x 5 USD", journal.PeriodicTransactions[0].Postings[0].Account.Name)
}

func TestParser_AccountDirectiveWithLoneTab(t *testing.T) {
	journal, errs := Parse("account e:x\t; c\naccount a:b\tc\naccount q:w\t\t; c\n")
	require.Empty(t, errs)
	var names []string
	for _, directive := range journal.Directives {
		if account, ok := directive.(ast.AccountDirective); ok {
			names = append(names, account.Account.Name)
		}
	}
	assert.Equal(t, []string{"e:x ; c", "a:b c", "q:w"}, names)
}
