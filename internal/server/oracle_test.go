package server

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/lsputil"
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

// TestOracle_FormatKeepsValuesWhenIncludedUnderConflictingMarks formats a file
// that the journal includes twice, once under `decimal-mark ,` and once under
// `decimal-mark .`. hledger must read the same amount at both occurrences after
// Format as before it, whatever the directives in force and however the number
// is spelled; HledgerReadFile covers the second occurrence, the amounts list both.
func TestOracle_FormatKeepsValuesWhenIncludedUnderConflictingMarks(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	orders := []struct{ name, first, second string }{
		{"comma then dot", ",", "."},
		{"dot then comma", ".", ","},
	}
	for _, order := range orders {
		for _, directive := range testutil.NumberDirectives {
			for _, spelling := range testutil.NumberSpellings {
				t.Run(fmt.Sprintf("%s %q reads %q", order.name, directive, spelling), func(t *testing.T) {
					tx := "2024-01-01 x\n    a  " + spelling + "\n    b\n"
					dir := t.TempDir()
					writeJournals(t, dir, map[string]string{
						"main.journal":   directive + "include first.journal\ninclude second.journal\n",
						"first.journal":  "decimal-mark " + order.first + "\ninclude tx.journal\n",
						"second.journal": "decimal-mark " + order.second + "\ninclude tx.journal\n",
						"tx.journal":     tx,
					})
					root := filepath.Join(dir, "main.journal")
					path := filepath.Join(dir, "tx.journal")

					wantSecond := testutil.HledgerReadFile(t, hledgerPath, root)
					wantAll, accepted := testutil.HledgerPostingAmounts(t, hledgerPath, root)
					if !accepted || !wantSecond.Accepted {
						t.Skip("hledger rejects this journal")
					}

					ts, docURI := formatServer(t, dir, path, tx)
					require.NoError(t, os.WriteFile(path, []byte(formatted(t, ts, docURI)), 0o644))

					assert.Equal(t, wantSecond, testutil.HledgerReadFile(t, hledgerPath, root), "second occurrence after Format")
					gotAll, accepted := testutil.HledgerPostingAmounts(t, hledgerPath, root)
					require.True(t, accepted, "hledger rejects the journal after Format")
					assert.Equal(t, wantAll, gotAll, "every occurrence after Format")
				})
			}
		}
	}
}

// TestOracle_InsertInferredAmountBalancesInHledger applies the inferred-amount
// edit and has hledger read the result. A wrong decimal mark makes it read a
// different quantity and reject the transaction as unbalanced.
func TestOracle_InsertInferredAmountBalancesInHledger(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)
	for _, layout := range inferredLayouts() {
		for _, crlf := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s crlf=%v", layout.name, crlf), func(t *testing.T) {
				root, _ := applyInferredAmount(t, layout, crlf)
				got := testutil.HledgerReadFile(t, hledgerPath, root)
				require.True(t, got.Accepted, "hledger rejects the journal after the edit")
				assert.Equal(t, layout.quantity, got.Quantity)
			})
		}
	}
}

// TestOracle_FixUnbalancedBalancesInHledger applies the final-posting fix and
// has hledger read the result: the rewritten amount must keep the decimal mark
// in force, or the transaction stays unbalanced.
func TestOracle_FixUnbalancedBalancesInHledger(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)
	tests := []struct {
		name, directives, postings, want string
	}{
		{"decimal-mark comma, dot format", "decimal-mark ,\ncommodity 1,000.00 USD\n", "    a  15,00 USD\n    b  -10,00 USD\n", "-15,00 USD"},
		{"decimal-mark comma, thousands", "decimal-mark ,\ncommodity 1,000.00 USD\n", "    a  1.500,50 USD\n    b  -10,00 USD\n", "-1.500,50 USD"},
		{"decimal-mark dot, comma format", "decimal-mark .\ncommodity 1.000,00 USD\n", "    a  15.00 USD\n    b  -10.00 USD\n", "-15.00 USD"},
		{"comma thousands under dot mark", "decimal-mark .\ncommodity 1000.00 USD\n", "    a  1,234 USD\n    b  -1 USD\n", "-1234.00 USD"},
		{"Cyrillic accounts", "decimal-mark ,\ncommodity 1,000.00 USD\n", "    счёт  15,00 USD\n    расходы  -10,00 USD\n", "-15,00 USD"},
	}
	for _, tc := range tests {
		for _, crlf := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s crlf=%v", tc.name, crlf), func(t *testing.T) {
				tx := tc.directives + "2024-01-01 x\n" + tc.postings
				dir := t.TempDir()
				writeJournals(t, dir, map[string]string{"main.journal": "include tx.journal\n", "tx.journal": tx})
				text := tx
				if crlf {
					text = strings.ReplaceAll(tx, "\n", "\r\n")
				}
				ts, docURI := formatServer(t, dir, filepath.Join(dir, "tx.journal"), text)
				doc, _ := ts.GetDocument(docURI)
				journal, _ := ts.cachedJournal(docURI, doc)
				edit, title, ok := buildUnbalancedFix(docURI, doc, lsputil.NewPositionMapper(doc), ts.commodityFormatsForDocument(docURI), 0, &journal.Transactions[0])
				require.True(t, ok)
				assert.Contains(t, title, tc.want)

				applied := applyTextEdits(doc, edit.Changes[docURI])
				require.NoError(t, os.WriteFile(filepath.Join(dir, "tx.journal"), []byte(applied), 0o644))
				assert.True(t, testutil.HledgerReadFile(t, hledgerPath, filepath.Join(dir, "main.journal")).Accepted, applied)
			})
		}
	}
}

