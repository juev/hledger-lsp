package analyzer

import (
	"math"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/juev/hledger-lsp/internal/textutil"
)

func TestParsePostingDateValue_YearTooLargeForAnInt(t *testing.T) {
	date, ok := parsePostingDateValue("99999999999999999999-01-05", 2024)
	require.True(t, ok)
	assert.Equal(t, ast.Date{Year: math.MaxInt, Month: 1, Day: 5, YearDigits: "99999999999999999999"}, date)

	date, ok = parsePostingDateValue("000100000000000000000000-01-05", 2024)
	require.True(t, ok)
	assert.Equal(t, "100000000000000000000", date.YearDigits, "leading zeros do not count")

	// Leap years come from the real year: ...96 is one, ...99 is not.
	_, ok = parsePostingDateValue("99999999999999999996-02-29", 2024)
	assert.True(t, ok)
	_, ok = parsePostingDateValue("99999999999999999999-02-29", 2024)
	assert.False(t, ok)

	date, ok = parsePostingDateValue("2025-01-05", 2024)
	require.True(t, ok)
	assert.Equal(t, ast.Date{Year: 2025, Month: 1, Day: 5}, date)
}

func TestCompareDates_OrdersYearsOfAnyLength(t *testing.T) {
	big := func(digits string, month, day int) ast.Date {
		return ast.Date{Year: math.MaxInt, Month: month, Day: day, YearDigits: digits}
	}
	ordered := []ast.Date{
		{},
		{Year: 2025, Month: 1, Day: 1},
		{Year: 9000000000000000000, Month: 2, Day: 28},
		{Year: math.MaxInt - 1, Month: 12, Day: 31},
		big("9223372036854775808", 1, 1),
		big("99999999999999999999", 1, 5),
		big("99999999999999999999", 1, 6),
		big("100000000000000000000", 1, 1),
	}
	for i := range ordered {
		for j := range ordered {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			assert.Equal(t, want, compareDates(ordered[i], ordered[j]), "%v vs %v", ordered[i], ordered[j])
		}
	}
}

// largeYearJournal holds posting dates whose years do not fit an int. hledger
// 1.52.4 lists them after every ordinary date, in the order of their years.
const largeYearJournal = `2024-01-01 t
    расход  5 USD  ; [99999999999999999999-01-05]
    b

2024-01-02 u
    расход  1 USD  ; date:100000000000000000000-01-05
    b

2025-01-01 normal
    расход  2 USD = 2 USD
    b

2025-06-01 big19
    расход  4 USD  ; date:9000000000000000000-02-29
    b
`

func TestAnalyzer_LargeYearPostingsAreOrderedAfterOrdinaryDates(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		input := textutil.NormalizeLineEndings(strings.ReplaceAll(largeYearJournal, "\n", newline))
		resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: input})
		diags := CheckJournalBalance(resolved, New().balanceTolerance())
		for _, d := range diags {
			assert.NotEqual(t, CodeBalanceAssertionFailed, d.Code, "the 2025 assertion sees only its own posting: %s", d.Message)
		}
	}
}

func TestOracle_LargeYearPostingDatesMatchHledger(t *testing.T) {
	if os.Getenv("HLEDGER_ORACLE") == "" {
		t.Skip("set HLEDGER_ORACLE=1 to compare against the installed hledger binary")
	}
	hledgerPath, err := exec.LookPath("hledger")
	if err != nil {
		t.Skip("hledger is not installed")
	}

	t.Run("running balances", func(t *testing.T) {
		assertRunningBalancesMatchRegister(t, hledgerPath, largeYearJournal)
	})

	for _, input := range []string{
		largeYearJournal,
		"2024-01-01 t\n    a  5 USD  ; date:99999999999999999996-02-29\n    b\n",
		"2024-01-01 t\n    a  5 USD  ; date:99999999999999999999-02-29\n    b\n",
		"2024-01-01 t\n    a  5 USD  ; [99999999999999999999-02-29]\n    b\n",
		"2024-01-01 t\n    a  5 USD  ; [99999999999999999996-02-29]\n    b\n",
		"2024-01-01 t\n    a  = 5 USD  ; date:99999999999999999999-01-05\n    b\n",
	} {
		journal, errs := parser.Parse(input)
		require.Empty(t, errs)
		failed := false
		for _, d := range New().Analyze(journal).Diagnostics {
			failed = failed || d.Severity == SeverityError
		}
		resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: input})
		for _, d := range CheckJournalBalance(resolved, New().balanceTolerance()) {
			failed = failed || d.Severity == SeverityError
		}
		assert.Equal(t, !hledgerAccepts(t, hledgerPath, input), failed, "our verdict must match hledger for:\n%s", input)
	}
}
