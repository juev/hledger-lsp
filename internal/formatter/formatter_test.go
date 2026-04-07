package formatter

import (
	"sort"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/lsputil"
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

func TestDetectExistingAmountColumn(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "empty journal",
			input:    "",
			expected: 0,
		},
		{
			name: "no amounts",
			input: `2024-01-15 test
    expenses:food
    assets:cash`,
			expected: 0,
		},
		{
			name: "single posting with amount",
			input: `2024-01-15 test
    expenses:food  $50.00
    assets:cash`,
			// "    expenses:food  $50.00"
			// 0123456789012345678901
			// $ at 0-indexed col 19
			expected: 19,
		},
		{
			name: "multiple postings same column",
			input: `2024-01-15 test
    expenses:food  $50.00
    assets:cash    $-50.00`,
			// $ at col 19 in both postings (cash padded with extra spaces)
			expected: 19,
		},
		{
			name: "multiple postings different columns picks MIN",
			input: `2024-01-15 first
    expenses:food                $50.00
    assets:cash

2024-01-16 second
    expenses:food:coffee          $5.00
    assets:cash`,
			// First posting: 4 indent + 13 "expenses:food" + 16 spaces = $ at col 33
			// Second posting: 4 indent + 20 "expenses:food:coffee" + 10 spaces = $ at col 34
			// MIN = 33 (canonical accountCol — both amounts end at col 39 in right-align)
			expected: 33,
		},
		{
			name: "cyrillic account uses rune column",
			input: `2024-01-15 test
    активы:наличные  $50`,
			// 4 indent + 15 runes "активы:наличные" (6+1+8) + 2 spaces = $ at col 21 (0-indexed)
			expected: 21,
		},
		{
			name: "mix of postings with and without amounts",
			input: `2024-01-15 test
    expenses:food
    assets:cash  $-50.00`,
			// First has no amount → skip
			// Second: 4 + 11 + 2 = 17
			expected: 17,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			journal, errs := parser.Parse(tt.input)
			require.Empty(t, errs)

			col := DetectExistingAmountColumn(journal.Transactions)
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
			if edit.NewText == "    assets:bank    100,00 EUR  = 1 000,00 EUR" {
				found = true
				break
			}
		}
	}
	assert.True(t, found, "Expected formatted balance assertion with commodity format, got edits: %v", edits)
}

func TestFormatDocument_BalanceAssertionAlignment(t *testing.T) {
	input := `2024-01-15 opening
    assets:bank:checking  100 USD = 1000 USD
    assets:cash  50 USD = 50 USD
    equity:opening`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	edits := FormatDocument(journal, input)
	require.NotEmpty(t, edits)

	var formattedLines []string
	for _, edit := range edits {
		if edit.NewText != "" {
			formattedLines = append(formattedLines, edit.NewText)
		}
	}

	require.Len(t, formattedLines, 3, "Expected 3 formatted postings")

	line1 := formattedLines[0]
	line2 := formattedLines[1]

	idx1 := findEqualSignIndex(line1)
	idx2 := findEqualSignIndex(line2)

	require.NotEqual(t, -1, idx1, "First line should have = sign")
	require.NotEqual(t, -1, idx2, "Second line should have = sign")
	assert.Equal(t, idx1, idx2, "= signs should be aligned at the same column, got %d and %d", idx1, idx2)
}

func findEqualSignIndex(s string) int {
	for i, r := range s {
		if r == '=' {
			return i
		}
	}
	return -1
}

func TestFormatDocument_InclusiveBalanceAssertion(t *testing.T) {
	input := `2024-01-15 check
    assets:checking  $100 =* $1000
    income:salary`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, "=* $1000")
	assert.NotContains(t, result, "== ")

	journal2, errs2 := parser.Parse(result)
	require.Empty(t, errs2)
	edits2 := FormatDocument(journal2, result)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "formatting should be idempotent")
}

func TestFormatDocument_ExactInclusiveBalanceAssertion(t *testing.T) {
	input := `2024-01-15 check
    assets:checking  $100 ==* $1000
    income:salary`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, "==* $1000")

	journal2, errs2 := parser.Parse(result)
	require.Empty(t, errs2)
	edits2 := FormatDocument(journal2, result)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "formatting should be idempotent")
}

func TestFormatDocument_GlobalAlignment(t *testing.T) {
	input := `2024-01-15 first
    short:a  100 RUB
    assets:cash

2024-01-16 second
    very:long:account:name  500 RUB
    assets:bank

2024-01-17 third
    mid:acc  200 RUB
    assets:wallet`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 3)

	edits := FormatDocument(journal, input)
	require.NotEmpty(t, edits)

	var amountPositions []int
	for _, edit := range edits {
		if edit.NewText != "" && containsAmount(edit.NewText) {
			pos := findAmountPosition(edit.NewText)
			if pos > 0 {
				amountPositions = append(amountPositions, pos)
			}
		}
	}

	require.GreaterOrEqual(t, len(amountPositions), 3, "Expected at least 3 postings with amounts")

	firstPos := amountPositions[0]
	for i, pos := range amountPositions {
		assert.Equal(t, firstPos, pos, "All amounts should be at the same column, posting %d is at %d, expected %d", i, pos, firstPos)
	}
}

