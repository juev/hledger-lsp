package analyzer

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/parser"
)

type journalSource struct {
	path    string
	content string
}

// resolvedFromSources builds an occurrence-based resolved journal whose items
// follow the given textual order, which is how the loader presents a real
// include tree.
func resolvedFromSources(t *testing.T, sources ...journalSource) *include.ResolvedJournal {
	t.Helper()

	resolved := &include.ResolvedJournal{}
	for i, source := range sources {
		journal, errs := parser.Parse(source.content)
		require.Empty(t, errs, "source %s", source.path)

		id := include.OccurrenceID(i + 1)
		resolved.Occurrences = append(resolved.Occurrences, include.JournalOccurrence{
			ID:      id,
			Path:    source.path,
			Journal: journal,
		})
		for index := range journal.Transactions {
			resolved.Items = append(resolved.Items, include.ResolvedItem{
				OccurrenceID: id,
				Kind:         include.ResolvedItemTransaction,
				Index:        index,
			})
		}
	}
	if len(resolved.Occurrences) > 0 {
		resolved.Primary = resolved.Occurrences[0].Journal
	}
	return resolved
}

func codesOf(diagnostics []SourcedDiagnostic) []string {
	codes := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		codes = append(codes, diagnostic.Code)
	}
	return codes
}

func TestCheckJournalBalance_PassingAssertionIsSilent(t *testing.T) {
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-01-01 x
    assets:cash  $100 = $100
    equity`})

	assert.Empty(t, CheckJournalBalance(resolved, decimal.Zero))
}

func TestCheckJournalBalance_FailedAssertionReported(t *testing.T) {
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-01-01 x
    assets:cash  $-100 = $50
    expenses:food  $100`})

	diagnostics := CheckJournalBalance(resolved, decimal.Zero)
	require.Len(t, diagnostics, 1)

	diagnostic := diagnostics[0]
	assert.Equal(t, CodeBalanceAssertionFailed, diagnostic.Code)
	assert.Equal(t, SeverityError, diagnostic.Severity)
	assert.Equal(t, "/main.journal", diagnostic.Path)
	assert.Contains(t, diagnostic.Message, "balance assertion failed in assets:cash")
	assert.Contains(t, diagnostic.Message, "asserted 50 $")
	assert.Contains(t, diagnostic.Message, "calculated -100 $")
	assert.Contains(t, diagnostic.Message, "difference -150")

	// The range must point at the assertion itself, not at the transaction.
	assert.Equal(t, 2, diagnostic.Range.Start.Line)
	assert.Equal(t, "assets:cash", diagnostic.Data["account"])
	assert.Equal(t, "50", diagnostic.Data["expected"])
	assert.Equal(t, "-100", diagnostic.Data["actual"])
	assert.Equal(t, false, diagnostic.Data["strict"])
}

func TestCheckJournalBalance_PostingDateBeforeTransactionDate(t *testing.T) {
	// Issue juev/hledger-vscode#316: the February 6 assertion must not see the
	// February 7 card activity, even though its transaction is dated February 8.
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2026-02-01 opening
    负债:信用卡:中国银行:万事达卡3796  -699.41 CNY
    equity

2026-02-07 later
    负债:信用卡:中国银行:万事达卡3796  -6130.28 CNY
    equity

2026-02-08 reconcile
    负债:信用卡:中国银行:万事达卡3796  0 CNY = -699.41 CNY  ; date:2026-02-06
    equity  0 CNY
`})

	assert.Empty(t, CheckJournalBalance(resolved, decimal.Zero))
}

func TestCheckJournalBalance_PostingDateAfterTransactionDate(t *testing.T) {
	journal := `2026-02-01 opening
    assets:bank  10 CNY
    equity

2026-02-04 deposit
    assets:bank  5 CNY
    equity

2026-02-02 reconcile
    assets:bank  0 CNY = 10 CNY  ; date:2026-02-05
    equity  0 CNY
`
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: journal})

	diagnostics := CheckJournalBalance(resolved, decimal.Zero)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, CodeBalanceAssertionFailed, diagnostics[0].Code)
	assert.Equal(t, "15", diagnostics[0].Data["actual"])
}

func TestCheckJournalBalance_BackdatedPostingContributesBeforeAssertion(t *testing.T) {
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2026-02-01 opening
    assets:bank  10 CNY
    equity

2026-02-07 deposit
    assets:bank  5 CNY  ; date:2026-02-04
    equity

2026-02-05 reconcile
    assets:bank  0 CNY = 15 CNY
    equity  0 CNY
`})

	assert.Empty(t, CheckJournalBalance(resolved, decimal.Zero))
}

func TestCheckJournalBalance_BackdatedInferredAmountContributesBeforeAssertion(t *testing.T) {
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2026-02-01 opening
    assets:bank  10 CNY
    equity

2026-02-08 backdated
    assets:bank  5 CNY  ; date:2026-02-04
    equity  ; date:2026-02-04

2026-02-05 reconcile
    equity  0 CNY = -15 CNY
    assets:bank  0 CNY
`})

	assert.Empty(t, CheckJournalBalance(resolved, decimal.Zero))
}

func TestCheckJournalBalance_SamePostingDateUsesParseOrder(t *testing.T) {
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2026-02-01 opening
    assets:bank  10 CNY
    equity

2026-02-08 reconcile
    assets:bank  0 CNY = 10 CNY  ; date:2026-02-06
    equity  0 CNY

2026-02-07 deposit
    assets:bank  5 CNY  ; date:2026-02-06
    equity
`})

	assert.Empty(t, CheckJournalBalance(resolved, decimal.Zero))
}

func TestCheckJournalBalance_PostingDateSyntaxAndDate2(t *testing.T) {
	cases := []struct {
		name     string
		comment  string
		expected string
	}{
		{name: "yearless date tag", comment: "; date:2/6", expected: "10"},
		{name: "bracketed posting date", comment: "; [2026-02-06]", expected: "10"},
		{name: "secondary date does not change primary balance", comment: "; date2:2026-02-06", expected: "15"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := fmt.Sprintf(`2026-02-01 opening
    assets:bank  10 CNY
    equity

2026-02-07 deposit
    assets:bank  5 CNY
    equity

2026-02-08 reconcile
    assets:bank  0 CNY = %s CNY  %s
    equity  0 CNY
`, tc.expected, tc.comment)
			resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: content})
			assert.Empty(t, CheckJournalBalance(resolved, decimal.Zero))
		})
	}
}

func TestCheckJournalBalance_DateOrderBeatsFileOrder(t *testing.T) {
	// The assertion is written in the file that is included last, but dated
	// earlier than the transaction that funds the account. hledger evaluates in
	// date order, so the assertion holds and nothing is reported.
	sources := []journalSource{
		{path: "/february.journal", content: `2024-02-01 february
    a:aa  $10
    b:bb`},
		{path: "/january.journal", content: `2024-01-01 january
    a:aa  $5 = $5
    c:cc`},
	}

	assert.Empty(t, CheckJournalBalance(resolvedFromSources(t, sources...), decimal.Zero),
		"date order must decide which balance an assertion sees")

	// Swapping the dates makes the same tree order fail: the assertion now looks
	// at 15 while asserting 5.
	swapped := []journalSource{
		{path: "/january.journal", content: `2024-01-01 january
    a:aa  $10
    b:bb`},
		{path: "/february.journal", content: `2024-02-01 february
    a:aa  $5 = $5
    c:cc`},
	}

	diagnostics := CheckJournalBalance(resolvedFromSources(t, swapped...), decimal.Zero)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, CodeBalanceAssertionFailed, diagnostics[0].Code)
	assert.Equal(t, "/february.journal", diagnostics[0].Path)
	assert.Contains(t, diagnostics[0].Message, "calculated 15 $")
}

