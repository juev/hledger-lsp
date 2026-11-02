package formatter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/juev/hledger-lsp/internal/testutil"
)

// acceptedAnnotations are posting amounts hledger accepts: costs, lot costs,
// lot dates and notes, and balance assertions, with quoted commodities that
// contain spaces in either position and in non-ASCII scripts.
var acceptedAnnotations = []string{
	`1 Q {{"A B" -5}}`,
	`1 Q {"A B" -5}`,
	`1 Q {-5 "A B"}`,
	`1 Q {{-5 "A B"}}`,
	`1 Q {-"A B" 5}`,
	`1 Q {=  "A B" 5}`,
	`1 Q {{=5 "A B"}}`,
	`"X Y" 1 {"A B" 5}`,
	`"Ж Ы" 1 {"П Р" 5}`,
	`1 "漢 字" {{5 "漢 字"}}`,
	`1 Q {"漢 字" 5} [2024-01-01] (заметка)`,
	`1 Q @ "A B" 5`,
	`1 Q @@ 5 "A B"`,
	`"X Y" 1 @ -"A B" 5`,
	`1 Q @ 5 "漢 字"`,
	`1 Q {"A B" 5} [2024-01-01] (n)`,
	`1 Q (note) [2024-01-01] {{5 "A B"}} @ "C D" 2`,
	`1 Q @ 3 "C D" {"A B" 5}`,
	`"A B" 1 = "A B" 1`,
	`1 "A B" = 1 "A B"`,
	`1 Q = 1 Q @ "A B" 5`,
	`"Ж Ы" 1 = "Ж Ы" 1`,
	`1 "漢 字" = 1 "漢 字" @@ 5 "Ж Ы"`,
}

// rejectedAnnotations are amounts hledger 1.52.4 refuses to read.
var rejectedAnnotations = []string{
	`1 Q @ -5 X @ 5 Y`,
	`1 Q @ 5 X @@ 6 X`,
	`1 Q {{-5;c}}`,
	`1 Q {5 X} {6 Y}`,
	`1 Q [2024-01-01] [2024-01-02]`,
	`1 Q (a) {5 X} (b)`,
	`1 Q {5 X`,
	`1 Q {{5 X}`,
	`1 Q {5 X}}`,
}

func annotationJournal(amount string) string {
	return "2024-01-01 x\n    a  " + amount + "\n    b\n"
}

func TestOracle_FormattingKeepsAnnotationMeaning(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	for _, amount := range acceptedAnnotations {
		t.Run(amount, func(t *testing.T) {
			input := annotationJournal(amount)
			before, ok := testutil.HledgerPostingDetails(t, hledgerPath, input)
			require.True(t, ok, "hledger must accept %q", amount)

			once := formatForOracle(t, input)
			after, ok := testutil.HledgerPostingDetails(t, hledgerPath, once)
			require.True(t, ok, "formatted journal rejected:\n%s", once)
			assert.Equal(t, before, after, "formatted journal:\n%s", once)
			assert.Equal(t, once, formatForOracle(t, once), "format(format(x)) must equal format(x)")
		})
	}
}

func TestOracle_RejectedAnnotationsAreRejectedByTheParser(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	for _, amount := range rejectedAnnotations {
		t.Run(amount, func(t *testing.T) {
			input := annotationJournal(amount)
			_, ok := testutil.HledgerPostingDetails(t, hledgerPath, input)
			require.False(t, ok, "hledger must reject %q", amount)

			_, errs := parser.Parse(input)
			assert.NotEmpty(t, errs, "the parser must report %q", amount)
		})
	}
}

func TestFormatDocument_AnnotationsKeepTheirQuotedCommodity(t *testing.T) {
	cases := map[string]string{
		`1 Q {{"A B" -5}}`:                 `1 Q {{"A B" -5}}`,
		`1 Q {"A B" -5}`:                   `1 Q {"A B" -5}`,
		`1 Q {-5 "A B"}`:                   `1 Q {-5 "A B"}`,
		`1 Q {=  "A B" 5}`:                 `1 Q {="A B" 5}`,
		`"Ж Ы" 1 {"П Р" 5}`:                `"Ж Ы" 1 {"П Р" 5}`,
		`1 "漢 字" {{5 "漢 字"}}`:              `1 "漢 字" {{5 "漢 字"}}`,
		`1 Q @ "A B" 5`:                    `1 Q @ "A B" 5`,
		`1 Q (n) [2024-01-01] {{5 "A B"}}`: `1 Q {{5 "A B"}} [2024-01-01] (n)`,
		`1 Q = 1 Q @@ 5 "漢 字"`:             `1 Q = 1 Q @@ 5 "漢 字"`,
	}

	for amount, want := range cases {
		t.Run(amount, func(t *testing.T) {
			input := annotationJournal(amount)
			once := formatForOracle(t, input)
			assert.Equal(t, "2024-01-01 x\n    a  "+want+"\n    b\n", once)
			assert.Equal(t, once, formatForOracle(t, once), "formatting must be idempotent")

			crlf := strings.ReplaceAll(strings.ReplaceAll(input, "\n", "\r\n"), "\r\n", "\n")
			assert.Equal(t, once, formatForOracle(t, crlf))
		})
	}
}

func TestFormatDocument_RejectedAnnotationLinesAreLeftAlone(t *testing.T) {
	for _, amount := range rejectedAnnotations {
		t.Run(amount, func(t *testing.T) {
			input := "2024-01-01 x\n    a   " + amount + "\n    b\n"
			journal, errs := parser.Parse(input)
			require.NotEmpty(t, errs)
			assert.Equal(t, input, applyEdits(input, FormatDocument(journal, input)))
		})
	}
}
