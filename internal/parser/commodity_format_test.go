package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParser_CommodityFormatSymbolMustMatchCommodity(t *testing.T) {
	tests := []struct {
		name, input, message string
		line, startCol       int
		endCol               int
	}{
		{
			name:    "different symbol",
			input:   "commodity X\n    format 1000.00 Y\n",
			message: `commodity directive symbol "X" and format directive symbol "Y" should be the same`,
			line:    2, startCol: 12, endCol: 21,
		},
		{
			name:    "symbol before the number",
			input:   "commodity X\n    format Y 1000.00\n",
			message: `commodity directive symbol "X" and format directive symbol "Y" should be the same`,
			line:    2, startCol: 12, endCol: 21,
		},
		{
			name:    "currency sign",
			input:   "commodity $\n    format 1000.00 EUR\n",
			message: `commodity directive symbol "$" and format directive symbol "EUR" should be the same`,
			line:    2, startCol: 12, endCol: 23,
		},
		{
			name:    "no symbol",
			input:   "commodity X\n    format 1000.00\n",
			message: `format for commodity "X" has no commodity symbol; it must use "X"`,
			line:    2, startCol: 12, endCol: 19,
		},
		{
			name:    "quoted symbol",
			input:   "commodity \"A B\"\n    format 1000.00 \"A C\"\n",
			message: `commodity directive symbol "A B" and format directive symbol "A C" should be the same`,
			line:    2, startCol: 12, endCol: 25,
		},
		{
			name:    "Cyrillic symbol",
			input:   "commodity руб\n    format 1000.00 рубл\n",
			message: `commodity directive symbol "руб" and format directive symbol "рубл" should be the same`,
			line:    2, startCol: 12, endCol: 24,
		},
		{
			name:    "CJK symbol",
			input:   "commodity 元\n    format 1000.00 圆\n",
			message: `commodity directive symbol "元" and format directive symbol "圆" should be the same`,
			line:    2, startCol: 12, endCol: 21,
		},
		{
			name:    "later format after a note",
			input:   "commodity X\n    note a\n    format 1000.00 Y\n",
			message: `commodity directive symbol "X" and format directive symbol "Y" should be the same`,
			line:    3, startCol: 12, endCol: 21,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, errs := Parse(tt.input)
			require.Len(t, errs, 1)
			assert.Equal(t, tt.message, errs[0].Message)
			assert.Equal(t, tt.line, errs[0].Pos.Line)
			assert.Equal(t, tt.startCol, errs[0].Pos.Column)
			assert.Equal(t, tt.endCol, errs[0].End.Column)
		})
	}
}

func TestParser_CommodityFormatSymbolAccepted(t *testing.T) {
	for _, input := range []string{
		"commodity X\n    format 1000.00 X\n",
		"commodity X\n    format X 1000.00\n",
		"commodity X\n    format 1000.00 \"X\"\n",
		"commodity $\n    format $1000.00\n",
		"commodity $\n    format -$1000.00\n",
		"commodity \"A B\"\n    format 1000.00 \"A B\"\n",
		"commodity \"X Y\"\n    format \"X Y\" 1000.00\n",
		"commodity X\n    format 1 000.00 X\n",
		"commodity руб\n    format 1000.00 руб\n",
		"commodity 元\n    format 1000.00 元\n",
		"commodity X\n    note a\n    format 1000.00 X\n",
		"commodity X\n    format 1000.00 X\n    note a\n",
		"commodity X\n    format 1000.00 X\n    format 1,000.00 X\n",
		"commodity X\n    ; comment\n    format 1000.00 X ; comment\n",
		"commodity 1000.00 X\n    ; only a comment\n",
		"commodity 1000.00 X\n",
	} {
		_, errs := Parse(input)
		assert.Empty(t, errs, input)
	}
}

func TestParser_CommodityWithInlineFormatTakesNoSubdirectives(t *testing.T) {
	const suffix = " is not allowed after a commodity directive that already has a format; write the format as a \"format\" subdirective instead"
	tests := []struct {
		name, input, subdirective string
		line, startCol, endCol    int
	}{
		{"format after number first", "commodity 1000.00 X\n    format 1000.00 X\n", "format", 2, 5, 11},
		{"format with another symbol", "commodity 1000.00 X\n    format 1000.00 Y\n", "format", 2, 5, 11},
		{"format after symbol first", "commodity X 1000.00\n    format 1000.00 X\n", "format", 2, 5, 11},
		{"format after currency sign", "commodity $1000.00\n    format 1000.00 $\n", "format", 2, 5, 11},
		{"note", "commodity 1000.00 X\n    note hello\n", "note", 2, 5, 9},
		{"default", "commodity 1000.00 X\n    default\n", "default", 2, 5, 12},
		{"alias", "commodity 1000.00 X\n    alias x\n", "alias", 2, 5, 10},
		{"unknown subdirective", "commodity 1000.00 X\n    foo bar\n", "foo", 2, 5, 8},
		{"no symbol", "commodity 1000.00\n    note hi\n", "note", 2, 5, 9},
		{"after a comment line", "commodity 1000.00 X\n    ; c\n    note a\n", "note", 3, 5, 9},
		{"Cyrillic symbol", "commodity 1000.00 руб\n    note заметка\n", "note", 2, 5, 9},
		{"CJK symbol", "commodity 1000.00 元\n    note 备注\n", "note", 2, 5, 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, errs := Parse(tt.input)
			require.Len(t, errs, 1, "only the first subdirective is reported")
			assert.Equal(t, `subdirective "`+tt.subdirective+`"`+suffix, errs[0].Message)
			assert.Equal(t, tt.line, errs[0].Pos.Line)
			assert.Equal(t, tt.startCol, errs[0].Pos.Column)
			assert.Equal(t, tt.endCol, errs[0].End.Column)
		})
	}
}

func TestParser_CommodityWithInlineFormatReportsOnlyFirstSubdirective(t *testing.T) {
	_, errs := Parse("commodity 1000.00 X\n    note a\n    format 1000.00 Y\n")
	require.Len(t, errs, 1)
	assert.Equal(t, 2, errs[0].Pos.Line)
}

func TestParser_CommodityFormatDiagnosticsDoNotBreakFollowingDirectives(t *testing.T) {
	journal, errs := Parse("commodity X\n    format 1000.00 Y\n\n2024-01-01 x\n    a  1 X\n    b\n")
	require.Len(t, errs, 1)
	assert.Len(t, journal.Transactions, 1)
}