func TestCheckJournalBalance_StrictRejectsOtherCommoditiesHeld(t *testing.T) {
	// `==` requires the account to hold nothing else: hledger 1.52.4 reports
	// "Balance assertion failed ... Across all commodities ... the asserted
	// balance is: 0 EUR".
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-01-01 first
    a:aa  $10
    a:aa  5 EUR
    b:bb

2024-01-02 second
    a:aa  $5 == $15
    c:cc`})

	diagnostics := CheckJournalBalance(resolved, decimal.Zero)
	require.Len(t, diagnostics, 1)

	assert.Equal(t, CodeBalanceAssertionFailed, diagnostics[0].Code)
	assert.Contains(t, diagnostics[0].Message, "also holds")
	assert.Contains(t, diagnostics[0].Message, "EUR")
	assert.Equal(t, true, diagnostics[0].Data["strict"])
}

func TestCheckJournalBalance_InclusiveCoversSubaccounts(t *testing.T) {
	// `=*` counts subaccounts, so the parent assertion sees the child balance.
	inclusive := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-01-01 first
    a:aa  $10
    b:bb

2024-01-02 second
    a  $5 =* $15
    c:cc`})
	assert.Empty(t, CheckJournalBalance(inclusive, decimal.Zero),
		"=* must include subaccount balances")

	// The same journal with a plain `=` compares only the account itself.
	exclusive := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-01-01 first
    a:aa  $10
    b:bb

2024-01-02 second
    a  $5 = $15
    c:cc`})

	diagnostics := CheckJournalBalance(exclusive, decimal.Zero)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, CodeBalanceAssertionFailed, diagnostics[0].Code)
	assert.Contains(t, diagnostics[0].Message, "calculated 5 $")
	assert.Equal(t, false, diagnostics[0].Data["inclusive"])
}

func TestCheckJournalBalance_VirtualPostingsCounted(t *testing.T) {
	tests := map[string]string{
		"parenthesised": `2024-01-01 x
    assets:cash  $10
    equity
    (memo:food)  $5

2024-01-02 y
    (memo:food)  $1 = $6
    equity`,
		"bracketed": `2024-01-01 x
    assets:cash  $10
    equity
    [memo:food]  $5
    [equity:v]  $-5

2024-01-02 y
    [memo:food]  $1 = $6
    [equity:v]  $-1`,
	}

	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: content})
			assert.Empty(t, CheckJournalBalance(resolved, decimal.Zero),
				"virtual postings count towards assertion balances; the assertion is satisfied only if they do")
		})
	}
}

func TestCheckJournalBalance_AssertionOnlyPostingInfersAmount(t *testing.T) {
	// hledger infers the amount that satisfies the assertion, so a posting that
	// only asserts a balance neither fails nor unbalances the transaction.
	// Verified: `hledger -f - print` exits 0 for this journal.
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-01-01 first
    b:bb  $50
    a:aa

2024-01-02 second
    b:bb  = $50
    a:aa  $0`})

	assert.Empty(t, CheckJournalBalance(resolved, decimal.Zero))
}

func TestCheckJournalBalance_AssertionOnlyPostingUnbalancesTransaction(t *testing.T) {
	// With nothing to absorb the inferred amount, hledger reports the posting's
	// inferred -100 as the transaction residual.
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-01-01 x
    b:bb  = $-100`})

	diagnostics := CheckJournalBalance(resolved, decimal.Zero)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, CodeUnbalanced, diagnostics[0].Code)
	assert.Contains(t, diagnostics[0].Message, "$ off by -100")
}

func TestCheckJournalBalance_MultipleInferredOneDiagnosticPerPosting(t *testing.T) {
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-01-01 x
    expenses:food  $100
    assets:cash
    assets:bank`})

	diagnostics := CheckJournalBalance(resolved, decimal.Zero)
	require.Len(t, diagnostics, 2, "one diagnostic per amount-less posting")

	for _, diagnostic := range diagnostics {
		assert.Equal(t, CodeMultipleInferred, diagnostic.Code)
		assert.Equal(t, SeverityError, diagnostic.Severity)
		assert.Contains(t, diagnostic.Message, "two or more spaces")
	}
	assert.Equal(t, "assets:cash", diagnostics[0].Data["account"])
	assert.Equal(t, "assets:bank", diagnostics[1].Data["account"])
	assert.NotEqual(t, diagnostics[0].Range.Start.Line, diagnostics[1].Range.Start.Line,
		"each diagnostic points at its own posting line")
}

func TestCheckJournalBalance_UnbalancedMessageIsSortedAndSigned(t *testing.T) {
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-01-01 x
    a:aa  $50
    a:aa  5 EUR
    a:aa  3 GBP
    b:bb  $-40
    b:bb  -11 EUR
    b:bb  -1 GBP`})

	diagnostics := CheckJournalBalance(resolved, decimal.Zero)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, CodeUnbalanced, diagnostics[0].Code)
	assert.Contains(t, diagnostics[0].Message, "$ off by 10")
	assert.Contains(t, diagnostics[0].Message, "EUR off by -6")
	assert.Contains(t, diagnostics[0].Message, "GBP off by 2")
}

func TestCheckJournalBalance_CommodityLessResidualIsLabelled(t *testing.T) {
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-01-01 x
    a:aa  50
    b:bb  -40`})

	diagnostics := CheckJournalBalance(resolved, decimal.Zero)
	require.Len(t, diagnostics, 1)
	assert.Contains(t, diagnostics[0].Message, "(no commodity) off by 10")
}

func TestCheckJournalBalance_ConversionRuleApplies(t *testing.T) {
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-01-15 convert
    assets:bank:eur  100 EUR
    assets:bank:usd  $-110`})

	assert.Empty(t, CheckJournalBalance(resolved, decimal.Zero),
		"a cost-free two-commodity conversion is accepted by hledger")
}

func TestCheckJournalBalance_IncludeTreeContributionsAndPaths(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.journal")
	childPath := filepath.Join(dir, "child.journal")

	require.NoError(t, os.WriteFile(childPath, []byte(`2024-01-02 child
    a:aa  $5 = $99
    c:cc
`), 0o600))
	mainContent := "include child.journal\n\n2024-01-01 root\n    a:aa  $10\n    b:bb\n"
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o600))

	loader := include.NewLoader()
	resolved, loadErrors := loader.LoadFromContent(mainPath, mainContent)
	require.Empty(t, loadErrors)
	require.NotNil(t, resolved)

	diagnostics := CheckJournalBalance(resolved, decimal.Zero)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, CodeBalanceAssertionFailed, diagnostics[0].Code)
	assert.Equal(t, childPath, diagnostics[0].Path,
		"the assertion lives in the included file, so the diagnostic must be attributed there")
	assert.Contains(t, diagnostics[0].Message, "calculated 15 $")
}

func TestCheckJournalBalance_LegacyProjectionPaths(t *testing.T) {
	primary, errs := parser.Parse(`2024-01-01 root
    a:aa  $10
    b:bb`)
	require.Empty(t, errs)
	included, errs2 := parser.Parse(`2024-01-02 child
    a:aa  $5 = $99
    c:cc`)
	require.Empty(t, errs2)

	resolved := &include.ResolvedJournal{
		Primary:   primary,
		Files:     map[string]*ast.Journal{"/child.journal": included},
		FileOrder: []string{"/child.journal"},
	}

	diagnostics := CheckJournalBalance(resolved, decimal.Zero)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, "/child.journal", diagnostics[0].Path,
		"legacy projection keeps the included file path and leaves the root path empty")
}

func TestTransactionsWithSource_OccurrenceOrder(t *testing.T) {
	resolved := resolvedFromSources(t,
		journalSource{path: "/a.journal", content: "2024-01-02 a\n    x:xx  $1\n    y:yy"},
		journalSource{path: "/b.journal", content: "2024-01-01 b\n    x:xx  $2\n    y:yy"},
	)

	sourced := resolved.TransactionsWithSource()
	require.Len(t, sourced, 2)
	assert.Equal(t, "/a.journal", sourced[0].Path)
	assert.Equal(t, "/b.journal", sourced[1].Path)
	assert.Equal(t, "a", sourced[0].Transaction.Description)
	assert.Equal(t, "b", sourced[1].Transaction.Description)
	assert.Equal(t, include.OccurrenceID(1), sourced[0].OccurrenceID)
}

func TestTransactionsWithSource_ExcludesPeriodicAndAutoRules(t *testing.T) {
	journal, errs := parser.Parse(`~ monthly
    expenses:food  $100
    assets:cash

= expenses:food
    (budget:food)  $100

2024-01-01 x
    expenses:food  $10
    assets:cash
`)
	require.Empty(t, errs)
	require.Len(t, journal.PeriodicTransactions, 1)
	require.Len(t, journal.AutoPostingRules, 1)

	resolved := &include.ResolvedJournal{Primary: journal}
	sourced := resolved.TransactionsWithSource()
	require.Len(t, sourced, 1)
	assert.Equal(t, "x", sourced[0].Transaction.Description)
}

func TestCheckJournalBalance_NoDiagnosticsForBalancedJournal(t *testing.T) {
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-01-15 grocery
    expenses:food  $50
    assets:cash

2024-01-16 buy
    assets:stocks  10 AAPL @ $15
    assets:cash  $-150

2024-01-17 sell
    assets:stocks  -10 AAPL @ $16
    assets:cash  $160`})

	assert.Empty(t, CheckJournalBalance(resolved, decimal.Zero))
	assert.Empty(t, codesOf(CheckJournalBalance(resolved, decimal.Zero)))
}