func containsAmount(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func findAmountPosition(s string) int {
	inSpaces := false
	for i, r := range s {
		if r == ' ' {
			inSpaces = true
		} else if inSpaces && (r >= '0' && r <= '9') {
			return i
		} else {
			inSpaces = false
		}
	}
	return -1
}

func TestFormatDocumentWithOptions_PreservesExistingAlignment(t *testing.T) {
	// Hand-formatted journal with amounts at col 33 and col 34.
	// Formula natural = 4 + 20 ("expenses:food:coffee") + 2 = 26.
	// MIN detected from existing amounts = 33 (leftmost = widest amount).
	// Expected: Format preserves col 33 (canonical accountCol), not collapsing to 26.
	input := `2024-01-15 * grocery store
    expenses:food                $50.00
    assets:cash

2024-01-16 * coffee shop
    expenses:food:coffee          $5.00
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	opts := Options{IndentSize: 4, AlignAmounts: true, MinAlignmentColumn: 0}
	edits := FormatDocumentWithOptions(journal, input, nil, opts)

	// Find edits that contain a $ amount and check its position
	foundAmountEdit := false
	for _, edit := range edits {
		dollarPos := strings.Index(edit.NewText, "$")
		if dollarPos < 0 {
			continue
		}
		foundAmountEdit = true
		// 0-indexed rune col should be 33: leftmost existing amount preserved
		assert.Equal(t, 33, dollarPos,
			"Format should preserve detected MIN amount column (33), not collapse to formula natural (26). Edit: %q", edit.NewText)
	}
	require.True(t, foundAmountEdit, "expected at least one edit containing a $ amount")
}

func TestFormatDocument_GlobalAlignment_EdgeCases(t *testing.T) {
	t.Run("transactions with different posting counts", func(t *testing.T) {
		input := `2024-01-15 single posting
    very:long:account:name  100 RUB

2024-01-16 three postings
    short:a  50 RUB
    short:b  30 RUB
    short:c  20 RUB`

		journal, errs := parser.Parse(input)
		require.Empty(t, errs)

		edits := FormatDocument(journal, input)
		require.NotEmpty(t, edits)

		var positions []int
		for _, edit := range edits {
			if pos := findAmountPosition(edit.NewText); pos > 0 {
				positions = append(positions, pos)
			}
		}

		require.GreaterOrEqual(t, len(positions), 4)
		for i, pos := range positions {
			assert.Equal(t, positions[0], pos, "posting %d misaligned", i)
		}
	})

	t.Run("postings without amounts", func(t *testing.T) {
		input := `2024-01-15 test
    very:long:account:name  100 RUB
    short:a

2024-01-16 test2
    mid:account  50 RUB
    assets:bank`

		journal, errs := parser.Parse(input)
		require.Empty(t, errs)

		edits := FormatDocument(journal, input)
		require.NotEmpty(t, edits)

		var positions []int
		for _, edit := range edits {
			if pos := findAmountPosition(edit.NewText); pos > 0 {
				positions = append(positions, pos)
			}
		}

		require.GreaterOrEqual(t, len(positions), 2)
		for i, pos := range positions {
			assert.Equal(t, positions[0], pos, "posting %d misaligned", i)
		}
	})

	t.Run("with costs and balance assertions", func(t *testing.T) {
		input := `2024-01-15 buy
    assets:crypto  1 BTC @ $50000
    assets:bank

2024-01-16 check
    very:long:account:name  100 USD = 1000 USD
    equity:opening`

		journal, errs := parser.Parse(input)
		require.Empty(t, errs)

		edits := FormatDocument(journal, input)
		require.NotEmpty(t, edits)

		var positions []int
		for _, edit := range edits {
			if pos := findAmountPosition(edit.NewText); pos > 0 {
				positions = append(positions, pos)
			}
		}

		require.GreaterOrEqual(t, len(positions), 2)
		for i, pos := range positions {
			assert.Equal(t, positions[0], pos, "posting %d misaligned", i)
		}
	})
}

func TestFormatDocumentWithOptions_IndentSize(t *testing.T) {
	journal, _ := parser.Parse(`2024-01-15 test
    expenses:food  $50
    assets:cash`)

	t.Run("custom indent size 2", func(t *testing.T) {
		opts := Options{IndentSize: 2, AlignAmounts: true}
		edits := FormatDocumentWithOptions(journal, "", nil, opts)

		require.NotEmpty(t, edits)
		assert.True(t, strings.HasPrefix(edits[0].NewText, "  "),
			"should use 2-space indent")
		assert.False(t, strings.HasPrefix(edits[0].NewText, "    "),
			"should not use 4-space indent")
	})

	t.Run("custom indent size 8", func(t *testing.T) {
		opts := Options{IndentSize: 8, AlignAmounts: true}
		edits := FormatDocumentWithOptions(journal, "", nil, opts)

		require.NotEmpty(t, edits)
		assert.True(t, strings.HasPrefix(edits[0].NewText, "        "),
			"should use 8-space indent")
	})
}

func TestFormatDocumentWithOptions_AlignAmounts(t *testing.T) {
	input := `2024-01-15 test
    short:a  100 RUB
    very:long:account:name  500 RUB`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	t.Run("align amounts enabled", func(t *testing.T) {
		opts := Options{IndentSize: 4, AlignAmounts: true}
		edits := FormatDocumentWithOptions(journal, input, nil, opts)

		require.Len(t, edits, 2)

		pos1 := findAmountPosition(edits[0].NewText)
		pos2 := findAmountPosition(edits[1].NewText)

		require.NotEqual(t, -1, pos1)
		require.NotEqual(t, -1, pos2)
		assert.Equal(t, pos1, pos2, "amounts should be aligned at same column")
	})

	t.Run("align amounts disabled", func(t *testing.T) {
		opts := Options{IndentSize: 4, AlignAmounts: false}
		edits := FormatDocumentWithOptions(journal, input, nil, opts)

		require.Len(t, edits, 2)

		pos1 := findAmountPosition(edits[0].NewText)
		pos2 := findAmountPosition(edits[1].NewText)

		require.NotEqual(t, -1, pos1)
		require.NotEqual(t, -1, pos2)
		assert.NotEqual(t, pos1, pos2, "amounts should NOT be aligned when disabled")

		assert.Contains(t, edits[0].NewText, "short:a  100",
			"short account should have only 2 spaces before amount")
		assert.Contains(t, edits[1].NewText, "very:long:account:name  500",
			"long account should have only 2 spaces before amount")
	})
}

func TestFormatDocumentWithOptions_MinAlignmentColumn(t *testing.T) {
	input := `2024-01-15 test
    short:a  100 RUB
    very:long:account:name  500 RUB`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	t.Run("min alignment column 0 uses pure auto calculation", func(t *testing.T) {
		opts := Options{IndentSize: 4, AlignAmounts: true, MinAlignmentColumn: 0}
		edits := FormatDocumentWithOptions(journal, input, nil, opts)

		require.Len(t, edits, 2)

		pos1 := findAmountPosition(edits[0].NewText)
		pos2 := findAmountPosition(edits[1].NewText)

		assert.Equal(t, pos1, pos2, "amounts should be aligned")
	})

	t.Run("min alignment column larger than auto uses minimum", func(t *testing.T) {
		opts := Options{IndentSize: 4, AlignAmounts: true, MinAlignmentColumn: 50}
		edits := FormatDocumentWithOptions(journal, input, nil, opts)

		require.Len(t, edits, 2)

		pos1 := findAmountPosition(edits[0].NewText)
		pos2 := findAmountPosition(edits[1].NewText)

		assert.Equal(t, 49, pos1, "amount should be at column 50 (0-indexed: 49)")
		assert.Equal(t, 49, pos2, "amount should be at column 50 (0-indexed: 49)")
	})

	t.Run("min alignment column smaller than auto uses auto", func(t *testing.T) {
		opts := Options{IndentSize: 4, AlignAmounts: true, MinAlignmentColumn: 10}
		edits := FormatDocumentWithOptions(journal, input, nil, opts)

		require.Len(t, edits, 2)

		pos1 := findAmountPosition(edits[0].NewText)
		pos2 := findAmountPosition(edits[1].NewText)

		assert.Equal(t, pos1, pos2, "amounts should be aligned at auto-calculated column")
		assert.Greater(t, pos1, 10, "auto-calculated column should be greater than min")
	})
}

func TestFormatDocument_TrimsTrailingSpaces(t *testing.T) {
	input := "2024-01-15 test   \n    expenses:food  $50  \n    assets:cash   "

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	require.NotEmpty(t, edits)

	result := applyEdits(input, edits)

	lines := strings.Split(result, "\n")
	for i, line := range lines {
		assert.Equal(t, strings.TrimRight(line, " \t"), line,
			"line %d should have no trailing spaces: %q", i, line)
	}
}

func TestFormatDocument_TrimsEmptyLinesWithSpaces(t *testing.T) {
	input := "2024-01-15 test\n    expenses:food  $50\n   \n    assets:cash"

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	require.NotEmpty(t, edits)

	result := applyEdits(input, edits)

	lines := strings.Split(result, "\n")
	require.GreaterOrEqual(t, len(lines), 3)
	assert.Equal(t, "", lines[2], "empty line with spaces should become truly empty")
}

func TestFormatDocument_TrimsTransactionHeader(t *testing.T) {
	input := "2024-01-15 test with trailing spaces   \n    expenses:food  $50\n    assets:cash"

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	require.NotEmpty(t, edits)

	result := applyEdits(input, edits)

	lines := strings.Split(result, "\n")
	assert.Equal(t, "2024-01-15 test with trailing spaces", lines[0],
		"transaction header should have no trailing spaces")
}

func TestFormatDocument_TrimsComments(t *testing.T) {
	input := "; this is a comment   \n2024-01-15 test\n    expenses:food  $50\n    assets:cash"

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	require.NotEmpty(t, edits)

	result := applyEdits(input, edits)

	lines := strings.Split(result, "\n")
	assert.Equal(t, "; this is a comment", lines[0],
		"comment line should have no trailing spaces")
}

func TestFormatDocument_TrimsDirectives(t *testing.T) {
	input := "account expenses:food   \n\n2024-01-15 test\n    expenses:food  $50\n    assets:cash"

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	require.NotEmpty(t, edits)

	result := applyEdits(input, edits)

	lines := strings.Split(result, "\n")
	assert.Equal(t, "account expenses:food", lines[0],
		"directive line should have no trailing spaces")
}

func TestFormatDocument_TrimsNonASCIIText(t *testing.T) {
	input := "2024-01-15 Покупка в магазине   \n    расходы:еда  100 RUB  \n    активы:наличные"

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	require.NotEmpty(t, edits)

	result := applyEdits(input, edits)

	lines := strings.Split(result, "\n")
	for i, line := range lines {
		assert.Equal(t, strings.TrimRight(line, " \t"), line,
			"line %d should have no trailing spaces: %q", i, line)
	}

	assert.Equal(t, "2024-01-15 Покупка в магазине", lines[0],
		"Cyrillic transaction header should have trailing spaces removed")
}

func TestFormatDocument_PreservesSignBeforeCommodity(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "-MAU66 preserves sign before commodity with space",
			input: `2024-01-15 test
    expenses:food  -MAU66
    assets:cash`,
			expected: "    expenses:food  -MAU 66",
		},
		{
			name: "MAU-66 preserves sign after commodity with space",
			input: `2024-01-15 test
    expenses:food  MAU-66
    assets:cash`,
			expected: "    expenses:food  MAU -66",
		},
		{
			name: "-$100 preserves sign before symbol",
			input: `2024-01-15 test
    expenses:food  -$100
    assets:cash`,
			expected: "    expenses:food  -$100",
		},
		{
			name: "$-100 preserves sign after symbol",
			input: `2024-01-15 test
    expenses:food  $-100
    assets:cash`,
			expected: "    expenses:food  $-100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			journal, errs := parser.Parse(tt.input)
			require.Empty(t, errs, "parsing should succeed")
			require.Len(t, journal.Transactions, 1)

			edits := FormatDocument(journal, tt.input)
			require.NotEmpty(t, edits, "should produce formatting edits")

			formattedPosting := edits[0].NewText
			assert.Equal(t, tt.expected, formattedPosting,
				"sign position relative to commodity should be preserved")
		})
	}
}

func TestFormatDocument_AmountFormatVariations(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantAmountInOut bool
	}{
		{
			name: "-USD222 format",
			input: `2024-01-15 test
    expenses:food  -USD222
    assets:cash
`,
			wantAmountInOut: true,
		},
		{
			name: "USD222 format",
			input: `2024-01-15 test
    expenses:food  USD222
    assets:cash
`,
			wantAmountInOut: true,
		},
		{
			name: "USD-222 format",
			input: `2024-01-15 test
    expenses:food  USD-222
    assets:cash
`,
			wantAmountInOut: true,
		},
		{
			name: "$-100 format",
			input: `2024-01-15 test
    expenses:food  $-100
    assets:cash
`,
			wantAmountInOut: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			journal, errs := parser.Parse(tt.input)
			require.Empty(t, errs, "parsing should succeed")
			require.Len(t, journal.Transactions, 1)

			posting := journal.Transactions[0].Postings[0]
			if tt.wantAmountInOut {
				require.NotNil(t, posting.Amount, "amount should not be nil after parsing")
			}

			edits := FormatDocument(journal, tt.input)
			require.NotEmpty(t, edits, "should produce formatting edits")

			formattedPosting := edits[0].NewText
			if tt.wantAmountInOut {
				assert.NotEqual(t, "    expenses:food", formattedPosting,
					"amount should not be deleted during formatting")
			}
		})
	}
}

func TestFormatDocument_WithInlineCommodityFormat(t *testing.T) {
	input := `commodity 1 000,00 RUB

2024-01-15 test
    expenses:food  846661.89 RUB
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	edits := FormatDocument(journal, input)
	require.NotEmpty(t, edits)

	found := false
	for _, edit := range edits {
		if edit.NewText != "" && strings.Contains(edit.NewText, "846 661,89 RUB") {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected amount formatted with inline commodity format (846 661,89 RUB), got edits: %v", edits)
}

func TestFormatDocument_WithDefaultCommodityFormat(t *testing.T) {
	input := `D $1,000.00

2024-01-15 test
    expenses:food  $1234.56
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	edits := FormatDocument(journal, input)
	require.NotEmpty(t, edits)

	found := false
	for _, edit := range edits {
		if edit.NewText != "" && strings.Contains(edit.NewText, "$1,234.56") {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected amount formatted with default commodity format ($1,234.56), got edits: %v", edits)
}

func TestFormatDocument_DefaultFormatFallback(t *testing.T) {
	input := `D 1 000,00 RUB

2024-01-15 test
    expenses:food  846661,89 USD
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	edits := FormatDocument(journal, input)
	require.NotEmpty(t, edits)

	found := false
	for _, edit := range edits {
		if edit.NewText != "" && strings.Contains(edit.NewText, "846 661,89 USD") {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected amount formatted with default format fallback (846 661,89 USD), got edits: %v", edits)
}

func TestFormatDocument_NoCommodityAmount(t *testing.T) {
	t.Run("basic no commodity amount", func(t *testing.T) {
		input := `Y2019

12/31 * Apple
    Расходы:Развлечения:Музыка       169
    Активы:Тинькофф:Текущий`

		journal, errs := parser.Parse(input)
		require.Empty(t, errs)

		posting := journal.Transactions[0].Postings[0]
		require.NotNil(t, posting.Amount, "amount must be parsed")
		assert.Equal(t, "", posting.Amount.Commodity.Symbol, "amount should have empty commodity symbol")
		assert.Equal(t, ast.CommodityLeft, posting.Amount.Commodity.Position, "default position should be CommodityLeft")
		assert.Equal(t, "169", posting.Amount.Quantity.String(), "amount quantity should be 169")

		edits := FormatDocument(journal, input)
		result := applyEdits(input, edits)

		assert.Contains(t, result, "169", "amount should be preserved in formatted output")

		lines := strings.Split(result, "\n")
		require.GreaterOrEqual(t, len(lines), 4)

		postingLine := lines[3]
		assert.Contains(t, postingLine, "169", "posting line should contain amount 169")
		assert.Contains(t, postingLine, "Расходы:Развлечения:Музыка", "posting line should contain account name")

		t.Logf("Formatted posting line: %q", postingLine)
	})

	t.Run("no commodity with multiple transactions for global alignment", func(t *testing.T) {
		input := `Y2019

12/30 * Transaction 1
    Short:Account  100
    Assets:Cash

12/31 * Apple
    Расходы:Развлечения:Музыка       169
    Активы:Тинькофф:Текущий`

		journal, errs := parser.Parse(input)
		require.Empty(t, errs)

		edits := FormatDocument(journal, input)
		result := applyEdits(input, edits)

		assert.Contains(t, result, "100", "first amount should be preserved")
		assert.Contains(t, result, "169", "second amount should be preserved")

		lines := strings.Split(result, "\n")
		t.Logf("Formatted result:\n%s", result)

		for i, line := range lines {
			if strings.Contains(line, "Расходы:Развлечения:Музыка") {
				assert.Contains(t, line, "169", "line %d should contain amount 169: %q", i, line)
			}
		}
	})
}

func applyEdits(content string, edits []protocol.TextEdit) string {
	sort.Slice(edits, func(i, j int) bool {
		if edits[i].Range.Start.Line != edits[j].Range.Start.Line {
			return edits[i].Range.Start.Line > edits[j].Range.Start.Line
		}
		return edits[i].Range.Start.Character > edits[j].Range.Start.Character
	})

	result := content
	for _, edit := range edits {
		lines := strings.Split(result, "\n")

		startLine := int(edit.Range.Start.Line)
		endLine := int(edit.Range.End.Line)

		if startLine >= len(lines) {
			continue
		}

		if startLine == endLine {
			line := lines[startLine]
			startByte := lsputil.UTF16OffsetToByteOffset(line, int(edit.Range.Start.Character))
			endByte := lsputil.UTF16OffsetToByteOffset(line, int(edit.Range.End.Character))
			if startByte > len(line) {
				startByte = len(line)
			}
			if endByte > len(line) {
				endByte = len(line)
			}
			lines[startLine] = line[:startByte] + edit.NewText + line[endByte:]
		} else {
			startLineContent := lines[startLine]
			startByte := lsputil.UTF16OffsetToByteOffset(startLineContent, int(edit.Range.Start.Character))
			if startByte > len(startLineContent) {
				startByte = len(startLineContent)
			}
			endLineContent := ""
			endByte := 0
			if endLine < len(lines) {
				endLineContent = lines[endLine]
				endByte = lsputil.UTF16OffsetToByteOffset(endLineContent, int(edit.Range.End.Character))
				if endByte > len(endLineContent) {
					endByte = len(endLineContent)
				}
			}

			newLine := startLineContent[:startByte] + edit.NewText + endLineContent[endByte:]
			newLines := append(lines[:startLine], newLine)
			if endLine+1 < len(lines) {
				newLines = append(newLines, lines[endLine+1:]...)
			}
			lines = newLines
		}
		result = strings.Join(lines, "\n")
	}
	return result
}

func TestFormatter_ApplyAccountPreservesOriginalNames(t *testing.T) {
	input := `apply account business

2024-01-15 Sale
    revenue                                $100
    checking

end apply account
`
	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	// Format the posting
	posting := &journal.Transactions[0].Postings[0]
	result := FormatPosting(posting, 40)

	// Formatting should preserve original name without prefix
	assert.Contains(t, result, "revenue")
	assert.NotContains(t, result, "business:revenue")
}

func TestFormatDocument_CommodityPositionFromFormatDirective(t *testing.T) {
	input := `commodity RUB
  format 1.000,00 RUB

2024-01-15 test
    expenses:food  RUB 43
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	edits := FormatDocument(journal, input)
	require.NotEmpty(t, edits)

	result := applyEdits(input, edits)

	assert.Contains(t, result, "43,00 RUB",
		"format directive says '1.000,00 RUB' so commodity should be right with space, got: %s", result)
	assert.NotContains(t, result, "RUB43",
		"should not have commodity glued to number without space")
}

func TestExtractCommodityFormats(t *testing.T) {
	t.Run("commodity directive", func(t *testing.T) {
		directives := []ast.Directive{
			ast.CommodityDirective{
				Commodity: ast.Commodity{Symbol: "RUB"},
				Format:    "1.000,00 RUB",
			},
		}
		formats := ExtractCommodityFormats(directives)
		require.Contains(t, formats, "RUB")
		assert.Equal(t, ',', formats["RUB"].DecimalMark)
		assert.Equal(t, ".", formats["RUB"].ThousandsSep)
		assert.Equal(t, ast.CommodityRight, formats["RUB"].Position)
		assert.True(t, formats["RUB"].SpaceBetween)
	})

	t.Run("default commodity directive", func(t *testing.T) {
		directives := []ast.Directive{
			ast.DefaultCommodityDirective{Symbol: "EUR", Format: "1.000,00 EUR"},
		}
		formats := ExtractCommodityFormats(directives)
		require.Contains(t, formats, "EUR")
		require.Contains(t, formats, "")
		assert.Equal(t, ',', formats[""].DecimalMark)
	})

	t.Run("nil directives returns empty map", func(t *testing.T) {
		formats := ExtractCommodityFormats(nil)
		assert.Empty(t, formats)
	})

	t.Run("last default wins", func(t *testing.T) {
		directives := []ast.Directive{
			ast.DefaultCommodityDirective{Symbol: "EUR", Format: "1.000,00 EUR"},
			ast.DefaultCommodityDirective{Symbol: "$", Format: "$1,000.00"},
		}
		formats := ExtractCommodityFormats(directives)
		assert.Equal(t, '.', formats[""].DecimalMark)
		assert.Equal(t, ",", formats[""].ThousandsSep)
	})

	t.Run("decimal-mark directive sets fallback format", func(t *testing.T) {
		directives := []ast.Directive{
			ast.DecimalMarkDirective{Mark: ","},
		}
		formats := ExtractCommodityFormats(directives)
		require.Contains(t, formats, "")
		assert.Equal(t, ',', formats[""].DecimalMark)
		assert.Equal(t, ".", formats[""].ThousandsSep)
		assert.Equal(t, 0, formats[""].DecimalPlaces)
	})

	t.Run("D directive overrides decimal-mark for default key", func(t *testing.T) {
		directives := []ast.Directive{
			ast.DecimalMarkDirective{Mark: ","},
			ast.DefaultCommodityDirective{Symbol: "$", Format: "$1,000.00"},
		}
		formats := ExtractCommodityFormats(directives)
		require.Contains(t, formats, "")
		assert.Equal(t, '.', formats[""].DecimalMark,
			"D directive should override decimal-mark for the default format")
	})

	t.Run("commodity-specific format overrides decimal-mark", func(t *testing.T) {
		directives := []ast.Directive{
			ast.DecimalMarkDirective{Mark: ","},
			ast.CommodityDirective{
				Commodity: ast.Commodity{Symbol: "USD"},
				Format:    "$1,000.00",
			},
		}
		formats := ExtractCommodityFormats(directives)
		require.Contains(t, formats, "USD")
		assert.Equal(t, '.', formats["USD"].DecimalMark,
			"commodity-specific format should override decimal-mark")
		require.Contains(t, formats, "")
		assert.Equal(t, ',', formats[""].DecimalMark,
			"decimal-mark should still be the fallback for other commodities")
	})

	t.Run("D directive declared before decimal-mark takes priority", func(t *testing.T) {
		directives := []ast.Directive{
			ast.DefaultCommodityDirective{Symbol: "$", Format: "$1,000.00"},
			ast.DecimalMarkDirective{Mark: ","},
		}
		formats := ExtractCommodityFormats(directives)
		require.Contains(t, formats, "")
		assert.Equal(t, '.', formats[""].DecimalMark,
			"D directive should take priority over decimal-mark for the default format")
		assert.Equal(t, ",", formats[""].ThousandsSep)
		assert.Equal(t, 2, formats[""].DecimalPlaces,
			"D-derived format should preserve decimal places from format string")
	})

	t.Run("decimal-mark dot sets comma as thousands sep", func(t *testing.T) {
		directives := []ast.Directive{
			ast.DecimalMarkDirective{Mark: "."},
		}
		formats := ExtractCommodityFormats(directives)
		require.Contains(t, formats, "")
		assert.Equal(t, '.', formats[""].DecimalMark)
		assert.Equal(t, ",", formats[""].ThousandsSep)
		assert.Equal(t, 0, formats[""].DecimalPlaces)
	})
}

func TestFormatDocument_DecimalMarkPreservesTrailingZeros(t *testing.T) {
	input := `decimal-mark .

2024-01-15 grocery store
    expenses:food  50.00 EUR
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, "50.00 EUR",
		"decimal-mark must preserve trailing zeros from user input")
}

func TestFormatDocument_DecimalMarkPreservesUserPrecision(t *testing.T) {
	input := `decimal-mark .

2024-01-15 grocery store
    expenses:food  1234.50 EUR
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, "1234.50 EUR",
		"decimal-mark must preserve user precision (trailing .50)")
}

func TestFormatDocument_DecimalMarkWithDDirective(t *testing.T) {
	input := `decimal-mark ,
D 1.000,00 EUR

2024-01-15 grocery store
    expenses:food  50 EUR
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, "50,00 EUR",
		"D directive format should apply over decimal-mark fallback")
}

func TestFormatDocument_DecimalMarkWithCommodityDirective(t *testing.T) {
	input := `decimal-mark ,
commodity USD
  format 1,000.00 USD

2024-01-15 test
    expenses:food  50 USD
    expenses:rent  1234,50 EUR
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, "50.00 USD",
		"commodity-specific format applies for USD")
	assert.Contains(t, result, "1234,50 EUR",
		"decimal-mark fallback preserves raw quantity for EUR")
}

func TestFormatDocument_DecimalMarkIdempotency(t *testing.T) {
	input := `decimal-mark .

2024-01-15 grocery store
    expenses:food  50.00 EUR
    expenses:rent  1234.50 EUR
    assets:cash`

	journal1, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits1 := FormatDocument(journal1, input)
	result1 := applyEdits(input, edits1)

	journal2, errs := parser.Parse(result1)
	require.Empty(t, errs)

	edits2 := FormatDocument(journal2, result1)
	result2 := applyEdits(result1, edits2)

	assert.Equal(t, result1, result2,
		"formatting with decimal-mark must be idempotent")
}

func TestFormatDocument_DecimalMarkCRLF(t *testing.T) {
	input := "decimal-mark .\r\n\r\n2024-01-15 grocery store\r\n    expenses:food  50.00 EUR\r\n    assets:cash\r\n"
	normalized := strings.ReplaceAll(input, "\r\n", "\n")

	journal, errs := parser.Parse(normalized)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	edits := FormatDocument(journal, normalized)
	result := applyEdits(normalized, edits)

	assert.Contains(t, result, "50.00 EUR",
		"decimal-mark must preserve trailing zeros in CRLF input")

	journal2, errs := parser.Parse(result)
	require.Empty(t, errs)
	edits2 := FormatDocument(journal2, result)
	result2 := applyEdits(result, edits2)

	assert.Equal(t, result, result2,
		"CRLF decimal-mark formatting must be idempotent")
}

func TestFormatBalance(t *testing.T) {
	tests := []struct {
		name             string
		quantity         string
		commodity        string
		commodityFormats map[string]CommodityFormat
		expected         string
	}{
		{
			name:             "basic quantity and symbol without format map",
			quantity:         "50",
			commodity:        "USD",
			commodityFormats: nil,
			expected:         "50 USD",
		},
		{
			name:      "left position commodity format",
			quantity:  "50",
			commodity: "$",
			commodityFormats: map[string]CommodityFormat{
				"$": {
					NumberFormat: NumberFormat{DecimalMark: '.', DecimalPlaces: 2, HasDecimal: true, ThousandsSep: ","},
					Position:     ast.CommodityLeft,
					SpaceBetween: false,
				},
			},
			expected: "$50.00",
		},
		{
			name:      "right position commodity with space",
			quantity:  "1000",
			commodity: "EUR",
			commodityFormats: map[string]CommodityFormat{
				"EUR": {
					NumberFormat: NumberFormat{DecimalMark: ',', DecimalPlaces: 2, HasDecimal: true, ThousandsSep: "."},
					Position:     ast.CommodityRight,
					SpaceBetween: true,
				},
			},
			expected: "1.000,00 EUR",
		},
		{
			name:      "default commodity format via empty key",
			quantity:  "80",
			commodity: "$",
			commodityFormats: map[string]CommodityFormat{
				"": {
					NumberFormat: NumberFormat{DecimalMark: '.', DecimalPlaces: 2, HasDecimal: true, ThousandsSep: ","},
					Position:     ast.CommodityLeft,
					SpaceBetween: false,
				},
				"$": {
					NumberFormat: NumberFormat{DecimalMark: '.', DecimalPlaces: 2, HasDecimal: true, ThousandsSep: ","},
					Position:     ast.CommodityLeft,
					SpaceBetween: false,
				},
			},
			expected: "$80.00",
		},
		{
			name:      "negative balance",
			quantity:  "-150",
			commodity: "$",
			commodityFormats: map[string]CommodityFormat{
				"$": {
					NumberFormat: NumberFormat{DecimalMark: '.', DecimalPlaces: 2, HasDecimal: true, ThousandsSep: ","},
					Position:     ast.CommodityLeft,
					SpaceBetween: false,
				},
			},
			expected: "-$150.00",
		},
		{
			name:      "empty commodity uses raw decimal",
			quantity:  "100",
			commodity: "",
			commodityFormats: map[string]CommodityFormat{
				"": {
					NumberFormat: NumberFormat{DecimalMark: '.', DecimalPlaces: 2, HasDecimal: true},
					Position:     ast.CommodityLeft,
					SpaceBetween: false,
				},
			},
			expected: "100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qty, err := decimal.NewFromString(tt.quantity)
			require.NoError(t, err)
			result := FormatBalance(qty, tt.commodity, tt.commodityFormats)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatDocument_PrefixCommodityAfterBareNumber(t *testing.T) {
	input := "2024-01-15 test\n    Расходы:Продукты  698,43\n    Активы:Альфа  RUB100,00\n    Активы:Бета  RUB11,00"

	journal, errs := parser.Parse(input)
	require.Empty(t, errs, "expected no parse errors, got: %v", errs)
	require.Len(t, journal.Transactions, 1)

	edits := FormatDocument(journal, input)

	result := applyEdits(input, edits)
	assert.Contains(t, result, "698,43", "bare number amount must survive formatting")
	assert.Contains(t, result, "RUB 100,00", "prefix word commodity must have space")
	assert.Contains(t, result, "RUB 11,00", "prefix word commodity must have space")
}

func TestFormatDocument_WordCommoditySpace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{
			name: "stock buy preserves spacing",
			input: `2024-01-15 Buy stock
    assets:brokerage  10 AAPL @ $150.00
    assets:cash`,
			contains: "10 AAPL",
		},
		{
			name: "left-position word commodity preserves space",
			input: `2024-01-15 test
    expenses:food  AAPL 10
    assets:cash`,
			contains: "AAPL 10",
		},
		{
			name: "left-position currency symbol has no space",
			input: `2024-01-15 test
    expenses:food  $100
    assets:cash`,
			contains: "$100",
		},
		{
			name: "left-position multi-char currency symbol has no space",
			input: `2024-01-15 test
    expenses:food  AU$100
    assets:cash`,
			contains: "AU$100",
		},
		{
			name: "USD word commodity gets space",
			input: `2024-01-15 test
    expenses:food  USD 100
    assets:cash`,
			contains: "USD 100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			journal, errs := parser.Parse(tt.input)
			require.Empty(t, errs, "parsing should succeed")
			require.Len(t, journal.Transactions, 1)

			edits := FormatDocument(journal, tt.input)
			result := applyEdits(tt.input, edits)
			assert.Contains(t, result, tt.contains)
		})
	}
}

func TestFormatDocument_WordCommodityIdempotency(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"word commodity left AAPL", "2024-01-15 test\n    expenses:food  AAPL 10\n    assets:cash"},
		{"symbol commodity $", "2024-01-15 test\n    expenses:food  $100\n    assets:cash"},
		{"word commodity left RUB", "2024-01-15 test\n    expenses:food  RUB 100,00\n    assets:cash"},
		{"multi-char symbol AU$", "2024-01-15 test\n    expenses:food  AU$100\n    assets:cash"},
		{"word commodity right AAPL", "2024-01-15 test\n    expenses:food  10 AAPL\n    assets:cash"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			journal, errs := parser.Parse(tt.input)
			require.Empty(t, errs)

			edits := FormatDocument(journal, tt.input)
			first := applyEdits(tt.input, edits)

			journal2, errs2 := parser.Parse(first)
			require.Empty(t, errs2)

			edits2 := FormatDocument(journal2, first)
			second := applyEdits(first, edits2)

			assert.Equal(t, first, second, "formatting must be idempotent")
		})
	}
}

func TestIsSymbolCommodity(t *testing.T) {
	tests := []struct {
		symbol   string
		expected bool
	}{
		{"", false},
		{"$", true},
		{"¥", true},
		{"€", true},
		{"£", true},
		{"AU$", true},
		{"NZ$", true},
		{"USD", false},
		{"EUR", false},
		{"AAPL", false},
		{"BTC", false},
		{"RUB", false},
		{"MAU", false},
		{"\xff", false},
	}

	for _, tt := range tests {
		t.Run(tt.symbol, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsSymbolCommodity(tt.symbol))
		})
	}
}

func TestDefaultSpaceBetween(t *testing.T) {
	tests := []struct {
		name     string
		position ast.CommodityPosition
		symbol   string
		expected bool
	}{
		{"right position always spaced", ast.CommodityRight, "AAPL", true},
		{"right position symbol spaced", ast.CommodityRight, "$", true},
		{"left symbol no space", ast.CommodityLeft, "$", false},
		{"left multi-char symbol no space", ast.CommodityLeft, "AU$", false},
		{"left word commodity spaced", ast.CommodityLeft, "USD", true},
		{"left word commodity AAPL spaced", ast.CommodityLeft, "AAPL", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, DefaultSpaceBetween(tt.position, tt.symbol))
		})
	}
}

func assertNoTrailingWhitespace(t *testing.T, result string) {
	t.Helper()
	for i, line := range strings.Split(result, "\n") {
		assert.Equal(t, strings.TrimRight(line, " \t"), line,
			"line %d should have no trailing spaces: %q", i, line)
	}
}

func TestFormatDocument_ChineseAccountNames(t *testing.T) {
	input := `2024-01-15 超市购物
    支出:食品  ¥50.00
    资产:现金`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, "支出:食品", "Chinese account names must be preserved")
	assert.Contains(t, result, "¥50.00", "amount must be preserved")
	assert.Contains(t, result, "资产:现金", "Chinese account without amount must be preserved")
	assertNoTrailingWhitespace(t, result)
}

func TestFormatDocument_ChineseTrailingSpaces(t *testing.T) {
	input := "2024-01-15 超市购物   \n    支出:食品  ¥50.00  \n    资产:现金   "

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assertNoTrailingWhitespace(t, result)
}

func TestFormatDocument_ChineseWithGlobalAlignment(t *testing.T) {
	input := `2024-01-15 超市购物
    支出:食品  ¥50.00
    资产:现金

2024-01-16 网上购物
    支出:电子产品  ¥2000.00
    资产:银行卡`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 2)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	inputLineCount := len(strings.Split(input, "\n"))
	resultLineCount := len(strings.Split(result, "\n"))
	assert.Equal(t, inputLineCount, resultLineCount,
		"formatting should not add extra blank lines")

	var amountPositions []int
	for _, edit := range edits {
		if pos := findAmountStartPosition(edit.NewText); pos > 0 {
			amountPositions = append(amountPositions, pos)
		}
	}

	require.GreaterOrEqual(t, len(amountPositions), 2, "expected at least 2 postings with amounts")
	for i, pos := range amountPositions {
		assert.Equal(t, amountPositions[0], pos,
			"all amounts should be at the same column, posting %d is at %d, expected %d", i, pos, amountPositions[0])
	}

	assertNoTrailingWhitespace(t, result)
}

func TestFormatDocument_MixedChineseLatinAccounts(t *testing.T) {
	input := `2024-01-15 mixed transaction
    expenses:食品  $50.00
    assets:现金

2024-01-16 another one
    支出:groceries  $30.00
    资产:bank`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 2)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, "expenses:食品", "mixed account names must be preserved")
	assert.Contains(t, result, "assets:现金", "mixed account names must be preserved")
	assert.Contains(t, result, "支出:groceries", "mixed account names must be preserved")
	assert.Contains(t, result, "资产:bank", "mixed account names must be preserved")

	inputLineCount := len(strings.Split(input, "\n"))
	resultLineCount := len(strings.Split(result, "\n"))
	assert.Equal(t, inputLineCount, resultLineCount,
		"formatting should not add extra blank lines")

	var amountPositions []int
	for _, edit := range edits {
		if pos := findAmountStartPosition(edit.NewText); pos > 0 {
			amountPositions = append(amountPositions, pos)
		}
	}

	require.GreaterOrEqual(t, len(amountPositions), 2, "expected at least 2 postings with amounts")
	for i, pos := range amountPositions {
		assert.Equal(t, amountPositions[0], pos,
			"all amounts should be at the same column, posting %d is at %d, expected %d", i, pos, amountPositions[0])
	}

	assertNoTrailingWhitespace(t, result)
}

func findAmountStartPosition(s string) int {
	spaceCount := 0
	for i, r := range s {
		if r == ' ' {
			spaceCount++
		} else {
			if spaceCount >= 2 {
				return i
			}
			spaceCount = 0
		}
	}
	return -1
}

func TestFormatDocument_CommentIdempotency(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "no space before comment text",
			input: `2024-01-15 test
    expenses:food  $50  ;date:2026-02-21
    assets:cash`,
			expected: "    expenses:food  $50  ; date:2026-02-21",
		},
		{
			name: "one space before comment text",
			input: `2024-01-15 test
    expenses:food  $50  ; date:2026-02-21
    assets:cash`,
			expected: "    expenses:food  $50  ; date:2026-02-21",
		},
		{
			name: "two spaces before comment text (growing spaces bug)",
			input: `2024-01-15 test
    expenses:food  $50  ;  date:2026-02-21
    assets:cash`,
			expected: "    expenses:food  $50  ; date:2026-02-21",
		},
		{
			name: "many spaces before comment text",
			input: `2024-01-15 test
    expenses:food  $50  ;    date:2026-02-21
    assets:cash`,
			expected: "    expenses:food  $50  ; date:2026-02-21",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			journal, errs := parser.Parse(tt.input)
			require.Empty(t, errs)

			edits := FormatDocument(journal, tt.input)
			require.NotEmpty(t, edits)

			assert.Equal(t, tt.expected, edits[0].NewText,
				"comment formatting should normalize to single space after semicolon")
		})
	}
}

func TestFormatDocument_CommentDoubleFormat(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $50  ;date:2026-02-21
    assets:cash`

	journal1, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits1 := FormatDocument(journal1, input)
	require.NotEmpty(t, edits1)
	result1 := applyEdits(input, edits1)

	journal2, errs := parser.Parse(result1)
	require.Empty(t, errs)

	edits2 := FormatDocument(journal2, result1)
	result2 := applyEdits(result1, edits2)

	assert.Equal(t, result1, result2,
		"double formatting must be idempotent")
}

func TestFormatDocument_CJKWithInlineComment(t *testing.T) {
	input := `2024-01-15 购买基金
    资产:微信wx  $50  ;date:2026-02-21
    资产:待报销费用bx`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, "; date:2026-02-21",
		"comment should have single space after semicolon")
	assert.NotContains(t, result, ";  date:",
		"comment must not have double spaces after semicolon")

	journal2, errs := parser.Parse(result)
	require.Empty(t, errs)
	edits2 := FormatDocument(journal2, result)
	result2 := applyEdits(result, edits2)

	assert.Equal(t, result, result2,
		"double formatting with CJK accounts must be idempotent")
}

func TestFormatDocument_QuotedCommodityPreserved(t *testing.T) {
	input := `2024-01-15 buy ETF
    assets:broker  10 "VWCE"
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, `10 "VWCE"`,
		"quoted commodity must preserve quotes after formatting")
}

func TestFormatDocument_UnquotedCommodityNoQuotes(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  10 USD
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, "10 USD",
		"unquoted commodity must not get quotes")
	assert.NotContains(t, result, `"USD"`,
		"unquoted commodity must not get quotes")
}

func TestFormatDocument_QuotedCommodityIdempotent(t *testing.T) {
	input := `2024-01-15 buy ETF
    assets:broker  10 "VWCE"
    assets:cash`

	journal1, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits1 := FormatDocument(journal1, input)
	result1 := applyEdits(input, edits1)

	journal2, errs := parser.Parse(result1)
	require.Empty(t, errs)

	edits2 := FormatDocument(journal2, result1)
	result2 := applyEdits(result1, edits2)

	assert.Equal(t, result1, result2,
		"formatting quoted commodity must be idempotent")
	assert.Contains(t, result2, `"VWCE"`,
		"quotes must survive round-trip")
}

func TestFormatDocument_QuotedCommodityMultiWord(t *testing.T) {
	input := `2024-01-15 buy items
    assets:items  3 "Chocolate Frogs"
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, `3 "Chocolate Frogs"`,
		"multi-word quoted commodity must preserve quotes")
}

func TestFormatDocument_QuotedPrefixCommodity(t *testing.T) {
	input := `2024-01-15 buy ETF
    assets:broker  "VWCE" 10
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, `"VWCE"`,
		"prefix quoted commodity must preserve quotes")
}

func TestFormatDocument_QuotedCommodityCRLF(t *testing.T) {
	input := "2024-01-15 buy ETF\r\n    assets:broker  10 \"VWCE\"\r\n    assets:cash\r\n"
	normalized := strings.ReplaceAll(input, "\r\n", "\n")

	journal, errs := parser.Parse(normalized)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	edits := FormatDocument(journal, normalized)
	result := applyEdits(normalized, edits)

	assert.Contains(t, result, `10 "VWCE"`,
		"quoted commodity must preserve quotes in CRLF input")

	journal2, errs := parser.Parse(result)
	require.Empty(t, errs)
	edits2 := FormatDocument(journal2, result)
	result2 := applyEdits(result, edits2)

	assert.Equal(t, result, result2,
		"CRLF quoted commodity formatting must be idempotent")
}

func TestFormatDocument_MixedQuotedAndUnquotedAlignment(t *testing.T) {
	input := `2024-01-15 portfolio
    assets:broker       10 "VWCE"
    assets:checking     $-1000
    expenses:fees       5 USD`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, `"VWCE"`,
		"quoted commodity must preserve quotes in mixed transaction")
	assert.Contains(t, result, "USD",
		"unquoted commodity must remain unquoted")
	assert.NotContains(t, result, `"USD"`,
		"unquoted commodity must not gain quotes")

	journal2, errs := parser.Parse(result)
	require.Empty(t, errs)
	edits2 := FormatDocument(journal2, result)
	result2 := applyEdits(result, edits2)

	assert.Equal(t, result, result2,
		"mixed quoted/unquoted alignment must be idempotent")
}

func TestFormatDocument_LotPriceBalanceAssertionAlignment(t *testing.T) {
	input := `2024-01-15 buy stocks
    assets:stocks  10 AAPL {$150} = 10 AAPL
    assets:cash  -$1500 = -$1500`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	lines := strings.Split(result, "\n")
	require.True(t, len(lines) >= 3)

	eqCol1 := strings.Index(lines[1], "= ")
	eqCol2 := strings.Index(lines[2], "= ")
	require.NotEqual(t, -1, eqCol1, "posting 1 must have balance assertion")
	require.NotEqual(t, -1, eqCol2, "posting 2 must have balance assertion")
	assert.Equal(t, eqCol1, eqCol2, "balance assertions must align at same column")

	journal2, errs := parser.Parse(result)
	require.Empty(t, errs)
	edits2 := FormatDocument(journal2, result)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "lot price BA alignment must be idempotent")
}

func TestFormatDocument_TotalLotPriceBalanceAssertionAlignment(t *testing.T) {
	input := `2024-01-15 buy stocks
    assets:stocks  10 AAPL {{$1500}} = 10 AAPL
    assets:cash  -$1500 = -$1500`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	lines := strings.Split(result, "\n")
	require.True(t, len(lines) >= 3)

	eqCol1 := strings.Index(lines[1], "= ")
	eqCol2 := strings.Index(lines[2], "= ")
	require.NotEqual(t, -1, eqCol1, "posting 1 must have balance assertion")
	require.NotEqual(t, -1, eqCol2, "posting 2 must have balance assertion")
	assert.Equal(t, eqCol1, eqCol2, "balance assertions must align at same column")

	journal2, errs := parser.Parse(result)
	require.Empty(t, errs)
	edits2 := FormatDocument(journal2, result)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "total lot price BA alignment must be idempotent")
}

func TestFormatDocument_WithLotPrice(t *testing.T) {
	input := `2024-01-15 buy stocks
    assets:stocks  10 AAPL {$150}
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, "{$150}")

	journal2, errs := parser.Parse(result)
	require.Empty(t, errs)
	edits2 := FormatDocument(journal2, result)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "lot price formatting must be idempotent")
}

