package analyzer

import (
	"os"
	"os/exec"
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

func TestAnalyzer_YearlessDateTagUsesTheTransactionYear(t *testing.T) {
	for name, tc := range map[string]struct {
		input   string
		invalid bool
	}{
		"February 29 in a common year": {"2023-01-01 t\n    a  5 USD  ; date:02-29\n    b\n", true},
		"February 29 in a leap year":   {"2024-01-01 t\n    a  5 USD  ; date:02-29\n    b\n", false},
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
}