func TestCheckJournalBalance_AssertionCheckedAtItsPosting(t *testing.T) {
	// hledger checks a balance assertion when the posting is processed: postings
	// written after the assertion must not change its verdict. Verified: this
	// journal exits 0 with hledger 1.52.4.
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-01-01 first
    assets:cash  $100
    equity

2024-01-02 second
    assets:cash  $10 = $110
    assets:cash  $-30
    expenses  $20
`})

	assert.Empty(t, CheckJournalBalance(resolved, decimal.Zero))

	// The mirror case: the asserted balance is wrong at the posting, even though
	// the end-of-transaction balance would match.
	broken := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-01-01 first
    assets:cash  $100
    equity

2024-01-02 second
    assets:cash  $-30 = $110
    assets:cash  $40
    income  $-10
`})

	diagnostics := CheckJournalBalance(broken, decimal.Zero)
	require.Len(t, diagnostics, 1, "the assertion fails at its own posting")
	assert.Equal(t, CodeBalanceAssertionFailed, diagnostics[0].Code)
	assert.Contains(t, diagnostics[0].Message, "calculated 70 $")
}

func TestCheckJournalBalance_OneElidedPostingPerGroup(t *testing.T) {
	// hledger infers one amount per balancing group and never infers a
	// parenthesised virtual posting. Verified: both journals exit 0.
	tests := map[string]string{
		"real and balanced virtual": `2024-01-01 x
    a  $10
    b
    [c]  $5
    [d]
`,
		"real and unbalanced virtual": `2024-01-01 x
    a  $10
    b
    (c)
`,
	}

	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: content})
			assert.Empty(t, CheckJournalBalance(resolved, decimal.Zero))
		})
	}
}

func TestCheckJournalBalance_StrictRejectsSmallOtherCommodity(t *testing.T) {
	// `==` compares every other commodity with its own precision: 0.20 EUR still
	// fails a dollar assertion (hledger reports "Across all commodities ...").
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-01-01 first
    assets:cash  $10
    assets:cash  0.20 EUR
    equity

2024-01-02 second
    assets:cash  $5 == $15
    equity
`})

	diagnostics := CheckJournalBalance(resolved, decimal.Zero)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, CodeBalanceAssertionFailed, diagnostics[0].Code)
	assert.Contains(t, diagnostics[0].Message, "also holds")
	assert.Contains(t, diagnostics[0].Message, "EUR")
}

func TestCheckJournalBalance_ResidualEqualToToleranceIsBalanced(t *testing.T) {
	// hledger accepts a residual exactly equal to the tolerance: with `$`
	// precision 0 the tolerance is 0.5 and 10 AAPL @ $2.05 plus $-20 balances.
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-01-01 x
    a:aa  10 AAPL @ $2.05
    b:bb  $-20
`})

	assert.Empty(t, CheckJournalBalance(resolved, decimal.Zero))

	beyond := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-01-01 x
    a:aa  10 AAPL @ $2.5
    b:bb  $-20
`})
	assert.Len(t, CheckJournalBalance(beyond, decimal.Zero), 1, "5.00 exceeds the 0.5 tolerance")
}

func TestCheckJournalBalance_DefaultCommodityMakesBareActivityMatchAssertions(t *testing.T) {
	// The clopen workflow the user journals follow: a year file records activity
	// as bare numbers while the closing and opening entries assert RUB amounts.
	// hledger reads every bare number as RUB because of the D directive, so the
	// assertions hold.
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `D 1.000,00 RUB
commodity RUB

2019-06-01 salary
    Активы:Наличные        1.755,00
    income:salary

2019-12-31 closing balances  ; clopen:
    Активы:Наличные      -1.755,00 RUB = 0,00 RUB
    equity:closing

2020-01-01 opening balances  ; clopen:
    Активы:Наличные       1.755,00 RUB = 1.755,00 RUB
    equity:opening

2020-01-02 groceries
    expenses:food          -200,00
    Активы:Наличные         200,00
`})

	assert.Empty(t, CheckJournalBalance(resolved, decimal.Zero),
		"bare amounts must be checked as the default commodity, got %v", codesOf(CheckJournalBalance(resolved, decimal.Zero)))
}

func TestCheckJournalBalance_DefaultCommodityStillRejectsOtherCommodities(t *testing.T) {
	// Control: inheriting the default commodity must not make every commodity
	// equal to every other one. hledger rejects this journal.
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `D 1.000,00 RUB

2024-01-01 opening
    assets:cash   1.000,00 = 1.000,00 USD
    equity
`})

	diagnostics := CheckJournalBalance(resolved, decimal.Zero)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, CodeBalanceAssertionFailed, diagnostics[0].Code)
	assert.Contains(t, diagnostics[0].Message, "balance assertion failed in assets:cash")
}

func TestCheckJournalBalance_DefaultCommodityAppliesToBareAssertions(t *testing.T) {
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `D 1.000,00 RUB

2024-01-01 opening
    assets:cash   1.000,00 = 1.000,00
    equity

2024-01-02 spend
    assets:cash   -200,00
    expenses       200,00

2024-01-03 check
    assets:cash   = 800,00
    equity
`})

	assert.Empty(t, CheckJournalBalance(resolved, decimal.Zero))
}

func TestCheckJournalBalance_StatusMarkerDescriptionKeepsPostings(t *testing.T) {
	resolved := resolvedFromSources(t, journalSource{path: "main.journal", content: `2024-01-01 * *STAR
    a  -64 USD
    b

2024-01-02 check
    a  0 USD = -64 USD
    b  0 USD
`})

	assert.Empty(t, CheckJournalBalance(resolved, decimal.Zero))

	tx := resolved.Primary.Transactions[0]
	assert.Equal(t, ast.StatusCleared, tx.Status)
	assert.Equal(t, "*STAR", tx.Description)
	assert.Len(t, tx.Postings, 2)
}

// assignmentOrderCases cover balance assignments crossed by custom posting dates.
// wantMessages lists one fragment per expected diagnostic; none means hledger
// accepts the journal. TestOracle_AssertionVerdictsMatchHledger replays them
// against the hledger binary.
var assignmentOrderCases = []struct {
	name         string
	input        string
	wantMessages []string
}{
	{
		name: "strict assignment mixed amount blocks the inferred conversion price",
		input: `2024-01-01 t1
    bank  -9 EUR
    b

2024-01-17 t0
    bank  == -3 USD
    a  1 EUR
    bank  = -4 EUR
`,
		wantMessages: []string{"EUR off by 6; USD off by -3"},
	},
	{
		name: "strict assignment mixed amount with a second posting in its commodity",
		input: `2024-01-01 t1
    bank  -9 EUR
    b

2024-01-17 t0
    bank  == -3 USD
    a  -6 EUR
`,
		wantMessages: []string{"EUR off by 3; USD off by -3"},
	},
	{
		name: "strict assignment mixed amount balanced by plain postings",
		input: `2024-01-01 t1
    bank  -9 EUR
    b

2024-01-17 t0
    bank  == -3 USD
    a  -9 EUR
    c  3 USD
`,
	},
	{
		name: "mixed postings that net to zero in the priced commodity keep the conversion",
		input: `2024-01-01 open
    bank  2 EUR
    equity

