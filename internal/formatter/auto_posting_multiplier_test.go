package formatter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/parser"
)

func formatWithOptions(t *testing.T, input string, opts Options) string {
	t.Helper()
	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	return applyLineEdits(t, input, FormatDocumentWithOptions(journal, input, nil, opts))
}

// columnIn returns the display column where substr starts on the line for account.
func columnIn(t *testing.T, content, account, substr string) int {
	t.Helper()
	return displayColumnOf(t, amountLineOf(t, content, account), substr)
}

// The `*` of an auto posting rule multiplier is part of the amount text: the
// formatter keeps it and aligns it where the amount starts. Issue #86.
func TestFormatDocument_AutoPostingMultiplier(t *testing.T) {
	input := "= expenses:groceries\n" +
		"    (budget:groceries)  *0.25\n" +
		"    [budget:b]  *-$2\n" +
		"    (бюджет:еда)  *  ; без суммы\n" +
		"\n" +
		"2026-10-01 shop\n" +
		"    expenses:groceries  $10.00\n" +
		"    assets:cash\n"

	formatted := formatWithOptions(t, input, DefaultOptions())

	assert.Equal(t, "*0.25", amountTextOf(t, formatted, "(budget:groceries)"))
	assert.Equal(t, "*-$2", amountTextOf(t, formatted, "[budget:b]"))
	assert.Equal(t, "*  ; без суммы", amountTextOf(t, formatted, "(бюджет:еда)"))

	amountCol := columnIn(t, formatted, "expenses:groceries  ", "$10.00")
	assert.Equal(t, amountCol, columnIn(t, formatted, "(budget:groceries)", "*0.25"))
	assert.Equal(t, amountCol, columnIn(t, formatted, "[budget:b]", "*-$2"))
	assert.Equal(t, amountCol, columnIn(t, formatted, "(бюджет:еда)", "*"))

	assert.Equal(t, formatted, formatWithOptions(t, formatted, DefaultOptions()), "formatting is idempotent")
}

func TestFormatDocument_AutoPostingMultiplierDecimalAlignment(t *testing.T) {
	input := "= expenses\n" +
		"    (budget)  *0.25\n" +
		"\n" +
		"2026-10-01 shop\n" +
		"    expenses  $10.00\n" +
		"    assets\n"
	opts := DefaultOptions()
	opts.AmountAlignmentMode = "decimal"

	formatted := formatWithOptions(t, input, opts)

	assert.Equal(t,
		columnIn(t, formatted, "expenses  ", "."),
		columnIn(t, formatted, "(budget)", "."))
	assert.Equal(t, formatted, formatWithOptions(t, formatted, opts), "formatting is idempotent")
}

func TestFormatDocument_AutoPostingMultiplierRightAlignment(t *testing.T) {
	input := "= expenses\n" +
		"    (budget)          *2 EUR\n" +
		"\n" +
		"2026-10-01 shop\n" +
		"    expenses          10.00 EUR\n" +
		"    assets\n"

	formatted := formatWithOptions(t, input, DefaultOptions())

	assert.Equal(t, "*2 EUR", amountTextOf(t, formatted, "(budget)"))
	assert.Equal(t,
		columnIn(t, formatted, "expenses  ", "EUR"),
		columnIn(t, formatted, "(budget)", "EUR"))
	assert.Equal(t, formatted, formatWithOptions(t, formatted, DefaultOptions()), "formatting is idempotent")
}

// A hand-aligned document keeps its amount column when a multiplier is present.
func TestFormatDocument_AutoPostingMultiplierKeepsHandAlignedColumn(t *testing.T) {
	input := "= expenses\n" +
		"    (budget)                      *0.25\n" +
		"\n" +
		"2026-10-01 shop\n" +
		"    expenses                      $10.00\n" +
		"    assets\n"

	assert.Equal(t, input, formatWithOptions(t, input, DefaultOptions()))
}
