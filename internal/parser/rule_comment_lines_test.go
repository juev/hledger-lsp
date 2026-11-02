package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParser_CommentLinesBelowRulePostingsAreKept checks that the ; lines under a
// posting of a periodic transaction or an auto posting rule stay with it, as they
// do for an ordinary transaction: hledger reads them as that posting's comment.
func TestParser_CommentLinesBelowRulePostingsAreKept(t *testing.T) {
	journal, errs := Parse("~ monthly  ; заголовок\n    расход  5 USD\n    ; заметка [2024-01-05]\n    капитал\n\n= расход\n    налог  *2\n    ; 日本 [2024-01-06]\n")
	require.Empty(t, errs)

	require.Len(t, journal.PeriodicTransactions, 1)
	periodic := journal.PeriodicTransactions[0]
	require.Len(t, periodic.Comments, 2)
	assert.Equal(t, 3, periodic.Comments[1].Range.Start.Line)
	assert.Equal(t, byte(';'), periodic.Comments[1].Marker)
	assert.Contains(t, periodic.Comments[1].Text, "заметка [2024-01-05]")

	require.Len(t, journal.AutoPostingRules, 1)
	rule := journal.AutoPostingRules[0]
	require.Len(t, rule.Comments, 1)
	assert.Equal(t, 8, rule.Comments[0].Range.Start.Line)
	assert.Contains(t, rule.Comments[0].Text, "日本 [2024-01-06]")

	require.Empty(t, journal.Transactions)
}