2024-02-02 t
    cash  -4 USD
    bank  == -1 USD
    bank  == 9 EUR
`,
	},
	{
		name: "mixed postings of opposite sign cancel in the priced commodity",
		input: `2024-01-01 open
    wallet  5 EUR
    equity

2024-01-02 open
    broker  -5 EUR
    equity

2024-02-01 t
    shop     10 USD
    wallet   == 3 USD
    broker   == -3 USD
    cash     -8 EUR
`,
	},
	{
		name: "Cyrillic and CJK accounts: mixed postings cancel in the priced commodity",
		input: `2024-01-01 открытие
    кошелёк  5 EUR
    капитал

2024-01-02 открытие
    брокер  -5 EUR
    капитал

2024-02-01 покупка
    店  10 USD
    кошелёк  == 3 USD
    брокер  == -3 USD
    現金  -8 EUR
`,
	},
	{
		name: "non-strict assignments keep the inferred conversion price",
		input: `2024-01-01 t1
    bank  -9 EUR
    b

2024-01-17 t0
    bank  = -3 USD
    a  1 EUR
    bank  = -4 EUR
`,
	},
	{
		name: "Cyrillic and CJK accounts: strict assignment mixed amount is unbalanced",
		input: `2024-01-01 открытие
    банк  -9 EUR
    капитал

2024-01-17 сверка
    банк  == -3 USD
    расходы:食  1 EUR
    банк  = -4 EUR
`,
		wantMessages: []string{"EUR off by 6; USD off by -3"},
	},
	{
		name: "later dated posting crosses an assignment",
		input: `2024-12-31 * charge
    liabilities:card  -10 USD  ; date:2025-01-02
    expenses:x

2025-01-01 * opening
    liabilities:card  =0 USD
    equity:o

2025-01-31 * check
    liabilities:card  -10 USD = -20 USD
    expenses:x
`,
	},
	{
		name: "earlier dated posting crosses an assignment",
		input: `2025-01-01 opening
    liabilities:card  =0 USD
    equity:o

2025-01-05 charge
    liabilities:card  -10 USD  ; date:2024-12-30
    expenses:x

2025-01-31 check
    liabilities:card  -5 USD = -5 USD
    expenses:x
`,
	},
	{
		name: "bracketed date crosses an assignment",
		input: `2024-12-31 charge
    card  -10 USD  ; [2025-01-02]
    expenses:x

2025-01-01 opening
    card  =0 USD
    equity:o

2025-01-31 check
    card  0 USD = -10 USD
    expenses:x  0 USD
`,
	},
	{
		name: "cyrillic and CJK accounts",
		input: `2024-12-31 покупка
    долг:カード  -10 USD  ; date:2025-01-02
    расходы

2025-01-01 открытие
    долг:カード  =0 USD
    капитал

2025-01-31 проверка
    долг:カード  -10 USD = -20 USD
    расходы
`,
	},
	{
		name: "genuine failure after a crossing posting",
		input: `2024-12-31 charge
    card  -10 USD  ; date:2025-01-02
    expenses:x

2025-01-01 opening
    card  =0 USD
    equity:o

2025-01-31 check
    card  -10 USD = -25 USD
    expenses:x
`,
		wantMessages: []string{"asserted -25 USD, calculated -20 USD (difference 5)"},
	},
	{
		name: "date2 does not move a posting across an assignment",
		input: `2024-12-31 charge
    card  -10 USD  ; date2:2025-01-02
    expenses:x

2025-01-01 opening
    card  =0 USD
    equity:o

2025-01-31 check
    card  -10 USD = -10 USD
    expenses:x
`,
	},
	{
		name: "same date ties use parse order",
		input: `2025-01-05 assign
    a  =1 USD
    e

2025-01-01 dated
    a  10 USD  ; date:2025-01-05
    e

2025-01-31 check
    a  0 USD = 11 USD
    e  0 USD
`,
	},
	{
		name: "transaction partly applied when an assignment runs",
		input: `2025-01-01 split
    a  10 USD
    b  -10 USD  ; date:2025-01-10

2025-01-05 assign a
    a  =1 USD
    c

2025-01-06 assign b
    b  =2 USD
    c

2025-01-31 check
    a  0 USD = 1 USD
    b  0 USD = -8 USD
    c  0 USD
`,
	},
	{
		name: "elided posting of a plain transaction keeps its own date",
		input: `2025-01-10 plain
    b  5 USD
    a  ; date:2025-01-20

2025-01-15 assign
    a  =0 USD
    e

2025-01-31 check
    a  0 USD = -5 USD
    e  0 USD
`,
	},
	{
		name: "written posting of an assignment transaction ignores its date",
		input: `2025-01-01 seed
    a  100 USD
    e

2025-01-10 assign
    b  =5 USD
    a  1 USD  ; date:2025-01-20
    e

2025-01-15 check
    a  0 USD = 101 USD
    e  0 USD
`,
	},
	{
		name: "elided posting of an assignment transaction ignores its date",
		input: `2025-01-10 assign
    b  =5 USD
    a  ; date:2025-01-20

2025-01-15 check
    a  0 USD = -5 USD
    e  0 USD
`,
	},
	{
		name: "elided amount feeds a later assignment",
		input: `2024-12-31 charge
    a  -10 USD  ; date:2025-01-02
    e

2025-01-01 assign
    a  =5 USD
    b

2025-01-02 assign elided account
    b  =-3 USD
    c

2025-01-03 check
    c  0 USD = -2 USD
    a  0 USD = -5 USD
    e  0 USD
`,
	},
	{
		name: "assignments alone balance at the replayed balance",
		input: `2025-01-01 seed
    a  10 USD  ; date:2025-01-05
    e

2025-01-02 pair
    a  =5 USD
    b  =-5 USD

2025-01-31 check
    a  0 USD = 15 USD
    b  0 USD
`,
	},
	{
		name: "assignments alone stay unbalanced at the replayed balance",
		input: `2025-01-01 seed
    a  10 USD  ; date:2025-01-05
    e

2025-01-02 pair
    a  =5 USD
    b  =5 USD
`,
		wantMessages: []string{"USD off by 10"},
	},
	{
		name: "two assignments in different commodities",
		input: `2025-01-01 seed
    a  1 USD
    a  1 EUR
    e

2025-01-02 assign
    a  =5 USD
    a  =6 EUR
    e

2025-01-03 check
    e  0 USD = -5 USD
    e  0 EUR = -6 EUR
    a  0 USD
`,
	},
	{
		name: "assignments on an account and its subaccount",
		input: `2024-12-31 charge
    a:b  3 USD  ; date:2025-01-02
    e

2025-01-01 assign
    a  =1 USD
    a:b  =2 USD
    e

2025-01-31 check
    a  0 USD = 1 USD
    a:b  0 USD = 5 USD
    a  0 USD =* 6 USD
    e  0 USD
`,
	},
	{
		name: "inclusive assignment reads subaccounts",
		input: `2025-01-01 seed
    a  1 USD
    a:b  2 USD  ; date:2025-01-03
    e

2025-01-02 assign
    a  =*10 USD
    e

2025-01-04 check
    a  0 USD = 10 USD
    a:b  0 USD = 2 USD
    a  0 USD =* 12 USD
    e  0 USD
`,
	},
	{
		name: "strict assignment clears other commodities",
		input: `2025-01-01 seed
    a  3 EUR
    a  7 USD
    e

2025-01-02 assign
    a  ==5 USD
    e

2025-01-03 check
    a  0 USD == 5 USD
    e  0 USD = 15 USD
`,
		wantMessages: []string{"asserted 15 USD, calculated -5 USD (difference -20)"},
	},
	{
		name: "strict assignment moves other commodities to its balancing posting",
		input: `2025-01-01 seed
    a  3 EUR
    e

2025-01-02 assign
    a  ==5 USD
    e

2025-01-03 check
    e  0 EUR = 0 EUR
    a  0 USD
`,
	},
	{
		name: "strict assignment unbalances without a balancing posting",
		input: `2025-01-01 seed
    a  3 EUR
    e

2025-01-02 assign
    a  ==0 USD
