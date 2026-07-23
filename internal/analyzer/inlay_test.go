package analyzer

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/parser"
)

func TestCalculatePostingEffects_InferenceCostAndSnapshots(t *testing.T) {
	journal, errs := parser.Parse(`2024-01-15 trade
    assets:stock  -2 AAPL @ $10
    assets:cash
    [budget:stock]  3 AAPL @@ $30
    [budget:cash]
    (memo:shares)  7 AAPL
`)
	require.Empty(t, errs)

	effects := CalculatePostingEffects(journal)
	require.Len(t, effects, 5)

	assert.Equal(t, 1, effects[1].PostingIndex)
	assert.Equal(t, decimal.NewFromInt(20), effects[1].InferredAmounts["$"])
	assert.Equal(t, decimal.NewFromInt(-20), effects[0].CostContribution["$"])
	assert.Equal(t, decimal.NewFromInt(30), effects[2].CostContribution["$"])
	assert.Equal(t, decimal.NewFromInt(-30), effects[3].InferredAmounts["$"])
	assert.Equal(t, decimal.NewFromInt(7), effects[4].BalanceAfter["memo:shares"]["AAPL"])

	// Results must not share mutable maps with one another.
	effects[0].BalanceAfter["assets:stock"]["AAPL"] = decimal.Zero
	assert.Equal(t, decimal.NewFromInt(-2), effects[1].BalanceAfter["assets:stock"]["AAPL"])
}

func TestCalculatePostingEffects_MultipleElidedAndGroups(t *testing.T) {
	journal, errs := parser.Parse(`2024-01-15 test
    assets:cash
    expenses:food
    [budget:one]  $5
    [budget:two]
    (memo:ignored)
`)
	require.Empty(t, errs)

	effects := CalculatePostingEffects(journal)
	require.Len(t, effects, 5)
	assert.Empty(t, effects[0].InferredAmounts)
	assert.Empty(t, effects[1].InferredAmounts)
	assert.Equal(t, decimal.NewFromInt(-5), effects[3].InferredAmounts["$"])
	assert.Empty(t, effects[4].InferredAmounts)
}

func TestCalculatePostingEffects_InferredMultiCommodityPreservesSourceOrder(t *testing.T) {
	journal, errs := parser.Parse(`2024-01-15 test
    assets:cash  $10
    assets:bank  EUR 20
    equity:opening
`)
	require.Empty(t, errs)

	effects := CalculatePostingEffects(journal)
	require.Len(t, effects, 3)
	assert.Equal(t, 0, effects[0].PostingIndex)
	assert.Equal(t, 1, effects[1].PostingIndex)
	assert.Equal(t, 2, effects[2].PostingIndex)
	assert.Equal(t, decimal.NewFromInt(-10), effects[2].InferredAmounts["$"])
	assert.Equal(t, decimal.NewFromInt(-20), effects[2].InferredAmounts["EUR"])
}
