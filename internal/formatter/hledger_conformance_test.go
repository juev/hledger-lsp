package formatter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/parser"
)

// applyLineEdits applies whole-line replacements, which is what the document
// formatter emits for posting lines.
func applyLineEdits(t *testing.T, content string, edits []protocol.TextEdit) string {
	t.Helper()

	lines := strings.Split(content, "\n")
	for _, edit := range edits {
		require.Equal(t, edit.Range.Start.Line, edit.Range.End.Line, "expected a single-line edit")
		line := int(edit.Range.Start.Line)
		require.Less(t, line, len(lines), "edit line must exist")
		lines[line] = edit.NewText
	}
	return strings.Join(lines, "\n")
}

func TestFormatDocument_FixedLotCostPreserved(t *testing.T) {
	// hledger 1.52.4 accepts {=PRICE} as a fixed lot cost. Formatting used to
	// re-emit it as a balance assertion (`= $100`), silently changing the meaning.
	input := "2024-01-01 buy\n    assets:stocks  10 AAPL {=$100}\n    assets:cash  $-100\n"

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	require.NotNil(t, journal.Transactions[0].Postings[0].LotPrice)

	formatted := applyLineEdits(t, input, FormatDocument(journal, input))
	assert.Contains(t, formatted, "{=$100}")
	assert.NotContains(t, formatted, " = $100")
}

func TestFormatDocument_TotalFixedLotCostPreserved(t *testing.T) {
	input := "2024-01-01 buy\n    assets:stocks  10 AAPL {{=$1000}}\n    assets:cash  $-1000\n"

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	formatted := applyLineEdits(t, input, FormatDocument(journal, input))
	assert.Contains(t, formatted, "{{=$1000}}")
}

func TestFormatDocument_LeavesUnparsedLineAlone(t *testing.T) {
	// `:=` is not hledger syntax; the parser reports it and marks the posting with
	// UnparsedTail. Formatting must not rebuild that line, or the text would be
	// lost, but it must still format the rest of the transaction.
	input := "2024-01-01 x\n    a:aa  10 AAPL := $1000\n    b:bb  $5\n    c:c  $7\n"

	journal, errs := parser.Parse(input)
	require.NotEmpty(t, errs, ":= is reported as an error")
	require.Len(t, journal.Transactions, 1)
	require.Len(t, journal.Transactions[0].Postings, 3)

	formatted := applyLineEdits(t, input, FormatDocument(journal, input))
	formattedLines := strings.Split(formatted, "\n")

	assert.Equal(t, "    a:aa  10 AAPL := $1000", formattedLines[1],
		"the line with unparsed text must stay exactly as written")
	assert.Equal(t, "    c:c   $7", formattedLines[3],
		"the other postings are still aligned to the same amount column")
}

func TestFormatDocument_IdempotentForFixedLotCost(t *testing.T) {
	input := "2024-01-01 buy\n    assets:stocks  10 AAPL {=$100}\n    assets:cash  $-100\n"

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	once := applyLineEdits(t, input, FormatDocument(journal, input))

	second, errs2 := parser.Parse(once)
	require.Empty(t, errs2)
	twice := applyLineEdits(t, once, FormatDocument(second, once))

	assert.Equal(t, once, twice, "format(format(x)) must equal format(x)")
}

func TestFormatDocument_PreservesAlignedCommentColumn(t *testing.T) {
	// A hand-aligned block of inline comments must keep its column: formatting
	// used to pull every comment to two spaces after its own amount.
	input := "2024-01-01 test\n" +
		"    expenses:food  $50        ; groceries\n" +
		"    assets:cash    $-50       ; cash\n"

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	formatted := applyLineEdits(t, input, FormatDocument(journal, input))
	lines := strings.Split(formatted, "\n")

	assert.Equal(t, 30, displayColumnOf(t, lines[1], "; groceries"), "comment column is preserved")
	assert.Equal(t, 30, displayColumnOf(t, lines[2], "; cash"), "both comments stay in one column")

	second, errs2 := parser.Parse(formatted)
	require.Empty(t, errs2)
	assert.Equal(t, formatted, applyLineEdits(t, formatted, FormatDocument(second, formatted)),
		"format(format(x)) must equal format(x)")
}