`,
		wantMessages: []string{"EUR off by -3"},
	},
	{
		name: "inclusive strict assignment clears subaccount commodities",
		input: `2025-01-01 seed
    a  1 USD
    a  4 EUR
    a:b  2 USD
    a:b  3 EUR
    e

2025-01-02 assign
    a  ==*10 USD
    e

2025-01-03 check
    a  0 USD =* 10 USD
    a  0 EUR =* 0 EUR
    a  0 EUR = -3 EUR
    e  0 USD
`,
	},
	{
		name: "strict inclusive assertion nets subaccounts per commodity",
		input: `2025-01-01 seed
    a  -3 EUR
    a:b  3 EUR
    a  10 USD = 10 USD
    e

2025-01-02 check
    a  0 USD ==* 10 USD
    e  0 USD
`,
	},
	{
		name: "inclusive assignment offsets subaccounts in other commodities",
		input: `2025-01-01 seed
    a  4 EUR
    a:b  3 EUR
    a:b  2 USD
    e

2025-01-02 assign
    a  =*10 USD
    e

2025-01-03 check
    a  0 EUR = 1 EUR
    a  0 USD = 8 USD
    e  0 USD
`,
	},
	{
		name: "posting dated before its transaction reaches the assignment",
		input: `2024-01-16 t
    c  = -24 EUR
    c  -20 EUR  ; date:2024-01-05
    e

2024-01-17 check
    c  0 EUR = -24 EUR
    e  0 EUR
`,
	},
	{
		name: "posting dated after its transaction misses the assignment",
		input: `2024-01-16 t
    c  -20 EUR  ; date:2024-01-25
    c  = -24 EUR
    e

2024-01-31 check
    c  0 EUR = -44 EUR
    e  0 EUR
`,
	},
	{
		name: "postings of an assignment transaction run in date order",
		input: `2024-01-16 t
    c  -1 EUR  ; date:2024-01-25
    c  -2 EUR  ; date:2024-01-20
    c  = -24 EUR
    c  -3 EUR  ; date:2024-01-05
    e

2024-01-31 check
    c  0 EUR = -27 EUR
    e  0 EUR
`,
	},
	{
		name: "dated subaccount posting reaches an inclusive assignment",
		input: `2024-01-16 t
    долг  =* -24 ₽
    долг:カード  -20 ₽  ; date:2024-01-05
    e

2024-01-17 check
    долг  0 ₽ = -4 ₽
    e  0 ₽
`,
	},
	{
		name: "one assignment per balancing group",
		input: `2025-01-01 seed
    a  2 USD
    b  -2 USD

2025-01-02 assign
    a  =5 USD
    b
    [v]  =7 USD
    [w]

2025-01-03 check
    b  0 USD = -5 USD
    [w]  0 USD = -7 USD
    a  0 USD
`,
	},
	{
		name: "assertion cost converts the inferred amount",
		input: `2024-01-01 t
    assets:cash  = 2 EUR @ 2 USD
    equity  -4 USD
`,
	},
	{
		name: "assertion total cost converts the inferred amount",
		input: `2024-01-01 t
    assets:cash  = 2 EUR @@ 6 USD
    equity  -6 USD
`,
	},
	{
		name: "assertion cost leaves the asserted commodity unbalanced",
		input: `2024-01-01 t
    assets:cash  = 2 EUR @ 2 USD
    equity  -2 EUR
`,
		wantMessages: []string{"EUR off by -2; USD off by 4"},
	},
	{
		name: "assertion total cost with a negative quantity",
		input: `2024-01-01 t
    assets:cash  = -2 EUR @@ 6 USD
    equity  6 USD
`,
	},
	{
		name: "assertion cost replaces the balance that was there before",
		input: `2024-01-01 t1
    assets:cash  1 EUR
    equity

2024-01-02 t2
    assets:cash  = 3 EUR @ 2 USD
    equity  1 EUR
    equity  -6 USD
`,
	},
	{
		name: "assertion cost replaces the balance that was there before and misses one commodity",
		input: `2024-01-01 t1
    assets:cash  1 EUR
    equity

2024-01-02 t2
    assets:cash  = 3 EUR @ 2 USD
    equity  2 EUR
    equity  -6 USD
`,
		wantMessages: []string{"EUR off by 1"},
	},
	{
		name: "assertion cost restated later moves nothing",
		input: `2024-01-01 t1
    касса  = 2 EUR @ 2 USD
    капитал

2024-01-02 t2
    касса  0 EUR = 2 EUR
    капитал  0 EUR
`,
	},
	{
		name: "strict inclusive assertion with a cost covers the subaccounts",
		input: `2024-01-01 t1
    現金:a  1 EUR
    現金:b  2 EUR
    equity

2024-01-02 t2
    現金  ==* 5 EUR @@ 7 USD
    equity  3 EUR
    equity  -7 USD
`,
	},
	{
		name: "assertion cost at another price keeps the conversion price explicit",
		input: `2024-01-01 t1
    bank  = 7 GBP @ 1 EUR
    c

2024-01-02 t2
    bank  4.5 GBP
    c

2024-01-03 t3
    bank  = 3 GBP @ 3 EUR
    b  1.5 GBP
`,
		wantMessages: []string{"EUR off by 2; GBP off by -3"},
	},
	{
		name: "balanced virtual assertion cost at another price keeps the conversion price explicit",
		input: `2024-01-12 t2
    bank  4.5 GBP
    c

2024-01-14 t3
    bank  7 GBP @ 1 EUR
    [bank]  == 3 GBP @ 3 EUR
    b
`,
		wantMessages: []string{"EUR off by 2; GBP off by -4.5"},
	},
	{
		name: "assertion cost over prices that cancel in quantity keeps the conversion price explicit",
		input: `2024-01-01 t1
    b  -10 EUR @ 2 USD
    b  10 EUR @ 3 USD
    c  = 0 EUR
    d

2024-01-02 t2
    b  = 2 EUR @ 5 USD
    x  1 GBP
    y  -3 EUR
`,
		wantMessages: []string{"EUR off by -3; GBP off by 1"},
	},
	{
		name: "assertion cost replaces balances held at two prices",
		input: `2024-01-01 t1
    bank  1 GBP @ 1 EUR
    bank  1 GBP @ 3 EUR
    d  = 0 EUR
    c

2024-01-02 t2
    bank  = 2 GBP @ 2 EUR
    b  3 GBP
    b  -5 EUR
`,
		wantMessages: []string{"EUR off by -5; GBP off by 3"},
	},
	{
		name: "assertion cost replaces balances held at two prices on Cyrillic and CJK accounts",
		input: `2024-01-01 t1
    банк:現金  1 GBP @ 1 EUR
    банк:現金  1 GBP @ 3 EUR
    касса  = 0 EUR
    капитал

2024-01-02 t2
    банк:現金  = 2 GBP @ 2 EUR
    資本  3 GBP
    資本  -5 EUR
`,
		wantMessages: []string{"EUR off by -5; GBP off by 3"},
	},
	{
		name: "assertion cost replaces balances held at two prices and balances",
		input: `2024-01-01 t1
    bank  1 GBP @ 1 EUR
    bank  1 GBP @ 3 EUR
    d  = 0 EUR
    c

2024-01-02 t2
    bank  = 2 GBP @ 2 EUR
    b
`,
	},
	{
		name: "assertion off by a half is rejected",
		input: `2024-01-01 t
    a  0.5 EUR
    b

2024-01-02 c
    a  0 EUR = 1 EUR
    b
`,
		wantMessages: []string{"asserted 1 EUR, calculated 0.5 EUR (difference -0.5)"},
	},
	{
		name: "assertion off by 0.4 from zero is rejected",
		input: `2024-01-01 t
    a  0.4 EUR
    b

2024-01-02 c
    a  0 EUR = 0 EUR
    b
`,
		wantMessages: []string{"asserted 0 EUR, calculated 0.4 EUR (difference 0.4)"},
	},
	{
		name: "assertion off by less than its own precision is rejected",
		input: `2024-01-01 t
    a  1.004 EUR
    b

2024-01-02 c
    a  0 EUR = 1.00 EUR
    b
`,
		wantMessages: []string{"calculated 1.004 EUR (difference 0.004)"},
	},
	{
		name: "assertion with more decimals than the balance is rejected",
		input: `2024-01-01 t
    a  1 EUR
    b

