package analyzer

import (
	"os"
	"os/exec"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/parser"
)

// assignmentDateCases are balance assignments with and without a posting date.
// refused marks the ones hledger 1.52.4 rejects with "Balance assignments and
// custom posting dates may not be combined."; the rest it loads, or rejects for
// another reason that has its own diagnostic (invalid dates).
var assignmentDateCases = []struct {
	name    string
	input   string
	refused bool
	// invalid marks cases hledger rejects as a malformed date, not as a refused
	// assignment.
	invalid bool
}{
	{name: "date tag", input: "2024-01-01 t\n    c  = -4 GBP  ; date:2024-01-19\n    b\n", refused: true},
	{name: "date tag equal to the transaction date", input: "2024-01-01 t\n    c  = -4 GBP  ; date:2024-01-01\n    b\n", refused: true},
	{name: "date tag without a year", input: "2024-01-01 t\n    c  = -4 GBP  ; date:01-19\n    b\n", refused: true},
	{name: "date tag with other tags", input: "2024-01-01 t\n    c  = -4 GBP  ; foo:1, date:2024-01-19\n    b\n", refused: true},
	{name: "date tag followed by text", input: "2024-01-01 t\n    c  = -4 GBP  ; date:2024-01-19 text\n    b\n", refused: true},
	{name: "date tag glued to text", input: "2024-01-01 t\n    c  = -4 GBP  ; date:2024-01-19x\n    b\n", refused: true},
	{name: "date tag on a comment line of the next posting", input: "2024-01-01 t\n    c  = -4 GBP\n    b\n    ; date:2024-01-19\n"},
	{name: "date tag on a comment line below", input: "2024-01-01 t\n    c  = -4 GBP\n    ; date:2024-01-19\n    b\n", refused: true},
	{name: "date tag with a secondary date", input: "2024-01-01 t\n    c  = -4 GBP  ; date2:2024-01-20, date:2024-01-19\n    b\n", refused: true},
	{name: "bracketed date on a comment line below", input: "2024-01-01 t\n    c  = -4 GBP\n    ; [2024-01-19]\n    b\n", refused: true},
	{name: "bracketed date", input: "2024-01-01 t\n    c  = -4 GBP  ; [2024-01-05]\n    b\n", refused: true},
	{name: "bracketed date without a year", input: "2024-01-01 t\n    c  = -4 GBP  ; [01-05]\n    b\n", refused: true},
	{name: "bracketed primary and secondary dates", input: "2024-01-01 t\n    c  = -4 GBP  ; [2024-01-05=2024-01-19]\n    b\n", refused: true},
	{name: "bracketed date inside a note", input: "2024-01-01 t\n    c  = -4 GBP  ; note [2024-01-19] x\n    b\n", refused: true},
	{name: "strict assignment", input: "2024-01-01 t\n    c  == -4 GBP  ; date:2024-01-19\n    b\n", refused: true},
	{name: "inclusive assignment", input: "2024-01-01 t\n    c  =* -4 GBP  ; date:2024-01-19\n    b\n", refused: true},
	{name: "assignment with a cost", input: "2024-01-01 t\n    c  = -4 GBP @ 2 EUR  ; date:2024-01-19\n    b\n", refused: true},
	{name: "unbalanced virtual assignment", input: "2024-01-01 t\n    (c)  = -4 GBP  ; date:2024-01-19\n", refused: true},
	{name: "balanced virtual assignment", input: "2024-01-01 t\n    [c]  = -4 GBP  ; date:2024-01-19\n    [b]\n", refused: true},
	{name: "dated assignment among postings", input: "2024-01-01 t\n    c  = -4 GBP  ; date:2024-01-19\n    c  = -4 GBP\n    b\n", refused: true},
	{name: "Cyrillic account", input: "2024-01-01 касса\n    касса:наличные  = -4 GBP  ; date:2024-01-19\n    капитал\n", refused: true},
	{name: "CJK account", input: "2024-01-01 現金\n    現金:口座  = -4 GBP  ; [2024-01-19]\n    資本\n", refused: true},

	{name: "secondary date tag", input: "2024-01-01 t\n    c  = -4 GBP  ; date2:2024-01-19\n    b\n"},
	{name: "bracketed secondary date only", input: "2024-01-01 t\n    c  = -4 GBP  ; [=2024-01-19]\n    b\n"},
	{name: "capitalised tag name", input: "2024-01-01 t\n    c  = -4 GBP  ; Date:2024-01-19\n    b\n"},
	{name: "tag name with a prefix", input: "2024-01-01 t\n    c  = -4 GBP  ; xdate:2024-01-19\n    b\n"},
	{name: "tag name with a space before the colon", input: "2024-01-01 t\n    c  = -4 GBP  ; date :2024-01-19\n    b\n"},
	{name: "bracket that is not a date", input: "2024-01-01 t\n    c  = -4 GBP  ; [garbage]\n    b\n"},
	{name: "bracket with a day only", input: "2024-01-01 t\n    c  = -4 GBP  ; [5]\n    b\n"},
	{name: "bracketed date followed by text", input: "2024-01-01 t\n    c  = -4 GBP  ; [2024-01-19 text]\n    b\n"},
	{name: "date on the other posting", input: "2024-01-01 t\n    c  = -4 GBP\n    b  ; date:2024-01-19\n"},
	{name: "date on the transaction", input: "2024-01-01 t ; date:2024-01-19\n    c  = -4 GBP\n    b\n"},
	{name: "date on a posting with an amount and an assertion", input: "2024-01-01 t\n    c  -4 GBP = -4 GBP  ; date:2024-01-19\n    b\n"},
	{name: "date beside an assignment that has none", input: "2024-01-01 t\n    c  = -4 GBP\n    b  4 GBP  ; date:2024-01-19\n"},
	{name: "invalid date tag", input: "2024-01-01 t\n    c  = -4 GBP  ; date:garbage\n    b\n", invalid: true},
	{name: "impossible date tag", input: "2024-01-01 t\n    c  = -4 GBP  ; date:2024-02-30\n    b\n", invalid: true},
	{name: "empty date tag", input: "2024-01-01 t\n    c  = -4 GBP  ; date:\n    b\n", invalid: true},
}

