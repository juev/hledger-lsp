package formatter

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/testutil"
)

// hledger reads a lone tab as part of the account name. Formatting must keep
// that reading: it may not split the line into account and amount, and it must
// not change how hledger sees the journal.
var tabSeparatorJournals = map[string]string{
	"lone tab before amount":        "2024-01-01 a\n    e:x\t5 USD\n    a:y  -5 USD\n",
	"lone tab before comment":       "2024-01-01 a\n    e:x\t; c\n    a:y  -5 USD\n",
	"lone tab before assertion":     "2024-01-01 a\n    e:x\t= 5 USD\n    a:y  -5 USD\n",
	"two tabs":                      "2024-01-01 a\n    e:x\t\t5 USD\n    a:y\t\t-5 USD\n",
	"tab and space":                 "2024-01-01 a\n    e:x\t 5 USD\n    a:y \t-5 USD\n",
	"tab indentation":               "2024-01-01 a\n\te:x  5 USD\n\ta:y\n",
	"Cyrillic lone tab":             "2024-01-01 a\n    расходы:еда\t5 ₽\n    активы:наличные  -5 ₽\n",
	"CJK two tabs":                  "2024-01-01 a\n    支出:食品\t\t100 円\n    資産:現金\n",
	"virtual lone tab":              "2024-01-01 a\n    (e:x)\t5 USD\n    a:y  -5 USD\n",
	"lone tab, longer neighbouring": "2024-01-01 a\n    expenses:food:restaurants  5 USD\n    a:y\t-5 USD\n    b:z  0 USD\n",
}

func TestFormatter_LoneTabStaysPartOfTheAccountName(t *testing.T) {
	once := formatForOracle(t, "2024-01-01 a\n    e:x\t5 USD\n    a:y  -5 USD\n")
	assert.Contains(t, once, "    e:x 5 USD\n", "a lone tab is a single space inside the name, not a separator")
}

func TestFormatter_TabSeparatorJournalsAreIdempotent(t *testing.T) {
	for name, input := range tabSeparatorJournals {
		t.Run(name, func(t *testing.T) {
			once := formatForOracle(t, input)
			assert.Equal(t, once, formatForOracle(t, once), "format(format(x)) must equal format(x)")
		})
	}
}

func TestOracle_FormattingKeepsHledgerReadingOfTabs(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	for name, input := range tabSeparatorJournals {
		t.Run(name, func(t *testing.T) {
			before, beforeOK := hledgerPrint(t, hledgerPath, input)
			once := formatForOracle(t, input)
			after, afterOK := hledgerPrint(t, hledgerPath, once)

			assert.Equal(t, beforeOK, afterOK, "hledger's verdict must not change; formatted journal:\n%s", once)
			assert.Equal(t, before, after, "hledger must read the same journal; formatted journal:\n%s", once)
		})
	}
}

func hledgerPrint(t *testing.T, hledgerPath, journal string) (string, bool) {
	t.Helper()
	cmd := exec.Command(hledgerPath, "-f", "-", "print")
	cmd.Stdin = strings.NewReader(journal)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr)
		return "", false
	}
	return string(out), true
}