2024-01-02 c
    a  0 EUR = 1.004 EUR
    b
`,
		wantMessages: []string{"asserted 1.004 EUR, calculated 1 EUR (difference -0.004)"},
	},
	{
		name: "assertion off by less than the commodity style is rejected",
		input: `commodity 1,000.00 EUR

2024-01-01 t
    a  0.504 EUR
    b

2024-01-02 c
    a  0 EUR = 0.50 EUR
    b
`,
		wantMessages: []string{"calculated 0.504 EUR (difference 0.004)"},
	},
	{
		name: "assertion off by a millionth is rejected",
		input: `2024-01-01 t
    a  1.0000001 EUR
    b

2024-01-02 c
    a  0 EUR = 1 EUR
    b
`,
		wantMessages: []string{"difference 0.0000001"},
	},
	{
		name: "assertion that adds a zero decimal is met",
		input: `2024-01-01 t
    a  0.5 EUR
    b

2024-01-02 c
    a  0 EUR = 0.50 EUR
    b
`,
	},
	{
		name: "assertion that drops a zero decimal is met",
		input: `2024-01-01 t
    a  1.50 EUR
    b

2024-01-02 c
    a  0 EUR = 1.5 EUR
    b
`,
	},
	{
		name: "assertion over a sum of fractions is exact",
		input: `2024-01-01 t
    a  0.1 EUR
    a  0.2 EUR
    b

2024-01-02 c
    a  0 EUR = 0.3 EUR
    b
`,
	},
	{
		name: "assertion off by a fraction on Cyrillic and CJK accounts",
		input: `2024-01-01 касса
    касса:наличные  0.5 EUR
    現金

2024-01-02 проверка
    касса:наличные  0 EUR = 1 EUR
    現金  0 EUR
`,
		wantMessages: []string{"asserted 1 EUR, calculated 0.5 EUR"},
	},
	{
		name: "inclusive assertion off by a fraction",
		input: `2024-01-01 t
    a:x  0.5 EUR
    b

2024-01-02 c
    a  0 EUR =* 1 EUR
    b
`,
		wantMessages: []string{"asserted 1 EUR, calculated 0.5 EUR"},
	},
	{
		name: "strict assertion off by a fraction",
		input: `2024-01-01 t
    a  0.5 EUR
    b

2024-01-02 c
    a  0 EUR == 1 EUR
    b
`,
		wantMessages: []string{"asserted 1 EUR, calculated 0.5 EUR"},
	},
	{
		name: "strict assertion with a small balance in another commodity",
		input: `2024-01-01 t
    a  1 EUR
    a  0.004 USD
    b

2024-01-02 c
    a  0 EUR == 1 EUR
    b
`,
		wantMessages: []string{"EUR also holds 0.004 USD"},
	},
	{
		name: "strict inclusive assertion with a small balance in another commodity",
		input: `2024-01-01 t
    a  1 EUR
    a  0.004 USD
    b

2024-01-02 c
    a  0 EUR ==* 1 EUR
    b
`,
		wantMessages: []string{"EUR also holds 0.004 USD"},
	},
	{
		name: "strict assertion with another commodity that nets to zero",
		input: `2024-01-01 t
    a  1 EUR
    a  0.4 USD
    a  -0.4 USD
    b

2024-01-02 c
    a  0 EUR == 1 EUR
    b
`,
	},
	{
		name: "assertion with a cost off by a fraction",
		input: `2024-01-01 t
    a  1 EUR
    b

2024-01-02 c
    a  0 EUR = 1.004 EUR @ 5 USD
    b
`,
		wantMessages: []string{"asserted 1.004 EUR, calculated 1 EUR"},
	},
	{
		name: "assignment restated stays exact",
		input: `2024-01-01 t
    a  0.5 EUR
    b

2024-01-02 c
    a  = 1 EUR
    b  -0.5 EUR

2024-01-03 d
    a  0 EUR = 1 EUR
    b
`,
	},
	{
		name: "inclusive strict assertion ignores another commodity held only by a subaccount",
		input: `2024-01-01 a
    a:x  1 EUR
    a:y  1 USD
    e

2024-01-02 b
    a  0 EUR ==* 1 EUR
    e
`,
	},
	{
		name: "inclusive assertion ignores another commodity held only by a subaccount",
		input: `2024-01-01 a
    a:x  1 EUR
    a:y  1 USD
    e

2024-01-02 b
    a  0 EUR =* 1 EUR
    e
`,
	},
	{
		name: "inclusive strict assertion ignores another commodity held only by a deeper subaccount",
		input: `2024-01-01 a
    a:x:z  1 EUR
    a:x  1 USD
    e

2024-01-02 b
    a  0 EUR ==* 1 EUR
    e
`,
	},
	{
		name: "inclusive strict assertion with an own balance in the asserted commodity only",
		input: `2024-01-01 a
    a  1 EUR
    a:y  1 USD
    e

2024-01-02 b
    a  0 EUR ==* 1 EUR
    e
`,
	},
	{
		name: "inclusive strict assertion lets a subaccount offset the own balance",
		input: `2024-01-01 a
    a  1 EUR
    a  1 USD
    a:y  -1 USD
    e

2024-01-02 b
    a  0 EUR ==* 1 EUR
    e
`,
	},
	{
		name: "inclusive strict assertion after an inclusive strict assignment",
		input: `2024-01-01 a
    a  1 EUR
    a:y  1 USD
    e

2024-01-02 b
    a  ==* 1 EUR
    e

2024-01-03 c
    a  0 EUR ==* 1 EUR
    e
`,
	},
	{
		name: "inclusive strict assertion with an own balance in another commodity",
		input: `2024-01-01 a
    a  1 EUR
    a  1 USD
    e

2024-01-02 b
    a  0 EUR ==* 1 EUR
    e
`,
		wantMessages: []string{"EUR also holds 1 USD"},
	},
	{
		name: "inclusive strict assertion with an own balance only partly offset by a subaccount",
		input: `2024-01-01 a
    a  1 EUR
    a  1 USD
    a:y  -0.5 USD
    e

2024-01-02 b
    a  0 EUR ==* 1 EUR
    e
`,
		wantMessages: []string{"EUR also holds 0.5 USD"},
	},
	{
		name: "inclusive strict assertion adds the subaccount to an own balance",
		input: `2024-01-01 a
    a  1 EUR
    a  1 GBP
    a:x  1 GBP
    e

2024-01-02 b
    a  0 EUR ==* 1 EUR
    e
`,
		wantMessages: []string{"EUR also holds 2 GBP"},
	},
	{
		name: "inclusive strict assertion counts an own commodity that netted to zero",
		input: `2024-01-01 a
    a  1 EUR
    a  1 USD
    a  -1 USD
    a:y  1 USD
    e

2024-01-02 b
    a  0 EUR ==* 1 EUR
    e
`,
		wantMessages: []string{"EUR also holds 1 USD"},
	},
	{
		name: "inclusive strict assertion counts an own commodity written as zero",
		input: `2024-01-01 a
    a  1 EUR
    a  0 USD
    a:y  1 USD
    e

2024-01-02 b
    a  0 EUR ==* 1 EUR
    e
`,
		wantMessages: []string{"EUR also holds 1 USD"},
	},
	{
		name: "strict assertion ignores a subaccount's commodity",
		input: `2024-01-01 a
    a  1 EUR
    a:y  1 USD
    e

2024-01-02 b
    a  0 EUR == 1 EUR
    e
`,
	},
	{
		name: "strict assertion ignores an own commodity that netted to zero",
		input: `2024-01-01 a
    a  1 EUR
    a  1 USD
    a  -1 USD
    e

2024-01-02 b
    a  0 EUR == 1 EUR
    e
`,
	},
	{
		name: "inclusive strict assertion on Cyrillic and CJK accounts",
		input: `2024-01-01 касса
    касса:現金  1 EUR
    касса:счёт  1 USD
    капитал

2024-01-02 проверка
    касса  0 EUR ==* 1 EUR
    капитал
