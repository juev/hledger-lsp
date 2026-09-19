package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/ast"
)

func TestParser_AsteriskCommentLine(t *testing.T) {
	// hledger treats a line starting with '*' as a comment, which makes org-mode
	// outline headings legal in a journal (docs/hledger.md: the comment forms are
	// ';', '#' and '*'). Verified with hledger 1.52.4: this journal exits 0.
	input := "* My outline heading\n\n2024-01-01 x\n    a:aa  $1\n    b:bb\n"

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)
	require.Len(t, journal.Comments, 1)
	assert.Contains(t, journal.Comments[0].Text, "My outline heading")
}

func TestParser_AsteriskCommentDoesNotSwallowNextTransaction(t *testing.T) {
	input := "* heading\n2024-01-01 x\n    a:aa  $1\n    b:bb\n"

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)
	assert.Equal(t, "x", journal.Transactions[0].Description)
}

func TestParser_StatusMarksStillWorkInPostings(t *testing.T) {
	input := "2024-01-01 x\n    * a:aa  $1\n    ! b:bb\n"

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)
	require.Len(t, journal.Transactions[0].Postings, 2)
	assert.Equal(t, ast.StatusCleared, journal.Transactions[0].Postings[0].Status)
	assert.Equal(t, ast.StatusPending, journal.Transactions[0].Postings[1].Status)
}

func TestParser_PercentLineReportsError(t *testing.T) {
	// '%' is not a comment form in hledger 1.52.4: it refuses the journal, so the
	// server must report the line instead of ignoring it.
	input := "% percent comment\n2024-01-01 x\n    a:aa  $1\n    b:bb\n"

	_, errs := Parse(input)
	require.NotEmpty(t, errs)
	assert.Equal(t, CodeParseUnexpected, errs[0].Code)
	assert.Contains(t, errs[0].Message, "unexpected content")
}

func TestParser_PriceDirectiveWithTime(t *testing.T) {
	// hledger accepts an optional clock time between the date and the commodity:
	// P 2024-01-15 09:30 EUR $1.08 (verified with hledger 1.52.4).
	tests := map[string]string{
		"hours and minutes":       "P 2024-01-15 09:30 EUR $1.08\n",
		"single digit hour":       "P 2024-01-15 9:30 EUR $1.08\n",
		"hours minutes seconds":   "P 2024-01-15 09:30:15 EUR $1.08\n",
		"without time still work": "P 2024-01-15 EUR $1.08\n",
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			journal, errs := Parse(input)
			require.Empty(t, errs)
			require.Len(t, journal.Directives, 1)

			price, ok := journal.Directives[0].(ast.PriceDirective)
			require.True(t, ok, "directive is a price directive")
			assert.Equal(t, 2024, price.Date.Year)
			assert.Equal(t, 1, price.Date.Month)
			assert.Equal(t, 15, price.Date.Day)
			assert.Equal(t, "EUR", price.Commodity.Symbol)
			assert.Equal(t, "1.08", price.Price.Quantity.String())
		})
	}
}

func TestParser_FixedLotCostParsing(t *testing.T) {
	tests := []struct {
		name      string
		amount    string
		wantTotal bool
		wantFixed bool
		wantCost  string
	}{
		{name: "plain lot price", amount: "10 AAPL {$100}", wantCost: "100"},
		{name: "fixed lot cost", amount: "10 AAPL {=$100}", wantFixed: true, wantCost: "100"},
		{name: "total fixed lot cost", amount: "10 AAPL {{=$1000}}", wantTotal: true, wantFixed: true, wantCost: "1000"},
		{name: "total lot price", amount: "10 AAPL {{$1000}}", wantTotal: true, wantCost: "1000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := "2024-01-01 buy\n    assets:stocks  " + tt.amount + "\n    assets:cash\n"

			journal, errs := Parse(input)
			require.Empty(t, errs)
			require.Len(t, journal.Transactions, 1)
			require.Len(t, journal.Transactions[0].Postings, 2)

			lot := journal.Transactions[0].Postings[0].LotPrice
			require.NotNil(t, lot, "lot price must be parsed")
			assert.Equal(t, tt.wantTotal, lot.IsTotal)
			assert.Equal(t, tt.wantFixed, lot.Fixed)
			require.NotNil(t, lot.Cost)
			assert.Equal(t, tt.wantCost, lot.Cost.Quantity.String())
		})
	}
}

