package analyzer

import (
	"encoding/csv"
	"fmt"
	"os"
	"os/exec"
	"strings"
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
	{name: "date tag after words", input: "2024-01-01 t\n    a  = 5 USD  ; a date:2024-01-19\n    b\n", refused: true},
	{name: "date tag after words and a comma", input: "2024-01-01 t\n    c  = -4 GBP  ; paid on date:2024-01-19, ref:5\n    b\n", refused: true},
	{name: "date tag after words on a comment line below", input: "2024-01-01 t\n    c  = 4 GBP  ; note text\n    ; paid on date:2024-01-19\n    b\n", refused: true},
	{name: "date tag after Cyrillic words", input: "2024-01-01 касса\n    касса:наличные  = -4 GBP  ; оплата 日本 date:2024-01-19\n    капитал\n", refused: true},
	{name: "date tag after an empty tag name", input: "2024-01-01 t\n    c  = -4 GBP  ; :date:2024-01-19\n    b\n", refused: true},
	{name: "date tag after an empty tag name and a comma", input: "2024-01-01 t\n    c  = -4 GBP  ; :,date:2024-01-19\n    b\n", refused: true},
	{name: "date tag after an empty tag name and a comma on a comment line below", input: "2024-01-01 t\n    c  = 4 GBP\n    ; : ,date:2024-01-19\n    b\n", refused: true},

	{name: "secondary date tag", input: "2024-01-01 t\n    c  = -4 GBP  ; date2:2024-01-19\n    b\n"},
	{name: "bracketed secondary date only", input: "2024-01-01 t\n    c  = -4 GBP  ; [=2024-01-19]\n    b\n"},
	{name: "capitalised tag name", input: "2024-01-01 t\n    c  = -4 GBP  ; Date:2024-01-19\n    b\n"},
	{name: "tag name with a prefix", input: "2024-01-01 t\n    c  = -4 GBP  ; xdate:2024-01-19\n    b\n"},
	{name: "tag name with a space before the colon", input: "2024-01-01 t\n    c  = -4 GBP  ; date :2024-01-19\n    b\n"},
	{name: "date glued to a word by a comma", input: "2024-01-01 t\n    c  = -4 GBP  ; x,date:2024-01-19\n    b\n"},
	{name: "date glued to a word by a comma on a comment line below", input: "2024-01-01 t\n    c  = 4 GBP  ; note text\n    ; paid on,date:2024-01-19\n    b\n"},
	{name: "date inside the value of another tag", input: "2024-01-01 t\n    c  = -4 GBP  ; foo:bar date:2024-01-19\n    b\n"},
	{name: "secondary date tag after words", input: "2024-01-01 t\n    c  = -4 GBP  ; paid on date2:2024-01-19\n    b\n"},
	{name: "bracket that is not a date", input: "2024-01-01 t\n    c  = -4 GBP  ; [garbage]\n    b\n"},
	{name: "bracket with a day only", input: "2024-01-01 t\n    c  = -4 GBP  ; [5]\n    b\n"},
	{name: "bracketed date followed by text", input: "2024-01-01 t\n    c  = -4 GBP  ; [2024-01-19 text]\n    b\n"},
	{name: "date on the other posting", input: "2024-01-01 t\n    c  = -4 GBP\n    b  ; date:2024-01-19\n"},
	{name: "date on the transaction", input: "2024-01-01 t ; date:2024-01-19\n    c  = -4 GBP\n    b\n"},
	{name: "date on a posting with an amount and an assertion", input: "2024-01-01 t\n    c  -4 GBP = -4 GBP  ; date:2024-01-19\n    b\n"},
	{name: "date beside an assignment that has none", input: "2024-01-01 t\n    c  = -4 GBP\n    b  4 GBP  ; date:2024-01-19\n"},
	{name: "bracketed date inside double brackets", input: "2024-01-01 t\n    c  = -4 GBP  ; [[2024-01-20]]\n    b\n", refused: true},
	{name: "first of a bracketed date and a date tag below", input: "2024-01-01 t\n    c  = -4 GBP  ; [2024-01-05]\n    ; date:2024-01-20\n    b\n", refused: true},
	{name: "bracketed date with spaces inside", input: "2024-01-01 t\n    c  = -4 GBP  ; [ 2024-01-20 ]\n    b\n"},
	{name: "bracketed date with a trailing space", input: "2024-01-01 t\n    c  = -4 GBP  ; [2024-01-20 ]\n    b\n"},
	{name: "bracketed date with a leading tab", input: "2024-01-01 t\n    c  = -4 GBP  ; [\t2024-01-20]\n    b\n"},
	{name: "bracketed date with spaces inside a note", input: "2024-01-01 t\n    c  = -4 GBP  ; paid [ 2024-01-20 ] by card\n    b\n"},
	{name: "bracketed date with spaces on a comment line below", input: "2024-01-01 t\n    c  = -4 GBP\n    ; [ 2024-01-20 ]\n    b\n"},
	{name: "bracket split across comment lines", input: "2024-01-01 t\n    c  = -4 GBP  ; ref [\n    ; 2024-01-20] x\n    b\n"},
	{name: "bracket with text after =", input: "2024-01-01 t\n    c  = -4 GBP  ; [2024-01-05=garbage]\n    b\n"},
	{name: "bracket with Cyrillic text after =", input: "2024-01-01 касса\n    касса:наличные  = -4 GBP  ; [2024-01-05=заметка]\n    капитал\n"},
	{name: "bracket with a third part after = that is text", input: "2024-01-01 t\n    c  = -4 GBP  ; [2024-01-05=2024-01-06=x]\n    b\n"},
	{name: "bracket with text after = on a comment line below", input: "2024-01-01 t\n    c  = -4 GBP\n    ; [2024-01-05=garbage]\n    b\n"},
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

	assert.Equal(t, 19, registerDates(&journal.Transactions[0])[0].Day)
	assert.Equal(t, 20, registerDates(&journal.Transactions[1])[0].Day)
}

