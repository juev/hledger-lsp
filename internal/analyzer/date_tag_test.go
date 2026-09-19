package analyzer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/parser"
)

func TestAnalyzer_DateTagMonthOnlyRejected(t *testing.T) {
	// hledger 1.52.4 refuses to load "; date:2024-01" (it needs a day) while
	// "; date:1/5" is a valid smart date. Verified with `hledger -f - print`.
	input := `2024-01-01 x
    a:aa  1  ; date:2024-01
    b:bb`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	result := New().Analyze(journal)
	require.Len(t, result.Diagnostics, 1)
	assert.Equal(t, "INVALID_DATE_TAG", result.Diagnostics[0].Code)
	assert.Contains(t, result.Diagnostics[0].Message, "2024-01")
}

func TestAnalyzer_PartialDateTagAccepted(t *testing.T) {
	input := `2024-01-01 x
    a:aa  1  ; date:1/5
    b:bb`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	result := New().Analyze(journal)
	assert.Empty(t, result.Diagnostics)
}

func TestAnalyzer_DateTagDiagnosticsAreErrors(t *testing.T) {
	// hledger treats a bad date: tag as a load failure, so the diagnostics are
	// errors rather than warnings.
	tests := map[string]string{
		"invalid value": `2024-01-01 x
    a:aa  1  ; date:not-a-date
    b:bb`,
		"empty value": `2024-01-01 x
    a:aa  1  ; date:
    b:bb`,
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			journal, errs := parser.Parse(input)
			require.Empty(t, errs)

			result := New().Analyze(journal)
			require.Len(t, result.Diagnostics, 1)
			assert.Equal(t, SeverityError, result.Diagnostics[0].Severity)
			assert.Contains(t, []string{"EMPTY_DATE_TAG", "INVALID_DATE_TAG"}, result.Diagnostics[0].Code)
		})
	}
}

func TestAnalyzer_IndentedTransactionCommentTagsAreVisible(t *testing.T) {
	// hledger applies tags from an indented comment line to the transaction, so
	// tag completion and validation must see them.
	input := `2024-01-01 x
    a:aa  1
    ; project:trip
    b:bb`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	result := New().Analyze(journal)
	assert.Contains(t, result.Tags, "project")
	require.Contains(t, result.TagValues["project"], "trip")
}
