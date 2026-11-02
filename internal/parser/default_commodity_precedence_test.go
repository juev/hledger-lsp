package parser

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/ast"
)

// TestParser_DefaultCommodityAndCommodityStyleDisagree pins how hledger 1.52.4
// reads a number when a commodity directive and a D directive declare different
// decimal marks. A bare number takes the D directive's mark, and a symbol that
// has its own style takes that style whatever D says. A D number that ends in
// its mark is accepted whatever mark the commodity declares. A symbol-less
// commodity style declared before D wins for bare numbers; one declared after D
// changes nothing. The expected readings come from hledger.
func TestParser_DefaultCommodityAndCommodityStyleDisagree(t *testing.T) {
	tests := []struct {
		directives, amount string
		accepted           bool
		qty, commodity     string
	}{
		{"commodity 1.000,00 X\nD 1000. X\n", "15,000", true, "15000", "X"},
		{"commodity 1.000,00 X\nD 1000. X\n", "1.500", true, "1.5", "X"},
		{"commodity 1.000,00 X\nD 1000. X\n", "15,000 X", true, "15", "X"},
		{"commodity 1.000,00 X\nD 1000. X\n", "1.500 X", true, "1500", "X"},
		{"commodity 1.000,00 X\nD 1000. X\n", "15,000 Y", true, "15000", "Y"},
		{"commodity 1,000.00 X\nD 1000, X\n", "15,000", true, "15", "X"},
		{"commodity 1,000.00 X\nD 1000, X\n", "1.500", true, "1500", "X"},
		{"commodity 1,000.00 X\nD 1000, X\n", "15,000 X", true, "15000", "X"},
		{"commodity 1,000.00 X\nD 1000, X\n", "1.500 X", true, "1.5", "X"},
		{"D 1000. X\ncommodity 1.000,00 X\n", "15,000", true, "15000", "X"},
		{"D 1000. X\ncommodity 1.000,00 X\n", "1.500", true, "1.5", "X"},
		{"D 1000. X\ncommodity 1.000,00 X\n", "15,000 X", true, "15", "X"},
		{"D 1000. X\ncommodity 1.000,00 X\n", "1.500 X", true, "1500", "X"},
		{"D 1000, X\ncommodity 1,000.00 X\n", "15,000", true, "15", "X"},
		{"D 1000, X\ncommodity 1,000.00 X\n", "1.500", true, "1500", "X"},
		{"D 1000, X\ncommodity 1,000.00 X\n", "15,000 X", true, "15000", "X"},
		{"commodity 1.000,00 X\nD 1000. Y\n", "15,000", true, "15000", "Y"},
		{"commodity 1.000,00 X\nD 1000. Y\n", "15,000 X", true, "15", "X"},
		{"commodity 1.000,00 X\nD 1000. Y\n", "15,000 Y", true, "15000", "Y"},
		{"commodity 1.000,00 Y\nD 1000. X\n", "15,000", true, "15000", "X"},
		{"commodity 1.000,00 Y\nD 1000. X\n", "15,000 X", true, "15000", "X"},
		{"commodity 1.000,00 Y\nD 1000. X\n", "15,000 Y", true, "15", "Y"},
		{"D 1000. X\ncommodity 1.000,00\n", "15,000", true, "15000", "X"},
		{"D 1000. X\ncommodity 1.000,00\n", "15,000 X", true, "15000", "X"},
		{"commodity 1.000,00\nD 1000. X\n", "15,000", true, "15", "X"},
		{"commodity 1.000,00\nD 1000. X\n", "15,000 X", true, "15000", "X"},
		{"commodity 1000, X\nD 1000. X\n", "15,000", true, "15000", "X"},
		{"commodity 1000, X\nD 1000. X\n", "15,000 X", true, "15", "X"},
		{"decimal-mark ,\ncommodity 1.000,00 X\nD 1000. X\n", "15,000", true, "15", "X"},
		{"decimal-mark ,\ncommodity 1.000,00 X\nD 1000. X\n", "15,000 X", true, "15", "X"},
		{"decimal-mark .\ncommodity 1.000,00 X\nD 1000, X\n", "15,000", true, "15000", "X"},
		{"decimal-mark .\ncommodity 1.000,00 X\nD 1000, X\n", "15,000 X", true, "15000", "X"},
		{"commodity 1.000,00 X\nD 1000.00 X\n", "15,000", false, "", ""},
		{"commodity 1.000,00\nD 1000.00\n", "15,000", false, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.directives+tt.amount, func(t *testing.T) {
			qty, commodity, errs := firstAmount(t, tt.directives+"\n2024-01-01 x\n    a  "+tt.amount+"\n    b\n")
			if !tt.accepted {
				assert.NotEmpty(t, errs)
				return
			}
			require.Empty(t, errs)
			assert.Equal(t, tt.qty, qty)
			assert.Equal(t, tt.commodity, commodity)
		})
	}
}

