package formatter

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"

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
			if edit.NewText == "    assets:bank    EUR100,00  = 1 000,00 EUR" {
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

		assert.Equal(t, 50, pos1, "amount should be at minimum column 50")
		assert.Equal(t, 50, pos2, "amount should be at minimum column 50")
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
			name: "-MAU66 preserves sign before commodity",
			input: `2024-01-15 test
    expenses:food  -MAU66
    assets:cash`,
			expected: "    expenses:food  -MAU66",
		},
		{
			name: "MAU-66 preserves sign after commodity",
			input: `2024-01-15 test
    expenses:food  MAU-66
    assets:cash`,
			expected: "    expenses:food  MAU-66",
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
