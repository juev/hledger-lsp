package formatter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/parser"
)

// amountLineOf returns the formatted line for the posting whose account name
// contains account, which is where a rewritten amount would show up.
func amountLineOf(t *testing.T, content, account string) string {
	t.Helper()

	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, account) {
			return line
		}
	}
	t.Fatalf("no line for account %q in:\n%s", account, content)
	return ""
}

// amountTextOf returns everything a posting line writes after its account name.
func amountTextOf(t *testing.T, content, account string) string {
	t.Helper()

	line := amountLineOf(t, content, account)
	idx := strings.Index(line, account)
	require.GreaterOrEqual(t, idx, 0)
	return strings.TrimSpace(line[idx+len(account):])
}

// Formatting must not spell out a commodity the journal writes bare: the amount
// means RUB because of the D directive, but the document keeps its own style.
func TestFormatDocument_KeepsBareAmountsBareUnderDefaultCommodity(t *testing.T) {
	input := `D 1.000,00 RUB
commodity RUB

2024-01-15 groceries
    expenses:food      -609.000,00
    assets:cash         609.000,00
`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	formatted := applyLineEdits(t, input, FormatDocument(journal, input))

	assert.Equal(t, input, formatted, "bare amounts stay exactly as written")
	assert.NotContains(t, formatted, "609.000,00 RUB")
}

func TestFormatDocument_DefaultCommodityDoesNotTouchWrittenSymbols(t *testing.T) {
	input := `D 1.000,00 RUB

2024-01-15 mixed
    assets:cash         1.000,00 USD
    assets:bank           500,00
    expenses:food        -500,00 EUR
`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	formatted := applyLineEdits(t, input, FormatDocument(journal, input))

	assert.Equal(t, "1.000,00 USD", amountTextOf(t, formatted, "assets:cash"))
	assert.Equal(t, "-500,00 EUR", amountTextOf(t, formatted, "expenses:food"))
	assert.Equal(t, "500,00", amountTextOf(t, formatted, "assets:bank"),
		"a bare amount gains no symbol from the D directive")
}

func TestFormatDocument_DefaultCommodityFormattingIsIdempotent(t *testing.T) {
	input := "D 1.000,00 RUB\n\n2024-01-15 x\n    expenses:food   10,00\n    assets:cash  -10,00\n"

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	once := applyLineEdits(t, input, FormatDocument(journal, input))
	twice := applyLineEdits(t, once, FormatDocument(mustParse(t, once), once))

	assert.Equal(t, once, twice)
	assert.NotContains(t, twice, "RUB ", "no pass may introduce the commodity symbol")
}

// A commodity-left D directive (`D $1,000.00`) must not inject a `$` into bare
// amounts, and must not leave the space that the declared layout would put
// between the symbol and the number.
func TestFormatDocument_LeftDefaultCommodityKeepsBareAmountsBare(t *testing.T) {
	input := "D $1,000.00\n\n2024-01-15 x\n    expenses:food  10.00\n    assets:cash\n"

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	formatted := applyLineEdits(t, input, FormatDocument(journal, input))

	assert.Equal(t, "10.00", amountTextOf(t, formatted, "expenses:food"),
		"the amount is bare, without an injected symbol or separator space")
	assert.NotContains(t, amountLineOf(t, formatted, "expenses:food"), "$")

	twice := applyLineEdits(t, formatted, FormatDocument(mustParse(t, formatted), formatted))
	assert.Equal(t, formatted, twice, "a commodity-left default commodity stays idempotent")
}

// Display paths state the commodity an amount belongs to, so FormatAmount
// renders the amount as written and hover clears the flag itself.
func TestFormatAmount_DefaultCommodityRendersAsWritten(t *testing.T) {
	input := "D 1.000,00 RUB\n\n2024-01-15 x\n    expenses:food  10,00\n    assets:cash\n"

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	formats := ExtractCommodityFormats(journal.Directives)
	amount := journal.Transactions[0].Postings[0].Amount
	require.NotNil(t, amount)
	assert.Equal(t, "RUB", amount.Commodity.Symbol, "the amount is RUB …")
	assert.Equal(t, "10,00", FormatAmount(amount, formats), "… but it was written without a symbol")

	declared := *amount
	declared.Commodity.Inferred = false
	assert.Equal(t, "10,00 RUB", FormatAmount(&declared, formats),
		"clearing the flag states the commodity, which is what hover does")
}

// A bare amount is read with the D directive's number format, so formatting it
// in the style of the commodity it inherits would change its value: here the
// bare `1.234` means 1234 and must not be rewritten as `1,234.00`.
func TestFormatDocument_BareAmountKeepsItsValueWhenCommodityStyleDiffers(t *testing.T) {
	input := "D 1.000,00 RUB\ncommodity 1,000.00 RUB\n\n2024-01-01 mix\n    a:aa      1.234\n    b:bb     -1,234.00 RUB\n"

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	before := journal.Transactions[0].Postings[0].Amount.Quantity

	formatted := applyLineEdits(t, input, FormatDocument(journal, input))
	assert.Equal(t, "1.234,00", amountTextOf(t, formatted, "a:aa"),
		"a bare amount is written in the D directive's format")

	reparsed, errs := parser.Parse(formatted)
	require.Empty(t, errs)
	assert.Equal(t, before.String(), reparsed.Transactions[0].Postings[0].Amount.Quantity.String(),
		"formatting must not change the value of a bare amount")
}
