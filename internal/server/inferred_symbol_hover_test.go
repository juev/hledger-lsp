package server

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/parser"
)

// Hover names the D commodity of a bare amount, so it writes the number with
// the mark that amounts written with that symbol use. With `D 1000. X` and
// `commodity 1.000,00 X`, a bare 1.500 is 1.5, and hledger 1.52.4 reads the
// hover text `1,50 X` back as 1.5, while `1.50 X` would be 150.
func TestHover_InferredSymbolIsWrittenWithItsOwnMark(t *testing.T) {
	tests := []struct {
		name, content string
		line, char    uint32
		want          string
	}{
		{
			"LF",
			"D 1000. X\ncommodity 1.000,00 X\n\n2024-01-01 x\n    a  1.500\n    b\n",
			4, 8, "**Amount:** 1,50 X",
		},
		{
			"Cyrillic CRLF",
			"D 1000. руб\r\ncommodity 1.000,00 руб\r\n\r\n2024-01-01 покупка\r\n    расходы:еда  1.500\r\n    активы:касса\r\n",
			4, 18, "**Amount:** 1,50 руб",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestServer()
			docURI := uri.URI("file:///test.journal")
			require.NoError(t, ts.openDocument(docURI, tt.content))
			assert.Equal(t, tt.want, firstHoverLine(hoverAt(t, ts, docURI, tt.line, tt.char)))
		})
	}
}

// A commodity style declared in an included file reaches the hover the same
// way: the D directive in the parent decides how the bare amount reads, and the
// included style decides how the named symbol is written.
func TestHover_InferredSymbolUsesAStyleFromAnInclude(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.journal")
	text := "D 1000. X\ninclude c.journal\n\n2024-01-01 x\n    a  1.500\n    b\n"
	writeJournals(t, dir, map[string]string{"main.journal": text, "c.journal": "commodity 1.000,00 X\n"})
	ts, docURI := formatServer(t, dir, path, text)
	assert.Equal(t, "**Amount:** 1,50 X", firstHoverLine(hoverAt(t, ts, docURI, 4, 8)))
}

// Whatever style the D directive and the commodity directive set, in either
// order, the hover text of a bare amount reads back as the amount's own value
// under the same directives.
func TestHover_InferredSymbolTextReadsBackAsTheSameValue(t *testing.T) {
	prefixes := []string{
		"D 1000. X\ncommodity 1.000,00 X\n",
		"commodity 1.000,00 X\nD 1000. X\n",
		"commodity 1.000,00 X\nD 1000. X\nD 1000,\n",
		"D 1000, X\ncommodity 1,000.00 X\n",
		"commodity 1,000.00 X\nD 1000, X\n",
		"D 1.000,00 RUB\nD 1000.\n",
	}
	for _, prefix := range prefixes {
		for _, bare := range []string{"1.500", "1,500", "1,5", "1.5"} {
			t.Run(prefix+bare, func(t *testing.T) {
				original := hoverTestAmount(t, prefix, bare)
				ts := newTestServer()
				docURI := uri.URI("file:///test.journal")
				content := prefix + "2024-01-01 x\n    a  " + bare + "\n    b\n"
				require.NoError(t, ts.openDocument(docURI, content))
				text := strings.TrimPrefix(firstHoverLine(hoverAt(t, ts, docURI, uint32(strings.Count(prefix, "\n")+1), 8)), "**Amount:** ")
				reread := hoverTestAmount(t, prefix, text)
				assert.True(t, original.Quantity.Equal(reread.Quantity), "%s reads %s, hover %q reads %s", bare, original.Quantity, text, reread.Quantity)
				assert.Equal(t, original.Commodity.Symbol, reread.Commodity.Symbol)
			})
		}
	}
}

func hoverTestAmount(t *testing.T, prefix, amount string) ast.Amount {
	t.Helper()
	journal, errs := parser.Parse(prefix + "2024-01-01 x\n    a  " + amount + "\n    b\n")
	require.Empty(t, errs)
	require.NotEmpty(t, journal.Transactions)
	require.NotNil(t, journal.Transactions[0].Postings[0].Amount)
	return *journal.Transactions[0].Postings[0].Amount
}

func firstHoverLine(content string) string {
	line, _, _ := strings.Cut(content, "\n")
	return line
}

// A file included under two D directives shows the value of each occurrence,
// and each is written with the mark of the named symbol at that occurrence.
// With `commodity 1.000,00 X` in force at both, a bare 1.500 is 1.5 under
// `D 1000. X` and 1500 under `D 1000, X`; hledger 1.52.4 reads `1,5 X` back as
// 1.5 and `1500 X` as 1500 there, while `1.5 X` would be 15.
func TestHover_InferredSymbolAcrossOccurrencesUsesEachOccurrencesMark(t *testing.T) {
	layout := occurrenceLayout{"two D directives", includeTwice("commodity 1.000,00 X\n", "D 1000. X\n", "D 1000, X\n")}
	for variant, shared := range crlfVariants("2024-01-01 x\n    a  1.500\n    b\n") {
		t.Run(variant, func(t *testing.T) {
			ts, _, docURI, _ := setupOccurrences(t, layout, shared)
			content := hoverAt(t, ts, docURI, 1, 8)
			assert.Contains(t, content, "**Amount:** 1,5 X")
			assert.Contains(t, content, "**Amount:** 1500 X")
			assert.NotContains(t, content, "**Amount:** 1.5 X")
		})
	}
}

// Formatting a journal whose separator symbol is written before a number that
// ends in the same separator keeps the values hledger 1.52.4 reads: with
// `format , 1.000,` the mark is the trailing comma, so `15,000 ,` is 15.
func TestFormat_SeparatorSymbolFormatKeepsValues(t *testing.T) {
	tests := []struct{ format, amount, qty string }{
		{", 1.000,", "15,000 ,", "15"},
		{",1.000,", ", 15,000", "15"},
		{",1,000.", "15,000 ,", "15000"},
		{"1.000, ,", "1.500 ,", "1500"},
		{"1,000. ,", "1,5 ,", "15"},
	}
	for _, tt := range tests {
		for variant, text := range crlfVariants("commodity ,\n    format " + tt.format + "\n2024-01-01 x\n    a  " + tt.amount + "\n    b\n") {
			t.Run(tt.format+" "+tt.amount+" "+variant, func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "main.journal")
				writeJournals(t, dir, map[string]string{"main.journal": text})
				ts, docURI := formatServer(t, dir, path, text)
				once := formatted(t, ts, docURI)
				journal, errs := parser.Parse(once)
				require.Empty(t, errs, once)
				assert.Equal(t, tt.qty, journal.Transactions[0].Postings[0].Amount.Quantity.String(), once)
			})
		}
	}
}
