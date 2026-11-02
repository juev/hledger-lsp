package analyzer

import (
	"encoding/csv"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOracle_RunningBalancesCoverTheRegisterRows checks which postings carry a
// running balance against the rows `hledger reg` prints. It is opt-in:
//
//	HLEDGER_ORACLE=1 go test ./internal/analyzer -run Oracle
func TestOracle_RunningBalancesCoverTheRegisterRows(t *testing.T) {
	if os.Getenv("HLEDGER_ORACLE") == "" {
		t.Skip("set HLEDGER_ORACLE=1 to compare against the installed hledger binary")
	}
	hledgerPath, err := exec.LookPath("hledger")
	if err != nil {
		t.Skip("hledger is not installed")
	}

	journal := `2024-01-01 seed
    assets:cash  100 USD
    equity:opening

2024-01-02 no-op assignment
    assets:cash  = 100 USD
    equity:opening

2024-01-03 written zero
    assets:cash  0 USD
    equity:opening

2024-01-04 zero that cancels
    assets:cash  5 USD
    assets:bank  -5 USD
    equity:opening

2024-01-05 no-op assignment in a second commodity
    assets:cash  = 100 USD
    assets:bank  = 0 EUR
    equity:opening

2024-01-06 moving assignment
    assets:cash  = 150 USD
    equity:opening

2024-01-07 касса
    活动:现金  = 0 ₽
    капитал

2024-01-10 lone elided in its group
    assets:cash  1 USD
    equity:opening
    [virtual:b]

2024-01-11 no-op assignment beside a lone elided virtual
    assets:cash  = 151 USD
    equity:opening
    [virtual:b]

2024-01-12 only an elided posting
    solo

2024-01-13 elided real posting with balanced virtuals
    other
    [virtual:a]  1 USD
    [virtual:c]  -1 USD

2024-01-14 assignment with cost
    assets:cash  = 151 USD @ 2 EUR
    equity:opening

2024-01-15 assignment with total cost
    assets:cash  = 151 USD @@ 5 EUR
    equity:opening
`
	cmd := exec.Command(hledgerPath, "-f", "-", "reg", "-O", "csv")
	cmd.Stdin = strings.NewReader(journal)
	out, err := cmd.Output()
	require.NoError(t, err)
	rows, err := csv.NewReader(strings.NewReader(string(out))).ReadAll()
	require.NoError(t, err)
	require.Greater(t, len(rows), 1)

	listed := map[[2]string]bool{}
	for _, row := range rows[1:] {
		listed[[2]string{row[0], strings.Trim(row[4], "[]()")}] = true
	}

	balances := primaryRunningBalances(t, journal)
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: journal})
	for _, source := range resolved.TransactionsWithSource() {
		for i, posting := range source.Transaction.Postings {
			_, has := balances[PostingRef{TransactionIndex: source.Index, PostingIndex: i}]
			key := [2]string{strconv.Itoa(source.Index + 1), posting.Account.GetResolvedName()}
			assert.Equal(t, listed[key], has, "transaction %d posting %s", source.Index+1, key[1])
		}
	}
}
