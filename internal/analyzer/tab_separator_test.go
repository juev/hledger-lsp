package analyzer

import (
	"os"
	"os/exec"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/parser"
)

// Each case was run through hledger 1.52.4 (`hledger -f - print`): a lone tab
// between account and amount makes the amount part of the account name, which
// leaves two postings without an amount, so hledger rejects the transaction.
func tabSeparatorCases() []hledgerBalanceCase {
	return []hledgerBalanceCase{
		{"lone tab before amount", "2024-01-01 a\n    e:x\t5 USD\n    a:y\n", false},
		{"lone tab, tab indentation", "2024-01-01 a\n\te:x\t5 USD\n\ta:y\n", false},
		{"lone tab on a virtual posting", "2024-01-01 a\n    [e:x]\t5 USD\n    a:y\n", false},
		{"lone tab, Cyrillic", "2024-01-01 a\n    расходы:еда\t5 ₽\n    активы:наличные\n", false},
		{"lone tab, CJK", "2024-01-01 a\n    支出:食品\t100 円\n    資産:現金\n", false},
		{"two tabs", "2024-01-01 a\n    e:x\t\t5 USD\n    a:y\n", true},
		{"tab then space", "2024-01-01 a\n    e:x\t 5 USD\n    a:y\n", true},
		{"space then tab", "2024-01-01 a\n    e:x \t5 USD\n    a:y\n", true},
		{"two spaces then tab", "2024-01-01 a\n    e:x  \t5 USD\n    a:y\n", true},
		{"lone tab after the amount", "2024-01-01 a\n    e:x  5 USD\t; c\n    a:y\n", true},
		{"lone tab with an amount on the other posting", "2024-01-01 a\n    e:x\t5 USD\n    a:y  -5 USD\n", true},
	}
}

func TestBalance_LoneTabIsNotAnAmountSeparator(t *testing.T) {
	for _, tc := range tabSeparatorCases() {
		t.Run(tc.name, func(t *testing.T) {
			journal, errs := parser.Parse(tc.input)
			require.Empty(t, errs)
			require.Len(t, journal.Transactions, 1)
			assert.Equal(t, tc.balanced, CheckBalance(&journal.Transactions[0], decimal.Zero).Balanced)
		})
	}
}

func TestOracle_TabSeparatorVerdictsMatchHledger(t *testing.T) {
	if os.Getenv("HLEDGER_ORACLE") == "" {
		t.Skip("set HLEDGER_ORACLE=1 to compare against the installed hledger binary")
	}
	hledgerPath, err := exec.LookPath("hledger")
	if err != nil {
		t.Skip("hledger is not installed")
	}

	for _, tc := range tabSeparatorCases() {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.balanced, hledgerAccepts(t, hledgerPath, tc.input), "the recorded verdict must be hledger's")
		})
	}
}