func TestPostingDateOverride_ReadsCommentLinesBelowAPosting(t *testing.T) {
	journal, errs := parser.Parse("2024-01-01 t\n    c  4 GBP\n    ; date:2024-01-19\n    b\n    ; [2024-01-20]\n")
	require.Empty(t, errs)

	tx := &journal.Transactions[0]
	assert.Equal(t, 19, registerDates(tx)[0].Day)
	assert.Equal(t, 20, registerDates(tx)[1].Day)
}

// postingDateCases are postings of an ordinary transaction with a comment that may
// carry a date. wantDay is the day hledger lists the posting at, or 0 for the
// transaction's own date (2024-01-01).
var postingDateCases = []struct {
	name    string
	posting string
	wantDay int
}{
	{"bracketed date", "    c  4 GBP  ; [2024-01-20]", 20},
	{"bracketed date with spaces inside", "    c  4 GBP  ; [ 2024-01-20 ]", 0},
	{"bracketed date with a trailing space", "    c  4 GBP  ; [2024-01-20 ]", 0},
	{"bracketed date with a leading tab", "    c  4 GBP  ; [\t2024-01-20]", 0},
	{"bracketed date with spaces inside a note", "    c  4 GBP  ; paid [ 2024-01-20 ] by card", 0},
	{"bracket split across comment lines", "    c  4 GBP  ; ref [\n    ; 2024-01-20] x", 0},
	{"double brackets", "    c  4 GBP  ; [[2024-01-20]]", 20},
	{"two bracketed dates", "    c  4 GBP  ; [2024-01-20] [2024-01-05]", 20},
	{"bracket with an empty primary date first", "    c  4 GBP  ; [=2024-01-30] [2024-01-20]", 20},
	{"bracket inside a tag value", "    c  4 GBP  ; foo:x [2024-01-20]", 20},
	{"bracket before a date tag on one line", "    c  4 GBP  ; [2024-01-20] date:2024-01-05", 20},
	{"date tag before a bracket on one line", "    c  4 GBP  ; date:2024-01-05 [2024-01-20]", 5},
	{"date tag before a bracket after a comma", "    c  4 GBP  ; date:2024-01-05, [2024-01-20]", 5},
	{"bracket before a date tag after commas", "    c  4 GBP  ; foo:1, [2024-01-20], date:2024-01-05", 20},
	{"bracket on the posting line, date tag below", "    c  4 GBP  ; [2024-01-05]\n    ; date:2024-01-20", 5},
	{"bracket on the posting line, other date tag below", "    c  4 GBP  ; [2024-01-20]\n    ; date:2024-01-05", 20},
	{"date tag on a comment line below", "    c  4 GBP  ; note\n    ; date:2024-01-20", 20},
	{"date tag with trailing text", "    c  4 GBP  ; date:2024-01-20 text", 20},
	{"capitalised tag name", "    c  4 GBP  ; Date:2024-01-20", 0},
	{"Cyrillic and CJK text around a tag", "    c  4 GBP  ; заметка, date:2024-01-20 日本", 20},
	{"bracket with text after =", "    c  4 GBP  ; [2024-01-20=garbage]", 0},
	{"text bracket before a bracketed date", "    c  4 GBP  ; [2024-01-05=x] [2024-01-20]", 20},
	{"bracket with Cyrillic text after = below", "    c  4 GBP\n    ; [2024-01-20=заметка]", 0},
	{"date tag after words", "    c  4 GBP  ; paid on date:2024-01-20", 20},
	{"date tag after Cyrillic and CJK words", "    c  4 GBP  ; оплата 日本 date:2024-01-20", 20},
	{"date tag after words on a comment line below", "    c  4 GBP  ; note\n    ; paid on date:2024-01-20", 20},
	{"date tag after an empty tag name", "    c  4 GBP  ; :date:2024-01-20", 20},
	{"date tag after an empty tag name and a comma", "    c  4 GBP  ; :,date:2024-01-20", 20},
	{"date tag after an empty tag name and a comma on a comment line below", "    c  4 GBP\n    ; :\u3000,date:2024-01-20", 20},
	{"date tag after an empty tag name and two commas", "    c  4 GBP  ; :,,date:2024-01-20", 0},
	{"date glued to a word by a comma", "    c  4 GBP  ; x,date:2024-01-20", 0},
	{"date after a leading comma", "    c  4 GBP  ; ,date:2024-01-20", 0},
	{"date inside the value of another tag", "    c  4 GBP  ; foo:bar date:2024-01-20", 0},
	{"date tag after the value of another tag", "    c  4 GBP  ; foo:bar, then date:2024-01-20", 20},
}

