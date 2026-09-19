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
