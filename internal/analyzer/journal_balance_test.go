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