func TestFormatDocument_WithTotalLotPrice(t *testing.T) {
	input := `2024-01-15 buy stocks
    assets:stocks  10 AAPL {{$1500}}
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, "{{$1500}}")

	journal2, errs := parser.Parse(result)
	require.Empty(t, errs)
	edits2 := FormatDocument(journal2, result)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "total lot price formatting must be idempotent")
}

func TestFormatDocument_WithLotPriceAndCost(t *testing.T) {
	input := `2024-01-15 buy stocks
    assets:stocks  10 AAPL {$150} @ $180
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, "{$150}")
	assert.Contains(t, result, "@ $180")

	journal2, errs := parser.Parse(result)
	require.Empty(t, errs)
	edits2 := FormatDocument(journal2, result)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "lot+cost formatting must be idempotent")
}

func TestFormatDocument_WithLotDateAndLabel(t *testing.T) {
	input := `2024-01-15 buy stocks
    assets:stocks  10 AAPL {$150} [2024-01-15] (lot1)
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, "{$150}")
	assert.Contains(t, result, "[2024-01-15]")
	assert.Contains(t, result, "(lot1)")

	journal2, errs := parser.Parse(result)
	require.Empty(t, errs)
	edits2 := FormatDocument(journal2, result)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "lot annotations formatting must be idempotent")
}

func TestFormatDocument_WithLotPriceCRLF(t *testing.T) {
	input := strings.ReplaceAll("2024-01-15 buy stocks\r\n    assets:stocks  10 AAPL {$150} @ $180\r\n    assets:cash\r\n", "\r\n", "\n")

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, "{$150}")
	assert.Contains(t, result, "@ $180")

	journal2, errs := parser.Parse(result)
	require.Empty(t, errs)
	edits2 := FormatDocument(journal2, result)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "lot price CRLF formatting must be idempotent")
}

func TestFormatDocument_WithConsolidatedLotDateCost(t *testing.T) {
	input := `2024-01-15 buy stocks
    assets:stocks  10 AAPL {2026-01-15, $50}
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	journal2, errs := parser.Parse(result)
	require.Empty(t, errs)
	edits2 := FormatDocument(journal2, result)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "consolidated lot (date+cost) formatting must be idempotent")
}