func TestAnalyzer_AssignmentWithPostingDate(t *testing.T) {
	for _, tc := range assignmentDateCases {
		t.Run(tc.name, func(t *testing.T) {
			journal, errs := parser.Parse(tc.input)
			require.Empty(t, errs)

			var refused []Diagnostic
			for _, d := range New().Analyze(journal).Diagnostics {
				if d.Code == "ASSIGNMENT_POSTING_DATE" {
					refused = append(refused, d)
				}
			}

			if !tc.refused {
				assert.Empty(t, refused)
				return
			}
			require.Len(t, refused, 1)
			assert.Equal(t, SeverityError, refused[0].Severity)
			assert.Equal(t, "balance assignments and custom posting dates may not be combined: write the amount explicitly or remove the posting date", refused[0].Message)
			assert.Equal(t, 2, refused[0].Range.Start.Line, "the error belongs to the assignment posting")
		})
	}
}

func TestAnalyzer_AssignmentWithPostingDateIsReportedOncePerPosting(t *testing.T) {
	journal, errs := parser.Parse("2024-01-01 t\n    c  = -4 GBP  ; date:2024-01-19\n    d  = 1 GBP  ; [2024-01-05]\n    b\n")
	require.Empty(t, errs)

	var lines []int
	for _, d := range New().Analyze(journal).Diagnostics {
		if d.Code == "ASSIGNMENT_POSTING_DATE" {
			lines = append(lines, d.Range.Start.Line)
		}
	}
	assert.Equal(t, []int{2, 3}, lines)
}

// TestOracle_AssignmentPostingDateVerdictsMatchHledger compares whether the
// analyzer reports an error for the journal with whether hledger loads it.
func TestOracle_AssignmentPostingDateVerdictsMatchHledger(t *testing.T) {
	if os.Getenv("HLEDGER_ORACLE") == "" {
		t.Skip("set HLEDGER_ORACLE=1 to compare against the installed hledger binary")
	}
	hledgerPath, err := exec.LookPath("hledger")
	if err != nil {
		t.Skip("hledger is not installed")
	}

	for _, tc := range assignmentDateCases {
		t.Run(tc.name, func(t *testing.T) {
			journal, errs := parser.Parse(tc.input)
			require.Empty(t, errs)

			failed := false
			for _, d := range New().Analyze(journal).Diagnostics {
				failed = failed || d.Severity == SeverityError
			}
			resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: tc.input})
			for _, d := range CheckJournalBalance(resolved, decimal.Zero) {
				failed = failed || d.Severity == SeverityError
			}

			assert.Equal(t, !hledgerAccepts(t, hledgerPath, tc.input), failed,
				"our verdict must match hledger for:\n%s", tc.input)
			assert.Equal(t, tc.refused || tc.invalid, failed)
		})
	}
}

func TestPostingDateOverride_ReadsADateTagLikeHledger(t *testing.T) {
	journal, errs := parser.Parse("2024-01-01 t\n    c  4 GBP  ; date:2024-01-19x\n    b\n\n2024-01-02 t\n    c  4 GBP  ; date:2024-01-20 text\n    b\n")
	require.Empty(t, errs)

	assert.Equal(t, 19, registerDate(&journal.Transactions[0], &journal.Transactions[0].Postings[0]).Day)
	assert.Equal(t, 20, registerDate(&journal.Transactions[1], &journal.Transactions[1].Postings[0]).Day)
}

func TestPostingDateOverride_ReadsCommentLinesBelowAPosting(t *testing.T) {
	journal, errs := parser.Parse("2024-01-01 t\n    c  4 GBP\n    ; date:2024-01-19\n    b\n    ; [2024-01-20]\n")
	require.Empty(t, errs)

	tx := &journal.Transactions[0]
	assert.Equal(t, 19, registerDate(tx, &tx.Postings[0]).Day)
	assert.Equal(t, 20, registerDate(tx, &tx.Postings[1]).Day)
}
