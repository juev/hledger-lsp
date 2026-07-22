package analyzer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/parser"
)

func TestCollectPayeeAccounts_Empty(t *testing.T) {
	input := ``

	journal, _ := parser.Parse(input)

	result := CollectPayeeAccounts(journal)

	assert.Empty(t, result)
}

func TestCollectPayeeAccounts_NoPayee(t *testing.T) {
	input := `2024-01-15
    expenses:food  $50
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	result := CollectPayeeAccounts(journal)

	assert.Empty(t, result)
}

func TestCollectPayeeAccounts_SingleTransaction(t *testing.T) {
	input := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	result := CollectPayeeAccounts(journal)

	require.Contains(t, result, "Grocery Store")
	accounts := result["Grocery Store"]
	assert.Contains(t, accounts, "expenses:food")
	assert.Contains(t, accounts, "assets:cash")
	assert.Len(t, accounts, 2)
}

func TestCollectPayeeAccounts_MultipleTransactionsSamePayee(t *testing.T) {
	input := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash

2024-01-16 Grocery Store
    expenses:food  $30
    assets:bank`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	result := CollectPayeeAccounts(journal)

	require.Contains(t, result, "Grocery Store")
	accounts := result["Grocery Store"]
	assert.Contains(t, accounts, "expenses:food")
	assert.Contains(t, accounts, "assets:cash")
	assert.Contains(t, accounts, "assets:bank")
	assert.Len(t, accounts, 3, "Should deduplicate accounts but include all unique ones")
}

func TestCollectPayeeAccounts_MultiplePayees(t *testing.T) {
	input := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash

2024-01-16 Coffee Shop
    expenses:food  $5
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	result := CollectPayeeAccounts(journal)

	require.Contains(t, result, "Grocery Store")
	require.Contains(t, result, "Coffee Shop")

	assert.Contains(t, result["Grocery Store"], "expenses:food")
	assert.Contains(t, result["Coffee Shop"], "expenses:food")
}

func TestCollectPayeeAccounts_UsesDescriptionWhenNoPayee(t *testing.T) {
	input := `2024-01-15 grocery store
    expenses:food  $50
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	result := CollectPayeeAccounts(journal)

	require.Contains(t, result, "grocery store")
	assert.Contains(t, result["grocery store"], "expenses:food")
}

func TestCollectPayeeAccounts_Unicode(t *testing.T) {
	input := `2024-01-15 Пятёрочка
    expenses:food  $50
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	result := CollectPayeeAccounts(journal)

	require.Contains(t, result, "Пятёрочка")
	assert.Contains(t, result["Пятёрочка"], "expenses:food")
}

func TestCollectPayeeAccounts_UsesResolvedName(t *testing.T) {
	input := `apply account personal

2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash

end apply account`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	result := CollectPayeeAccounts(journal)

	require.Contains(t, result, "Grocery Store")
	accounts := result["Grocery Store"]
	assert.Contains(t, accounts, "personal:expenses:food")
	assert.Contains(t, accounts, "personal:assets:cash")
}

func TestCollectPayeeAccountPairUsage_Empty(t *testing.T) {
	input := ``

	journal, _ := parser.Parse(input)

	result := CollectPayeeAccountPairUsage(journal)

	assert.Empty(t, result)
}

func TestCollectPayeeAccountPairUsage_SingleTransaction(t *testing.T) {
	input := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	result := CollectPayeeAccountPairUsage(journal)

	assert.Equal(t, 1, result["Grocery Store::expenses:food"])
	assert.Equal(t, 1, result["Grocery Store::assets:cash"])
}

func TestCollectPayeeAccountPairUsage_MultipleTransactions(t *testing.T) {
	input := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash

2024-01-16 Grocery Store
    expenses:food  $30
    assets:cash

2024-01-17 Grocery Store
    expenses:food  $20
    assets:bank`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	result := CollectPayeeAccountPairUsage(journal)

	assert.Equal(t, 3, result["Grocery Store::expenses:food"])
	assert.Equal(t, 2, result["Grocery Store::assets:cash"])
	assert.Equal(t, 1, result["Grocery Store::assets:bank"])
}

func TestCollectPayeeAccountPairUsage_MultiplePayees(t *testing.T) {
	input := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash

2024-01-16 Coffee Shop
    expenses:food  $5
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	result := CollectPayeeAccountPairUsage(journal)

	assert.Equal(t, 1, result["Grocery Store::expenses:food"])
	assert.Equal(t, 1, result["Coffee Shop::expenses:food"])
}

func TestCollectPayeeAccountPairUsage_UsesResolvedName(t *testing.T) {
	input := `apply account personal

2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash

end apply account`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	result := CollectPayeeAccountPairUsage(journal)

	assert.Equal(t, 1, result["Grocery Store::personal:expenses:food"])
	assert.Equal(t, 1, result["Grocery Store::personal:assets:cash"])
}

func TestCollectPayeeAccountLastUsed_Empty(t *testing.T) {
	journal, _ := parser.Parse(``)

	result := CollectPayeeAccountLastUsed(journal)

	assert.Empty(t, result)
}

func TestCollectPayeeAccountLastUsed_SingleTransaction(t *testing.T) {
	input := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	result := CollectPayeeAccountLastUsed(journal)

	assert.Equal(t, "2024-01-15", result["Grocery Store::expenses:food"])
	assert.Equal(t, "2024-01-15", result["Grocery Store::assets:cash"])
}

func TestCollectPayeeAccountLastUsed_KeepsLatestDate(t *testing.T) {
	input := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash

2024-03-20 Grocery Store
    expenses:food  $30
    assets:bank

2024-02-10 Grocery Store
    expenses:food  $20
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	result := CollectPayeeAccountLastUsed(journal)

	assert.Equal(t, "2024-03-20", result["Grocery Store::expenses:food"])
	assert.Equal(t, "2024-02-10", result["Grocery Store::assets:cash"])
	assert.Equal(t, "2024-03-20", result["Grocery Store::assets:bank"])
}

func TestCollectPayeeAccountLastUsed_UsesDescriptionWhenNoPayee(t *testing.T) {
	input := `2024-06-01 coffee shop
    expenses:food  $5
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	result := CollectPayeeAccountLastUsed(journal)

	assert.Equal(t, "2024-06-01", result["coffee shop::expenses:food"])
}