// TestParser_SymbolLessNumberUnderDefaultCommodity pins how hledger 1.52.4 reads
// a D or commodity number written without a symbol while a D directive is in
// force: it is an amount of the D directive's commodity in the D directive's
// style. It neither changes the mark in force nor needs a decimal mark of its
// own, and a symbol-less D keeps the D commodity. The expected readings come
// from hledger.
func TestParser_SymbolLessNumberUnderDefaultCommodity(t *testing.T) {
	tests := []struct {
		directives, amount string
		qty, commodity     string
	}{
		{"D 1000.00\nD 1000,\n", "15,000", "15000", ""},
		{"D 1000.00\nD 1000,\n", "1,5", "15", ""},
		{"D 1000.00\nD 1000,\n", "1.500", "1.5", ""},
		{"D 1000.\nD 1000,\n", "15,000", "15000", ""},
		{"D 1,000.00\nD 1000,\n", "15,000", "15000", ""},
		{"D 1000,00\nD 1000.\n", "15,000", "15", ""},
		{"D 1000,00\nD 1000.\n", "1.500", "1500", ""},
		{"D 1000. X\nD 1000,\n", "15,000", "15000", "X"},
		{"D 1000. X\nD 1000,\n", "1,5", "15", "X"},
		{"D 1.000,00 RUB\nD 1.000,00\n", "1.000,00", "1000", "RUB"},
		{"D 1.000,00 RUB\nD 1.000,00\n", "15,000", "15", "RUB"},
		{"D 1000.00\nD 1000\n", "15,000", "15000", ""},
		{"D 1000.00\nD 1000,00\n", "15,000", "15000", ""},
		{"D 1000.00 X\ncommodity 1000\n", "15,000", "15000", "X"},
		{"D 1000.00 X\ncommodity 1000,00\n", "15,000", "15000", "X"},
		{"D 1000.00 X\ncommodity 1000,00\n", "15,000 X", "15000", "X"},
		{"D 1000.00 X\ncommodity 1,000.\n", "15,000", "15000", "X"},
		{"D 1000,\ncommodity 1,000.00\nD 1000.00\n", "15,000", "15", ""},
		{"D 1000,\ncommodity 1,000.00\nD 1000.00\n", "1.500", "1500", ""},
		{"D 1000. X\nD 1000, X\n", "15,000", "15", "X"},
	}
	for _, tt := range tests {
		t.Run(tt.directives+tt.amount, func(t *testing.T) {
			qty, commodity, errs := firstAmount(t, tt.directives+"\n2024-01-01 x\n    a  "+tt.amount+"\n    b\n")
			require.Empty(t, errs)
			assert.Equal(t, tt.qty, qty)
			assert.Equal(t, tt.commodity, commodity)
		})
	}
}

// A symbol-less D keeps the side the D in force writes its symbol on, so a
// bare amount is still laid out the way that D writes it.
func TestParser_SymbolLessDefaultCommodityKeepsCommoditySide(t *testing.T) {
	journal, errs := Parse("D X 1000.00\nD 1000,\n\n2024-01-01 x\n    a  15\n    b\n")
	require.Empty(t, errs)
	commodity := journal.Transactions[0].Postings[0].Amount.Commodity
	assert.Equal(t, "X", commodity.Symbol)
	assert.Equal(t, ast.CommodityLeft, commodity.Position)
}

// TestParser_DirectiveNumberEndsInInvalidSeparator pins the D and
// commodity numbers hledger 1.52.4 rejects with "invalid use of separator": a
// separator at the end that the number already uses before it, or one after a
// number whose decimal mark is already settled by a space digit group and the
// other separator. A D directive in force does not make them valid, and neither
// does a number the lexer reads as a date.
func TestParser_DirectiveNumberEndsInInvalidSeparator(t *testing.T) {
	rejected := []string{
		"D 1.000. X\n",
		"D 1,000, X\n",
		"D 1.000.000. X\n",
		"D 1,000.00. X\n",
		"D 1.000,00, X\n",
		"D 1,000.00,\n",
		"commodity 1.000. X\n",
		"commodity 1,000, X\n",
		"D 1000.00\nD 1.000.\n",
		"D 1000.00 X\ncommodity 1.000.\n",
		"D 1000.00 X\ncommodity 1,000.00.\n",
		"D 1000.00 X\nD 1,000.00,\n",
		"D 1000.00.\n",
		"D 1000.00. X\n",
		"D 1000.00 X\nD 1000.00.\n",
		"D 1 000.00, X\n",
		"D 1 000,00. X\n",
		"D 1 000,000. X\n",
		"D 1 000.000, X\n",
		"commodity 1 000.00, X\n",
		"D 1000.00 X\nD 1 000.000,\n",
	}
	for _, directives := range rejected {
		t.Run(directives, func(t *testing.T) {
			_, errs := Parse(directives + "\n2024-01-01 x\n    a  15,000\n    b\n")
			require.Len(t, errs, 1)
			assert.Equal(t, strings.Count(directives, "\n"), errs[0].Pos.Line, "the error points at the last directive")
			assert.Contains(t, errs[0].Message, "invalid number")
		})
	}

	accepted := []struct{ directives, qty string }{
		{"D 1,000. X\n", "15000"},
		{"D 1.000, X\n", "15"},
		{"commodity 1,000. X\n", "15000"},
		{"D 1,5. X\n", "15000"},
		{"D 1 000. X\n", "15000"},
		{"D 1 000 000, X\n", "15"},
		{"D 1.000.000, X\n", "15"},
		{"D 1,000,000. X\n", "15000"},
	}
	for _, tt := range accepted {
		t.Run(tt.directives, func(t *testing.T) {
			qty, _, errs := firstAmount(t, tt.directives+"\n2024-01-01 x\n    a  15,000 X\n    b\n")
			require.Empty(t, errs)
			assert.Equal(t, tt.qty, qty)
		})
	}
}

