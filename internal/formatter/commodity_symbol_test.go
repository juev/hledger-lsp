package formatter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/juev/hledger-lsp/internal/textutil"
)

// Formatting must not change how the amount is read: a comma glued to a symbol
// is part of it, so a number that starts with the decimal comma keeps its space.
func TestFormatter_CommoditySymbolWithPunctuationKeepsReading(t *testing.T) {
	amounts := []string{
		",5", "$,5", ", 5", "€,5", "EUR,5", "a,b 5", "AU$5", "!5", "%5", "(5",
		"5 !", "5 (x)", "5 EUR!", "5 x|y", "5 x:y", "5 ,", "5 $,", "-,5", "-! 5",
		"$ ,5", "$ ,500", "-$ ,5", "$ -,5", "EUR ,5", "-EUR ,5", `"A B" ,5`, ", ,5",
		"5 руб,", "5 元,", "руб,5", "元,5", "руб ,5", "5 «»", "😀 5", "5 ‰", "×5",
		"-EUR - 5", "-AU$ - 5", "-AU$5",
	}
	for _, amount := range amounts {
		t.Run(amount, func(t *testing.T) {
			for _, eol := range []string{"\n", "\r\n"} {
				input := textutil.NormalizeLineEndings(strings.ReplaceAll("2024-01-01 x\n    a  "+amount+"\n    b\n", "\n", eol))
				before, errs := parser.Parse(input)
				require.Empty(t, errs)

				once := formatForOracle(t, input)
				after, errs := parser.Parse(once)
				require.Empty(t, errs, "formatted journal:\n%s", once)

				want, got := before.Transactions[0].Postings[0].Amount, after.Transactions[0].Postings[0].Amount
				assert.True(t, want.Quantity.Equal(got.Quantity), "quantity %s became %s in:\n%s", want.Quantity, got.Quantity, once)
				assert.Equal(t, want.Commodity.Symbol, got.Commodity.Symbol, "formatted journal:\n%s", once)
				assert.Equal(t, once, formatForOracle(t, once), "format(format(x)) must equal format(x)")
			}
		})
	}
}

func TestFormatter_CommoditySymbolWithPunctuationAlignsWithOtherAmounts(t *testing.T) {
	input := "2024-01-01 x\n    a  5 元,\n    b  -10 USD\n    c  $ ,5\n    d  5 ‰\n"
	once := formatForOracle(t, input)
	assert.Equal(t, once, formatForOracle(t, once))
	journal, errs := parser.Parse(once)
	require.Empty(t, errs)
	symbols := []string{"元,", "USD", "$", "‰"}
	for i, posting := range journal.Transactions[0].Postings {
		assert.Equal(t, symbols[i], posting.Amount.Commodity.Symbol)
	}
}
