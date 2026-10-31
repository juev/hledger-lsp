package fmtcmd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/testutil"
)

// TestOracle_WriteKeepsAmountValues has hledger read every posting before and
// after -w: rewriting a number in the commodity's style must not change what it
// is worth.
func TestOracle_WriteKeepsAmountValues(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	for _, tc := range amountValueCases {
		for _, layout := range amountValueLayouts {
			t.Run(tc.name+"/"+layout.name, func(t *testing.T) {
				path, args := layout.layout(t, t.TempDir(), tc.directive, tc.journal)
				journal := path
				if len(args) == 2 && args[0] == "--root" {
					journal = args[1]
				}
				before, accepted := testutil.HledgerPostingAmounts(t, hledgerPath, journal)
				require.True(t, accepted, "hledger rejects the journal before formatting")

				code, _, stderr := run(t, "", append(args, "-w", path)...)
				require.Equal(t, ExitOK, code, stderr)

				after, accepted := testutil.HledgerPostingAmounts(t, hledgerPath, journal)
				require.True(t, accepted, "hledger rejects the journal after formatting:\n%s", readFile(t, path))
				assert.Equal(t, before, after, "formatted:\n%s", readFile(t, path))
			})
		}
	}

	t.Run("file included under two decimal marks", func(t *testing.T) {
		dir := t.TempDir()
		path := writeJournal(t, dir, "tx.journal", "2024-01-01 a\n  e:x  1,000 EUR\n  a:y\n")
		root := writeJournal(t, dir, "main.journal", strings.Join([]string{
			"decimal-mark .", "include tx.journal", "decimal-mark ,", "include tx.journal", "",
		}, "\n"))
		before, accepted := testutil.HledgerPostingAmounts(t, hledgerPath, root)
		require.True(t, accepted)

		code, _, stderr := run(t, "", "--root", root, "-w", path)
		require.Equal(t, ExitOK, code, stderr)

		after, accepted := testutil.HledgerPostingAmounts(t, hledgerPath, root)
		require.True(t, accepted)
		assert.Equal(t, before, after)
	})
}
