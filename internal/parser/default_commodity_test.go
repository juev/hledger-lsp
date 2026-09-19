package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/ast"
)

// hledger reads a bare number as the commodity set by the D directive that
// precedes it, so `D 1.000,00 RUB` turns `609.000,00` into RUB 609000.00 while
// the amount still has no symbol text of its own.
func TestParse_DefaultCommodityAppliesToBareAmounts(t *testing.T) {
	input := `D 1.000,00 RUB
commodity 1.000,00 RUB

2024-01-15 groceries
    expenses:food  -609.000,00
    assets:cash     609.000,00
`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	for _, posting := range journal.Transactions[0].Postings {
		amount := posting.Amount
		require.NotNil(t, amount)
		assert.Equal(t, "RUB", amount.Commodity.Symbol)
		assert.True(t, amount.Commodity.Inferred, "a bare amount inherits the default commodity")
		assert.Equal(t, "", amount.Commodity.WrittenSymbol(), "there is no symbol text to point at")
		assert.Equal(t, ast.CommodityRight, amount.Commodity.Position)
		assert.Equal(t, "609000", amount.Quantity.Abs().String())
	}
}

func TestParse_DefaultCommodityLeavesWrittenSymbolsAlone(t *testing.T) {
	input := `D 1.000,00 RUB

2024-01-15 mixed
    assets:cash        1.000,00 USD
    assets:bank          500,00
    expenses:food       -500,00 EUR
`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	postings := journal.Transactions[0].Postings
	assert.Equal(t, "USD", postings[0].Amount.Commodity.Symbol)
	assert.False(t, postings[0].Amount.Commodity.Inferred)
	assert.Equal(t, "RUB", postings[1].Amount.Commodity.Symbol)
	assert.True(t, postings[1].Amount.Commodity.Inferred)
	assert.Equal(t, "EUR", postings[2].Amount.Commodity.Symbol)
	assert.False(t, postings[2].Amount.Commodity.Inferred)
}

func TestParse_DefaultCommodityOnlyAffectsLaterAmounts(t *testing.T) {
	input := `2024-01-15 before
    assets:cash   1.000,00
    equity

D 1.000,00 RUB

2024-01-16 after
    assets:cash   1.000,00
    equity
`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 2)

	before := journal.Transactions[0].Postings[0].Amount.Commodity
	assert.Equal(t, "", before.Symbol, "hledger applies D only to amounts that follow it")
	assert.False(t, before.Inferred)

	after := journal.Transactions[1].Postings[0].Amount.Commodity
	assert.Equal(t, "RUB", after.Symbol)
	assert.True(t, after.Inferred)
}

func TestParse_DefaultCommodityPositionFollowsDirective(t *testing.T) {
	tests := []struct {
		name      string
		directive string
		position  ast.CommodityPosition
	}{
		{"commodity right", "D 1.000,00 RUB", ast.CommodityRight},
		{"commodity left", "D $1,000.00", ast.CommodityLeft},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			journal, errs := Parse(tc.directive + "\n\n2024-01-15 x\n    expenses:food  10,00\n    assets:cash\n")
			require.Empty(t, errs)

			commodity := journal.Transactions[0].Postings[0].Amount.Commodity
			assert.True(t, commodity.Inferred)
			assert.Equal(t, tc.position, commodity.Position,
				"a bare amount is laid out the way the D directive writes its symbol")
		})
	}
}

func TestParse_DefaultCommodityWithoutSymbolClearsIt(t *testing.T) {
	input := `D 1.000,00 RUB

2024-01-15 with default
    assets:cash   1.000,00
    equity

D 1.000,00

2024-01-16 without default
    assets:cash   1.000,00
    equity
`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 2)

	assert.Equal(t, "RUB", journal.Transactions[0].Postings[0].Amount.Commodity.Symbol)
	assert.Equal(t, "", journal.Transactions[1].Postings[0].Amount.Commodity.Symbol,
		"a D directive without a symbol clears the default commodity")
}

