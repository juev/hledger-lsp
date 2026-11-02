package analyzer

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/parser"
)

// bracketDateCases are journals with a bracketed posting date. invalid marks the
// ones hledger 1.52.4 refuses to load because the bracket holds a date that does
// not exist; the rest it loads, the bracket being text or a real date.
var bracketDateCases = []struct {
	name    string
	input   string
	invalid bool
}{
	{name: "month 13", input: "2024-01-01 t\n    a  5 USD  ; [2024-13-05]\n    b\n", invalid: true},
	{name: "day 30 of February", input: "2024-01-01 t\n    a  5 USD  ; [2024-02-30]\n    b\n", invalid: true},
	{name: "secondary only", input: "2024-01-01 t\n    a  5 USD  ; [=2024-13-05]\n    b\n", invalid: true},
	{name: "invalid secondary after a valid primary", input: "2024-01-01 t\n    a  5 USD  ; [2024-01-05=2024-13-05]\n    b\n", invalid: true},
	{name: "invalid primary before a valid secondary", input: "2024-01-01 t\n    a  5 USD  ; [2024-13-05=2024-01-05]\n    b\n", invalid: true},
	{name: "yearless month 13", input: "2024-01-01 t\n    a  5 USD  ; [13-05]\n    b\n", invalid: true},
	{name: "yearless 30 February", input: "2024-01-01 t\n    a  5 USD  ; [02-30]\n    b\n", invalid: true},
	{name: "yearless February 29 in a common year", input: "2023-01-01 t\n    a  5 USD  ; [02-29]\n    b\n", invalid: true},
	{name: "year and month read as month and day", input: "2024-01-01 t\n    a  5 USD  ; [2024-01]\n    b\n", invalid: true},
	{name: "slashes", input: "2024-01-01 t\n    a  5 USD  ; [2024/13/05]\n    b\n", invalid: true},
	{name: "month 0", input: "2024-01-01 t\n    a  5 USD  ; [2024-00-05]\n    b\n", invalid: true},
	{name: "day 0", input: "2024-01-01 t\n    a  5 USD  ; [2024-01-00]\n    b\n", invalid: true},
	{name: "day 32", input: "2024-01-01 t\n    a  5 USD  ; [2024-01-32]\n    b\n", invalid: true},
	{name: "February 29 in a common year", input: "2024-01-01 t\n    a  5 USD  ; [2023-02-29]\n    b\n", invalid: true},
	{name: "yearless slash date out of range", input: "2024-01-01 t\n    a  5 USD  ; [01/32]\n    b\n", invalid: true},
	{name: "day first with the year last", input: "2024-01-01 t\n    a  5 USD  ; [1.1.2024]\n    b\n", invalid: true},
	{name: "double brackets", input: "2024-01-01 t\n    a  5 USD  ; [[2024-13-05]]\n    b\n", invalid: true},
	{name: "after a valid bracket", input: "2024-01-01 t\n    a  5 USD  ; [2024-01-05] [2024-13-05]\n    b\n", invalid: true},
	{name: "inside a note", input: "2024-01-01 t\n    a  5 USD  ; note [2024-13-05] x\n    b\n", invalid: true},
	{name: "inside a tag value", input: "2024-01-01 t\n    a  5 USD  ; foo:[2024-13-05]\n    b\n", invalid: true},
	{name: "on a comment line below the posting", input: "2024-01-01 t\n    a  5 USD\n    ; [2024-13-05]\n    b\n", invalid: true},
	{name: "on an assignment", input: "2024-01-01 t\n    a  = 5 USD  ; [2024-13-05]\n    b\n", invalid: true},
	{name: "Cyrillic and CJK text before it", input: "2024-01-01 касса\n    現金  5 USD  ; заметка 日本 [2024-13-05]\n    капитал\n", invalid: true},

	{name: "three parts with a two-digit year", input: "2024-01-01 t\n    a  5 USD  ; [24-01-05]\n    b\n", invalid: true},
	{name: "three parts with a one-digit year", input: "2024-01-01 t\n    a  5 USD  ; [1-1-1]\n    b\n", invalid: true},
	{name: "mixed separators", input: "2024-01-01 t\n    a  5 USD  ; [2024-01/05]\n    b\n", invalid: true},
	{name: "mixed separators in a yearless date", input: "2024-01-01 t\n    a  5 USD  ; [01-05/06]\n    b\n", invalid: true},
	{name: "valid primary and an empty secondary", input: "2024-01-01 t\n    a  5 USD  ; [2024-01-05=]\n    b\n", invalid: true},
	{name: "valid primary and a secondary of one number", input: "2024-01-01 t\n    a  5 USD  ; [2024-01-05=5]\n    b\n", invalid: true},
	{name: "valid primary and a secondary of separators", input: "2024-01-01 t\n    a  5 USD  ; [2024-01-05=-]\n    b\n", invalid: true},
	{name: "valid primary and a secondary with a short year", input: "2024-01-01 t\n    a  5 USD  ; [2024-01-05=24-01-05]\n    b\n", invalid: true},

	{name: "primary of one number before a date", input: "2024-01-01 t\n    a  5 USD  ; [5=2024-01-05]\n    b\n", invalid: true},

	{name: "primary of one number before a number", input: "2024-01-01 t\n    a  5 USD  ; [5=6]\n    b\n"},
	{name: "primary of one number and text after =", input: "2024-01-01 t\n    a  5 USD  ; [5=x]\n    b\n"},
	{name: "primary of a letter and a digit before a date", input: "2024-01-01 t\n    a  5 USD  ; [5x=2024-01-05]\n    b\n"},
	{name: "primary yearless before a date", input: "2024-01-01 t\n    a  5 USD  ; [5-6=2024-01-05]\n    b\n"},
	{name: "year 0 with an impossible day", input: "2024-01-01 t\n    a  5 USD  ; [0000-12-2023]\n    b\n", invalid: true},
	{name: "year 0 with day 30 of February", input: "2024-01-01 t\n    a  5 USD  ; [0000-02-30]\n    b\n", invalid: true},

	{name: "year of four digits with leading zeros", input: "2024-01-01 t\n    a  5 USD  ; [0001-01-05]\n    b\n"},
	{name: "empty primary and a secondary of one number", input: "2024-01-01 t\n    a  5 USD  ; [=5]\n    b\n"},
	{name: "primary of one number and an empty secondary", input: "2024-01-01 t\n    a  5 USD  ; [5=]\n    b\n"},
	{name: "valid primary and a secondary of one number and a letter", input: "2024-01-01 t\n    a  5 USD  ; [2024-01-05=5x]\n    b\n"},
	{name: "valid primary and a secondary yearless", input: "2024-01-01 t\n    a  5 USD  ; [2024-01-05=05-06]\n    b\n"},
	{name: "text", input: "2024-01-01 t\n    a  5 USD  ; [garbage]\n    b\n"},
	{name: "a day only", input: "2024-01-01 t\n    a  5 USD  ; [5]\n    b\n"},
	{name: "yearless date", input: "2024-01-01 t\n    a  5 USD  ; [1/5]\n    b\n"},
	{name: "yearless February 29 in a leap year", input: "2024-01-01 t\n    a  5 USD  ; [02-29]\n    b\n"},
	{name: "date without leading zeros", input: "2024-01-01 t\n    a  5 USD  ; [2024-1-5]\n    b\n"},
	{name: "dots", input: "2024-01-01 t\n    a  5 USD  ; [2024.01.05]\n    b\n"},
	{name: "year 0", input: "2024-01-01 t\n    a  5 USD  ; [0000-01-05]\n    b\n"},
	{name: "year 0 February 29", input: "2024-01-01 t\n    a  5 USD  ; [0000-02-29]\n    b\n"},
	{name: "five-digit year", input: "2024-01-01 t\n    a  5 USD  ; [12345-01-05]\n    b\n"},
	{name: "empty halves", input: "2024-01-01 t\n    a  5 USD  ; [=]\n    b\n"},
	{name: "secondary only", input: "2024-01-01 t\n    a  5 USD  ; [=1/5]\n    b\n"},
	{name: "digits without separators", input: "2024-01-01 t\n    a  5 USD  ; [20240105]\n    b\n"},
	{name: "a lone dash", input: "2024-01-01 t\n    a  5 USD  ; [-]\n    b\n"},
	{name: "date followed by a letter", input: "2024-01-01 t\n    a  5 USD  ; [2024-13-05x]\n    b\n"},
	{name: "invalid date followed by a space", input: "2024-01-01 t\n    a  5 USD  ; [2024-13-05 ]\n    b\n"},
	{name: "invalid date after a space", input: "2024-01-01 t\n    a  5 USD  ; [ 2024-13-05]\n    b\n"},
	{name: "invalid date followed by a comma", input: "2024-01-01 t\n    a  5 USD  ; [2024-13-05, foo]\n    b\n"},
	{name: "valid primary and a secondary that is text", input: "2024-01-01 t\n    a  5 USD  ; [2024-01-05=garbage]\n    b\n"},
	{name: "invalid primary and a secondary that is text", input: "2024-01-01 t\n    a  5 USD  ; [2024-13-05=garbage]\n    b\n"},
	{name: "invalid primary and a secondary with a letter", input: "2024-01-01 t\n    a  5 USD  ; [2024-13-05=2024-01-05x]\n    b\n"},
	{name: "invalid secondary followed by a letter", input: "2024-01-01 t\n    a  5 USD  ; [2024-01-05=2024-13-05x]\n    b\n"},
	{name: "invalid secondary followed by a space", input: "2024-01-01 t\n    a  5 USD  ; [2024-01-05=2024-13-05 ]\n    b\n"},
	{name: "in the transaction comment", input: "2024-01-01 t ; [2024-13-05]\n    a  5 USD\n    b\n"},
	{name: "on a comment line before the postings", input: "2024-01-01 t\n    ; [2024-13-05]\n    a  5 USD\n    b\n"},
	{name: "in a comment of its own", input: "2024-01-01 t\n    a  5 USD\n    b\n\n; [2024-13-05]\n"},

	// A bracket of date characters with a digit and a separator commits hledger
	// to [date], [date=date] or [=date]; anything left over is refused.
	{name: "date followed by a part after another separator", input: "2024-01-01 t\n    a  5 USD  ; [2024-01-05/06]\n    b\n", invalid: true},
	{name: "date followed by a fourth part", input: "2024-01-01 t\n    a  5 USD  ; [2024-01-05-06]\n    b\n", invalid: true},
	{name: "three dates", input: "2024-01-01 t\n    a  5 USD  ; [2024-01-05=2024-01-06=2024-01-07]\n    b\n", invalid: true},
	{name: "secondary with mixed separators", input: "2024-01-01 t\n    a  5 USD  ; [=2024-01-05/06]\n    b\n", invalid: true},
	{name: "date followed by a dash", input: "2024-01-01 t\n    a  5 USD  ; [2024-01-05-]\n    b\n", invalid: true},
	{name: "date followed by a dot", input: "2024-01-01 t\n    a  5 USD  ; [2024-01-05.]\n    b\n", invalid: true},
	{name: "yearless date followed by a dash", input: "2024-01-01 t\n    a  5 USD  ; [01-05-]\n    b\n", invalid: true},
	{name: "date after a dash", input: "2024-01-01 t\n    a  5 USD  ; [-2024-01-05]\n    b\n", invalid: true},
	{name: "empty month", input: "2024-01-01 t\n    a  5 USD  ; [2024--05]\n    b\n", invalid: true},
	{name: "two equals signs between dates", input: "2024-01-01 t\n    a  5 USD  ; [2024-01-05==2024-01-06]\n    b\n", invalid: true},
	{name: "two equals signs before a date", input: "2024-01-01 t\n    a  5 USD  ; [==2024-01-06]\n    b\n", invalid: true},
	{name: "secondary followed by an equals sign", input: "2024-01-01 t\n    a  5 USD  ; [=2024-01-05=]\n    b\n", invalid: true},
	{name: "number followed by a dash", input: "2024-01-01 t\n    a  5 USD  ; [5-]\n    b\n", invalid: true},
	{name: "number after a dash", input: "2024-01-01 t\n    a  5 USD  ; [-5]\n    b\n", invalid: true},
	{name: "number followed by a dot", input: "2024-01-01 t\n    a  5 USD  ; [5.]\n    b\n", invalid: true},
	{name: "dashed number before a number", input: "2024-01-01 t\n    a  5 USD  ; [5-=6]\n    b\n", invalid: true},
	{name: "number and a dashed number after =", input: "2024-01-01 t\n    a  5 USD  ; [5=-]\n    b\n", invalid: true},
	{name: "dash before a number after =", input: "2024-01-01 t\n    a  5 USD  ; [-=5]\n    b\n", invalid: true},
	{name: "secondary followed by a dash", input: "2024-01-01 t\n    a  5 USD  ; [=5-]\n    b\n", invalid: true},
	{name: "four parts", input: "2024-01-01 t\n    a  5 USD  ; [1-1-1-1]\n    b\n", invalid: true},
	{name: "year of four digits and a month", input: "2024-01-01 t\n    a  5 USD  ; [0002-05]\n    b\n", invalid: true},
	{name: "no digit after =", input: "2024-01-01 t\n    a  5 USD  ; [=-]\n    b\n"},
	{name: "yearless secondary only", input: "2024-01-01 t\n    a  5 USD  ; [=5-6]\n    b\n"},
	{name: "dotted yearless date", input: "2024-01-01 t\n    a  5 USD  ; [1.5]\n    b\n"},
	{name: "month with leading zeros", input: "2024-01-01 t\n    a  5 USD  ; [2024-001-05]\n    b\n"},

	// A yearless secondary takes the year of the primary.
	{name: "yearless secondary in the common year of the primary", input: "2024-01-01 t\n    a  5 USD  ; [2023-01-05=02-29]\n    b\n", invalid: true},
	{name: "yearless secondary in the leap year of the primary", input: "2023-01-01 t\n    a  5 USD  ; [2024-01-05=02-29]\n    b\n"},
	{name: "yearless secondary in year 0 of the primary", input: "2023-01-01 t\n    a  5 USD  ; [0000-01-05=02-29]\n    b\n"},
	{name: "yearless secondary below the posting in the year of the primary", input: "2024-01-01 касса\n    現金  5 USD\n    ; заметка [2023-01-05=02-29]\n    капитал\n", invalid: true},

	// The year is read whole, not cut to a machine integer.
	{name: "February 29 in a century year that is not leap", input: "2024-01-01 t\n    a  5 USD  ; [99999999999999999700-02-29]\n    b\n", invalid: true},
	{name: "February 29 in a leap century year", input: "2024-01-01 t\n    a  5 USD  ; [99999999999999999600-02-29]\n    b\n"},

	// An indented # or * line is a posting of its own to hledger, not a comment.
	{name: "on a # line below the posting", input: "2024-01-01 t\n    a  5 USD\n    # paid [2024-13-05]\n    b  -5 USD\n"},
	{name: "on a * line below the posting", input: "2024-01-01 t\n    a  5 USD\n    * paid [2024-13-05]\n    b  -5 USD\n"},
}

