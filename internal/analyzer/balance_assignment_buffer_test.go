package analyzer

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/parser"
)

// A posting with a balance assignment gets its amount from the running balance,
// which one buffer cannot know: the earlier postings can be in another file.
// hledger accepts `x  ==-5 USD` alone in a transaction when the balance is already
// -5, so the per-buffer pass must leave the verdict to CheckJournalBalance.
func TestAnalyze_LeavesBalanceAssignmentsToTheJournalPass(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"single posting", "2024-01-02 check\n    x  ==-5 USD\n"},
		{"non-ascii account", "2024-01-02 check\n    счёт:資産  ==-5 USD\n"},
		{"single equals with a space", "2024-01-02 check\n    x  = -5 USD\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			journal, errs := parser.Parse(tt.input)
			require.Empty(t, errs)

			for _, diagnostic := range New().Analyze(journal).Diagnostics {
				assert.NotEqual(t, "UNBALANCED", diagnostic.Code)
			}
		})
	}
}

// The journal pass still reports the assignment hledger rejects.
func TestCheckJournalBalance_ReportsUnbalancedAssignment(t *testing.T) {
	journal, errs := parser.Parse("2024-01-01 a\n    x  -5 USD\n    y\n\n2024-01-02 check\n    x  ==-7 USD\n")
	require.Empty(t, errs)

	diagnostics := CheckJournalBalance(&include.ResolvedJournal{Primary: journal}, decimal.Zero)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, CodeUnbalanced, diagnostics[0].Code)
}
