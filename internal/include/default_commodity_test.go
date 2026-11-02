package include

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A symbol-less style in an included file, written under a parent D directive,
// is D's mark on D's commodity, and a symbol-less D after the include changes
// nothing: hledger 1.52.4 loads this without an error and reads 15,000 as 15.
func TestLoader_SymbolLessDirectivesAroundIncludeKeepTheDefaultCommodityMark(t *testing.T) {
	tests := []struct{ amount, qty string }{
		{"15,000", "15"},
		{"1.500", "1500"},
		{"1,5", "1.5"},
	}
	for _, tt := range tests {
		t.Run(tt.amount, func(t *testing.T) {
			dir := writeJournals(t, map[string]string{
				"main.journal": "D 1000,\ninclude b.journal\nD 1000.00\n2024-01-01 x\n    a  " + tt.amount + "\n    b\n",
				"b.journal":    "commodity 1,000.00\n",
			})
			resolved, errs := NewLoader().Load(filepath.Join(dir, "main.journal"))
			require.Empty(t, errs)
			transactions := resolved.AllTransactions()
			require.Len(t, transactions, 1)
			assert.Equal(t, tt.qty, transactions[0].Postings[0].Amount.Quantity.String())
		})
	}
}
