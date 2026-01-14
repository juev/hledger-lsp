package analyzer

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/parser"
)

func TestCheckBalance_SimpleBalanced(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $50
    assets:cash  $-50`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	result := CheckBalance(&journal.Transactions[0])

	assert.True(t, result.Balanced)
	assert.Empty(t, result.Differences)
}

func TestCheckBalance_InferredAmount(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	result := CheckBalance(&journal.Transactions[0])

	assert.True(t, result.Balanced)
	assert.Equal(t, 1, result.InferredIdx)
}

func TestCheckBalance_Unbalanced(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $50
    assets:cash  $-40`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	result := CheckBalance(&journal.Transactions[0])

	assert.False(t, result.Balanced)
	assert.Equal(t, decimal.NewFromInt(10), result.Differences["$"])
}

func TestCheckBalance_MultiCommodity(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $50
    expenses:rent  EUR 100
    assets:cash  $-50
    assets:bank  EUR -100`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	result := CheckBalance(&journal.Transactions[0])

	assert.True(t, result.Balanced)
}

func TestCheckBalance_MultiCommodity_Unbalanced(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $50
    expenses:rent  EUR 100
    assets:cash  $-50
    assets:bank  EUR -90`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	result := CheckBalance(&journal.Transactions[0])

	assert.False(t, result.Balanced)
	assert.Equal(t, decimal.NewFromInt(10), result.Differences["EUR"])
}

func TestCheckBalance_MultipleInferred_Error(t *testing.T) {
	input := `2024-01-15 test
    expenses:food
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	result := CheckBalance(&journal.Transactions[0])

	assert.False(t, result.Balanced)
}

func TestCheckBalance_WithCost_UnitPrice(t *testing.T) {
	input := `2024-01-15 buy stocks
    assets:stocks  10 AAPL @ $150
    assets:cash  $-1500`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	result := CheckBalance(&journal.Transactions[0])

	assert.True(t, result.Balanced)
}

func TestCheckBalance_WithCost_TotalPrice(t *testing.T) {
	input := `2024-01-15 buy stocks
    assets:stocks  10 AAPL @@ $1500
    assets:cash  $-1500`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	result := CheckBalance(&journal.Transactions[0])

	assert.True(t, result.Balanced)
}

func TestCheckBalance_VirtualUnbalanced_Exempt(t *testing.T) {
	t.Skip("Parser does not yet support virtual postings (task 3.4)")
}

func TestCheckBalance_ZeroAmount(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $0
    assets:cash  $0`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	result := CheckBalance(&journal.Transactions[0])

	assert.True(t, result.Balanced)
}

func TestCheckBalance_NegativeAmounts(t *testing.T) {
	input := `2024-01-15 refund
    assets:cash  $100
    expenses:food  $-100`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	result := CheckBalance(&journal.Transactions[0])

	assert.True(t, result.Balanced)
}

func TestCheckBalance_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		balanced bool
	}{
		{
			name: "simple balanced",
			input: `2024-01-15 test
    expenses:food  $50
    assets:cash  $-50`,
			balanced: true,
		},
		{
			name: "inferred single posting",
			input: `2024-01-15 test
    expenses:food  $50
    assets:cash`,
			balanced: true,
		},
		{
			name: "unbalanced by $10",
			input: `2024-01-15 test
    expenses:food  $50
    assets:cash  $-40`,
			balanced: false,
		},
		{
			name: "three postings balanced",
			input: `2024-01-15 test
    expenses:food  $30
    expenses:drinks  $20
    assets:cash  $-50`,
			balanced: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			journal, errs := parser.Parse(tt.input)
			require.Empty(t, errs)
			require.Len(t, journal.Transactions, 1)

			result := CheckBalance(&journal.Transactions[0])

			assert.Equal(t, tt.balanced, result.Balanced)
		})
	}
}

func TestCheckBalance_MultiCurrencyInferred(t *testing.T) {
	input := `2024-01-01 opening balances
    assets:bank  1000 RUB
    assets:cash  100 USD
    equity:opening`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	result := CheckBalance(&journal.Transactions[0])

	assert.True(t, result.Balanced, "multi-currency transaction with single inferred posting should be balanced")
	assert.Equal(t, 2, result.InferredIdx)
}

func TestCheckBalance_MultiCurrencyWithBalanceAssertion(t *testing.T) {
	input := `2024-01-01 opening balances
    assets:bank  1000 RUB = 1000 RUB
    assets:cash  100 USD = 100 USD
    equity:opening`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	result := CheckBalance(&journal.Transactions[0])

	assert.True(t, result.Balanced, "multi-currency with balance assertions should be balanced")
}

func TestCheckBalance_MultiCurrencyExplicitlyBalanced(t *testing.T) {
	input := `2024-01-01 test
    assets:bank  1000 RUB
    assets:cash  100 USD
    equity:rub  -1000 RUB
    equity:usd  -100 USD`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	result := CheckBalance(&journal.Transactions[0])

	assert.True(t, result.Balanced, "explicitly balanced multi-currency should be balanced")
}
