package formatter

import (
	"testing"

	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
