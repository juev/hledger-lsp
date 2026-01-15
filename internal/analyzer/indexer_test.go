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

func TestCollectAccounts_FromDirectiveOnly(t *testing.T) {
	input := `account assets:checking
account expenses:food`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	idx := CollectAccounts(journal)

	assert.Contains(t, idx.All, "assets:checking")
	assert.Contains(t, idx.All, "expenses:food")
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
	input := `2024-01-15 test  ; project:alpha, status:done
    expenses:food  $50
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	tags := CollectTags(journal)

	assert.Contains(t, tags, "project")
	assert.Contains(t, tags, "status")
}

func TestCollectTags_FromPosting(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $50  ; category:groceries
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	tags := CollectTags(journal)

	assert.Contains(t, tags, "category")
}

func TestCollectTags_NoDuplicates(t *testing.T) {
	input := `2024-01-15 test1  ; project:alpha
    expenses:food  $50
    assets:cash

2024-01-16 test2  ; project:beta
    expenses:rent  $1000
    assets:bank`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	tags := CollectTags(journal)

	count := 0
	for _, tag := range tags {
		if tag == "project" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestCollectTagValues_FromTransaction(t *testing.T) {
	input := `2024-01-15 test  ; project:alpha
    expenses:food  $50
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	tagValues := CollectTagValues(journal)

	require.Contains(t, tagValues, "project")
	assert.Contains(t, tagValues["project"], "alpha")
}

func TestCollectTagValues_FromPosting(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $50  ; category:groceries
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	tagValues := CollectTagValues(journal)

	require.Contains(t, tagValues, "category")
	assert.Contains(t, tagValues["category"], "groceries")
}

func TestCollectTagValues_GroupedByTagName(t *testing.T) {
	input := `2024-01-15 test1  ; project:alpha
    expenses:food  $50
    assets:cash

2024-01-16 test2  ; project:beta
    expenses:rent  $1000
    assets:bank`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	tagValues := CollectTagValues(journal)

	require.Contains(t, tagValues, "project")
	assert.Contains(t, tagValues["project"], "alpha")
	assert.Contains(t, tagValues["project"], "beta")
}

func TestCollectTagValues_NoDuplicates(t *testing.T) {
	input := `2024-01-15 test1  ; project:alpha
    expenses:food  $50
    assets:cash

2024-01-16 test2  ; project:alpha
    expenses:rent  $1000
    assets:bank`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	tagValues := CollectTagValues(journal)

	require.Contains(t, tagValues, "project")
	count := 0
	for _, v := range tagValues["project"] {
		if v == "alpha" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestCollectTagValues_EmptyValues(t *testing.T) {
	input := `2024-01-15 test  ; billable:
    expenses:food  $50
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	tagValues := CollectTagValues(journal)

	require.Contains(t, tagValues, "billable")
	assert.Empty(t, tagValues["billable"])
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
