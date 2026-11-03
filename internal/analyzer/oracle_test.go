package analyzer

import (
	"encoding/csv"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/parser"
)

// TestOracle_JournalVerdictsMatchHledger compares the server's verdict with the
// installed hledger binary for the journals the table tests encode. It is opt-in
// because CI images may not ship hledger:
//
//	HLEDGER_ORACLE=1 go test ./internal/analyzer -run Oracle
func TestOracle_JournalVerdictsMatchHledger(t *testing.T) {
	if os.Getenv("HLEDGER_ORACLE") == "" {
		t.Skip("set HLEDGER_ORACLE=1 to compare against the installed hledger binary")
	}

	hledgerPath, err := exec.LookPath("hledger")
	if err != nil {
		t.Skip("hledger is not installed")
	}

	for _, tc := range hledgerVerifiedCases() {
		t.Run(tc.name, func(t *testing.T) {
			accepted := hledgerAccepts(t, hledgerPath, tc.input)

			journal, errs := parser.Parse(tc.input)
			require.Empty(t, errs)
			require.Len(t, journal.Transactions, 1)

			result := CheckBalance(&journal.Transactions[0], decimal.Zero)

			assert.Equal(t, accepted, result.Balanced,
				"our verdict must match hledger for:\n%s", tc.input)
		})
	}
}

func TestOracle_AssertionVerdictsMatchHledger(t *testing.T) {
	if os.Getenv("HLEDGER_ORACLE") == "" {
		t.Skip("set HLEDGER_ORACLE=1 to compare against the installed hledger binary")
	}

	hledgerPath, err := exec.LookPath("hledger")
	if err != nil {
		t.Skip("hledger is not installed")
	}

	cases := map[string]string{
		"failing assertion": `2024-01-01 x
    assets:cash  $-100 = $50
    expenses:food  $100
`,
		"passing assertion": `2024-01-01 x
    assets:cash  $100 = $100
    equity
`,
		"assertion-only posting": `2024-01-01 first
    b:bb  $50
    a:aa

2024-01-02 second
    b:bb  = $50
    a:aa  $0
`,
		"assertion sees only earlier postings": `2024-01-01 first
    assets:cash  $100
    equity

2024-01-02 second
    assets:cash  $10 = $110
    assets:cash  $-30
    expenses  $20
`,
		"backdated assertion": `2026-02-01 opening
    assets:bank  -699.41 CNY
    equity

2026-02-07 later
    assets:bank  -6130.28 CNY
    equity

2026-02-08 reconcile
    assets:bank  0 CNY = -699.41 CNY  ; date:2026-02-06
    equity  0 CNY
`,
		"future dated assertion fails": `2026-02-01 opening
    assets:bank  10 CNY
    equity

2026-02-04 deposit
    assets:bank  5 CNY
    equity

2026-02-02 reconcile
    assets:bank  0 CNY = 10 CNY  ; date:2026-02-05
    equity  0 CNY
`,
		"backdated posting contributes": `2026-02-01 opening
    assets:bank  10 CNY
    equity

2026-02-07 deposit
    assets:bank  5 CNY  ; date:2026-02-04
    equity

2026-02-05 reconcile
    assets:bank  0 CNY = 15 CNY
    equity  0 CNY
`,
		"backdated inferred amount contributes": `2026-02-01 opening
    assets:bank  10 CNY
    equity

2026-02-08 backdated
    assets:bank  5 CNY  ; date:2026-02-04
    equity  ; date:2026-02-04

2026-02-05 reconcile
    equity  0 CNY = -15 CNY
    assets:bank  0 CNY
`,
		"same posting date uses parse order": `2026-02-01 opening
    assets:bank  10 CNY
    equity

2026-02-08 reconcile
    assets:bank  0 CNY = 10 CNY  ; date:2026-02-06
    equity  0 CNY

2026-02-07 deposit
    assets:bank  5 CNY  ; date:2026-02-06
    equity
`,
		"bracketed posting date": `2026-02-01 opening
    assets:bank  10 CNY
    equity

2026-02-07 deposit
    assets:bank  5 CNY
    equity

2026-02-08 reconcile
    assets:bank  0 CNY = 10 CNY  ; [2026-02-06]
    equity  0 CNY
`,
		"one elided posting per group": `2024-01-01 x
    a  $10
    b
    [c]  $5
    [d]
`,
		// hledger reads a bare number as the commodity of the D directive, so
		// clopen-style journals keep balancing when activity is written bare
		// and the closing and opening entries assert symbol amounts.
		"bare amounts inherit the default commodity": `D 1.000,00 RUB
commodity RUB

2019-06-01 salary
    Активы:Наличные        1.755,00
    income:salary

2019-12-31 closing balances  ; clopen:
    Активы:Наличные       -1.755,00 RUB = 0,00 RUB
    equity:closing

2020-01-01 opening balances  ; clopen:
    Активы:Наличные        1.755,00 RUB = 1.755,00 RUB
    equity:opening

2020-01-02 groceries
    expenses:food           -200,00
    Активы:Наличные          200,00
`,
		"bare assertions inherit the default commodity": `D 1.000,00 RUB
commodity RUB

2024-01-01 opening
    assets:cash   1.000,00 = 1.000,00
    equity

2024-01-02 check
    assets:cash   = 1.000,00
    equity
`,
		// A bare number follows the D directive's number format even when the
		// commodity itself declares another style: `1.234` is 1234 here, which
		// is what makes the transaction balance.
		"bare numbers follow the default commodity number format": `D 1.000,00 RUB
commodity 1,000.00 RUB

2024-01-01 mix
    a:aa      1.234
    b:bb     -1,234.00 RUB
`,
		// The default commodity is not a wildcard: a non-zero assertion in
		// another commodity still fails in hledger.
		"description starting with a status marker keeps its postings": `2024-01-01 * *STAR
    a  -64 USD
    b

2024-01-02 check
    a  0 USD = -64 USD
    b  0 USD
`,
		"default commodity does not match another commodity": `D 1.000,00 RUB
commodity RUB

2024-01-01 opening
    assets:cash   1.000,00 = 1.000,00 USD
    equity
`,
	}
	for _, tc := range assignmentOrderCases {
		cases[tc.name] = tc.input
	}

	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			accepted := hledgerAccepts(t, hledgerPath, input)

			journal, errs := parser.Parse(input)
			require.Empty(t, errs)

			resolved := &include.ResolvedJournal{Primary: journal}
			diagnostics := CheckJournalBalance(resolved, decimal.Zero)

			assert.Equal(t, accepted, len(diagnostics) == 0,
				"our assertion verdict must match hledger for:\n%s\n got %v", input, diagnostics)
		})
	}
}