`,
	},
	{
		name: "inclusive strict assertion on Cyrillic and CJK accounts with an own balance in another commodity",
		input: `2024-01-01 касса
    касса  1 EUR
    касса  1 USD
    現金:счёт  0.5 GBP
    капитал

2024-01-02 проверка
    касса  0 EUR ==* 1 EUR
    капитал
`,
		wantMessages: []string{"EUR also holds 1 USD"},
	},
	{
		name: "inclusive strict assignment gives the account an entry in a commodity a subaccount holds at zero",
		input: `2024-01-01 a
    a:x  1 EUR
    a:y  0 USD
    e

2024-01-02 b
    a  ==* 1 EUR
    e

2024-01-03 c
    a:y  5 USD
    e

2024-01-04 d
    a  0 EUR ==* 1 EUR
`,
		wantMessages: []string{"EUR also holds 5 USD"},
	},
	{
		name: "inclusive assignment gives the account an entry in a commodity a subaccount holds at zero",
		input: `2024-01-01 a
    a:x  1 EUR
    a:y  0 USD
    e

2024-01-02 b
    a  =* 1 EUR
    e

2024-01-03 c
    a:y  5 USD
    e

2024-01-04 d
    a  0 EUR ==* 1 EUR
`,
		wantMessages: []string{"EUR also holds 5 USD"},
	},
	{
		name: "strict assignment drops the account's entries in other commodities",
		input: `2024-01-01 a
    a  1 USD
    a:x  1 EUR
    e

2024-01-02 b
    a  == 1 EUR
    e

2024-01-03 c
    a:y  5 USD
    e

2024-01-04 d
    a  0 EUR ==* 2 EUR
`,
	},
	{
		name: "inclusive strict assignment drops the account's entries in commodities no subaccount holds",
		input: `2024-01-01 a
    a  1 USD
    a:x  1 EUR
    e

2024-01-02 b
    a  ==* 1 EUR
    e

2024-01-03 c
    a:y  5 USD
    e

2024-01-04 d
    a  0 EUR ==* 1 EUR
`,
	},
	{
		name: "inclusive strict assignment on Cyrillic and CJK accounts keeps a subaccount's commodity and drops the rest",
		input: `2024-01-01 a
    касса  1 GBP
    касса:現金  0 USD
    капитал

2024-01-02 b
    касса  ==* 1 EUR
    капитал

2024-01-03 c
    касса:現金  5 GBP
    касса:現金  5 USD
    капитал

2024-01-04 d
    касса  0 EUR ==* 1 EUR
`,
		wantMessages: []string{"EUR also holds 5 USD"},
	},
	{
		name: "lone cost-converted balanced virtual posting is unbalanced",
		input: `2024-01-01 t
    [b]  2 USD @ 0.25 EUR
    a  1 USD
    c
`,
		wantMessages: []string{"EUR off by 0.5"},
	},
	{
		name: "costs that differ by a tenth of the cost commodity",
		input: `2024-01-01 t
    b  2 USD @ 0.25 EUR
    a  -2 USD @ 0.2 EUR
`,
		wantMessages: []string{"EUR off by 0.1"},
	},
	{
		name: "costs that differ by less than a thousandth",
		input: `2024-01-01 t
    b  2 USD @ 0.25 EUR
    a  -2 USD @ 0.2499 EUR
`,
		wantMessages: []string{"EUR off by 0.0002"},
	},
	{
		name: "costs that differ by a hundred-millionth",
		input: `2024-01-01 t
    b  2 USD @ 0.25 EUR
    a  -2 USD @ 0.24999999 EUR
`,
		wantMessages: []string{"EUR off by 0.00000002"},
	},
	{
		name: "total costs that differ by a tenth of the cost commodity",
		input: `2024-01-01 t
    b  2 USD @@ 0.5 EUR
    a  -2 USD @@ 0.4 EUR
`,
		wantMessages: []string{"EUR off by 0.1"},
	},
	{
		name: "equal costs balance exactly",
		input: `2024-01-01 t
    b  2 USD @ 0.25 EUR
    a  -2 USD @ 0.25 EUR
`,
	},
	{
		name: "cost commodity written once with no decimals gives half a unit",
		input: `2024-01-01 t
    b  2 USD @@ 3 EUR
    a  -2 USD @@ 2.5 EUR
    c  1 EUR
    d  -1 EUR
`,
	},
	{
		name: "cost commodity written once with no decimals does not give more than half a unit",
		input: `2024-01-01 t
    b  2 USD @@ 3 EUR
    a  -2 USD @@ 2.4 EUR
    c  1 EUR
    d  -1 EUR
`,
		wantMessages: []string{"EUR off by 0.6"},
	},
	{
		name: "cost commodity written with a decimal gives a twentieth",
		input: `2024-01-01 t
    b  2 USD @@ 0.5 EUR
    a  -2 USD @@ 0.451 EUR
    c  1.0 EUR
    d  -1.0 EUR
`,
	},
	{
		name: "cost commodity written with a decimal does not give more than a twentieth",
		input: `2024-01-01 t
    b  2 USD @@ 0.5 EUR
    a  -2 USD @@ 0.449 EUR
    c  1.0 EUR
    d  -1.0 EUR
`,
		wantMessages: []string{"EUR off by 0.051"},
	},
	{
		name: "precision of the cost commodity comes from the other group of the transaction",
		input: `2024-01-01 t
    b  2 USD @ 0.25 EUR
    a  -2 USD @ 0.2 EUR
    [x]  1 EUR
    [y]  -1 EUR
`,
	},
	{
		name: "precision of the cost commodity comes from the balanced virtual group and tightens it",
		input: `2024-01-01 t
    b  2 USD @ 0.25 EUR
    a  -2 USD @ 0.24 EUR
    [x]  1.00 EUR
    [y]  -1.00 EUR
`,
		wantMessages: []string{"EUR off by 0.02"},
	},
	{
		name: "precision of the cost commodity comes from an unbalanced virtual posting",
		input: `2024-01-01 t
    b  2 USD @ 0.25 EUR
    a  -2 USD @ 0.24 EUR
    (x)  1.00 EUR
`,
		wantMessages: []string{"EUR off by 0.02"},
	},
	{
		name: "precision of the cost commodity counts an amount of zero",
		input: `2024-01-01 t
    b  2 USD @ 0.25 EUR
    a  -2 USD @ 0.2 EUR
    c  1 EUR
    d  -1 EUR
    e  0.00 EUR
`,
		wantMessages: []string{"EUR off by 0.1"},
	},
	{
		name: "lone cost-converted posting on Cyrillic and CJK accounts",
		input: `2024-01-01 t
    [касса]  2 USD @ 0.25 EUR
    現金  1 USD
    капитал
`,
		wantMessages: []string{"EUR off by 0.5"},
	},
	{
		name: "lone converted posting is within half a unit of a unit price without decimals",
		input: `2024-01-01 t
    b  -0.1 USD @ 1 EUR
    [a]
`,
	},
	{
		name: "lone converted posting is within half a unit of a unit price with a decimal",
		input: `2024-01-01 t
    b  -0.1 USD @ 0.2 EUR
    [a]
`,
	},
	{
		name: "lone converted posting is outside half a unit of a unit price with two decimals",
		input: `2024-01-01 t
    b  -0.1 USD @ 0.25 EUR
    [a]
`,
		wantMessages: []string{"EUR off by -0.025"},
	},
	{
		name: "costs of different precisions take the finest",
		input: `2024-01-01 t
    b  1 USD @ 1 EUR
    a  -1 USD @ 0.96 EUR
`,
		wantMessages: []string{"EUR off by 0.04"},
	},
	{
		name: "costs of different precisions take the finest whatever their order",
		input: `2024-01-01 t
    b  1 USD @ 0.96 EUR
    a  -1 USD @ 1 EUR
`,
		wantMessages: []string{"EUR off by -0.04"},
	},
	{
		name: "costs of one decimal and none leave a residual of half",
		input: `2024-01-01 t
    b  1 USD @ 1 EUR
    a  -1 USD @ 0.5 EUR
`,
		wantMessages: []string{"EUR off by 0.5"},
	},
	{
		name: "balanced virtual cost takes the precision of the amount inferred for an elided real posting",
		input: `2024-01-01 t
    a:x  -9 USD @ 2 GBP
    [w]  -1 USD @ 0.5 GBP
    b
