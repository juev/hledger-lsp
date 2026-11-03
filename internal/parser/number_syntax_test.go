package parser

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/textutil"
)

func firstAmount(t *testing.T, input string) (qty, commodity string, errs []ParseError) {
	t.Helper()
	journal, errs := Parse(input)
	if len(errs) > 0 || journal == nil || len(journal.Transactions) == 0 {
		return "", "", errs
	}
	amount := journal.Transactions[0].Postings[0].Amount
	require.NotNil(t, amount)
	return amount.Quantity.String(), amount.Commodity.Symbol, nil
}

func TestParser_NumberSyntax_SignBeforePrefixCommodity(t *testing.T) {
	tests := []struct {
		amount, qty, commodity string
	}{
		{"-EUR 1,500", "-1.5", "EUR"},
		{"-EUR 1.500", "-1.5", "EUR"},
		{"- EUR 5", "-5", "EUR"},
		{"-EUR5", "-5", "EUR"},
		{"EUR -5", "-5", "EUR"},
		{"+EUR 5", "5", "EUR"},
		{"-EUR -5", "5", "EUR"},
		{"-$5", "-5", "$"},
		{"- $5", "-5", "$"},
		{`-"A B" 5`, "-5", "A B"},
		{"-руб 5", "-5", "руб"},
		{"-元 5", "-5", "元"},
	}
	for _, tt := range tests {
		t.Run(tt.amount, func(t *testing.T) {
			qty, commodity, errs := firstAmount(t, "2024-01-01 x\n    a  "+tt.amount+"\n    b\n")
			require.Empty(t, errs)
			assert.Equal(t, tt.qty, qty)
			assert.Equal(t, tt.commodity, commodity)
		})
	}
}

func TestParser_NumberSyntax_DoubleSignNeedsCommodityBetween(t *testing.T) {
	for _, amount := range []string{"--EUR 5", "--5", "- -5"} {
		_, _, errs := firstAmount(t, "2024-01-01 x\n    a  "+amount+"\n    b\n")
		assert.NotEmpty(t, errs, amount)
	}
}

func TestParser_NumberSyntax_LeadingDecimalMark(t *testing.T) {
	tests := []struct {
		directive, amount, qty, commodity string
	}{
		{"", ".5 EUR", "0.5", "EUR"},
		{"", "-.5 EUR", "-0.5", "EUR"},
		{"", "$.5", "0.5", "$"},
		{"", ".5", "0.5", ""},
		{"", ".5E2 EUR", "50", "EUR"},
		{"", ".500 EUR", "0.5", "EUR"},
		{"", ".5 руб", "0.5", "руб"},
		{"decimal-mark ,\n", ".5 EUR", "0.5", "EUR"},
		{"decimal-mark ,\n", ".5E2", "50", ""},
		{"commodity 1.000,00 EUR\n", ".5 EUR", "0.5", "EUR"},
	}
	for _, tt := range tests {
		t.Run(tt.directive+tt.amount, func(t *testing.T) {
			qty, commodity, errs := firstAmount(t, tt.directive+"2024-01-01 x\n    a  "+tt.amount+"\n    b\n")
			require.Empty(t, errs)
			assert.Equal(t, tt.qty, qty)
			assert.Equal(t, tt.commodity, commodity)
		})
	}
}

func TestParser_NumberSyntax_LeadingDecimalMarkInCostAndPrice(t *testing.T) {
	journal, errs := Parse("P 2024-01-01 Y .5 EUR\n2024-01-01 x\n    a  1 Y @ .5 EUR\n    b\n")
	require.Empty(t, errs)
	require.NotEmpty(t, journal.Directives)
	price, ok := journal.Directives[0].(ast.PriceDirective)
	require.True(t, ok)
	assert.Equal(t, "0.5", price.Price.Quantity.String())
	assert.Equal(t, "0.5", journal.Transactions[0].Postings[0].Cost.Amount.Quantity.String())
}

