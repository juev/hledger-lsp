package analyzer

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/juev/hledger-lsp/internal/textutil"
)

// ruleDateTagForms place a tag on a rule posting, on a comment line below it,
// and in the rule header, where hledger reads it as prose. A periodic
// transaction reads a date without a year in the year in force; an auto posting
// rule has none.
var ruleDateTagForms = []struct {
	name   string
	format string
	// prose is true where hledger never reads the tag as a date.
	prose bool
	// auto is true for an auto posting rule.
	auto bool
	// year is the year a periodic transaction reads a yearless date in.
	year int
}{
	{name: "periodic posting", format: "Y 2024\n~ monthly\n    расход  5 USD  ; %s\n    b\n", year: 2024},
	{name: "periodic comment line", format: "Y 2024\n~ monthly\n    расход  5 USD\n    ; %s\n    b\n", year: 2024},
	{name: "periodic header", format: "Y 2024\n~ monthly  ; %s\n    расход  5 USD\n    b\n", prose: true, year: 2024},
	{name: "periodic posting in 2023", format: "Y 2023\n~ monthly\n    расход  5 USD  ; %s\n    b\n", year: 2023},
	{name: "auto posting", format: "= расход\n    налог  *2  ; %s\n", auto: true},
	{name: "auto comment line", format: "= 支出\n    налог  *2\n    ; %s\n", auto: true},
	{name: "auto header", format: "= a  ; %s\n    b  *2\n", prose: true, auto: true},
}

// ruleDateTags are tag values with how hledger 1.52.4 reads them: invalid
// anywhere, invalid only without a known year, or invalid in a common year.
var ruleDateTags = []struct {
	tag           string
	invalid       bool
	yearless      bool
	leapDayOnly   bool
	emptyDateCode bool
}{
	{tag: "date:2024-13-05", invalid: true},
	{tag: "date:2024-01-05"},
	{tag: "date:01-05", yearless: true},
	{tag: "date:1/5", yearless: true},
	{tag: "date:02-29", yearless: true, leapDayOnly: true},
	{tag: "date:2024-01", invalid: true},
	{tag: "date:", invalid: true, emptyDateCode: true},
	{tag: "date2:2024-13-05", invalid: true},
	{tag: "date2:2024-02-30", invalid: true},
	{tag: "date:2024-01-05x"},
	{tag: "date: 2024-13-05", invalid: true},
	{tag: "date:2023-02-29", invalid: true},
	{tag: "date2:01-05", yearless: true},
}

type ruleDateTagCase struct {
	name    string
	input   string
	invalid bool
}

func ruleDateTagCases() []ruleDateTagCase {
	var cases []ruleDateTagCase
	for _, form := range ruleDateTagForms {
		for _, tag := range ruleDateTags {
			invalid := false
			if !form.prose {
				invalid = tag.invalid ||
					(form.auto && tag.yearless) ||
					(!form.auto && tag.leapDayOnly && form.year%4 != 0)
			}
			cases = append(cases, ruleDateTagCase{
				name:    fmt.Sprintf("%s %s", form.name, tag.tag),
				input:   fmt.Sprintf(form.format, tag.tag),
				invalid: invalid,
			})
		}
	}
	return cases
}

func ruleDateTagDiagnostics(t *testing.T, input string) []Diagnostic {
	t.Helper()
	journal, errs := parser.Parse(input)
	require.Empty(t, errs, "input %q", input)
	var reported []Diagnostic
	for _, d := range New().Analyze(journal).Diagnostics {
		if d.Code == "INVALID_DATE_TAG" || d.Code == "EMPTY_DATE_TAG" {
			reported = append(reported, d)
		}
	}
	return reported
}

func TestAnalyzer_DateTagsInRules(t *testing.T) {
	for _, tc := range ruleDateTagCases() {
		for _, newline := range []string{"\n", "\r\n"} {
			t.Run(fmt.Sprintf("%s %q", tc.name, newline), func(t *testing.T) {
				input := textutil.NormalizeLineEndings(strings.ReplaceAll(tc.input, "\n", newline))
				reported := ruleDateTagDiagnostics(t, input)
				if !tc.invalid {
					assert.Empty(t, reported)
					return
				}
				require.Len(t, reported, 1)
				assert.Equal(t, SeverityError, reported[0].Severity)
			})
		}
	}
}

// A rule posting reports its date tag on the same line and columns as the same
// posting of an ordinary transaction; offsets differ with the header length.
func TestAnalyzer_DateTagRangeInRulesMatchesTransactions(t *testing.T) {
	for _, line := range []string{
		"    расход:еда  5 USD  ; 日本 date:2024-13-05",
		"    ; заметка date2:2024-02-30",
	} {
		posting, comment := line, ""
		if strings.HasPrefix(strings.TrimSpace(line), ";") {
			posting, comment = "    расход:еда  5 USD", "\n"+line
		}
		body := posting + comment + "\n    b\n"
		want := ruleDateTagDiagnostics(t, "2024-01-01 t\n"+body)
		require.Len(t, want, 1)

		for _, header := range []string{"~ monthly\n", "= расход\n"} {
			rule := header + body
			if header[0] == '=' {
				rule = header + strings.Replace(body, "5 USD", "*2.00", 1)
			}
			got := ruleDateTagDiagnostics(t, rule)
			require.Len(t, got, 1, "rule %q", rule)
			for _, pair := range [][2]int{
				{want[0].Range.Start.Line, got[0].Range.Start.Line},
				{want[0].Range.Start.Column, got[0].Range.Start.Column},
				{want[0].Range.End.Column, got[0].Range.End.Column},
				{want[0].Range.End.Offset - want[0].Range.Start.Offset, got[0].Range.End.Offset - got[0].Range.Start.Offset},
			} {
				assert.Equal(t, pair[0], pair[1], "rule %q", rule)
			}
			assert.Equal(t, want[0].Code, got[0].Code)
		}
	}
}

func TestAnalyzer_YearlessDateTagInAnAutoRuleNamesTheMissingYear(t *testing.T) {
	reported := ruleDateTagDiagnostics(t, "= a\n    b  *2  ; date:01-05\n")
	require.Len(t, reported, 1)
	assert.Contains(t, reported[0].Message, "needs a year")
}

func TestOracle_DateTagsInRulesMatchHledger(t *testing.T) {
	if os.Getenv("HLEDGER_ORACLE") == "" {
		t.Skip("set HLEDGER_ORACLE=1 to compare against the installed hledger binary")
	}
	hledgerPath, err := exec.LookPath("hledger")
	if err != nil {
		t.Skip("hledger is not installed")
	}

	for _, tc := range ruleDateTagCases() {
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
