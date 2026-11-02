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

// settleLines are posting lines whose alignment depends on the layout of the
// pass: amountless postings with only a balance assertion, a posting status
// that makes one head wider than the natural account column, CJK and Cyrillic
// accounts and commodities, costs, and amounts on either side.
var settleLines = []string{
	"    a  1 EUR",
	"    cash  100.50 EUR",
	"    h  = 10 EUR",
	"    h  == 10 EUR",
	"    x:y  2 漢字 @ 2 USD",
	"    * активы:банк  1.00 EUR = 2 漢字",
	"    ! (活期)  $5",
	"    [e]  = 5 EUR",
	"    f",
	"    g  -5 руб",
	"    ! (assets:cash)  $100.50 @ 2 USD",
	"    活期  1,000.00 円",
}

func settleOptions() []Options {
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

func formatSettled(t *testing.T, input string, opts Options) string {
	t.Helper()
	journal, errs := parser.Parse(input)
	require.Empty(t, errs, "input %q", input)
	return applyEdits(input, FormatDocumentWithOptions(journal, input, ExtractCommodityFormats(journal.Directives), opts))
}

func settleJournals() []string {
	var journals []string
	for _, a := range settleLines {
		journals = append(journals, "2024-01-01 x\n"+a+"\n")
		for _, b := range settleLines {
			journals = append(journals, "2024-01-01 x\n"+a+"\n"+b+"\n")
			for _, c := range settleLines {
				journals = append(journals, "2024-01-01 x\n"+a+"\n"+b+"\n"+c+"\n")
			}
		}
	}
	return journals
}

func TestFormatDocument_AlignmentIsIdempotentForEveryOptionSet(t *testing.T) {
	var failures []string
	total := 0
	for _, input := range settleJournals() {
		for _, opts := range settleOptions() {
			total++
			once := formatSettled(t, input, opts)
			if twice := formatSettled(t, once, opts); once != twice {
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

// The layout from the report: an assertion-only posting is placed where the next
// pass would look for the amount column.
func TestFormatDocument_AssertionOnlyPostingKeepsItsColumn(t *testing.T) {
	input := "2024-01-01 x\n    a  1 EUR\n    cash  100.50 EUR\n    h  = 10 EUR\n"
	once := formatForOracle(t, input)
	assert.Equal(t, once, formatForOracle(t, once))

	crlf := strings.ReplaceAll(input, "\n", "\r\n")
	assert.Equal(t, once, formatForOracle(t, textutil.NormalizeLineEndings(crlf)))
}

// A posting status makes the head of the longest account wider than the natural
// account column; with CJK text around it the second pass used to move the other
// amounts by one cell.
func TestFormatDocument_WideHeadWithStatusKeepsTheAmountColumn(t *testing.T) {
	input := "commodity 1,000.00 EUR\n2024-01-01 * payee 0    ; tag:v, other:w\n     * x:y        1 EUR = 1 \"A B\"        ; note\n  [активы:банк]\n ! (активы:банк)    1 EUR = 2 漢字   ; \n 活期\n\n"
	once := formatForOracle(t, input)
	assert.Equal(t, once, formatForOracle(t, once))

	crlf := strings.ReplaceAll(input, "\n", "\r\n")
	assert.Equal(t, once, formatForOracle(t, textutil.NormalizeLineEndings(crlf)))
}

func TestComputeAmountAlignment_MatchesComputeAlignmentWithoutTheCommentColumn(t *testing.T) {
	input := "2024-01-01 x\n    a  1 EUR  ; one\n    b  2 EUR  ; two\n    活期  = 3 EUR\n"
	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	formats := ExtractCommodityFormats(journal.Directives)

	full := ComputeAlignment(journal, input, formats, DefaultOptions())
	amounts := ComputeAmountAlignment(journal, input, formats, DefaultOptions())

	assert.NotZero(t, full.CommentColumn)
	assert.Zero(t, amounts.CommentColumn)
	full.CommentColumn = 0
	assert.Equal(t, full, amounts)
}