// hledgerAccepts reports whether the installed hledger loads the journal.
func hledgerAccepts(t *testing.T, hledgerPath, journal string) bool {
	t.Helper()

	cmd := exec.Command(hledgerPath, "-f", "-", "print")
	cmd.Stdin = strings.NewReader(journal)
	cmd.Stdout = &strings.Builder{}
	cmd.Stderr = &strings.Builder{}

	return cmd.Run() == nil
}

// runningBalanceOracleJournals are journals whose running balances are compared
// with the register of the installed hledger.
var runningBalanceOracleJournals = map[string]string{
	"assignment reads the cost kept by an earlier assignment transaction": `2024-01-03 t3
    b  -29 EUR @ 2 USD
    c  = -7 EUR
    d

2024-01-16 t1
    b  = 13 EUR
    x
`,
	"ordinary transaction posting loses its cost": `2024-01-03 t3
    b  -29 EUR @ 2 USD
    c

2024-01-16 t1
    b  = 13 EUR
    x
`,
	"total costs merge": `2024-01-03 t3
    b  -29 EUR @@ 58 USD
    c  = -7 EUR
    d

2024-01-04 t4
    b  -1 EUR @@ 3 USD
    c  = -8 EUR
    d

2024-01-16 t1
    b  = 13 EUR
    x
`,
	"inclusive assignment reads subaccount costs": `2024-01-03 t3
    a:x  -29 EUR @ 2 USD
    c  = -7 EUR
    d

2024-01-16 t1
    a  =* 13 EUR
    x
`,
	"strict assignment clears another commodity with its cost": `2024-01-03 t3
    a:x  -29 EUR @ 2 USD
    c  = -7 EUR
    d

2024-01-16 t1
    a:x  == 5 USD
    x
`,
	"same transaction assignment sees the earlier cost": `2024-01-03 t3
    b  -29 EUR @ 2 USD
    b  = 13 EUR
    d
`,
	"cost of a later ordinary posting is dropped": `2024-01-03 t3
    b  -29 EUR @ 2 USD
    c  = -7 EUR
    d

2024-01-04 t4
    b  -5 EUR
    d

2024-01-16 t1
    b  = 13 EUR
    x
`,
	"non-ASCII accounts": `2024-01-03 покупка
    現金:食  -29 EUR @ 2 USD
    банк  = -7 EUR
    капитал

2024-01-16 сверка
    現金:食  = 13 EUR
    расходы
`,
	"strict assignment keeps the cost of a commodity that nets to zero": `2024-01-01 t1
    b  -29 EUR @ 2 USD
    c  = -7 EUR
    d

2024-01-02 t2
    b  29 EUR
    e

2024-01-03 t3
    b  == 5 USD
    x
`,
	"inclusive assignment keeps the cost of a subaccount commodity that nets to zero": `2024-01-01 t1
    a:x  -29 EUR @ 2 USD
    c  = -7 EUR
    d

2024-01-02 t2
    a:x  29 EUR
    e

2024-01-03 t3
    a  =* 5 USD
    x
`,
	"strict assignment after two prices that cancel in quantity": `2024-01-01 t1
    b  -10 EUR @ 2 USD
    c  = -7 EUR
    d

2024-01-02 t2
    b  10 EUR @ 3 USD
    c  = -8 EUR
    d

2024-01-03 t3
    b  == 5 USD
    x
`,
	"non-ASCII: cost of a commodity that nets to zero": `2024-01-01 покупка
    現金:食  -29 EUR @ 2 USD
    банк  = -7 EUR
    капитал

2024-01-02 возврат
    現金:食  29 EUR
    расходы

2024-01-03 сверка
    現金:食  == 5 USD
    итог
`,
	"assignment that nets to zero but carries a cost is listed": `2024-01-09 t2
    d  -3 EUR @ 4 USD
    d  =* -3 EUR
    a:x
`,
	"reported journal": `alias q = a:x

2024-01-01 t0
    a:x  ==* 24 EUR
    b  =* -5 EUR
    q

2024-01-16 t1
    b  = 13 EUR
    банк  3 EUR
    a:x  18 EUR @ 3 USD  ; date:2024-01-04
    q

2024-01-05 t2
    a  29 EUR
    a

2024-01-03 t3
    a:x  -20 USD  ; date:2024-01-10
    b  -29 EUR @ 2 USD
    現金:食  ==* -7 EUR
    банк

2024-01-08 t4
    a:x  ==* 1 EUR
    現金:食
`,
}

