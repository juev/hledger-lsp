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

// punctuatedSymbolAmounts are amounts whose commodity holds punctuation, with the
// quantity and symbol hledger 1.52.4 reads from them.
var punctuatedSymbolAmounts = []struct {
	amount, qty, commodity string
}{
	{",5", "5", ","},
	{"$,5", "5", "$,"},
	{", 5", "5", ","},
	{"€,5", "5", "€,"},
	{"EUR,5", "5", "EUR,"},
	{"a,b 5", "5", "a,b"},
	{"AU$5", "5", "AU$"},
	{"!5", "5", "!"},
	{"%5", "5", "%"},
	{"(5", "5", "("},
	{"5 !", "5", "!"},
	{"5 (x)", "5", "(x)"},
	{"5 EUR!", "5", "EUR!"},
	{"5 x|y", "5", "x|y"},
	{"5 x:y", "5", "x:y"},
	{"5 ,", "5", ","},
	{"5 $,", "5", "$,"},
	{"-,5", "-5", ","},
	{"-! 5", "-5", "!"},
	{"- ! 5", "-5", "!"},
	{"+,5", "5", ","},
	{"5 руб,", "5", "руб,"},
	{"5 元,", "5", "元,"},
	{"руб,5", "5", "руб,"},
	{"元,5", "5", "元,"},
	{"5 «»", "5", "«»"},
	{"😀 5", "5", "😀"},
	{"5 😀", "5", "😀"},
	{"5 ‰", "5", "‰"},
	{"×5", "5", "×"},
}

func TestParser_CommoditySymbol_PunctuationInEveryAmountSlot(t *testing.T) {
	for context, template := range signedAmountContexts {
		for _, tt := range punctuatedSymbolAmounts {
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

// A comma that follows a prefix commodity and a space starts a number without an
// integer part. Glued to the symbol, or at the start of an amount, it belongs to
// the symbol.
func TestParser_CommoditySymbol_LeadingCommaNumber(t *testing.T) {
	tests := []struct {
		amount, qty, commodity string
	}{
		{"$ ,5", "0.5", "$"},
		{"$ ,500", "0.5", "$"},
		{"$ ,5E2", "50", "$"},
		{"EUR ,5", "0.5", "EUR"},
		{"-EUR ,5", "-0.5", "EUR"},
		{"-$ ,5", "-0.5", "$"},
		{"$ -,5", "-0.5", "$"},
		{"$ - ,5", "-0.5", "$"},
		{`"A B" ,5`, "0.5", "A B"},
		{", ,5", "0.5", ","},
		{"руб ,5", "0.5", "руб"},
	}
	for context, template := range signedAmountContexts {
		for _, tt := range tests {
			t.Run(context+"/"+tt.amount, func(t *testing.T) {
				qty, commodity, errs := readSignedAmount(t, context, fmt.Sprintf(template, tt.amount))
				require.Empty(t, errs)
				assert.Equal(t, tt.qty, qty)
				assert.Equal(t, tt.commodity, commodity)
			})
		}
	}
}

func TestParser_CommoditySymbol_MalformedPunctuation(t *testing.T) {
	for context, template := range signedAmountContexts {
		amounts := []string{"$ ,5,5", "$ ,5.5", "$ , 5", "$ ,"}
		if context != "price directive" {
			// A P directive ignores what follows its price.
			amounts = append(amounts, ",5 EUR", "5 EUR ,5", "$ ,5 EUR")
		}
		for _, amount := range amounts {
			t.Run(context+"/"+amount, func(t *testing.T) {
				_, _, errs := readSignedAmount(t, context, fmt.Sprintf(template, amount))
				assert.NotEmpty(t, errs)
			})
		}
	}
}

// hledger reads every character except digits, spaces and the ones below as part
// of an unquoted symbol: the sign and decimal mark start or end a number, "@",
// "=", ";", "*", "{", "}" and the quote belong to other syntax.
const notSymbolCharacters = `-+.@*;"{}=`

func TestParser_CommoditySymbol_EveryPrintableASCIICharacter(t *testing.T) {
	for c := '!'; c <= '~'; c++ {
		if c >= '0' && c <= '9' || strings.ContainsRune(notSymbolCharacters, c) {
			continue
		}
		symbol := string(c)
		for _, amount := range []string{symbol + "5", symbol + " 5", "5 " + symbol, "5" + symbol} {
			// "5," and "5 ," differ: a comma glued to the number is its decimal mark.
			if amount == "5," {
				continue
			}
			t.Run(amount, func(t *testing.T) {
				qty, commodity, errs := firstAmount(t, "2024-01-01 x\n    a  "+amount+"\n    b\n")
				require.Empty(t, errs)
				assert.Equal(t, "5", qty)
				assert.Equal(t, symbol, commodity)
			})
		}
	}
}

func TestParser_CommoditySymbol_AnnotationsAfterAmountAreNotSymbols(t *testing.T) {
	tests := []string{
		"2024-01-01 x\n    a  1 X {2 Y} [2024-01-01] (note) @ 3 Z\n    b\n",
		"2024-01-01 x\n    a  1 X [2024-01-01]\n    b\n",
		"2024-01-01 x\n    a  1 X (note)\n    b\n",
		"2024-01-01 x\n    (a)  1 X\n",
		"2024-01-01 x\n    [a]  1 X\n    [b]  -1 X\n",
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			journal, errs := Parse(input)
			require.Empty(t, errs)
			assert.Equal(t, "X", journal.Transactions[0].Postings[0].Amount.Commodity.Symbol)
		})
	}
}

// A D directive and a commodity directive read a symbol by the same rule as a
// posting, so a symbol that works in one works in all of them.
func TestParser_CommoditySymbol_DirectivesAgreeWithPostings(t *testing.T) {
	symbols := []string{",", "$,", "EUR,", "a,b", "AU$", "!", "(x)", "x|y", "x:y", "руб,", "元,", "«»", "😀", "‰", "×"}
	for _, symbol := range symbols {
		t.Run(symbol, func(t *testing.T) {
			_, commodity, errs := firstAmount(t, "2024-01-01 x\n    a  5 "+symbol+"\n    b\n")
			require.Empty(t, errs)
			assert.Equal(t, symbol, commodity)

			for _, line := range []string{"commodity 1,000.00 " + symbol, "commodity " + symbol, "commodity " + symbol + "\n    format 1,000.00 " + symbol} {
				journal, errs := Parse(line + "\n")
				require.Empty(t, errs, line)
				require.Len(t, journal.Directives, 1, line)
				dir, ok := journal.Directives[0].(ast.CommodityDirective)
				require.True(t, ok, line)
				assert.Equal(t, symbol, dir.Commodity.Symbol, line)
			}

			for _, line := range []string{"D 1,000.00 " + symbol, "D 1,000.00" + symbol, "D " + symbol + "1,000.00"} {
				_, errs := Parse(line + "\n2024-01-01 x\n    a  5\n    b\n")
				require.Empty(t, errs, line)
			}
			journal, errs := Parse("D 1,000.00 " + symbol + "\n2024-01-01 x\n    a  5\n    b\n")
			require.Empty(t, errs)
			assert.Equal(t, symbol, journal.Transactions[0].Postings[0].Amount.Commodity.Symbol)
		})
	}
}

func TestParser_CommoditySymbol_PricedCommodityOfPriceDirective(t *testing.T) {
	tests := []struct {
		line, priced, qty, priceCommodity string
	}{
		{"P 2024-01-01 , 5 USD", ",", "5", "USD"},
		{"P 2024-01-01 %% 5 USD", "%%", "5", "USD"},
		{"P 2024-01-01 A! 5 USD", "A!", "5", "USD"},
		{"P 2024-01-01 A:B 5 USD", "A:B", "5", "USD"},
		{"P 2024-01-01 A :5", "A", "5", ":"},
		{"P 2024-01-01 € 5 USD", "€", "5", "USD"},
		{"P 2024-01-01 руб, 5 USD", "руб,", "5", "USD"},
		{"P 2024-01-01 元! 5 USD", "元!", "5", "USD"},
		{"P 2024-01-01 $AB 5", "$AB", "5", ""},
		{"P 2024-01-01 €EUR 1,500", "€EUR", "1.5", ""},
		{"P 2024-01-01 $X 1,500", "$X", "1.5", ""},
		{"P 2024-01-01 AU$ 5", "AU$", "5", ""},
		{"P 2024-01-01 руб€ 5", "руб€", "5", ""},
		{"P 2024-01-01 09:30 $AB 5", "$AB", "5", ""},
		{`P 2024-01-01 "A B" 5`, "A B", "5", ""},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			journal, errs := Parse(tt.line + "\n")
			require.Empty(t, errs)
			price := journal.Directives[0].(ast.PriceDirective)
			assert.Equal(t, tt.priced, price.Commodity.Symbol)
			assert.Equal(t, tt.qty, price.Price.Quantity.String())
			assert.Equal(t, tt.priceCommodity, price.Price.Commodity.Symbol)
		})
	}
}

