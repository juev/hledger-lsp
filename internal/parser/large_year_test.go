package parser

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/textutil"
)

// hledger 1.52.4 reads a year of any length in a transaction date and in the Y
// directive; only year 0 is refused by Y.
func TestParse_YearsOfAnyLength(t *testing.T) {
	cases := []struct {
		input  string
		year   int
		digits string
	}{
		{input: "9223372036854775807-01-05 t\n    a  5 USD\n    b\n", year: math.MaxInt},
		{input: "9223372036854775808-01-05 t\n    a  5 USD\n    b\n", year: math.MaxInt, digits: "9223372036854775808"},
		{input: "99999999999999999999-01-05 t\n    a  5 USD\n    b\n", year: math.MaxInt, digits: "99999999999999999999"},
		{input: "0099999999999999999999/01/05 t\n    a  5 USD\n    b\n", year: math.MaxInt, digits: "99999999999999999999"},
		{input: "Y 10000\n01-05 t\n    a  5 USD\n    b\n", year: 10000},
		{input: "Y 99999999999999999999\n01-05 t\n    a  5 USD\n    b\n", year: math.MaxInt, digits: "99999999999999999999"},
	}
	for _, c := range cases {
		for _, newline := range []string{"\n", "\r\n"} {
			input := textutil.NormalizeLineEndings(strings.ReplaceAll(c.input, "\n", newline))
			journal, errs := Parse(input)
			require.Empty(t, errs, "input %q", input)
			require.Len(t, journal.Transactions, 1)
			date := journal.Transactions[0].Date
			assert.Equal(t, c.year, date.Year, "input %q", input)
			assert.Equal(t, c.digits, date.YearDigits, "input %q", input)
			assert.Equal(t, 1, date.Month)
			assert.Equal(t, 5, date.Day)
		}
	}
}

func TestParse_LeapDayOfALargeYear(t *testing.T) {
	_, errs := Parse("99999999999999999996-02-29 t\n    a  5 USD\n    b\n")
	assert.Empty(t, errs)
	_, errs = Parse("99999999999999999999-02-29 t\n    a  5 USD\n    b\n")
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Message, "invalid date")
	_, errs = Parse("Y 99999999999999999999\n02-29 t\n    a  5 USD\n    b\n")
	require.Len(t, errs, 1)
	_, errs = Parse("Y 99999999999999999996\n02-29 t\n    a  5 USD\n    b\n")
	assert.Empty(t, errs)
}

func TestParse_YearZeroInYIsRefused(t *testing.T) {
	_, errs := Parse("Y 0\n01-05 t\n    a  5 USD\n    b\n")
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Message, "invalid year")
}

func TestParse_LargeYCrossesAnInclude(t *testing.T) {
	var site Context
	_, errs := ParseWithContext("Y 99999999999999999996\ninclude child.journal\n", Context{}, func(s IncludeSite) ContextExports {
		site = s.Context
		return ContextExports{}
	})
	require.Empty(t, errs)
	child, childErrs := ParseWithContext("02-29 t\n    расход  5 USD\n    b\n\n~ monthly\n    a  1 USD\n    b\n", site, nil)
	require.Empty(t, childErrs)
	require.Len(t, child.Journal.Transactions, 1)
	assert.Equal(t, "99999999999999999996", child.Journal.Transactions[0].Date.YearDigits)
	// A periodic transaction reads its yearless dates in a year with the same leap
	// years as the large Y.
	require.Len(t, child.Journal.PeriodicTransactions, 1)
	assert.Equal(t, (ast.Date{Year: math.MaxInt, YearDigits: "99999999999999999996"}).CalendarYear(), child.Journal.PeriodicTransactions[0].Year)
}