// A D directive in the parent journal reaches amounts in files included after
// it: hledger's directive state flows into an include.
func TestParseWithContext_DefaultCommodityReachesIncludedFile(t *testing.T) {
	input := "D 1.000,00 RUB\ninclude child.journal\n"
	childInput := "2024-01-15 child\n    assets:cash   1.000,00\n    equity\n"

	var childAmount *ast.Amount
	result, errs := ParseWithContext(input, Context{}, func(site IncludeSite) ContextExports {
		child, childErrs := ParseWithContext(childInput, site.Context, nil)
		require.Empty(t, childErrs)
		childAmount = child.Journal.Transactions[0].Postings[0].Amount
		return child.Exports
	})
	require.Empty(t, errs)
	require.Len(t, result.Journal.Transactions, 0, "the parent has no transactions of its own")

	require.NotNil(t, childAmount)
	assert.Equal(t, "RUB", childAmount.Commodity.Symbol)
	assert.True(t, childAmount.Commodity.Inferred)
	assert.Equal(t, ast.CommodityRight, childAmount.Commodity.Position,
		"the parent directive writes its symbol on the right")
}

// A D directive inside an included file stays local: the parent keeps its own
// default commodity for the amounts that follow the include.
func TestParseWithContext_DefaultCommodityFromChildDoesNotLeakToParent(t *testing.T) {
	input := "D 1.000,00 EUR\n\n2024-01-15 before\n    assets:cash   1,00\n    equity\n\ninclude child.journal\n\n2024-01-17 after\n    assets:cash   3,00\n    equity\n"
	childInput := "D 1.000,00 USD\n\n2024-01-16 child\n    assets:cash   2,00\n    equity\n"

	var childAmount *ast.Amount
	result, errs := ParseWithContext(input, Context{}, func(site IncludeSite) ContextExports {
		child, childErrs := ParseWithContext(childInput, site.Context, nil)
		require.Empty(t, childErrs)
		childAmount = child.Journal.Transactions[0].Postings[0].Amount
		return child.Exports
	})
	require.Empty(t, errs)
	require.Len(t, result.Journal.Transactions, 2)

	require.NotNil(t, childAmount)
	assert.Equal(t, "USD", childAmount.Commodity.Symbol)
	assert.Equal(t, "EUR", result.Journal.Transactions[0].Postings[0].Amount.Commodity.Symbol)
	assert.Equal(t, "EUR", result.Journal.Transactions[1].Postings[0].Amount.Commodity.Symbol,
		"the parent's default commodity survives the include")
}

// Balance assertions written bare inherit the default commodity too, which is
// what makes the clopen workflow (bare activity, RUB assertions) balance.
func TestParse_DefaultCommodityAppliesToAssertions(t *testing.T) {
	input := `D 1.000,00 RUB

2024-01-15 x
    assets:cash   1.000,00 = 1.000,00
    equity
`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	assertion := journal.Transactions[0].Postings[0].BalanceAssertion
	require.NotNil(t, assertion)
	assert.Equal(t, "RUB", assertion.Amount.Commodity.Symbol)
	assert.True(t, assertion.Amount.Commodity.Inferred)
}

// Costs written bare are amounts like any other.
func TestParse_DefaultCommodityAppliesToCosts(t *testing.T) {
	input := `D 1.000,00 RUB

2024-01-15 buy
    assets:stocks   10 AAPL @ 1.000,00
    assets:cash
`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	cost := journal.Transactions[0].Postings[0].Cost
	require.NotNil(t, cost)
	assert.Equal(t, "RUB", cost.Amount.Commodity.Symbol)
	assert.True(t, cost.Amount.Commodity.Inferred)
}

// A bare number is read with the D directive's number format, which can differ
// from the style declared for the commodity itself. hledger 1.52.4 reads the
// bare `1.234` below as 1234 (D: dot = thousands, comma = decimal) while the
// explicitly written RUB amount follows `commodity 1,000.00 RUB`.
func TestParse_DefaultCommodityNumberFormatWinsForBareAmounts(t *testing.T) {
	input := `D 1.000,00 RUB
commodity 1,000.00 RUB

2024-01-01 mix
    a:aa      1.234
    b:bb     -1,234.00 RUB
`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	bare := journal.Transactions[0].Postings[0].Amount
	written := journal.Transactions[0].Postings[1].Amount
	require.NotNil(t, bare)
	require.NotNil(t, written)

	assert.Equal(t, "1234", bare.Quantity.String(), "the D directive's format decides")
	assert.True(t, bare.Commodity.Inferred)
	assert.Equal(t, "-1234", written.Quantity.String(), "an explicit amount keeps the commodity's style")
	assert.False(t, written.Commodity.Inferred)
}

