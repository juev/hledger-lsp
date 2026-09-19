package analyzer

import (
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