// TestOracle_FixUnbalancedWithAssignment checks the two sides of the assignment
// rule against hledger: a transaction whose group holds an assignment gets no
// fix, because its amount depends on the running balance (hledger needs 2 USD
// below, a local fix would write 0 USD); an assignment on an unbalanced-virtual
// posting does not take part in the check, so the fix is offered and works.
func TestOracle_FixUnbalancedWithAssignment(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	noFix := map[string]string{
		"real assignment":      "2024-01-01 opening\n    assets:bank  -5 USD\n    equity\n\n2024-01-02 check\n    assets:bank  ==-7 USD\n    expenses:x  3 USD\n",
		"Cyrillic assignment":  "2024-01-01 открытие\n    счёт  -5 USD\n    капитал\n\n2024-01-02 сверка\n    счёт  ==-7 USD\n    расходы  3 USD\n",
		"CJK balanced virtual": "2024-01-01 開設\n    資産:銀行  -5 USD\n    資本\n\n2024-01-02 照合\n    費用  3 USD\n    [資産:銀行]  ==-7 USD\n    [資本]  1 USD\n",
	}
	for name, text := range noFix {
		t.Run("no fix: "+name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "main.journal")
			writeJournals(t, dir, map[string]string{"main.journal": text})
			assert.False(t, hledgerFileAccepted(hledgerPath, path), "journal must be unbalanced in hledger")

			ts, docURI := formatServer(t, dir, path, text)
			doc, _ := ts.GetDocument(docURI)
			journal, _ := ts.cachedJournal(docURI, doc)
			_, _, ok := buildUnbalancedFix(docURI, doc, lsputil.NewPositionMapper(doc), ts.commodityFormatsForDocument(docURI), 0, &journal.Transactions[1])
			assert.False(t, ok)
		})
	}

	t.Run("fix offered for an unbalanced-virtual assignment", func(t *testing.T) {
		text := "2024-01-01 x\n    (assets:bank)  ==0 USD\n    a  5 USD\n    b  -3 USD\n"
		dir := t.TempDir()
		path := filepath.Join(dir, "main.journal")
		writeJournals(t, dir, map[string]string{"main.journal": text})
		assert.False(t, hledgerFileAccepted(hledgerPath, path))

		ts, docURI := formatServer(t, dir, path, text)
		doc, _ := ts.GetDocument(docURI)
		journal, _ := ts.cachedJournal(docURI, doc)
		edit, _, ok := buildUnbalancedFix(docURI, doc, lsputil.NewPositionMapper(doc), ts.commodityFormatsForDocument(docURI), 0, &journal.Transactions[0])
		require.True(t, ok)
		applied := applyTextEdits(doc, edit.Changes[docURI])
		require.NoError(t, os.WriteFile(path, []byte(applied), 0o644))
		assert.True(t, hledgerFileAccepted(hledgerPath, path), applied)
	})
}

// hledgerFileAccepted reports whether hledger loads the journal file. Unlike
// testutil.HledgerReadFile it does not decode the first posting's amount, which a
// zero-valued assignment posting leaves without a commodity.
func hledgerFileAccepted(hledgerPath, path string) bool {
	return exec.Command(hledgerPath, "-f", path, "print").Run() == nil
}