func TestFormatDocument_LotPriceUnicodeLabel(t *testing.T) {
	input := `2024-01-15 buy stocks
    assets:stocks  10 AAPL {$150} (ロット1)
    assets:cash  -$1500 = -$3000`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, "(ロット1)")

	journal2, errs := parser.Parse(result)
	require.Empty(t, errs)
	edits2 := FormatDocument(journal2, result)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "lot price with CJK label must be idempotent")
}

func TestFormatDocument_WithConsolidatedLotAllFields(t *testing.T) {
	input := `2024-01-15 buy stocks
    assets:stocks  10 AAPL {2026-01-15, "lot1", $50}
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	journal2, errs := parser.Parse(result)
	require.Empty(t, errs)
	edits2 := FormatDocument(journal2, result)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "consolidated lot (all fields) formatting must be idempotent")
}

func TestFormatDocument_BalanceAssertionWithCost(t *testing.T) {
	input := `2024-01-15 buy stocks
    assets:stocks  10 AAPL = 10 AAPL @ $150
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, "= 10 AAPL @ $150")

	journal2, errs := parser.Parse(result)
	require.Empty(t, errs)
	edits2 := FormatDocument(journal2, result)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "BA with cost formatting must be idempotent")
}

func TestFormatDocument_BalanceAssertionWithLotPrice(t *testing.T) {
	input := `2024-01-15 buy stocks
    assets:stocks  10 AAPL = 10 AAPL {$150}
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, "= 10 AAPL {$150}")

	journal2, errs := parser.Parse(result)
	require.Empty(t, errs)
	edits2 := FormatDocument(journal2, result)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "BA with lot price formatting must be idempotent")
}

func TestFormatDocument_BalanceAssertionWithAllAnnotations(t *testing.T) {
	input := `2024-01-15 buy stocks
    assets:stocks  10 AAPL = 10 AAPL {$150} [2024-01-15] @ $180
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, "= 10 AAPL {$150} [2024-01-15] @ $180")

	journal2, errs := parser.Parse(result)
	require.Empty(t, errs)
	edits2 := FormatDocument(journal2, result)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "BA with all annotations formatting must be idempotent")
}