// hledger refuses a priced commodity that holds a digit or a syntax character
// unless it is quoted, and one that touches the price.
func TestParser_CommoditySymbol_PricedCommodityMustBeAValidSymbol(t *testing.T) {
	for _, line := range []string{
		"P 2024-01-01 A1 EUR 5",
		"P 2024-01-01 A.B EUR 5",
		"P 2024-01-01 A-B EUR 5",
		"P 2024-01-01 A+ $-5",
		"P 2024-01-01 A+ $ 5",
		"P 2024-01-01 A5 5",
		`P 2024-01-01 "A B"5`,
		"P 2024-01-01 A@ 5",
		"P 2024-01-01 A= 5",
	} {
		t.Run(line, func(t *testing.T) {
			_, errs := Parse(line + "\n")
			assert.NotEmpty(t, errs)
		})
	}
}

// hledger ignores whatever follows the price of a P directive.
func TestParser_CommoditySymbol_TextAfterThePriceIsIgnored(t *testing.T) {
	for _, line := range []string{"P 2024-01-01 A 5 EUR junk", "P 2024-01-01 A 5 EUR ,5", "P 2024-01-01 A 5 5"} {
		_, errs := Parse(line + "\n")
		assert.Empty(t, errs, line)
	}
}

func TestParser_CommoditySymbol_TabsAroundSigns(t *testing.T) {
	tests := []struct {
		amount, qty, commodity string
	}{
		{"-EUR -\t5", "5", "EUR"},
		{"-EUR\t- 5", "5", "EUR"},
		{"-EUR\t5", "-5", "EUR"},
		{"EUR -\t5", "-5", "EUR"},
		{"- EUR\t- 5", "5", "EUR"},
		{"-AU$ - 5", "5", "AU$"},
		{"-AU$ 5", "-5", "AU$"},
		{"-AU$5", "-5", "AU$"},
		{"-руб\t- 5", "5", "руб"},
	}
	for context, template := range signedAmountContexts {
		for _, tt := range tests {
			t.Run(context+"/"+tt.amount, func(t *testing.T) {
				qty, commodity, errs := readSignedAmount(t, context, fmt.Sprintf(template, tt.amount))
				require.Empty(t, errs)
				assert.Equal(t, tt.qty, qty)
				assert.Equal(t, tt.commodity, commodity)
			})
		}
	}
}
