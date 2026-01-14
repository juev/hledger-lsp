package formatter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/parser"
)

func TestCalculateAlignmentColumn(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name: "simple accounts",
			input: `2024-01-15 test
    expenses:food  $50
    assets:cash  $-50`,
			expected: 19,
		},
		{
			name: "longer account",
			input: `2024-01-15 test
    expenses:food:groceries  $50
    assets:cash  $-50`,
			expected: 29,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			journal, errs := parser.Parse(tt.input)
			require.Empty(t, errs)
			require.Len(t, journal.Transactions, 1)

			col := CalculateAlignmentColumn(journal.Transactions[0].Postings)
			assert.Equal(t, tt.expected, col)
		})
	}
}

func TestFormatPosting(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		alignCol int
		expected string
	}{
		{
			name: "simple posting",
			input: `2024-01-15 test
    expenses:food  $50
    assets:cash`,
			alignCol: 20,
			expected: "    expenses:food   $50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			journal, errs := parser.Parse(tt.input)
			require.Empty(t, errs)
			require.Len(t, journal.Transactions, 1)
			require.NotEmpty(t, journal.Transactions[0].Postings)

			result := FormatPosting(&journal.Transactions[0].Postings[0], tt.alignCol)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatDocument(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $50
    assets:cash  $-50`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	assert.NotEmpty(t, edits)
}

func TestFormatDocument_PostingWithoutAmount(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	assert.NotNil(t, edits)
}

func TestFormatDocument_MultipleTransactions(t *testing.T) {
	input := `2024-01-15 first
    expenses:food  $50
    assets:cash

2024-01-16 second
    expenses:rent  $1000
    assets:bank`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	assert.NotNil(t, edits)
}

func TestFormatDocument_EmptyDocument(t *testing.T) {
	journal, _ := parser.Parse("")
	edits := FormatDocument(journal, "")
	assert.Empty(t, edits)
}

func TestFormatDocument_WithCommodityFormat(t *testing.T) {
	input := `commodity RUB
  format 1 000,00 RUB

2024-01-15 test
    expenses:food  846 661,89 RUB
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)
	require.NotEmpty(t, journal.Transactions[0].Postings)

	edits := FormatDocument(journal, input)
	require.NotEmpty(t, edits)

	found := false
	for _, edit := range edits {
		if edit.NewText != "" && len(edit.NewText) > 0 {
			if edit.NewText == "    expenses:food  846 661,89 RUB" {
				found = true
				break
			}
		}
	}
	assert.True(t, found, "Expected formatted amount with commodity format")
}

func TestFormatDocument_PreservesRawQuantityWithoutCommodityDirective(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  1 000,50 EUR
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)
	require.NotEmpty(t, journal.Transactions[0].Postings)

	edits := FormatDocument(journal, input)
	require.NotEmpty(t, edits)

	found := false
	for _, edit := range edits {
		if edit.NewText != "" && len(edit.NewText) > 0 {
			if edit.NewText == "    expenses:food  1 000,50 EUR" {
				found = true
				break
			}
		}
	}
	assert.True(t, found, "Expected preserved raw quantity format")
}

func TestFormatDocument_WithCostCommodityFormat(t *testing.T) {
	input := `commodity EUR
  format 1 000,00 EUR

2024-01-15 buy bitcoin
    assets:crypto  1 BTC @ 45000,00 EUR
    assets:bank`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)
	require.NotEmpty(t, journal.Transactions[0].Postings)

	edits := FormatDocument(journal, input)
	require.NotEmpty(t, edits)

	found := false
	for _, edit := range edits {
		if edit.NewText != "" && len(edit.NewText) > 0 {
			if edit.NewText == "    assets:crypto  1 BTC @ 45 000,00 EUR" {
				found = true
				break
			}
		}
	}
	assert.True(t, found, "Expected formatted cost amount with commodity format, got edits: %v", edits)
}

func TestFormatDocument_WithBalanceAssertionCommodityFormat(t *testing.T) {
	input := `commodity EUR
  format 1 000,00 EUR

2024-01-15 test
    assets:bank  EUR 100 = 1000,00 EUR
    expenses:food`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)
	require.NotEmpty(t, journal.Transactions[0].Postings)

	edits := FormatDocument(journal, input)
	require.NotEmpty(t, edits)

	found := false
	for _, edit := range edits {
		if edit.NewText != "" && len(edit.NewText) > 0 {
			if edit.NewText == "    assets:bank    EUR100,00 = 1 000,00 EUR" {
				found = true
				break
			}
		}
	}
	assert.True(t, found, "Expected formatted balance assertion with commodity format, got edits: %v", edits)
}