func TestFormatDocument_StrictBalanceAssertionWithCost(t *testing.T) {
	input := `2024-01-15 buy stocks
    assets:stocks  10 AAPL == 10 AAPL @ $150
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	edits := FormatDocument(journal, input)
	result := applyEdits(input, edits)

	assert.Contains(t, result, "== 10 AAPL @ $150")

	journal2, errs := parser.Parse(result)
	require.Empty(t, errs)
	edits2 := FormatDocument(journal2, result)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "strict BA with cost formatting must be idempotent")
}

func TestFormatDocument_DecimalAlignment(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  1000.00 USD
    expenses:drink  5.76 USD
    expenses:tax  0.60 USD
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	opts := Options{IndentSize: 4, AlignAmounts: true, AmountAlignmentMode: "decimal"}
	edits := FormatDocumentWithOptions(journal, input, nil, opts)
	result := applyEdits(input, edits)

	lines := strings.Split(result, "\n")
	// Find decimal point positions in formatted lines
	var decimalPositions []int
	for _, line := range lines {
		idx := strings.Index(line, ".")
		if idx > 0 && strings.Contains(line, "USD") {
			decimalPositions = append(decimalPositions, idx)
		}
	}

	require.GreaterOrEqual(t, len(decimalPositions), 3, "should have at least 3 postings with decimals")
	for i, pos := range decimalPositions {
		assert.Equal(t, decimalPositions[0], pos,
			"decimal point in line %d should be at same column as first line", i)
	}

	// Idempotency check
	journal2, errs2 := parser.Parse(result)
	require.Empty(t, errs2)
	edits2 := FormatDocumentWithOptions(journal2, result, nil, opts)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "decimal alignment must be idempotent")
}

func TestFormatDocument_DecimalAlignment_QuotedCommodityWithDot(t *testing.T) {
	input := `2024-01-15 buy
    assets:investments  0.2222 "VWXY.Z"
    assets:cash  -5.00 CAD`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	opts := Options{IndentSize: 4, AlignAmounts: true, AmountAlignmentMode: "decimal"}
	edits := FormatDocumentWithOptions(journal, input, nil, opts)
	result := applyEdits(input, edits)

	// Find decimal positions — use the CAD line (no dot in commodity) as reference
	lines := strings.Split(result, "\n")
	var cadDecimalPos, qtyDecimalPos int
	for _, line := range lines {
		if strings.Contains(line, "CAD") {
			cadDecimalPos = strings.Index(line, ".")
		}
		if strings.Contains(line, `"VWXY.Z"`) {
			// Find the FIRST dot — it's in the quantity, not in the commodity
			qtyDecimalPos = strings.Index(line, ".")
		}
	}

	require.Greater(t, cadDecimalPos, 0, "should find decimal in CAD line")
	require.Greater(t, qtyDecimalPos, 0, "should find decimal in VWXY.Z line")
	assert.Equal(t, cadDecimalPos, qtyDecimalPos,
		"decimal points should be aligned for quoted commodity with dot in symbol")

	// Idempotency
	journal2, errs2 := parser.Parse(result)
	require.Empty(t, errs2)
	edits2 := FormatDocumentWithOptions(journal2, result, nil, opts)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "formatting must be idempotent")
}

func TestFormatDocument_DecimalAlignment_QuotedCommodityWithDotAndCost(t *testing.T) {
	input := `2024-01-15 buy
    assets:investments  0.2222 "VWXY.Z" @@ 5.00 CAD
    assets:cash  -5.00 CAD`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	opts := Options{IndentSize: 4, AlignAmounts: true, AmountAlignmentMode: "decimal"}
	edits := FormatDocumentWithOptions(journal, input, nil, opts)
	result := applyEdits(input, edits)

	// Posting amounts should have aligned decimal points
	assert.Contains(t, result, `"VWXY.Z"`)
	assert.Contains(t, result, "@@ 5.00 CAD")

	// Idempotency
	journal2, errs2 := parser.Parse(result)
	require.Empty(t, errs2)
	edits2 := FormatDocumentWithOptions(journal2, result, nil, opts)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "formatting must be idempotent")
}

func TestFormatDocument_DecimalAlignment_NoDot(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  1000 USD
    expenses:drink  5 USD
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	opts := Options{IndentSize: 4, AlignAmounts: true, AmountAlignmentMode: "decimal"}
	edits := FormatDocumentWithOptions(journal, input, nil, opts)
	result := applyEdits(input, edits)

	// For amounts without decimal, the right edge of integer part should align
	lines := strings.Split(result, "\n")
	var numberEndPositions []int
	for _, line := range lines {
		// Find last digit before " USD"
		usdIdx := strings.Index(line, " USD")
		if usdIdx > 0 {
			numberEndPositions = append(numberEndPositions, usdIdx)
		}
	}

	require.GreaterOrEqual(t, len(numberEndPositions), 2)
	for i, pos := range numberEndPositions {
		assert.Equal(t, numberEndPositions[0], pos,
			"number end in line %d should be at same column", i)
	}

	// Idempotency
	journal2, errs2 := parser.Parse(result)
	require.Empty(t, errs2)
	edits2 := FormatDocumentWithOptions(journal2, result, nil, opts)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "decimal alignment (no dot) must be idempotent")
}

func TestFormatDocument_DecimalAlignment_LeftCommodity(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $1000.50
    expenses:drink  $5.76
    expenses:tax  -$98.24
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	opts := Options{IndentSize: 4, AlignAmounts: true, AmountAlignmentMode: "decimal"}
	edits := FormatDocumentWithOptions(journal, input, nil, opts)
	result := applyEdits(input, edits)

	lines := strings.Split(result, "\n")
	var decimalPositions []int
	for _, line := range lines {
		idx := strings.Index(line, ".")
		if idx > 0 && strings.Contains(line, "$") {
			decimalPositions = append(decimalPositions, idx)
		}
	}

	require.GreaterOrEqual(t, len(decimalPositions), 3, "should have at least 3 postings with decimals")
	for i, pos := range decimalPositions {
		assert.Equal(t, decimalPositions[0], pos,
			"decimal point in line %d should be at same column", i)
	}

	// Idempotency
	journal2, errs2 := parser.Parse(result)
	require.Empty(t, errs2)
	edits2 := FormatDocumentWithOptions(journal2, result, nil, opts)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "decimal alignment (left commodity) must be idempotent")
}

func TestFormatDocument_DecimalAlignment_CostAlignment(t *testing.T) {
	input := `2024-01-15 buy
    assets:investments  0.8687 BMO @@ 24.24 CAD
    assets:cash  -24.24 CAD`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	opts := Options{IndentSize: 4, AlignAmounts: true, AmountAlignmentMode: "decimal"}
	edits := FormatDocumentWithOptions(journal, input, nil, opts)
	result := applyEdits(input, edits)

	assert.Contains(t, result, "0.8687 BMO")
	assert.Contains(t, result, "-24.24 CAD")

	// Posting amount decimals should align with each other.
	// The cost amount's decimal is NOT used for alignment.
	lines := strings.Split(result, "\n")
	var postingDecimalPos, cashDecimalPos int
	for _, line := range lines {
		if strings.Contains(line, "@@") {
			// Find decimal in POSTING amount (before @@)
			amountPart := line[:strings.Index(line, " @@ ")]
			postingDecimalPos = strings.LastIndex(amountPart, ".")
		} else if strings.Contains(line, "-24.24") {
			cashDecimalPos = strings.Index(line, ".")
		}
	}

	require.Greater(t, postingDecimalPos, 0)
	require.Greater(t, cashDecimalPos, 0)
	assert.Equal(t, cashDecimalPos, postingDecimalPos,
		"posting amount decimal should align with non-cost posting amount decimal")

	// Idempotency
	journal2, errs2 := parser.Parse(result)
	require.Empty(t, errs2)
	edits2 := FormatDocumentWithOptions(journal2, result, nil, opts)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "decimal alignment with cost must be idempotent")
}

func TestFormatDocument_DecimalAlignment_UnitCostAlignment(t *testing.T) {
	input := `2024-01-15 buy
    assets:investments  10 AAPL @ 150.00 USD
    assets:cash  -1500.00 USD`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	opts := Options{IndentSize: 4, AlignAmounts: true, AmountAlignmentMode: "decimal"}
	edits := FormatDocumentWithOptions(journal, input, nil, opts)
	result := applyEdits(input, edits)

	// Posting amount decimals should align with each other.
	// "10 AAPL" has no decimal, so only verify non-cost posting.
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		if strings.Contains(line, "AAPL") {
			assert.Contains(t, line, "10 AAPL")
		}
	}

	assert.Contains(t, result, "-1500.00 USD")

	// Idempotency
	journal2, errs2 := parser.Parse(result)
	require.Empty(t, errs2)
	edits2 := FormatDocumentWithOptions(journal2, result, nil, opts)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "decimal alignment with unit cost must be idempotent")
}

func TestFormatDocument_DecimalAlignment_CostAlignment_NoCostPosting(t *testing.T) {
	input := `2024-01-15 buy
    assets:investments  0.8687 BMO @@ 24.24 CAD
    expenses:fees  1.50 CAD
    assets:cash  -25.74 CAD`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	opts := Options{IndentSize: 4, AlignAmounts: true, AmountAlignmentMode: "decimal"}
	edits := FormatDocumentWithOptions(journal, input, nil, opts)
	result := applyEdits(input, edits)

	// All POSTING amount decimals should be aligned.
	// Cost amount decimals are NOT used for alignment.
	lines := strings.Split(result, "\n")
	var decimalPositions []int
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "2024") {
			continue
		}
		if !strings.ContainsAny(trimmed, "0123456789") {
			continue
		}

		// For cost postings, find decimal in posting amount (before @@)
		amountPart := line
		if atIdx := strings.Index(line, " @@ "); atIdx > 0 {
			amountPart = line[:atIdx]
		}
		pos := strings.LastIndex(amountPart, ".")
		if pos > 0 {
			decimalPositions = append(decimalPositions, pos)
		}
	}

	require.GreaterOrEqual(t, len(decimalPositions), 3,
		"should have at least 3 posting amount decimal positions")
	for i := 1; i < len(decimalPositions); i++ {
		assert.Equal(t, decimalPositions[0], decimalPositions[i],
			"decimal at position %d should align with first", i)
	}

	// Idempotency
	journal2, errs2 := parser.Parse(result)
	require.Empty(t, errs2)
	edits2 := FormatDocumentWithOptions(journal2, result, nil, opts)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "decimal alignment with mixed cost/no-cost must be idempotent")
}

func TestFormatDocument_DecimalAlignment_CostDoesNotShiftPostingAmount(t *testing.T) {
	// Bug: when a posting has @@ cost, the formatter aligned the cost amount's
	// decimal point instead of the posting amount's decimal point.
	// The posting amount's decimal must align with other postings' decimals.
	input := `2024-01-15 groceries
    expenses:food      864.62 @@ 21.00 EUR
    assets:cash

2024-01-15 salary
    assets:checking  148582.00 USD
    income:salary

2024-01-15 family
    expenses:family  10000.00 USD
    assets:checking`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	opts := Options{IndentSize: 4, AlignAmounts: true, AmountAlignmentMode: "decimal"}
	edits := FormatDocumentWithOptions(journal, input, nil, opts)
	result := applyEdits(input, edits)

	t.Logf("Formatted result:\n%s", result)

	// Collect decimal dot positions of POSTING amounts (not cost amounts).
	lines := strings.Split(result, "\n")
	var decimalPositions []int
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Only check posting lines with amounts
		if !strings.HasPrefix(trimmed, "expenses:") && !strings.HasPrefix(trimmed, "assets:checking") {
			continue
		}
		if !strings.ContainsAny(trimmed, "0123456789") {
			continue
		}

		// For cost postings, look only at the POSTING amount (before @@)
		amountPart := line
		if atIdx := strings.Index(line, " @@ "); atIdx > 0 {
			amountPart = line[:atIdx]
		}
		// Find the last dot in the amount part (decimal point)
		pos := strings.LastIndex(amountPart, ".")
		if pos > 0 {
			decimalPositions = append(decimalPositions, pos)
		}
	}

	require.GreaterOrEqual(t, len(decimalPositions), 3,
		"should have at least 3 posting amounts with decimals, got lines:\n%s", result)
	for i := 1; i < len(decimalPositions); i++ {
		assert.Equal(t, decimalPositions[0], decimalPositions[i],
			"posting amount decimal at position %d should align with first", i)
	}

	// Idempotency
	journal2, errs2 := parser.Parse(result)
	require.Empty(t, errs2)
	edits2 := FormatDocumentWithOptions(journal2, result, nil, opts)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "formatting must be idempotent")
}

func TestFormatDocument_DecimalAlignment_WithBalanceAssertion(t *testing.T) {
	input := `2024-01-15 test
    assets:checking  1000.00 USD  = 5000.00 USD
    expenses:food  5.76 USD
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	opts := Options{IndentSize: 4, AlignAmounts: true, AmountAlignmentMode: "decimal"}
	edits := FormatDocumentWithOptions(journal, input, nil, opts)
	result := applyEdits(input, edits)

	// Balance assertion should still be present
	assert.Contains(t, result, "= 5000.00 USD")

	// Idempotency
	journal2, errs2 := parser.Parse(result)
	require.Empty(t, errs2)
	edits2 := FormatDocumentWithOptions(journal2, result, nil, opts)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "decimal alignment with balance assertion must be idempotent")
}