// `D RUB 1.000,00` is the word-commodity-first form hledger accepts, and it
// carries the same meaning: bare amounts are RUB and use its number format.
func TestParse_DefaultCommodityWithWordSymbolFirst(t *testing.T) {
	input := `D RUB 1.000,00

2024-01-15 x
    expenses:food   1.000,00
    assets:cash
`

	journal, errs := Parse(input)
	require.Empty(t, errs)

	dir, ok := journal.Directives[0].(ast.DefaultCommodityDirective)
	require.True(t, ok)
	assert.Equal(t, "RUB", dir.Symbol)
	assert.Equal(t, "RUB 1.000,00", dir.Format)

	amount := journal.Transactions[0].Postings[0].Amount
	require.NotNil(t, amount)
	assert.Equal(t, "RUB", amount.Commodity.Symbol)
	assert.True(t, amount.Commodity.Inferred)
	assert.Equal(t, ast.CommodityLeft, amount.Commodity.Position)
	assert.Equal(t, "1000", amount.Quantity.String())
}

// A D directive the parser cannot read must not wipe the commodity it already
// knows: hledger rejects `D RUB` and a bare `D` outright, and an explicit
// amount of the recorded commodity still has to be read with its number format.
func TestParse_UnreadableDefaultCommodityKeepsRecordedState(t *testing.T) {
	input := `D 1.000,00 RUB
D RUB

2024-01-15 x
    expenses:food   1.000 RUB
    assets:cash

D

2024-01-16 y
    expenses:food   2.000 RUB
    assets:cash
`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 2)

	assert.Equal(t, "1000", journal.Transactions[0].Postings[0].Amount.Quantity.String())
	assert.Equal(t, "2000", journal.Transactions[1].Postings[0].Amount.Quantity.String())
}

// Regression: `D 1.000,00 RUB` twice, the second time word-commodity-first, used
// to drop the recorded commodity, after which `1.000 RUB` fell back to the
// ambiguous-number heuristic and meant 1 instead of 1000.
func TestParse_WordSymbolFirstDirectiveKeepsNumberFormat(t *testing.T) {
	input := `D 1.000,00 RUB
D RUB 1.000,00

2024-01-15 x
    expenses:food   1.000 RUB
    assets:cash
`

	journal, errs := Parse(input)
	require.Empty(t, errs)

	amount := journal.Transactions[0].Postings[0].Amount
	require.NotNil(t, amount)
	assert.Equal(t, "RUB", amount.Commodity.Symbol)
	assert.Equal(t, "1000", amount.Quantity.String(), "hledger reads this amount as 1000 RUB")
}

// The layout of the parent's D directive reaches the included file together with
// the symbol, so a bare amount there is positioned like the directive writes it.
func TestParseWithContext_DefaultCommodityPositionReachesIncludedFile(t *testing.T) {
	input := "D $1,000.00\ninclude child.journal\n"
	childInput := "2024-01-15 child\n    assets:cash   1,000.00\n    equity\n"

	var childAmount *ast.Amount
	_, errs := ParseWithContext(input, Context{}, func(site IncludeSite) ContextExports {
		child, childErrs := ParseWithContext(childInput, site.Context, nil)
		require.Empty(t, childErrs)
		childAmount = child.Journal.Transactions[0].Postings[0].Amount
		return child.Exports
	})
	require.Empty(t, errs)
	require.NotNil(t, childAmount)

	assert.Equal(t, "$", childAmount.Commodity.Symbol)
	assert.True(t, childAmount.Commodity.Inferred)
	assert.Equal(t, ast.CommodityLeft, childAmount.Commodity.Position,
		"the parent directive writes its symbol on the left")
}