func postingDateJournal(posting string) string {
	return "2024-01-01 t\n" + posting + "\n    b\n"
}

func TestPostingDateOverride_FollowsHledgerOrderAndBrackets(t *testing.T) {
	for _, tc := range postingDateCases {
		t.Run(tc.name, func(t *testing.T) {
			journal, errs := parser.Parse(postingDateJournal(tc.posting))
			require.Empty(t, errs)

			tx := &journal.Transactions[0]
			want := tc.wantDay
			if want == 0 {
				want = 1
			}
			assert.Equal(t, want, registerDates(tx)[0].Day)
		})
	}
}

// TestOracle_PostingDatesMatchHledgerRegister compares the date a posting is
// listed at with `hledger reg`.
func TestOracle_PostingDatesMatchHledgerRegister(t *testing.T) {
	if os.Getenv("HLEDGER_ORACLE") == "" {
		t.Skip("set HLEDGER_ORACLE=1 to compare against the installed hledger binary")
	}
	hledgerPath, err := exec.LookPath("hledger")
	if err != nil {
		t.Skip("hledger is not installed")
	}

	for _, tc := range postingDateCases {
		t.Run(tc.name, func(t *testing.T) {
			input := postingDateJournal(tc.posting)
			cmd := exec.Command(hledgerPath, "-f", "-", "reg", "-O", "csv", "acct:^c$")
			cmd.Stdin = strings.NewReader(input)
			output, err := cmd.Output()
			require.NoError(t, err)

			rows, err := csv.NewReader(strings.NewReader(string(output))).ReadAll()
			require.NoError(t, err)
			require.Len(t, rows, 2)
			want := tc.wantDay
			if want == 0 {
				want = 1
			}
			assert.Equal(t, fmt.Sprintf("2024-01-%02d", want), rows[1][1], "hledger's date for:\n%s", input)
		})
	}
}

// dateTagCases are ordinary postings whose date tags hledger loads or refuses.
var dateTagCases = []struct {
	name    string
	posting string
	invalid bool
}{
	{"date followed by a letter", "    c  4 GBP  ; date:2024-01-20x", false},
	{"date followed by text", "    c  4 GBP  ; date:2024-01-20 text", false},
	{"date followed by a time", "    c  4 GBP  ; date:2024-01-20T10:00", false},
	{"date followed by a semicolon", "    c  4 GBP  ; date:2024-01-20;more", false},
	{"date between Cyrillic and CJK text", "    c  4 GBP  ; заметка, date:2024-01-20 日本", false},
	{"secondary date followed by a letter", "    c  4 GBP  ; date2:2024-01-20x", false},
	{"capitalised name with a bad value", "    c  4 GBP  ; Date:garbage", false},
	{"capitalised secondary name with a bad value", "    c  4 GBP  ; DATE2:garbage", false},
	{"bad value", "    c  4 GBP  ; date:garbage", true},
	{"impossible date followed by text", "    c  4 GBP  ; date:2024-02-30 text", true},
	{"month without a day", "    c  4 GBP  ; date:2024-01", true},
	{"bad secondary value", "    c  4 GBP  ; date2:garbage", true},
	{"bad value after words", "    c  4 GBP  ; a date:garbage", true},
	{"empty value after words", "    c  4 GBP  ; a date:", true},
	{"bad secondary value after words", "    c  4 GBP  ; note date2:garbage", true},
	{"bad value after Cyrillic words", "    c  4 GBP  ; оплата date:мусор", true},
	{"bad value glued to a word by a comma", "    c  4 GBP  ; x,date:garbage", false},
	{"bad value inside the value of another tag", "    c  4 GBP  ; foo:bar date:garbage", false},
	{"bad value after words on a comment line below", "    c  4 GBP\n    ; paid on date:garbage", true},
}

