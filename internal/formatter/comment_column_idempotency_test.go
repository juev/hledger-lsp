package formatter

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/juev/hledger-lsp/internal/textutil"
)

// commentLines are posting lines with comments written at different columns, on
// postings with amounts of different widths and on amountless postings, with
// CJK and Cyrillic text in accounts, amounts and comments.
var commentLines = []string{
	"    a  1 EUR ; x",
	"    b  2 EUR    ; y",
	"    c  ; z",
	"    d ; w",
	"    cash  100.50 EUR  ; один",
	"    活期  1,000.00 円      ; 漢字",
	"    расходы:еда  -5 руб ; еда",
	"    e",
	"    f  3 EUR",
	"    h  1 EUR = 10 EUR ; assertion",
	"    i  1 EUR @ 2 USD ; cost",
	"    j  1 EUR {2 USD} ; lot",
	"    k  1 EUR @ $6590.49 ; date",
	"    l  1 EUR @@ $6590.49 ; total",
}

func commentJournal(lines ...string) string {
	return "2024-01-01 x\n" + strings.Join(lines, "\n") + "\n"
}

// commentCases is every journal of two and three postings built from
// commentLines, so a coincidence of columns after one pass is among them.
func commentCases() []string {
	var cases []string
	for _, first := range commentLines {
		for _, second := range commentLines {
			cases = append(cases, commentJournal(first, second))
			for _, third := range commentLines {
				cases = append(cases, commentJournal(first, second, third))
			}
		}
	}
	cases = append(cases,
		"2024-01-01 x\n    a  1 EUR ; x\n    b  2 EUR    ; y\n    c  ; z\n",
		"2024-01-01 x\n    a  1 EUR ; x\n    b  2 EUR    ; y\n    c  ; z\n\n2024-01-02 y\n    a  1 EUR  ; x\n    b  2 EUR  ; y\n    c  ; z\n",
	)
	return cases
}

func commentModes() []Options {
	var all []Options
	for _, mode := range []string{"", "left", "decimal"} {
		for _, column := range []int{0, 30} {
			for _, minColumn := range []int{0, 20} {
				for _, target := range []string{"", AlignTargetPosting} {
					opts := DefaultOptions()
					opts.AmountAlignmentMode = mode
					opts.AmountAlignmentColumn = column
					opts.MinAlignmentColumn = minColumn
					opts.AmountAlignmentTarget = target
					all = append(all, opts)
				}
			}
		}
	}
	return all
}

func formatComments(t *testing.T, input string, opts Options) string {
	t.Helper()
	journal, errs := parser.Parse(input)
	require.Empty(t, errs, "input %q", input)
	return applyEdits(input, FormatDocumentWithOptions(journal, input, ExtractCommodityFormats(journal.Directives), opts))
}

func TestFormatDocument_CommentColumnIsIdempotent(t *testing.T) {
	var failures []string
	total := 0
	for _, input := range commentCases() {
		for _, opts := range commentModes() {
			total++
			once := formatComments(t, input, opts)
			twice := formatComments(t, once, opts)
			if once != twice {
				failures = append(failures, fmt.Sprintf("options %+v\n input %q\n once  %q\n twice %q", opts, input, once, twice))
			}
		}
	}
	if len(failures) > 0 {
		shown := failures
		if len(shown) > 5 {
			shown = shown[:5]
		}
		t.Fatalf("%d of %d journal/option pairs are not idempotent, first:\n%s", len(failures), total, strings.Join(shown, "\n"))
	}
}

// The layout the review found under decimal alignment anchored on the posting
// amount: a cost and a comment, with no column or minimum column set.
func TestFormatDocument_CommentAfterCostUnderDecimalPostingAlignment(t *testing.T) {
	input := "2024-01-01 x\n    c  ; z\n    k  1 EUR @ $6590.49 ; date\n    k  1 EUR @ $6590.49 ; date\n"
	opts := DefaultOptions()
	opts.AmountAlignmentMode = "decimal"
	opts.AmountAlignmentTarget = AlignTargetPosting
	once := formatComments(t, input, opts)
	assert.Equal(t, once, formatComments(t, once, opts))
}

// The layout from the report: comments of two postings share a column only
// after the first pass, and the amountless posting must follow on that pass.
func TestFormatDocument_CommentOnAmountlessPostingJoinsTheColumnAtOnce(t *testing.T) {
	input := "2024-01-01\n    a  1 EUR ; x\n    b  2 EUR    ; y\n    c  ; z\n"
	once := formatForOracle(t, input)
	assert.Equal(t, once, formatForOracle(t, once))

	lines := strings.Split(once, "\n")
	column := func(line string) int { return strings.Index(line, ";") }
	assert.Equal(t, column(lines[1]), column(lines[3]), "comments of one block share a column:\n%s", once)

	crlf := strings.ReplaceAll(input, "\n", "\r\n")
	assert.Equal(t, once, formatForOracle(t, textutil.NormalizeLineEndings(crlf)))
}