func TestFormatDocument_LoneCommentKeepsTwoSpaces(t *testing.T) {
	// With a single comment there is no column to preserve, so the comment stays
	// two spaces after its posting body.
	input := "2024-01-01 test\n" +
		"    expenses:food  $50  ; groceries\n" +
		"    assets:cash    $-50\n"

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	formatted := applyLineEdits(t, input, FormatDocument(journal, input))
	lines := strings.Split(formatted, "\n")
	assert.Contains(t, lines[1], "$50  ; groceries")
}

func TestFormatDocument_CommentColumnSurvivesCJKAccounts(t *testing.T) {
	// The preserved column is a display column, so wide characters before the
	// comment must not shift it.
	input := "2024-01-01 test\n" +
		"    费用:食物  $50             ; groceries\n" +
		"    資産:現金  $-50            ; cash\n"

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	formatted := applyLineEdits(t, input, FormatDocument(journal, input))
	lines := strings.Split(formatted, "\n")

	first := displayColumnOf(t, lines[1], "; groceries")
	second := displayColumnOf(t, lines[2], "; cash")
	assert.Equal(t, first, second, "comments stay in one display column")
	assert.Equal(t, 31, first)
}

func TestFormatDocument_CJKJournalSettlesAfterOnePass(t *testing.T) {
	// A CJK-only journal has no ASCII sibling to fall back on, so mixing rune
	// columns with display columns used to make every save shift the amounts.
	input := "2024-01-01 test\n" +
		"    資産:現金         100 CNY\n" +
		"    費用:food         20 CNY\n" +
		"    資産:銀行         -120 CNY\n"

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	first := applyLineEdits(t, input, FormatDocument(journal, input))

	second, errs2 := parser.Parse(first)
	require.Empty(t, errs2)
	assert.Equal(t, first, applyLineEdits(t, first, FormatDocument(second, first)),
		"format(format(x)) must equal format(x) for a CJK-only journal")

	lines := strings.Split(first, "\n")
	require.Len(t, lines, 5, "three postings and the trailing empty line")
	for _, line := range lines[1:4] {
		assert.Regexp(t, `\S {2,}\S`, line, "amount stays at least two spaces from the account: %q", line)
	}
}

func TestDetectExistingAmountColumn_UsesDisplayColumns(t *testing.T) {
	// The parser reports rune columns, so "資産:現金" before the amount must be
	// converted into display cells: the amount starts at display column 22 while
	// its rune column is 19.
	input := "2024-01-01 test\n    資産:現金         100 CNY\n    費用:food         20 CNY\n"

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	assert.Equal(t, 22, DetectExistingAmountColumn(input, AllPostings(journal)),
		"detection must return a display column")
}

func TestDetectExistingAmountEndColumn_UsesDisplayColumns(t *testing.T) {
	input := "2024-01-01 test\n    資産:現金         100 CNY\n"

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	// "100 CNY" starts at display column 22 and is 7 cells wide.
	assert.Equal(t, 29, DetectExistingAmountEndColumn(input, AllPostings(journal), nil, AlignTargetCost))
}

func TestFormatDocument_FormatsPeriodicAndAutoRulePostings(t *testing.T) {
	input := "~ monthly\n" +
		"    expenses:food  $500\n" +
		"    assets:cash\n" +
		"\n" +
		"= expenses:food\n" +
		"    (budget:food)  $500\n" +
		"\n" +
		"2024-01-01 real\n" +
		"    expenses:food  $10\n" +
		"    assets:cash\n"

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	formatted := applyLineEdits(t, input, FormatDocument(journal, input))
	lines := strings.Split(formatted, "\n")

	amountColumn := displayColumnOf(t, lines[1], "$500")
	assert.Equal(t, amountColumn, displayColumnOf(t, lines[5], "$500"),
		"periodic and auto-rule postings align with the transaction's postings")
	assert.Equal(t, amountColumn, displayColumnOf(t, lines[8], "$10"))
	assert.Contains(t, lines[1], "expenses:food  $500", "amount stays two spaces from the account")
}