// A D number the lexer reads as a date is still a number to hledger 1.52.4:
// `D 1000.00.00 X` has no decimal mark and is an error, while under a D already
// in force the symbol-less `D 1000.00.00` is read in that D's style.
func TestParser_DefaultCommodityNumberThatLooksLikeADate(t *testing.T) {
	_, errs := Parse("D 1000.00.00 X\n\n2024-01-01 x\n    a  1,5\n    b\n")
	require.Len(t, errs, 1)
	assert.Equal(t, missingDecimalMarkMessage, errs[0].Message)

	_, errs = Parse("D 2024-01-01\n\n2024-01-01 x\n    a  1,5\n    b\n")
	require.Len(t, errs, 1)
	assert.Equal(t, missingDecimalMarkMessage, errs[0].Message)

	qty, commodity, errs := firstAmount(t, "D 1000.00 X\nD 1000.00.00\n\n2024-01-01 x\n    a  1,5\n    b\n")
	require.Empty(t, errs)
	assert.Equal(t, "15", qty)
	assert.Equal(t, "X", commodity)

	// hledger reads only 2024 of `2024-01-15,` and ignores the rest of the line,
	// so under a D in force the line changes nothing.
	qty, commodity, errs = firstAmount(t, "D 1000.00 X\nD 2024-01-15,\n\n2024-01-01 x\n    a  1,5\n    b\n")
	require.Empty(t, errs)
	assert.Equal(t, "15", qty)
	assert.Equal(t, "X", commodity)
}

// A commodity directive with neither a symbol nor a number on its line is an
// error in hledger 1.52.4 ("unexpected newline"); a D directive in force must
// not make its format subdirective read as D's style without a diagnostic.
func TestParser_SymbolLessCommodityWithFormatSubdirectiveUnderDefaultCommodity(t *testing.T) {
	_, errs := Parse("D 1000.00 X\ncommodity\n    format 1000,00\n\n2024-01-01 x\n    a  1,5\n    b\n")
	assert.NotEmpty(t, errs)
}

// A format subdirective for a symbol that is a separator itself keeps the
// number's own separators, whichever side the symbol is written on and whether
// or not a space parts it from the number. A number that ends in the same
// separator as the symbol keeps that separator as its decimal mark when the
// symbol comes first (`, 1.000,`); glued after the number (`1.000,,`) it makes
// the number invalid. The marks come from hledger 1.52.4.
func TestParser_FormatSubdirectiveOfSeparatorSymbol(t *testing.T) {
	readings := map[string][]struct{ amount, qty string }{
		",": {{"15,000 ,", "15"}, {", 15,000", "15"}, {"1.500 ,", "1500"}, {"1,5 ,", "1.5"}},
		".": {{"15,000 ,", "15000"}, {", 15,000", "15000"}, {"1.500 ,", "1.5"}, {"1,5 ,", "15"}},
	}
	formats := []struct{ format, mark string }{
		{", 1.000,", ","},
		{",1.000,", ","},
		{", 1000,", ","},
		{",1000,", ","},
		{", 1.000,00", ","},
		{",1.000,00", ","},
		{", 1,000.", "."},
		{",1,000.", "."},
		{", 1,000.00", "."},
		{"1.000, ,", ","},
		{"1,000. ,", "."},
		{"1,000.00 ,", "."},
		{"1.000,00 ,", ","},
	}
	for _, f := range formats {
		for _, r := range readings[f.mark] {
			t.Run(f.format+" "+r.amount, func(t *testing.T) {
				qty, commodity, errs := firstAmount(t, "commodity ,\n    format "+f.format+"\n\n2024-01-01 x\n    a  "+r.amount+"\n    b\n")
				require.Empty(t, errs)
				assert.Equal(t, r.qty, qty)
				assert.Equal(t, ",", commodity)
			})
		}
	}

	for _, format := range []string{"1.000,,", "1,000.,"} {
		t.Run(format, func(t *testing.T) {
			_, errs := Parse("commodity ,\n    format " + format + "\n\n2024-01-01 x\n    a  15,000 ,\n    b\n")
			var messages []string
			for _, err := range errs {
				messages = append(messages, err.Message)
			}
			assert.Contains(t, messages, "invalid number: "+format)
		})
	}
}