func TestFormatDocument_DecimalAlignment_RightModeUnchanged(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  1000.00 USD
    expenses:drink  5.76 USD
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	// Right mode (default) should produce same result as no mode specified
	optsRight := Options{IndentSize: 4, AlignAmounts: true, AmountAlignmentMode: "right"}
	editsRight := FormatDocumentWithOptions(journal, input, nil, optsRight)
	resultRight := applyEdits(input, editsRight)

	optsDefault := Options{IndentSize: 4, AlignAmounts: true}
	editsDefault := FormatDocumentWithOptions(journal, input, nil, optsDefault)
	resultDefault := applyEdits(input, editsDefault)

	assert.Equal(t, resultDefault, resultRight, "right mode should produce same result as default")
}

func TestFormatDocument_DecimalAlignment_CRLF(t *testing.T) {
	input := "2024-01-15 test\r\n    expenses:food  1000.00 USD\r\n    expenses:drink  5.76 USD\r\n    assets:cash"
	normalized := strings.ReplaceAll(input, "\r\n", "\n")

	journal, errs := parser.Parse(normalized)
	require.Empty(t, errs)

	opts := Options{IndentSize: 4, AlignAmounts: true, AmountAlignmentMode: "decimal"}
	edits := FormatDocumentWithOptions(journal, normalized, nil, opts)
	result := applyEdits(normalized, edits)

	lines := strings.Split(result, "\n")
	var decimalPositions []int
	for _, line := range lines {
		idx := strings.Index(line, ".")
		if idx > 0 && strings.Contains(line, "USD") {
			decimalPositions = append(decimalPositions, idx)
		}
	}

	require.GreaterOrEqual(t, len(decimalPositions), 2)
	for i, pos := range decimalPositions {
		assert.Equal(t, decimalPositions[0], pos,
			"decimal point in line %d should be at same column (CRLF)", i)
	}

	// Idempotency
	journal2, errs2 := parser.Parse(result)
	require.Empty(t, errs2)
	edits2 := FormatDocumentWithOptions(journal2, result, nil, opts)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "decimal alignment (CRLF) must be idempotent")
}

