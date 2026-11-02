package parser

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/ast"
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
		"commodity US$\n    format US$1,000.00\n",
		"commodity US$\n    format US$ 1,000.00\n",
		"commodity US$\n    format 1,000.00 US$\n",
		"commodity HK$\n    format HK$1,000.00\n",
		"commodity GB£\n    format GB£1000.00\n",
		"commodity €X\n    format €X1,000.00\n",
		"commodity X€\n    format X€ 1000.00\n",
		"commodity $X\n    format $X 1000.00\n",
		"commodity R$\n    format R$ 1.000,00\n",
		"commodity Rp\n    format Rp1.000,00\n",
		"commodity EUR\n    format 1 000,00 EUR\n",
		"commodity EUR\n    format 1\u202f000,00 EUR\n",
		"commodity EUR\n    format 1\u00a0000,00 EUR\n",
		"commodity лв\n    format 1 000,00 лв\n",
		"commodity EUR\n    format - 1000.00 EUR\n",
		"commodity EUR\n    format 1000.00 EUR @ 2 USD\n",
		"commodity \"\"\n    format 1000.00 \"\"\n",
		"commodity EUR\n    format -EUR 1000.00\n",
		"commodity US$\n    format -US$1,000.00\n",
		"commodity US$\n    format - US$1,000.00\n",
		"commodity $\n    format - $1,000.00\n",
		"commodity $\n    format + $1,000.00\n",
		"commodity €\n    format - €1.000,00\n",
		"commodity Rp\n    format Rp-1.000,00\n",
		"commodity €X\n    format €X-1000.00\n",
		"commodity $\n    format $.50\n",
		"commodity $\n    format -$.50\n",
		"commodity EUR\n    format .50 EUR\n",
		"commodity EUR\n    format EUR.50\n",
		"commodity EUR\n    format EUR .50\n",
		"commodity $\n    format $ .50\n",
		"commodity US$\n    format US$.50\n",
		"commodity Rp\n    format Rp.50\n",
		"commodity \"10 Year Bond\"\n    format -\"10 Year Bond\" 1000.00\n",
		"commodity \"A B\"\n    format -\"A B\"1000.00\n",
		"commodity EUR\n    format 1.5E3 EUR\n",
		"commodity EUR\n    format 1\u2009000,00 EUR\n",
		"commodity EUR\n    format 1000,00\u00a0EUR\n",
		"commodity EUR\n    format 1000,00\u2007EUR\n",
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

func TestParser_CommodityFormatGluedSymbolMismatch(t *testing.T) {
	tests := []struct{ input, message string }{
		{"commodity US$\n    format US 1,000.00\n", `commodity directive symbol "US$" and format directive symbol "US" should be the same`},
		{"commodity US$\n    format $1,000.00\n", `commodity directive symbol "US$" and format directive symbol "$" should be the same`},
		{"commodity US\n    format US$1,000.00\n", `commodity directive symbol "US" and format directive symbol "US$" should be the same`},
		{"commodity \"\"\n    format 1000.00 X\n", `commodity directive symbol "" and format directive symbol "X" should be the same`},
		{"commodity X\n    format 1000.00 X Y\n", `invalid format "1000.00 X Y": expected a number and one commodity symbol`},
	}
	for _, tt := range tests {
		_, errs := Parse(tt.input)
		require.Len(t, errs, 1, tt.input)
		assert.Equal(t, tt.message, errs[0].Message)
	}
}

func TestParser_CommodityFormatUnicodeSpaces(t *testing.T) {
	// Accepted and rejected sets were read off hledger 1.52.4: a digit group
	// takes a fixed set of spaces, a symbol written before the number takes none.
	for _, sp := range []string{"\u00a0", "\u2002", "\u2003", "\u2008", "\u2009", "\u202f", "\u205f"} {
		_, errs := Parse("commodity EUR\n    format 1" + sp + "000,00 EUR\n")
		assert.Empty(t, errs, "digit group space %U", []rune(sp)[0])
	}
	for _, sp := range []string{"\u1680", "\u2000", "\u2001", "\u2004", "\u2005", "\u2006", "\u2007", "\u200a", "\u3000"} {
		_, errs := Parse("commodity EUR\n    format 1" + sp + "000,00 EUR\n")
		assert.NotEmpty(t, errs, "digit group space %U", []rune(sp)[0])
	}
	for _, sp := range []string{"\u00a0", "\u2007", "\u3000"} {
		_, errs := Parse("commodity EUR\n    format EUR" + sp + "1000,00\n")
		assert.NotEmpty(t, errs, "space after the symbol %U", []rune(sp)[0])
		_, errs = Parse("commodity EUR\n    format 1000,00" + sp + "EUR\n")
		assert.Empty(t, errs, "space before the symbol %U", []rune(sp)[0])
	}
}

func TestParser_CommodityDirectiveLineReadsLeadingDecimalMark(t *testing.T) {
	tests := []struct {
		directive string
		qty       string
	}{
		{"commodity $1.000,00\ncommodity $.50\n", "1.234"},
		{"commodity 1.000,00 EUR\ncommodity EUR.50\n", "1.234"},
		{"commodity $1.000,00\ncommodity .50 $\n", "1.234"},
		{"commodity $1.000,00\ncommodity $ .50\n", "1.234"},
		{"commodity $1.000,00\n", "1234"},
	}
	for _, tt := range tests {
		sym := "$"
		if strings.Contains(tt.directive, "EUR") {
			sym = "EUR"
		}
		journal, errs := Parse(tt.directive + "2024-01-02 t\n    a  " + sym + "1.234\n    c\n")
		require.Empty(t, errs, tt.directive)
		assert.Equal(t, tt.qty, journal.Transactions[0].Postings[0].Amount.Quantity.String(), tt.directive)
	}
}

func TestParser_CommodityDirectiveFormatKeepsWrittenSpacing(t *testing.T) {
	tests := []struct{ directive, format string }{
		{"commodity US$1.000,00\n", "US$1.000,00"},
		{"commodity US$ 1.000,00\n", "US$ 1.000,00"},
		{"commodity $1,000.00\n", "$1,000.00"},
		{"commodity 1.000,00 US$\n", "1.000,00 US$"},
		{"commodity EUR 1.000,00\n", "EUR 1.000,00"},
		{"commodity \"A B\" 1.000,00\n", "A B 1.000,00"},
	}
	for _, tt := range tests {
		journal, errs := Parse(tt.directive)
		require.Empty(t, errs, tt.directive)
		require.Len(t, journal.Directives, 1)
		dir, ok := journal.Directives[0].(ast.CommodityDirective)
		require.True(t, ok)
		assert.Equal(t, tt.format, dir.Format, tt.directive)
	}
}