`,
	},
	{
		name: "balanced virtual cost is rejected under the finer amount inferred for an elided real posting",
		input: `2024-01-01 t
    a:x  -9 USD @ 0.25 GBP
    [w]  -0.1 USD @ 2 GBP
    b
`,
		wantMessages: []string{"GBP off by -0.2"},
	},
	{
		name: "balanced virtual cost finer than the coarse amount inferred for an elided real posting is accepted",
		input: `2024-01-01 t
    a:x  -9 USD @ 2 GBP
    [w]  -0.1 USD @ 0.25 GBP
    b
`,
	},
	{
		name: "balanced virtual cost beyond half a unit of the coarse inferred amount is rejected",
		input: `2024-01-01 t
    a:x  -9 USD @ 2 GBP
    [w]  -1 USD @ 0.55 GBP
    b
`,
		wantMessages: []string{"GBP off by -0.55"},
	},
	{
		name: "balanced virtual costs of one decimal under the coarse inferred amount",
		input: `2024-01-01 t
    a:x  -9 USD @ 2 GBP
    [w]  -1 USD @ 0.5 GBP
    [v]  1 USD @ 0.4 GBP
    b
`,
	},
	{
		name: "balanced virtual cost under the inferred amount of one decimal is rejected",
		input: `2024-01-01 t
    a:x  -9 USD @ 2.0 GBP
    [w]  -1 USD @ 0.25 GBP
    b
`,
		wantMessages: []string{"GBP off by -0.25"},
	},
	{
		name: "each group takes the precision of its own costs",
		input: `2024-01-01 t
    [a]  0.10 USD @ 0.50 EUR
    b  -0.10 USD @ 0.5 EUR
`,
		wantMessages: []string{"EUR off by 0.05"},
	},
	{
		name: "each group takes the precision of its own costs, real group finer",
		input: `2024-01-01 t
    a  0.10 USD @ 0.50 EUR
    [b]  -0.10 USD @ 0.5 EUR
`,
		wantMessages: []string{"EUR off by 0.05"},
	},
	{
		name: "elided virtual posting infers a zero that counts as written",
		input: `2024-01-01 t
    [a]  1.0 USD @@ 0 EUR
    b  -1.0 USD @@ 0.2 EUR
    [c]
`,
	},
	{
		name: "elided virtual posting infers a zero in a commodity its group sums to nothing in",
		input: `2024-01-01 t
    x  1 USD @@ 0.4 EUR
    y  -1 USD @@ 0.36 EUR
    [v]  1 USD @@ 1 EUR
    [w]  -1 USD @@ 1 EUR
    [z]
`,
	},
	{
		name: "elided virtual posting infers the finest precision of what it offsets",
		input: `2024-01-01 t
    x  1 USD @@ 0.4 EUR
    y  -1 USD @@ 0.36 EUR
    [v]  1 USD @@ 1.00 EUR
    [w]  -1 USD @@ 1 EUR
    [z]
`,
		wantMessages: []string{"EUR off by 0.04"},
	},
	{
		name: "a cost of zero gives no precision to the other group",
		input: `2024-01-01 t
    [a]  1.00 USD @@ 0.50 EUR
    b  -1.00 USD @@ 0 EUR
`,
		wantMessages: []string{"EUR off by 0.5"},
	},
	{
		name: "a cost of zero in a balanced virtual posting gives no precision to the real group",
		input: `2024-01-01 t
    a  0 USD @@ 0 EUR
    [b]  -0 USD @@ 0.020 EUR
`,
		wantMessages: []string{"EUR off by 0.02"},
	},
}

func TestCheckJournalBalance_AssignmentOrder(t *testing.T) {
	for _, tc := range assignmentOrderCases {
		t.Run(tc.name, func(t *testing.T) {
			resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: tc.input})

			diagnostics := CheckJournalBalance(resolved, decimal.Zero)

			require.Len(t, diagnostics, len(tc.wantMessages), "got %v", diagnostics)
			for i, fragment := range tc.wantMessages {
				assert.Contains(t, diagnostics[i].Message, fragment)
			}
		})
	}
}

func TestCheckJournalBalance_FailureAfterCrossingReportsReplayedValues(t *testing.T) {
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-12-31 charge
    card  -10 USD  ; date:2025-01-02
    expenses:x

2025-01-01 opening
    card  =0 USD
    equity:o

2025-01-31 check
    card  -10 USD = -25 USD
    expenses:x
`})

	diagnostics := CheckJournalBalance(resolved, decimal.Zero)

	require.Len(t, diagnostics, 1)
	assert.Equal(t, CodeBalanceAssertionFailed, diagnostics[0].Code)
	assert.Equal(t, 10, diagnostics[0].Range.Start.Line)
	assert.Equal(t, "-25", diagnostics[0].Data["expected"])
	assert.Equal(t, "-20", diagnostics[0].Data["actual"])
}

func TestCheckJournalBalance_AssignmentsReachAcrossIncludeFiles(t *testing.T) {
	resolved := resolvedFromSources(t,
		journalSource{path: "/main.journal", content: `2025-01-01 opening
    card  =0 USD
    equity:o
`},
		journalSource{path: "/2024.journal", content: `2024-12-31 charge
    card  -10 USD  ; date:2025-01-02
    expenses:x

2025-01-31 check
    card  0 USD = -11 USD
    expenses:x  0 USD
`})

	diagnostics := CheckJournalBalance(resolved, decimal.Zero)

	require.Len(t, diagnostics, 1)
	assert.Equal(t, "/2024.journal", diagnostics[0].Path)
	assert.Contains(t, diagnostics[0].Message, "calculated -10 USD")
}

// hledger refuses a date: on an assignment posting outright. The server has no
// diagnostic for that yet, so the posting simply stays at the transaction date.
func TestCheckJournalBalance_DatedAssignmentStaysAtTransactionDate(t *testing.T) {
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2025-01-10 assign
    a  =5 USD  ; date:2025-01-20
    e

2025-01-15 check
    a  0 USD = 5 USD
    e  0 USD
`})

	assert.Empty(t, CheckJournalBalance(resolved, decimal.Zero))
}

func TestCheckJournalBalance_AssertionSeesCostKeptByAssignment(t *testing.T) {
	// The elided posting beside `b = 13 EUR` is -13 EUR, -58 USD because the
	// assignment carries the cost kept by the earlier assignment transaction.
	// hledger 1.52 accepts the journal.
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-01-03 t3
    b  -29 EUR @ 2 USD
    c  = -7 EUR
    d

2024-01-16 t1
    b  = 13 EUR
    x

2024-01-17 check
    x  0 USD = -58 USD
    x  0 EUR = -13 EUR
    y  0 USD
`})
	assert.Empty(t, CheckJournalBalance(resolved, decimal.Zero))
}

func TestCheckJournalBalance_AssertionsIgnoreTheUserTolerance(t *testing.T) {
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-01-01 t
    касса  0.5 EUR
    現金

2024-01-02 c
    касса  0 EUR = 1 EUR
    現金  0 EUR

2024-01-03 d
    касса  0.004 USD
    現金  -0.004 USD

2024-01-04 e
    касса  0 EUR == 0.5 EUR
    現金  0 EUR
`})

	diagnostics := CheckJournalBalance(resolved, decimal.RequireFromString("0.5"))

	require.Len(t, diagnostics, 2)
	assert.Equal(t, CodeBalanceAssertionFailed, diagnostics[0].Code)
	assert.Contains(t, diagnostics[0].Message, "asserted 1 EUR, calculated 0.5 EUR")
	assert.Equal(t, CodeBalanceAssertionFailed, diagnostics[1].Code)
	assert.Contains(t, diagnostics[1].Message, "EUR also holds 0.004 USD")
}

func TestCheckJournalBalance_UserToleranceStillWidensTheTransactionBalance(t *testing.T) {
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-01-01 t
    a  1.4 EUR
    b  -1 EUR
`})

	assert.Len(t, CheckJournalBalance(resolved, decimal.Zero), 1)
	assert.Empty(t, CheckJournalBalance(resolved, decimal.RequireFromString("0.5")))
}