func TestParser_IndentedCommentLineAttachedToTransaction(t *testing.T) {
	// hledger applies tags from an indented comment line inside a transaction to
	// the transaction and all its postings (verified: `hledger reg tag:project`
	// matches this transaction), so the parser must not discard the comment.
	input := "2024-01-01 x\n    a:aa  $1\n    ; project:trip\n    b:bb\n"

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	tx := journal.Transactions[0]
	require.Len(t, tx.Comments, 1)
	require.Len(t, tx.Comments[0].Tags, 1)
	assert.Equal(t, "project", tx.Comments[0].Tags[0].Name)
	assert.Equal(t, "trip", tx.Comments[0].Tags[0].Value)
	assert.Equal(t, 3, tx.Comments[0].Range.Start.Line)
}

func TestParser_UnparsedTailRecorded(t *testing.T) {
	// `:=` is not hledger syntax (1.52.4 rejects it with "unexpected ':'"), so the
	// parser reports it and records the leftover text. The formatter relies on
	// UnparsedTail to leave the line untouched instead of erasing the text.
	input := "2024-01-01 x\n    a:aa  10 AAPL := $1000\n    b:bb\n"

	journal, errs := Parse(input)
	require.NotEmpty(t, errs)
	assert.Equal(t, CodeParseUnexpected, errs[0].Code)
	require.Len(t, journal.Transactions, 1)
	require.Len(t, journal.Transactions[0].Postings, 2)

	posting := journal.Transactions[0].Postings[0]
	assert.Greater(t, posting.UnparsedTail.End.Offset, posting.UnparsedTail.Start.Offset,
		"the unconsumed `:= $1000` text must be recorded")
	assert.Equal(t, 2, posting.UnparsedTail.Start.Line)

	clean := journal.Transactions[0].Postings[1]
	assert.Zero(t, clean.UnparsedTail.End.Offset, "a clean posting has no unparsed tail")
}

func TestParser_ErrorCodes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantCode string
	}{
		{
			name:     "invalid calendar date",
			input:    "2024-02-31 x\n    a:aa  $1\n    b:bb\n",
			wantCode: CodeInvalidDate,
		},
		{
			name:     "unexpected content",
			input:    "2024-01-01 x\n    a:aa  $1\n    b:bb\n\n% not a comment\n",
			wantCode: CodeParseUnexpected,
		},
		{
			name:     "expected account name",
			input:    "2024-01-01 x\n    12345\n",
			wantCode: CodeExpectedAccount,
		},
		{
			name:     "expected commodity",
			input:    "P 2024-01-15\n",
			wantCode: CodeExpectedCommodity,
		},
		{
			name:     "unknown end directive",
			input:    "end nosuchthing\n",
			wantCode: CodeUnknownDirective,
		},
		{
			name:     "include without a path",
			input:    "include\n",
			wantCode: CodeMalformedInclude,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, errs := Parse(tt.input)
			require.NotEmpty(t, errs)
			assert.Equal(t, tt.wantCode, errs[0].Code)
		})
	}
}

func TestParser_HeaderFailureEmitsSingleDiagnostic(t *testing.T) {
	// A header the parser cannot read takes its posting block with it: the user
	// gets one error pointing at the header instead of one per posting line.
	input := "2024-02-31 broken\n    expenses:food  $50\n    assets:cash\n\n2024-03-01 good\n    expenses:food  $1\n    assets:cash\n"

	journal, errs := Parse(input)
	require.Len(t, errs, 1)
	assert.Equal(t, CodeInvalidDate, errs[0].Code)

	// The following transaction still parses: error recovery must stay intact.
	require.Len(t, journal.Transactions, 1)
	assert.Equal(t, "good", journal.Transactions[0].Description)
}