func TestFormatDocument_DecimalAlignment_CJK(t *testing.T) {
	input := `2024-01-15 テスト
    支出:食品  1000.00 JPY
    支出:飲料  5.76 JPY
    資産:現金`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	opts := Options{IndentSize: 4, AlignAmounts: true, AmountAlignmentMode: "decimal"}
	edits := FormatDocumentWithOptions(journal, input, nil, opts)
	result := applyEdits(input, edits)

	lines := strings.Split(result, "\n")
	var decimalPositions []int
	for _, line := range lines {
		idx := strings.Index(line, ".")
		if idx > 0 && strings.Contains(line, "JPY") {
			decimalPositions = append(decimalPositions, idx)
		}
	}

	require.GreaterOrEqual(t, len(decimalPositions), 2)
	for i, pos := range decimalPositions {
		assert.Equal(t, decimalPositions[0], pos,
			"decimal point in line %d should be at same column (CJK)", i)
	}

	// Idempotency
	journal2, errs2 := parser.Parse(result)
	require.Empty(t, errs2)
	edits2 := FormatDocumentWithOptions(journal2, result, nil, opts)
	result2 := applyEdits(result, edits2)
	assert.Equal(t, result, result2, "decimal alignment (CJK) must be idempotent")
}

func TestCalculateGlobalDecimalCol(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  1000.00 USD
    expenses:drink  5.76 USD
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	accountCol := CalculateAlignmentColumn(journal.Transactions[0].Postings)
	decimalCol := CalculateGlobalDecimalCol(journal.Transactions, nil, accountCol)

	// DecimalCol should be accountCol + maxPrefix (4 for "1000")
	assert.Equal(t, accountCol+4, decimalCol, "DecimalCol should account for longest integer part")
}

