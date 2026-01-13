package analyzer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/parser"
)

func TestCollectAccounts_FromPostings(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	idx := CollectAccounts(journal)

	assert.Contains(t, idx.All, "expenses:food")
	assert.Contains(t, idx.All, "assets:cash")
}

func TestCollectAccounts_FromDirective(t *testing.T) {
	input := `account expenses:food
account assets:cash

2024-01-15 test
    expenses:food  $50
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	idx := CollectAccounts(journal)

	assert.Contains(t, idx.All, "expenses:food")
	assert.Contains(t, idx.All, "assets:cash")
	assert.Len(t, idx.All, 2)
}

func TestCollectAccounts_ByPrefix(t *testing.T) {
	input := `2024-01-15 test
    expenses:food:groceries  $30
    expenses:food:restaurant  $20
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	idx := CollectAccounts(journal)

	assert.Contains(t, idx.ByPrefix["expenses:"], "expenses:food:groceries")
	assert.Contains(t, idx.ByPrefix["expenses:"], "expenses:food:restaurant")
	assert.Contains(t, idx.ByPrefix["expenses:food:"], "expenses:food:groceries")
	assert.Contains(t, idx.ByPrefix["expenses:food:"], "expenses:food:restaurant")
	assert.Contains(t, idx.ByPrefix["assets:"], "assets:cash")
}

func TestCollectAccounts_NoDuplicates(t *testing.T) {
	input := `2024-01-15 test1
    expenses:food  $50
    assets:cash

2024-01-16 test2
    expenses:food  $30
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	idx := CollectAccounts(journal)

	count := 0
	for _, acc := range idx.All {
		if acc == "expenses:food" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestCollectPayees(t *testing.T) {
	input := `2024-01-15 Grocery Store | weekly
    expenses:food  $50
    assets:cash

2024-01-16 Coffee Shop
    expenses:food  $5
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	payees := CollectPayees(journal)

	assert.Contains(t, payees, "Grocery Store")
	assert.Contains(t, payees, "Coffee Shop")
}

func TestCollectPayees_NoDuplicates(t *testing.T) {
	input := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash

2024-01-16 Grocery Store
    expenses:food  $30
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	payees := CollectPayees(journal)

	count := 0
	for _, p := range payees {
		if p == "Grocery Store" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestCollectPayees_UsesDescriptionWhenNoPayee(t *testing.T) {
	input := `2024-01-15 grocery store
    expenses:food  $50
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	payees := CollectPayees(journal)

	assert.Contains(t, payees, "grocery store")
}

func TestCollectCommodities_FromAmount(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $50
    assets:cash  EUR -40`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	commodities := CollectCommodities(journal)

	assert.Contains(t, commodities, "$")
	assert.Contains(t, commodities, "EUR")
}

func TestCollectCommodities_FromDirective(t *testing.T) {
	input := `commodity $1000.00
commodity EUR 1000.00

2024-01-15 test
    expenses:food  $50
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	commodities := CollectCommodities(journal)

	assert.Contains(t, commodities, "$")
	assert.Contains(t, commodities, "EUR")
}

func TestCollectCommodities_NoDuplicates(t *testing.T) {
	input := `2024-01-15 test1
    expenses:food  $50
    assets:cash  $-50

2024-01-16 test2
    expenses:rent  $1000
    assets:bank  $-1000`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	commodities := CollectCommodities(journal)

	count := 0
	for _, c := range commodities {
		if c == "$" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestCollectTags_FromTransaction(t *testing.T) {
	t.Skip("Parser does not yet support tag extraction from comments (task 3.4)")
}

func TestCollectTags_FromPosting(t *testing.T) {
	t.Skip("Parser does not yet support tag extraction from comments (task 3.4)")
}

func TestCollectTags_NoDuplicates(t *testing.T) {
	t.Skip("Parser does not yet support tag extraction from comments (task 3.4)")
}

func TestCollectAll_EmptyJournal(t *testing.T) {
	input := ``

	journal, _ := parser.Parse(input)

	idx := CollectAccounts(journal)
	payees := CollectPayees(journal)
	commodities := CollectCommodities(journal)
	tags := CollectTags(journal)

	assert.Empty(t, idx.All)
	assert.Empty(t, payees)
	assert.Empty(t, commodities)
	assert.Empty(t, tags)
}
