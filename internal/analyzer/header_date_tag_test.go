package analyzer

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/parser"
)

// headerDateTagCases are date:/date2: tags in the comment of a transaction and
// of its postings. hledger 1.52.4 reads a posting date only from the comment of
// a posting and the ; lines below it: in the header comment and on the comment
// lines before the first posting the tag is prose, so any value loads there.
// want holds the date tag diagnostic each journal must produce, nil if none.
var headerDateTagCases = []struct {
	name  string
	input string
	want  *dateTagDiagnostic
}{
	{name: "header, yearless month 13", input: "2023-01-01 t  ; date:13-05\n    a  5 USD\n    b\n"},
	{name: "header, month 13", input: "2023-01-01 t  ; date:2024-13-05\n    a  5 USD\n    b\n"},
	{name: "header, not a date", input: "2023-01-01 t  ; date:garbage\n    a  5 USD\n    b\n"},
	{name: "header, empty value", input: "2023-01-01 t  ; date:\n    a  5 USD\n    b\n"},
	{name: "header, secondary month 13", input: "2023-01-01 t  ; date2:2024-13-05\n    a  5 USD\n    b\n"},
	{name: "header, Cyrillic and CJK prose", input: "2023-01-01 касса  ; заметка date:2024-13-05 日本\n    現金  5 USD\n    капитал\n"},
	{name: "header continuation, month 13", input: "2023-01-01 t\n    ; date:2024-13-05\n    a  5 USD\n    b\n"},
	{name: "header continuation, empty value", input: "2023-01-01 t\n    ; date:\n    a  5 USD\n    b\n"},
	{name: "header continuation, secondary not a date", input: "2023-01-01 t  ; note\n    ; date2:garbage\n    a  5 USD\n    b\n"},

	{
		name:  "posting, month 13",
		input: "2023-01-01 t\n    a  5 USD  ; date:2024-13-05\n    b\n",
		want:  &dateTagDiagnostic{"INVALID_DATE_TAG", 2, 17},
	},
	{
		name:  "posting, yearless month 13",
		input: "2023-01-01 t\n    a  5 USD  ; date:13-05\n    b\n",
		want:  &dateTagDiagnostic{"INVALID_DATE_TAG", 2, 17},
	},
	{
		name:  "posting continuation, month 13",
		input: "2023-01-01 t\n    a  5 USD\n    ; date:2024-13-05\n    b\n",
		want:  &dateTagDiagnostic{"INVALID_DATE_TAG", 3, 7},
	},
	{
		name:  "posting continuation, yearless month 13",
		input: "2023-01-01 t\n    a  5 USD\n    ; date:13-05\n    b\n",
		want:  &dateTagDiagnostic{"INVALID_DATE_TAG", 3, 7},
	},
	{
		name:  "posting continuation, empty value",
		input: "2023-01-01 t\n    a  5 USD\n    ; date:\n    b\n",
		want:  &dateTagDiagnostic{"EMPTY_DATE_TAG", 3, 7},
	},
	{
		name:  "header and posting, only the posting is refused",
		input: "2023-01-01 t  ; date:2024-13-05\n    ; date2:garbage\n    a  5 USD  ; date2:2024-13-05\n    b\n",
		want:  &dateTagDiagnostic{"INVALID_DATE_TAG", 3, 17},
	},
}

type dateTagDiagnostic struct {
	code         string
	line, column int
}

func dateTagDiagnostics(diagnostics []Diagnostic) []dateTagDiagnostic {
	var got []dateTagDiagnostic
	for _, d := range diagnostics {
		if d.Code == "INVALID_DATE_TAG" || d.Code == "EMPTY_DATE_TAG" {
			got = append(got, dateTagDiagnostic{d.Code, d.Range.Start.Line, d.Range.Start.Column})
		}
	}
	return got
}

func TestAnalyzer_HeaderDateTagsAreProse(t *testing.T) {
	for _, tc := range headerDateTagCases {
		t.Run(tc.name, func(t *testing.T) {
			journal, errs := parser.Parse(tc.input)
			require.Empty(t, errs)

			var want []dateTagDiagnostic
			if tc.want != nil {
				want = []dateTagDiagnostic{*tc.want}
			}
			assert.Equal(t, want, dateTagDiagnostics(New().Analyze(journal).Diagnostics))
		})
	}
}

func TestOracle_HeaderDateTagVerdictsMatchHledger(t *testing.T) {
	if os.Getenv("HLEDGER_ORACLE") == "" {
		t.Skip("set HLEDGER_ORACLE=1 to compare against the installed hledger binary")
	}
	hledgerPath, err := exec.LookPath("hledger")
	if err != nil {
		t.Skip("hledger is not installed")
	}

	for _, tc := range headerDateTagCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, hledgerPath, "-f", "-", "check")
			cmd.Stdin = strings.NewReader(tc.input)
			output, err := cmd.CombinedOutput()
			require.NoError(t, ctx.Err(), "hledger did not finish")

			if tc.want == nil {
				assert.NoError(t, err, "hledger must load:\n%s\n%s", tc.input, output)
				return
			}
			assert.Error(t, err, "hledger must refuse:\n%s", tc.input)
			assert.Contains(t, string(output), "-:"+strconv.Itoa(tc.want.line)+":", "hledger must point at the same line")
		})
	}
}