func TestAnalyzer_DateTagValuesAreReadLikeHledger(t *testing.T) {
	for _, tc := range dateTagCases {
		t.Run(tc.name, func(t *testing.T) {
			journal, errs := parser.Parse(postingDateJournal(tc.posting))
			require.Empty(t, errs)

			reported := false
			for _, d := range New().Analyze(journal).Diagnostics {
				reported = reported || d.Code == "INVALID_DATE_TAG" || d.Code == "EMPTY_DATE_TAG"
			}
			assert.Equal(t, tc.invalid, reported)
		})
	}
}

func TestOracle_DateTagVerdictsMatchHledger(t *testing.T) {
	if os.Getenv("HLEDGER_ORACLE") == "" {
		t.Skip("set HLEDGER_ORACLE=1 to compare against the installed hledger binary")
	}
	hledgerPath, err := exec.LookPath("hledger")
	if err != nil {
		t.Skip("hledger is not installed")
	}

	for _, tc := range dateTagCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, !tc.invalid, hledgerAccepts(t, hledgerPath, postingDateJournal(tc.posting)))
		})
	}
}

// hashCommentCases are journals with an indented # or * line under a posting.
// hledger reads only ; lines below a posting as its comment: a # or * line is a
// posting of its own there, so a date written in it dates nothing and every one of
// these journals loads.
var hashCommentCases = []struct {
	name  string
	input string
}{
	{"bracketed date on a # line", "2024-01-01 open\n    c  100 A\n    b\n\n2024-01-10 t1\n    c  10 A\n    # paid [2024-01-20]\n    b  -10 A\n\n2024-01-15 t2\n    c  0 A = 110 A\n    b  0 A\n"},
	{"date tag on a # line", "2024-01-01 open\n    c  100 A\n    b\n\n2024-01-10 t1\n    c  10 A\n    # paid date:2024-01-20\n    b  -10 A\n\n2024-01-15 t2\n    c  0 A = 110 A\n    b  0 A\n"},
	{"bracketed date on a * line", "2024-01-01 open\n    c  100 A\n    b\n\n2024-01-10 t1\n    c  10 A\n    * paid [2024-01-20]\n    b  -10 A\n\n2024-01-15 t2\n    c  0 A = 110 A\n    b  0 A\n"},
	{"date tag on a * line", "2024-01-01 open\n    c  100 A\n    b\n\n2024-01-10 t1\n    c  10 A\n    * paid date:2024-01-20\n    b  -10 A\n\n2024-01-15 t2\n    c  0 A = 110 A\n    b  0 A\n"},
	{"bracketed date on a # line under an assignment", "2024-01-01 t\n    c  = 50 A\n    # paid [2024-01-20]\n    b  -50 A\n"},
	{"date tag on a # line under an assignment", "2024-01-01 t\n    c  = 50 A\n    # paid date:2024-01-20\n    b  -50 A\n"},
	{"date tag on a # line in Cyrillic and CJK", "2024-01-01 касса\n    現金  = 50 A\n    # оплата date:2024-01-20 日本\n    капитал  -50 A\n"},
}

func TestAnalyzer_HashCommentLinesBelowAPostingDateNothing(t *testing.T) {
	for _, tc := range hashCommentCases {
		t.Run(tc.name, func(t *testing.T) {
			journal, errs := parser.Parse(tc.input)
			require.Empty(t, errs)
			for _, d := range New().Analyze(journal).Diagnostics {
				assert.NotEqual(t, SeverityError, d.Severity, "%s: %s", d.Code, d.Message)
			}

			resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: tc.input})
			assert.Empty(t, CheckJournalBalance(resolved, decimal.Zero))
		})
	}
}

func TestOracle_HashCommentLinesBelowAPostingMatchHledger(t *testing.T) {
	if os.Getenv("HLEDGER_ORACLE") == "" {
		t.Skip("set HLEDGER_ORACLE=1 to compare against the installed hledger binary")
	}
	hledgerPath, err := exec.LookPath("hledger")
	if err != nil {
		t.Skip("hledger is not installed")
	}

	for _, tc := range hashCommentCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.True(t, hledgerAccepts(t, hledgerPath, tc.input), "hledger loads:\n%s", tc.input)
		})
	}
}
