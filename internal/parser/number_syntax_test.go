package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/ast"
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