func TestAnalyzer_BracketedPostingDate(t *testing.T) {
	for _, tc := range bracketDateCases {
		t.Run(tc.name, func(t *testing.T) {
			journal, errs := parser.Parse(tc.input)
			require.Empty(t, errs)

			var reported []Diagnostic
			for _, d := range New().Analyze(journal).Diagnostics {
				if d.Code == "INVALID_POSTING_DATE" {
					reported = append(reported, d)
				}
			}

			if !tc.invalid {
				assert.Empty(t, reported)
				return
			}
			require.NotEmpty(t, reported)
			assert.Equal(t, SeverityError, reported[0].Severity)
			assert.NotEmpty(t, reported[0].Message)
		})
	}
}

func TestAnalyzer_BracketedPostingDateRange(t *testing.T) {
	// "    現金  5 USD  ; заметка [2024-13-05]": the date starts at the rune column
	// after 4 spaces, 2 CJK letters, 2 spaces, "5 USD", 2 spaces, ";", a space,
	// "заметка", a space and "[", which is 27 (columns count from 1).
	journal, errs := parser.Parse("2024-01-01 касса\n    現金  5 USD  ; заметка [2024-13-05]\n    капитал\n")
	require.Empty(t, errs)

	var found *Diagnostic
	for _, d := range New().Analyze(journal).Diagnostics {
		if d.Code == "INVALID_POSTING_DATE" {
			found = &d
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, 2, found.Range.Start.Line)
	assert.Equal(t, 27, found.Range.Start.Column)
	assert.Equal(t, 37, found.Range.End.Column)
}

func TestAnalyzer_BracketedSecondaryDateRange(t *testing.T) {
	journal, errs := parser.Parse("2024-01-01 t\n    a  5 USD\n    ; [2024-01-05=2024-13-05]\n    b\n")
	require.Empty(t, errs)

	var found *Diagnostic
	for _, d := range New().Analyze(journal).Diagnostics {
		if d.Code == "INVALID_POSTING_DATE" {
			found = &d
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, 3, found.Range.Start.Line)
	assert.Equal(t, "2024-13-05", found.Data["value"])
	assert.Equal(t, found.Range.Start.Column+10, found.Range.End.Column)
}

func TestAnalyzer_BracketedDateRangeOnACommentLineBelow(t *testing.T) {
	// "    ; заметка [2023-01-05=02-29]": ";" is at column 5, so "[" is at 15, the
	// primary starts at 16 and the secondary, read in 2023, at 27.
	journal, errs := parser.Parse("2024-01-01 касса\n    現金  5 USD\n    ; заметка [2023-01-05=02-29]\n    капитал\n")
	require.Empty(t, errs)

	var reported []Diagnostic
	for _, d := range New().Analyze(journal).Diagnostics {
		if d.Code == "INVALID_POSTING_DATE" {
			reported = append(reported, d)
		}
	}
	require.Len(t, reported, 1)
	assert.Equal(t, 3, reported[0].Range.Start.Line)
	assert.Equal(t, 27, reported[0].Range.Start.Column)
	assert.Equal(t, 32, reported[0].Range.End.Column)
	assert.Equal(t, "02-29", reported[0].Data["value"])
}

func TestAnalyzer_BracketedDateWithALeftoverIsReportedOnItsHalf(t *testing.T) {
	journal, errs := parser.Parse("2024-01-01 t\n    a  5 USD  ; [2024-01-05=2024-01-06/07]\n    b\n")
	require.Empty(t, errs)

	var reported []Diagnostic
	for _, d := range New().Analyze(journal).Diagnostics {
		if d.Code == "INVALID_POSTING_DATE" {
			reported = append(reported, d)
		}
	}
	require.Len(t, reported, 1)
	assert.Equal(t, "2024-01-06/07", reported[0].Data["value"])
	assert.Equal(t, 29, reported[0].Range.Start.Column)
	assert.Equal(t, 42, reported[0].Range.End.Column)
}

func TestAnalyzer_YearlessDateTagUsesTheTransactionYear(t *testing.T) {
	for name, tc := range map[string]struct {
		input   string
		invalid bool
	}{
		"February 29 in a common year":                    {"2023-01-01 t\n    a  5 USD  ; date:02-29\n    b\n", true},
		"February 29 in a leap year":                      {"2024-01-01 t\n    a  5 USD  ; date:02-29\n    b\n", false},
		"February 29 on a comment line below the posting": {"2023-01-01 t\n    a  5 USD\n    ; date2:02-29\n    b\n", true},
		// A comment of the transaction holds no posting date, so its yearless
		// value is not checked against the transaction year.
		"February 29 in the transaction comment":            {"2023-01-01 t  ; date:02-29\n    a  5 USD\n    b\n", false},
		"February 30 in the transaction comment":            {"2024-01-01 t  ; date:02-30\n    a  5 USD\n    b\n", false},
		"February 29 on a comment line before the postings": {"2023-01-01 касса\n    ; заметка date:02-29\n    現金  5 USD\n    капитал\n", false},
	} {
		t.Run(name, func(t *testing.T) {
			journal, errs := parser.Parse(tc.input)
			require.Empty(t, errs)

			reported := false
			for _, d := range New().Analyze(journal).Diagnostics {
				reported = reported || d.Code == "INVALID_DATE_TAG"
			}
			assert.Equal(t, tc.invalid, reported)
		})
	}
}

// TestOracle_BracketedPostingDateVerdictsMatchHledger compares whether the
// analyzer reports an error with whether hledger loads the journal.
func TestOracle_BracketedPostingDateVerdictsMatchHledger(t *testing.T) {
	if os.Getenv("HLEDGER_ORACLE") == "" {
		t.Skip("set HLEDGER_ORACLE=1 to compare against the installed hledger binary")
	}
	hledgerPath, err := exec.LookPath("hledger")
	if err != nil {
		t.Skip("hledger is not installed")
	}

	for _, tc := range bracketDateCases {
		t.Run(tc.name, func(t *testing.T) {
			journal, errs := parser.Parse(tc.input)
			require.Empty(t, errs)

			failed := false
			for _, d := range New().Analyze(journal).Diagnostics {
				failed = failed || d.Severity == SeverityError
			}
			assert.Equal(t, !hledgerAccepts(t, hledgerPath, tc.input), failed, "our verdict must match hledger for:\n%s", tc.input)
			assert.Equal(t, tc.invalid, failed)
		})
	}
}

func TestOracle_YearlessDateTagVerdictsMatchHledger(t *testing.T) {
	if os.Getenv("HLEDGER_ORACLE") == "" {
		t.Skip("set HLEDGER_ORACLE=1 to compare against the installed hledger binary")
	}
	hledgerPath, err := exec.LookPath("hledger")
	if err != nil {
		t.Skip("hledger is not installed")
	}

	assert.False(t, hledgerAccepts(t, hledgerPath, "2023-01-01 t\n    a  5 USD  ; date:02-29\n    b\n"))
	assert.True(t, hledgerAccepts(t, hledgerPath, "2024-01-01 t\n    a  5 USD  ; date:02-29\n    b\n"))
	assert.False(t, hledgerAccepts(t, hledgerPath, "2023-01-01 t\n    a  5 USD\n    ; date2:02-29\n    b\n"))
	assert.True(t, hledgerAccepts(t, hledgerPath, "2023-01-01 t  ; date:02-29\n    a  5 USD\n    b\n"))
	assert.True(t, hledgerAccepts(t, hledgerPath, "2024-01-01 t  ; date:02-30\n    a  5 USD\n    b\n"))
	assert.True(t, hledgerAccepts(t, hledgerPath, "2023-01-01 касса\n    ; заметка date:02-29\n    現金  5 USD\n    капитал\n"))
}

// ruleBracketDateCases are periodic transactions and auto posting rules with a
// bracketed date on a posting. hledger 1.52.4 checks those brackets as it does in
// a transaction. A periodic transaction reads a date without a year in the default
// year (Y, else the current one); an auto posting rule has no year at all, so a
// date without one is refused there unless it follows a full primary date.
var ruleBracketDateCases = []struct {
	name    string
	input   string
	invalid bool
}{
	{name: "periodic, month 13", input: "~ monthly\n    a  5 USD  ; [2024-13-05]\n    b\n", invalid: true},
	{name: "periodic, on a comment line below", input: "~ monthly\n    расход  5 USD\n    ; заметка [2024-13-05]\n    капитал\n", invalid: true},
	{name: "periodic, secondary only", input: "~ monthly\n    a  5 USD  ; [=2024-13-05]\n    b\n", invalid: true},
	{name: "periodic, date followed by a dash", input: "~ monthly\n    a  5 USD  ; [2024-01-05-]\n    b\n", invalid: true},
	{name: "periodic, yearless 30 February", input: "~ monthly\n    a  5 USD  ; [02-30]\n    b\n", invalid: true},
	{name: "periodic, yearless secondary in the common year of the primary", input: "~ monthly\n    a  5 USD  ; [2023-01-05=02-29]\n    b\n", invalid: true},
	{name: "periodic, valid date", input: "~ monthly\n    a  5 USD  ; [2024-01-05]\n    b\n"},
	{name: "periodic, text after =", input: "~ monthly\n    a  5 USD  ; [2024-01-05=garbage]\n    b\n"},
	{name: "periodic, yearless date", input: "~ monthly\n    a  5 USD  ; [01-05]\n    b\n"},
	{name: "periodic, February 29 in a leap default year", input: "Y 2024\n~ monthly\n    a  5 USD  ; [02-29]\n    b\n"},
	{name: "periodic, February 29 in a common default year", input: "Y 2023\n~ monthly\n    a  5 USD  ; [02-29]\n    b\n", invalid: true},
	{name: "periodic, February 29 below the posting in a common default year", input: "Y 2023\n~ monthly\n    расход  5 USD\n    ; заметка [02-29]\n    капитал\n", invalid: true},
	{name: "periodic, February 29 below the posting in a leap default year", input: "Y 2024\n~ monthly\n    расход  5 USD\n    ; заметка [02-29]\n    капитал\n"},
	{name: "periodic, secondary February 29 in a common default year", input: "Y 2023\n~ monthly\n    a  5 USD  ; [=02-29]\n    b\n", invalid: true},
	{name: "periodic, secondary February 29 in a leap default year", input: "Y 2024\n~ monthly\n    a  5 USD  ; [=02-29]\n    b\n"},
	{name: "periodic, secondary in the leap year of the primary in a common default year", input: "Y 2023\n~ monthly\n    a  5 USD  ; [2024-01-05=02-29]\n    b\n"},
	{name: "periodic, the Y in force where it is written", input: "Y 2024\n~ monthly\n    a  5 USD  ; [02-29]\n    b\n\nY 2023\n"},
	{name: "periodic, yearless secondary in the leap year of the primary", input: "~ monthly\n    a  5 USD  ; [2024-01-05=02-29]\n    b\n"},
	{name: "periodic, in the header comment", input: "~ monthly  ; [2024-13-05]\n    a  5 USD\n    b\n"},
	{name: "periodic, on a comment line before the postings", input: "~ monthly\n    ; [2024-13-05]\n    a  5 USD\n    b\n"},

	{name: "auto, month 13", input: "= a\n    b  *2  ; [2024-13-05]\n", invalid: true},
	{name: "auto, on a comment line below", input: "= расход\n    налог  *2\n    ; 日本 [2024-13-05]\n", invalid: true},
	{name: "auto, secondary only", input: "= a\n    b  *2  ; [=2024-13-05]\n", invalid: true},
	{name: "auto, date followed by a dash", input: "= a\n    b  *2  ; [2024-01-05-]\n", invalid: true},
	{name: "auto, yearless date", input: "= a\n    b  *2  ; [01-05]\n", invalid: true},
	{name: "auto, yearless date with a default year", input: "Y 2024\n= a\n    b  *2  ; [02-29]\n", invalid: true},
	{name: "auto, yearless secondary only", input: "= a\n    b  *2  ; [=01-05]\n", invalid: true},
	{name: "auto, valid date", input: "= a\n    b  *2  ; [2024-01-05]\n"},
	{name: "auto, text after =", input: "= a\n    b  *2  ; [2024-01-05=garbage]\n"},
	{name: "auto, yearless secondary after a full primary", input: "= a\n    b  *2  ; [2024-01-05=02-29]\n"},
	{name: "auto, a day only", input: "= a\n    b  *2  ; [5]\n"},
	{name: "auto, in the rule comment", input: "= a  ; [2024-13-05]\n    b  *2\n"},
	{name: "auto, on a comment line before the postings", input: "= a\n    ; [2024-13-05]\n    b  *2\n"},
}

func TestAnalyzer_BracketedDateInRules(t *testing.T) {
	for _, tc := range ruleBracketDateCases {
		t.Run(tc.name, func(t *testing.T) {
			journal, errs := parser.Parse(tc.input)
			require.Empty(t, errs)

			var reported []Diagnostic
			for _, d := range New().Analyze(journal).Diagnostics {
				if d.Code == "INVALID_POSTING_DATE" {
					reported = append(reported, d)
				}
			}
			if !tc.invalid {
				assert.Empty(t, reported)
				return
			}
			require.NotEmpty(t, reported)
			assert.Equal(t, SeverityError, reported[0].Severity)
		})
	}
}

func TestAnalyzer_BracketedDateRangeInARuleCommentLine(t *testing.T) {
	// "    ; 日本 [2024-13-05]": ";" is at column 5, "日本" at 7 and 8, "[" at 10,
	// so the date runs from 11 to 21.
	journal, errs := parser.Parse("= расход\n    налог  *2\n    ; 日本 [2024-13-05]\n")
	require.Empty(t, errs)

	var reported []Diagnostic
	for _, d := range New().Analyze(journal).Diagnostics {
		if d.Code == "INVALID_POSTING_DATE" {
			reported = append(reported, d)
		}
	}
	require.Len(t, reported, 1)
	assert.Equal(t, 3, reported[0].Range.Start.Line)
	assert.Equal(t, 11, reported[0].Range.Start.Column)
	assert.Equal(t, 21, reported[0].Range.End.Column)
}

func TestAnalyzer_PeriodicBracketedDateUsesTheYearOfTheTransaction(t *testing.T) {
	// The year a periodic transaction reads a date without a year in comes from the
	// parser (Y, else the clock); set it here so the test does not depend on today.
	for _, tc := range []struct {
		year    int
		invalid bool
	}{
		{year: 2023, invalid: true},
		{year: 2024},
	} {
		journal, errs := parser.Parse("~ monthly\n    a  5 USD  ; [02-29]\n    b\n")
		require.Empty(t, errs)
		require.Len(t, journal.PeriodicTransactions, 1)
		journal.PeriodicTransactions[0].Year = tc.year

		reported := false
		for _, d := range New().Analyze(journal).Diagnostics {
			reported = reported || d.Code == "INVALID_POSTING_DATE"
		}
		assert.Equal(t, tc.invalid, reported, "year %d", tc.year)
	}
}

func TestAnalyzer_PeriodicBracketedDateInAnIncludedFileUsesTheIncludingYear(t *testing.T) {
	for _, tc := range []struct {
		year    string
		invalid bool
	}{
		{year: "2023", invalid: true},
		{year: "2024"},
	} {
		var site parser.Context
		_, errs := parser.ParseWithContext("Y "+tc.year+"\ninclude child.journal\n", parser.Context{}, func(s parser.IncludeSite) parser.ContextExports {
			site = s.Context
			return parser.ContextExports{}
		})
		require.Empty(t, errs)

		child, childErrs := parser.ParseWithContext("~ monthly\n    a  5 USD\n    ; [02-29]\n    b\n", site, nil)
		require.Empty(t, childErrs)

		reported := false
		for _, d := range New().Analyze(child.Journal).Diagnostics {
			reported = reported || d.Code == "INVALID_POSTING_DATE"
		}
		assert.Equal(t, tc.invalid, reported, "Y %s", tc.year)
	}
}

func TestOracle_PeriodicBracketedDateYearMatchesHledger(t *testing.T) {
	if os.Getenv("HLEDGER_ORACLE") == "" {
		t.Skip("set HLEDGER_ORACLE=1 to compare against the installed hledger binary")
	}
	hledgerPath, err := exec.LookPath("hledger")
	if err != nil {
		t.Skip("hledger is not installed")
	}

	// Without Y both hledger and the parser read the clock, so the verdicts agree
	// whatever year it is.
	for _, input := range []string{
		"~ monthly\n    a  5 USD  ; [02-29]\n    b\n",
		"~ monthly\n    a  5 USD  ; [02-29]\n    b\n\nY 2024\n",
	} {
		journal, errs := parser.Parse(input)
		require.Empty(t, errs)
		failed := false
		for _, d := range New().Analyze(journal).Diagnostics {
			failed = failed || d.Severity == SeverityError
		}
		assert.Equal(t, !hledgerAccepts(t, hledgerPath, input), failed, "our verdict must match hledger for:\n%s", input)
	}

	// A Y in the including file applies to the included one.
	dir := t.TempDir()
	child := "~ monthly\n    a  5 USD\n    ; [02-29]\n    b\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "child.journal"), []byte(child), 0o600))
	for _, tc := range []struct {
		year    string
		invalid bool
	}{
		{year: "2023", invalid: true},
		{year: "2024"},
	} {
		parent := filepath.Join(dir, "parent-"+tc.year+".journal")
		require.NoError(t, os.WriteFile(parent, []byte("Y "+tc.year+"\ninclude child.journal\n"), 0o600))
		cmd := exec.Command(hledgerPath, "-f", parent, "print")
		assert.Equal(t, tc.invalid, cmd.Run() != nil, "hledger with Y %s", tc.year)
	}
}

func TestOracle_BracketedDateInRulesMatchesHledger(t *testing.T) {
	if os.Getenv("HLEDGER_ORACLE") == "" {
		t.Skip("set HLEDGER_ORACLE=1 to compare against the installed hledger binary")
	}
	hledgerPath, err := exec.LookPath("hledger")
	if err != nil {
		t.Skip("hledger is not installed")
	}

	for _, tc := range ruleBracketDateCases {
		t.Run(tc.name, func(t *testing.T) {
			journal, errs := parser.Parse(tc.input)
			require.Empty(t, errs)

			failed := false
			for _, d := range New().Analyze(journal).Diagnostics {
				failed = failed || d.Severity == SeverityError
			}
			assert.Equal(t, !hledgerAccepts(t, hledgerPath, tc.input), failed, "our verdict must match hledger for:\n%s", tc.input)
			assert.Equal(t, tc.invalid, failed)
		})
	}
}
