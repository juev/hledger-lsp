package parser

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/testutil"
)

type hledgerPosting struct {
	Account   string `json:"paccount"`
	Comment   string `json:"pcomment"`
	Assertion any    `json:"pbalanceassertion"`
	Virtual   string `json:"ptype"`
}

// TestOracle_WhitespaceSeparatorMatchesHledger compares how the parser and
// hledger split a posting line into account, amount, comment and assertion for
// every pairing of whitespace characters between them.
func TestOracle_WhitespaceSeparatorMatchesHledger(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	separators := []string{" ", "\t", "  ", "\t\t", "\t ", " \t", "\t  ", "  \t", " \t "}
	// Every journal balances for hledger whatever the first posting turns into.
	lines := []string{
		"e:x%s5 USD",
		"e:x%s; note",
		"e:x%s= 5 USD",
		"(e:x)%s5 USD",
		"[e:x]%s5 USD",
		"(e:x)%s; note",
		"* e:x%s5 USD",
		"расходы:еда%s5 ₽",
		"支出:食品%s100 円",
		"e:x%sy",
		"e:x%sy%sz",
		"e:x  5 USD%s; note",
		"e:x  5 USD%s= 5 USD",
		"e:x  5 USD%s@ 2 EUR",
	}
	for _, line := range lines {
		for _, sep := range separators {
			body := strings.ReplaceAll(line, "%s", sep)
			for _, indent := range []string{"    ", "\t"} {
				input := "2024-01-01 a\n" + indent + body + "\n    a:y  -5 USD\n"
				t.Run(strings.NewReplacer("\t", `\t`, "\n", `\n`).Replace(indent+body), func(t *testing.T) {
					want, ok := hledgerPostings(t, hledgerPath, input)
					if !ok {
						t.Skip("hledger rejects this journal")
					}
					journal, errs := Parse(input)
					require.Empty(t, errs)
					require.Len(t, journal.Transactions, 1)
					got := journal.Transactions[0].Postings
					require.Len(t, got, len(want))
					for i, posting := range got {
						assert.Equal(t, want[i].Account, strings.TrimSpace(posting.Account.Name), "posting %d of:\n%s", i, input)
						assert.Equal(t, strings.TrimSpace(want[i].Comment), strings.TrimSpace(posting.Comment), "comment of posting %d of:\n%s", i, input)
						assert.Equal(t, want[i].Assertion != nil, posting.BalanceAssertion != nil, "assertion of posting %d of:\n%s", i, input)
					}
				})
			}
		}
	}
}

func hledgerPostings(t *testing.T, hledgerPath, input string) ([]hledgerPosting, bool) {
	t.Helper()
	cmd := exec.Command(hledgerPath, "-f", "-", "print", "-O", "json")
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	var transactions []struct {
		Postings []hledgerPosting `json:"tpostings"`
	}
	require.NoError(t, json.Unmarshal(out, &transactions))
	require.Len(t, transactions, 1)
	return transactions[0].Postings, true
}