func TestParser_NumberSyntax_MalformedLeadingDecimalMark(t *testing.T) {
	for _, amount := range []string{".5.5 EUR", ".5,5 EUR", ".5 5 EUR", ". EUR", ".E2 EUR", "1.5 .5"} {
		_, _, errs := firstAmount(t, "2024-01-01 x\n    a  "+amount+"\n    b\n")
		assert.NotEmpty(t, errs, amount)
	}
}

func TestParser_NumberSyntax_DotInDescriptionStaysText(t *testing.T) {
	journal, errs := Parse("2024-01-01 .5 coffee\n    a  1 EUR\n    b\n")
	require.Empty(t, errs)
	assert.Equal(t, ".5 coffee", journal.Transactions[0].Description)
}

func TestParser_NumberSyntax_ExponentWithDigitSeparators(t *testing.T) {
	const message = "digit separators and exponents may not be used together"
	tests := []struct {
		directive, amount string
		qty               string // empty means rejected
	}{
		{"decimal-mark ,\n", "1.5E3 X", ""},
		{"decimal-mark ,\n", "1.5e3 X", ""},
		{"decimal-mark ,\n", "1.5E3", ""},
		{"decimal-mark ,\n", "1.500E3 X", ""},
		{"decimal-mark ,\n", "1,5E3 X", "1500"},
		{"decimal-mark ,\n", "1,500E3 X", "1500"},
		{"decimal-mark ,\n", "1E3 X", "1000"},
		{"decimal-mark ,\n", "1.E3 X", "1000"},
		{"", "1.5E3 X", "1500"},
		{"", "1,5E3 X", "1500"},
		{"", "1,500E3 X", "1500"},
		{"", "1E3 X", "1000"},
		{"", "1 500E3 X", ""},
		{"", "1,234,567E3 X", ""},
		{"", "1,000.E3 X", ""},
		{"commodity 1000.00 X\n", "1,5E3 X", ""},
		{"commodity 1000.00 X\n", "1.5E3 X", "1500"},
		{"commodity 1.000,00 X\n", "1.5E3 X", ""},
		{"commodity 1.000,00 X\n", "1,5E3 X", "1500"},
		{"commodity 1.000,00 X\n", "1.5E3", "1500"},
		{"D 1000,00\n", "1.5E3 X", ""},
	}
	for _, tt := range tests {
		t.Run(tt.directive+tt.amount, func(t *testing.T) {
			qty, _, errs := firstAmount(t, tt.directive+"2024-01-01 x\n    a  "+tt.amount+"\n    b\n")
			if tt.qty == "" {
				require.NotEmpty(t, errs)
				assert.Contains(t, errs[0].Message, message)
				assert.NotContains(t, errs[0].Message, "Token")
				return
			}
			require.Empty(t, errs)
			assert.Equal(t, tt.qty, qty)
		})
	}
}

func TestParser_NumberSyntax_ExponentErrorRangeCoversNumber(t *testing.T) {
	_, errs := Parse("decimal-mark ,\n2024-01-01 x\n    a  1.5E3 X\n    b\n")
	require.Len(t, errs, 1)
	assert.Equal(t, 3, errs[0].Pos.Line)
	assert.Equal(t, 8, errs[0].Pos.Column)
	assert.Equal(t, 13, errs[0].End.Column)
}

// signedAmountContexts place one amount spelling in each slot that reads an
// amount: a posting, a unit cost, a total cost, a balance assertion and the
// price of a P directive.
var signedAmountContexts = map[string]string{
	"posting":           "2024-01-01 x\n    a  %s\n    b\n",
	"unit cost":         "2024-01-01 x\n    a  1 X @ %s\n    b\n",
	"total cost":        "2024-01-01 x\n    a  1 X @@ %s\n    b\n",
	"balance assertion": "2024-01-01 x\n    a  = %s\n    b\n",
	"price directive":   "P 2024-01-01 A %s\n",
}