func TestCalculateAmountDecimalPrefix(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "right commodity with decimal",
			input:    "100.50 USD",
			expected: 3, // "100"
		},
		{
			name:     "right commodity no decimal",
			input:    "100 USD",
			expected: 3, // "100"
		},
		{
			name:     "left commodity with decimal",
			input:    "$100.50",
			expected: 4, // "$100"
		},
		{
			name:     "negative left commodity",
			input:    "-$5.76",
			expected: 3, // "-$5"
		},
		{
			name:     "large number with decimal",
			input:    "1000.00 RUB",
			expected: 4, // "1000"
		},
		{
			name:     "small number with decimal",
			input:    "0.60 USD",
			expected: 1, // "0"
		},
		{
			name:     "quoted commodity with dot in symbol",
			input:    `0.2222 "VWXY.Z"`,
			expected: 1, // "0" — not 12 from finding dot in "VWXY.Z"
		},
		{
			name:     "quoted commodity without dot",
			input:    `10.50 "VWCE"`,
			expected: 2, // "10"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			journal, errs := parser.Parse("2024-01-15 test\n    expenses:food  " + tt.input + "\n    assets:cash")
			require.Empty(t, errs)
			require.Len(t, journal.Transactions, 1)
			require.NotNil(t, journal.Transactions[0].Postings[0].Amount)

			prefix := calculateAmountDecimalPrefix(
				&journal.Transactions[0].Postings[0],
				nil,
			)
			assert.Equal(t, tt.expected, prefix, "prefix for %q", tt.input)
		})
	}
}
