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
	assertRunningBalancesCoverRegisterRows(t, hledgerPath, journal)
}

// TestOracle_RunningBalancesCoverTheRowsOfAssertionCosts covers assignments whose
// assertion carries a cost: one that nets to nothing is left out of the register,
// one that leaves a priced part is listed.
func TestOracle_RunningBalancesCoverTheRowsOfAssertionCosts(t *testing.T) {
	if os.Getenv("HLEDGER_ORACLE") == "" {
		t.Skip("set HLEDGER_ORACLE=1 to compare against the installed hledger binary")
	}
	hledgerPath, err := exec.LookPath("hledger")
	if err != nil {
		t.Skip("hledger is not installed")
	}

	for name, journal := range assertionCostJournals {
		t.Run(name, func(t *testing.T) {
			assertRunningBalancesCoverRegisterRows(t, hledgerPath, journal)
		})
	}
}

// assertRunningBalancesCoverRegisterRows checks that a posting has a running
// balance exactly when `hledger reg` lists a row for it.
func assertRunningBalancesCoverRegisterRows(t *testing.T, hledgerPath, journal string) {
	t.Helper()

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

// assertionCostJournals are journals whose assignments carry a cost on the
// assertion. TestOracle_RunningBalancesMatchHledgerRegister compares their
// balances with the register, and the test above their listed rows.
var assertionCostJournals = map[string]string{
	"assertion cost is inferred in the cost commodity": `2024-01-01 t
    assets:cash  = 2 EUR @ 2 USD
    equity
`,
	"assertion cost restated moves nothing and is not listed": `2024-01-01 t
    assets:cash  = 2 EUR @ 2 USD
    equity

2024-01-02 t
    assets:cash  = 2 EUR @ 2 USD
    equity
`,
	"assertion total cost restated moves nothing": `2024-01-01 t
    assets:cash  = 2 EUR @@ 6 USD
    equity

2024-01-02 t
    assets:cash  == 2 EUR @@ 6 USD
    equity
`,
	"assertion cost with a negative quantity": `2024-01-01 t
    assets:cash  = -2 EUR @ 2 USD
    equity

2024-01-02 t
    assets:cash  = -2 EUR @@ 6 USD
    equity
`,
	"assertion cost of zero is not listed": `2023-12-31 seed
    other  1 USD
    equity

2024-01-01 t
    assets:cash  = 0 EUR @ 2 USD
    equity

2024-01-02 t
    assets:cash  = 0 EUR @ 2 USD
    equity
`,
	"assertion cost changes a plain balance": `2024-01-01 t
    assets:cash  1 EUR
    equity

2024-01-02 t
    assets:cash  = 3 EUR @ 2 USD
    equity
`,
	"assertion cost after a priced posting of an ordinary transaction": `2024-01-01 t
    assets:cash  1 EUR @ 5 USD
    equity

2024-01-02 t
    assets:cash  = 3 EUR @ 2 USD
    equity
`,
	"assertion cost over several commodities": `2024-01-01 t
    assets:cash  1 EUR
    assets:cash  1 USD
    equity

2024-01-02 t
    assets:cash  = 3 EUR @ 2 USD
    equity
`,
	"strict assertion cost clears the other commodities": `2024-01-01 t
    assets:cash  1 EUR
    assets:cash  1 USD
    equity

2024-01-02 t
    assets:cash  == 3 EUR @ 2 USD
    equity

2024-01-03 t
    assets:cash  == 1 USD @ 2 EUR
    equity
`,
	"inclusive assertion cost covers the subaccounts": `2024-01-01 t
    assets:cash:a  1 EUR
    assets:cash:b  2 EUR
    equity

2024-01-02 t
    assets:cash  =* 5 EUR @ 2 USD
    equity

2024-01-03 t
    assets:cash  ==* 5 EUR @@ 7 USD
    equity
`,
	"assertion cost replaced by another cost and then by none": `2024-01-01 t
    assets:cash  = 2 EUR @ 2 USD
    equity

2024-01-02 t
    assets:cash  = 3 EUR @ 2 USD
    equity

2024-01-03 t
    assets:cash  = 3 EUR @ 4 USD
    equity

2024-01-04 t
    assets:cash  = 1 EUR
    equity

2024-01-05 t
    assets:cash  = 2 EUR @ 3 USD
    equity
`,
	"assertion cost with the same total at another price": `2024-01-01 t
    assets:cash  = 2 EUR @ 3 USD
    equity

2024-01-02 t
    assets:cash  = 3 EUR @ 2 USD
    equity
`,
	"assertion cost beside a posting dated across it": `2024-01-05 t
    assets:cash  = 2 EUR @ 2 USD
    equity  ; date:2024-01-01

2024-01-03 t
    assets:cash  1 EUR  ; date:2024-01-09
    equity

2024-01-07 t
    assets:cash  = 2 EUR @ 2 USD
    equity
`,
	"assertion cost in a balanced virtual group": `2024-01-01 t
    [assets:cash]  = 2 EUR @ 2 USD
    [equity]

2024-01-02 t
    [assets:cash]  = 2 EUR @ 2 USD
    [equity]
`,
	"assertion cost on a parenthesised virtual posting": `2024-01-01 t
    (assets:cash)  = 2 EUR @ 2 USD

2024-01-02 t
    (assets:cash)  = 2 EUR @ 2 USD
`,
	"assertion cost on Cyrillic and CJK accounts": `2024-01-01 касса
    касса:наличные  = 2 EUR @ 2 USD
    капитал

2024-01-02 касса
    касса:наличные  = 2 EUR @ 2 USD
    капитал

2024-01-03 現金
    現金:口座  1 EUR
    現金:口座  1 USD
    資本

2024-01-04 現金
    現金:口座  == 3 EUR @@ 7 USD
    資本

2024-01-05 現金
    現金  ==* 3 EUR @@ 7 USD
    資本
`,
}