// readSignedAmount returns the amount a context's journal puts in its slot.
func readSignedAmount(t *testing.T, context, journalText string) (qty, commodity string, errs []ParseError) {
	t.Helper()
	journal, errs := Parse(journalText)
	if len(errs) > 0 {
		return "", "", errs
	}
	var amount ast.Amount
	switch context {
	case "posting":
		amount = *journal.Transactions[0].Postings[0].Amount
	case "unit cost", "total cost":
		amount = journal.Transactions[0].Postings[0].Cost.Amount
	case "balance assertion":
		amount = journal.Transactions[0].Postings[0].BalanceAssertion.Amount
	case "price directive":
		amount = journal.Directives[0].(ast.PriceDirective).Price
	}
	return amount.Quantity.String(), amount.Commodity.Symbol, nil
}

// TestParser_NumberSyntax_SecondSignAfterCommodity covers hledger's rule that a
// sign, a prefix commodity and a second sign multiply, whatever spaces sit
// between the parts (hledger 1.52.4).
func TestParser_NumberSyntax_SecondSignAfterCommodity(t *testing.T) {
	tests := []struct {
		amount, qty, commodity string
	}{
		{"-EUR - 1,500", "1.5", "EUR"},
		{"-X - 5", "5", "X"},
		{"- EUR - 5", "5", "EUR"},
		{"-EUR  -  5", "5", "EUR"},
		{"-EUR -  5", "5", "EUR"},
		{"+EUR - 5", "-5", "EUR"},
		{"-EUR + 5", "-5", "EUR"},
		{"+EUR + 5", "5", "EUR"},
		{"EUR - 5", "-5", "EUR"},
		{"-EUR - .5", "0.5", "EUR"},
		{"-EUR - 1 500", "1500", "EUR"},
		{`-"A B" - 5`, "5", "A B"},
		{"-$ - 5", "5", "$"},
		{"-руб - 5", "5", "руб"},
		{"-元 - 5", "5", "元"},
	}
	for context, template := range signedAmountContexts {
		for _, tt := range tests {
			t.Run(context+"/"+tt.amount, func(t *testing.T) {
				for _, eol := range []string{"\n", "\r\n"} {
					text := textutil.NormalizeLineEndings(strings.ReplaceAll(fmt.Sprintf(template, tt.amount), "\n", eol))
					qty, commodity, errs := readSignedAmount(t, context, text)
					require.Empty(t, errs, "%q", text)
					assert.Equal(t, tt.qty, qty)
					assert.Equal(t, tt.commodity, commodity)
				}
			})
		}
	}
}

func TestParser_NumberSyntax_SecondSignAfterCommodityNeedsOneSign(t *testing.T) {
	for context, template := range signedAmountContexts {
		for _, amount := range []string{"-EUR --5", "-EUR - - 5", "EUR - - 5", "-EUR - EUR 5"} {
			t.Run(context+"/"+amount, func(t *testing.T) {
				_, _, errs := readSignedAmount(t, context, fmt.Sprintf(template, amount))
				assert.NotEmpty(t, errs)
			})
		}
	}
}

// A P directive writes the commodity it prices before its price, so a sign in
// front of that commodity is not an amount.
func TestParser_NumberSyntax_PriceDirectiveSignBeforePricedCommodity(t *testing.T) {
	_, errs := Parse("P 2024-01-01 -EUR 5\n")
	assert.NotEmpty(t, errs)
}

func TestParser_NumberSyntax_PriceDirectiveLetterCommodityBeforeNumber(t *testing.T) {
	tests := []struct {
		line, qty, commodity string
	}{
		{"P 2024-01-01 A EUR 5", "5", "EUR"},
		{"P 2024-01-01 A EUR5", "5", "EUR"},
		{"P 2024-01-01 A EUR -5", "-5", "EUR"},
		{"P 2024-01-01 09:30 A EUR 5", "5", "EUR"},
		{"P 2024-01-01 A руб 5", "5", "руб"},
		{"P 2024-01-01 A руб -5", "-5", "руб"},
		{"P 2024-01-01 А 5 руб", "5", "руб"},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			journal, errs := Parse(tt.line + "\n")
			require.Empty(t, errs)
			price := journal.Directives[0].(ast.PriceDirective).Price
			assert.Equal(t, tt.qty, price.Quantity.String())
			assert.Equal(t, tt.commodity, price.Commodity.Symbol)
		})
	}
}

