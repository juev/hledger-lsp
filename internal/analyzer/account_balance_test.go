package analyzer

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/parser"
)

func TestCalculateAccountBalances_SingleTransaction(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $50
    assets:cash  $-50`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	balances := CalculateAccountBalances(journal)

	assert.Equal(t, decimal.NewFromInt(50), balances["expenses:food"]["$"])
	assert.Equal(t, decimal.NewFromInt(-50), balances["assets:cash"]["$"])
}

func TestCalculateAccountBalances_MultipleTransactions(t *testing.T) {
	input := `2024-01-15 grocery
    expenses:food  $50
    assets:cash  $-50

2024-01-16 restaurant
    expenses:food  $30
    assets:cash  $-30`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	balances := CalculateAccountBalances(journal)

	assert.Equal(t, decimal.NewFromInt(80), balances["expenses:food"]["$"])
	assert.Equal(t, decimal.NewFromInt(-80), balances["assets:cash"]["$"])
}

func TestCalculateAccountBalances_MultiCommodity(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $50
    expenses:food  EUR 20
    assets:cash  $-50
    assets:bank  EUR -20`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	balances := CalculateAccountBalances(journal)

	assert.Equal(t, decimal.NewFromInt(50), balances["expenses:food"]["$"])
	assert.Equal(t, decimal.NewFromInt(20), balances["expenses:food"]["EUR"])
	assert.Equal(t, decimal.NewFromInt(-50), balances["assets:cash"]["$"])
	assert.Equal(t, decimal.NewFromInt(-20), balances["assets:bank"]["EUR"])
}

func TestCalculateAccountBalances_InferredAmount(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	balances := CalculateAccountBalances(journal)

	assert.Equal(t, decimal.NewFromInt(50), balances["expenses:food"]["$"])
	_, hasCash := balances["assets:cash"]
	assert.False(t, hasCash)
}

func TestCalculateAccountBalances_EmptyJournal(t *testing.T) {
	input := ``

	journal, _ := parser.Parse(input)

	balances := CalculateAccountBalances(journal)

	assert.Empty(t, balances)
}

func TestCalculateAccountBalances_WithCost(t *testing.T) {
	input := `2024-01-15 buy stocks
    assets:stocks  10 AAPL @ $150
    assets:cash  $-1500`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	balances := CalculateAccountBalances(journal)

	assert.Equal(t, decimal.NewFromInt(10), balances["assets:stocks"]["AAPL"])
	assert.Equal(t, decimal.NewFromInt(-1500), balances["assets:cash"]["$"])
}

func TestCalculateAccountBalances_ZeroBalance(t *testing.T) {
	input := `2024-01-15 buy
    expenses:food  $50
    assets:cash  $-50

2024-01-16 refund
    expenses:food  $-50
    assets:cash  $50`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	balances := CalculateAccountBalances(journal)

	assert.True(t, balances["expenses:food"]["$"].IsZero())
	assert.True(t, balances["assets:cash"]["$"].IsZero())
}
