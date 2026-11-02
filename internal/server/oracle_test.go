package server

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/testutil"
)

// TestOracle_ServerMatchesHledgerAcrossIncludes runs the server the way an editor
// does, on a file whose directives arrive through its include tree. The server
// must read the amount as hledger does, and hledger must still read the same
// amount from what Format writes back.
func TestOracle_ServerMatchesHledgerAcrossIncludes(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	layouts := []struct {
		name string
		root func(directive string) (main, sibling string)
	}{
		{"sibling declares", func(directive string) (string, string) {
			return "include sibling.journal\ninclude tx.journal\n", directive
		}},
		{"parent declares", func(directive string) (string, string) {
			return directive + "include tx.journal\n", ""
		}},
	}

	for _, layout := range layouts {
		for _, directive := range testutil.NumberDirectives {
			for _, spelling := range testutil.NumberSpellings {
				t.Run(fmt.Sprintf("%s %q reads %q", layout.name, directive, spelling), func(t *testing.T) {
					tx := "2024-01-01 x\n    a  " + spelling + "\n    b\n"
					main, sibling := layout.root(directive)

					dir := t.TempDir()
					writeJournals(t, dir, map[string]string{
						"main.journal":    main,
						"sibling.journal": sibling,
						"tx.journal":      tx,
					})
					root := filepath.Join(dir, "main.journal")
					path := filepath.Join(dir, "tx.journal")

					want := testutil.HledgerReadFile(t, hledgerPath, root)
					if !want.Accepted {
						t.Skip("hledger rejects this journal")
					}

					ts, docURI := formatServer(t, dir, path, tx)

					journal, parseErrs := ts.cachedJournal(docURI, tx)
					require.Empty(t, parseErrs)
					amount := journal.Transactions[0].Postings[0].Amount
					assert.Equal(t, want.Quantity, amount.Quantity.String())

					require.NoError(t, os.WriteFile(path, []byte(formatted(t, ts, docURI)), 0o644))
					assert.Equal(t, want, testutil.HledgerReadFile(t, hledgerPath, root), "after Format")
				})
			}
		}
	}
}