func TestOracle_RunningBalancesMatchHledgerRegister(t *testing.T) {
	if os.Getenv("HLEDGER_ORACLE") == "" {
		t.Skip("set HLEDGER_ORACLE=1 to compare against the installed hledger binary")
	}

	hledgerPath, err := exec.LookPath("hledger")
	if err != nil {
		t.Skip("hledger is not installed")
	}

	for _, journals := range []map[string]string{runningBalanceOracleJournals, assertionCostJournals} {
		for name, input := range journals {
			t.Run(name, func(t *testing.T) {
				assertRunningBalancesMatchRegister(t, hledgerPath, input)
			})
		}
	}
}

// assertRunningBalancesMatchRegister compares every posting's running balance
// with the total column of `hledger reg -I`, which is the number the inlay hint
// is meant to show. The register totals across every posting it lists, so each
// account is queried on its own to get that account's balance.
func assertRunningBalancesMatchRegister(t *testing.T, hledgerPath, input string) {
	t.Helper()

	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: input})
	sourced := resolved.TransactionsWithSource()
	balances := RunningBalances(sourced, sourced[0].OccurrenceID)

	accounts := make(map[string]bool)
	for _, source := range sourced {
		for i := range source.Transaction.Postings {
			accounts[source.Transaction.Postings[i].Account.GetResolvedName()] = true
		}
	}

	for account := range accounts {
		cmd := exec.Command(hledgerPath, "-f", "-", "reg", "-I", "--output-format=csv", "acct:^"+regexp.QuoteMeta(account)+"$")
		cmd.Stdin = strings.NewReader(input)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		output, err := cmd.Output()
		require.NoError(t, err, "hledger: %s", stderr.String())

		rows, err := csv.NewReader(strings.NewReader(string(output))).ReadAll()
		require.NoError(t, err)

		// Rows of one account inside one transaction come out in register order,
		// which is posting-date order with parse order breaking ties.
		seen := make(map[string]int)
		for _, row := range rows[1:] {
			txn, err := strconv.Atoi(row[0])
			require.NoError(t, err)
			tx := &sourced[txn-1].Transaction

			var candidates []int
			for i := range tx.Postings {
				if tx.Postings[i].Account.GetResolvedName() == account {
					candidates = append(candidates, i)
				}
			}
			dates := registerDates(tx)
			sort.SliceStable(candidates, func(i, j int) bool {
				return compareDates(dates[candidates[i]], dates[candidates[j]]) < 0
			})
			require.Less(t, seen[row[0]], len(candidates), "register has more rows than postings for %s", account)
			posting := candidates[seen[row[0]]]
			seen[row[0]]++

			want := parseRegisterTotal(t, row[6])
			got := balances[PostingRef{TransactionIndex: sourced[txn-1].Index, PostingIndex: posting}]
			for commodity, quantity := range got {
				if !quantity.IsZero() {
					assert.True(t, want[commodity].Equal(quantity),
						"balance of %s in %s after transaction %d: ours %s, hledger %s\n%s", account, commodity, txn, quantity, want[commodity], input)
				}
			}
			for commodity, quantity := range want {
				assert.True(t, got[commodity].Equal(quantity),
					"balance of %s in %s after transaction %d: ours %s, hledger %s\n%s", account, commodity, txn, got[commodity], quantity, input)
			}
		}
	}
}

// parseRegisterTotal reads a register total such as "-2 EUR, -112 USD".
func parseRegisterTotal(t *testing.T, total string) map[string]decimal.Decimal {
	t.Helper()

	result := make(map[string]decimal.Decimal)
	if strings.TrimSpace(total) == "0" || strings.TrimSpace(total) == "" {
		return result
	}
	for _, part := range strings.Split(total, ", ") {
		fields := strings.Fields(part)
		require.Len(t, fields, 2, "total %q", total)
		quantity, err := decimal.NewFromString(fields[0])
		require.NoError(t, err)
		result[fields[1]] = quantity
	}
	return result
}