// hledger reads the longest number it can and rejects a "." or "," right after
// it as an invalid use of separator, pointing at that separator. Dropping it as
// a digit group separator instead reads 1,000.00. as 100000.
func TestParse_SeparatorAfterACompleteNumberIsRejected(t *testing.T) {
	cases := []struct {
		amount string
		column int // of the rejected separator, as hledger 1.52.4 reports it
	}{
		{"1..0 USD", 8},
		{"1,,0 USD", 8},
		{"1.,0 USD", 8},
		{"1,.0 USD", 8},
		{"1.. USD", 8},
		{"1...0 USD", 8},
		{"1.0.. USD", 9},
		{"1,000..00 USD", 12},
		{"1,000.,00 USD", 12},
		{"1 000..00 USD", 12},
		{"1.000. USD", 11},
		{"1,000.00. USD", 14},
		{"1..0E2 USD", 8},
		{"-1..0 USD", 9},
		{"USD 1..0", 12},
		{"1 USD = 1..0 USD", 16},
	}
	for _, c := range cases {
		for _, newline := range []string{"\n", "\r\n"} {
			for _, account := range []string{"a", "расходы:еда", "支出"} {
				// The server normalizes line endings before parsing, as here.
				input := textutil.NormalizeLineEndings(strings.ReplaceAll("2024-01-01 x\n  "+account+"  "+c.amount+"\n  b\n", "\n", newline))
				t.Run(fmt.Sprintf("%s %q %q", account, c.amount, newline), func(t *testing.T) {
					_, errs := Parse(input)
					require.Len(t, errs, 1, "input %q", input)
					assert.Equal(t, invalidSeparatorMessage, errs[0].Message)
					assert.Equal(t, 2, errs[0].Pos.Line)
					// The column counts characters, so the account shifts it by
					// the characters it has beyond "a".
					shift := len([]rune(account)) - 1
					assert.Equal(t, c.column+shift, errs[0].Pos.Column)
					assert.Contains(t, ".,", string(input[errs[0].Pos.Offset]), "the error points at a separator")
				})
			}
		}
	}
}

func TestParse_ValidSeparatorsStillRead(t *testing.T) {
	for amount, want := range map[string]string{
		"1,000.00 USD":     "1000",
		"1.000,50 USD":     "1000.5",
		"1 000,50 USD":     "1000.5",
		"1,000,000.00 USD": "1000000",
		"1.000.000,00 USD": "1000000",
		"10. USD":          "10",
		"10, USD":          "10",
		".5 USD":           "0.5",
		"1.5E3 USD":        "1500",
	} {
		t.Run(amount, func(t *testing.T) {
			qty, _, errs := firstAmount(t, "2024-01-01 x\n  a  "+amount+"\n  b\n")
			require.Empty(t, errs)
			assert.Equal(t, want, qty)
		})
	}
}

func TestInvalidSeparatorAt(t *testing.T) {
	for number, want := range map[string]int{
		"1..0": 2, "1.,0": 2, "1,000..00": 6, "1 000.00,": 8, "1.000.": 5, "1.0..": 3,
		"1,000.00": -1, "1.000,50": -1, "1 000": -1, "10.": -1, ".5": -1, "1.000.000": -1,
		"1..0E2": 2, "1.5E3": -1, "": -1, ".": -1, "..5": -1,
	} {
		assert.Equal(t, want, invalidSeparatorAt(number), "%q", number)
	}
}
