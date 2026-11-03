package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/testutil"
)

// tx.journal is included twice under different decimal marks, once by its own
// name and once as alias.journal, a symlink or a hard link to it. hledger reads
// 1,000 EUR as 1000 EUR at the first inclusion and as 1 EUR at the second.
const aliasedTx = "2024-01-01 покупка 買い物\n  расходы:食品  1,000 EUR\n  активы\n"

type aliasLayout struct {
	name  string
	roots []string
	files map[string]string
}

var aliasLayouts = []aliasLayout{
	{"one root", []string{"main.journal"}, map[string]string{
		"main.journal": "commodity 1000.00 EUR\ndecimal-mark .\ninclude tx.journal\ndecimal-mark ,\ninclude alias.journal\n",
	}},
	// The second tree includes only the alias, so no lookup by the formatted
	// file's name finds it.
	{"two roots", []string{"a-root.journal", "b-root.journal"}, map[string]string{
		"a-root.journal": "commodity 1000.00 EUR\ndecimal-mark .\ninclude tx.journal\n",
		"b-root.journal": "commodity 1000.00 EUR\ndecimal-mark ,\ninclude alias.journal\n",
	}},
}

var aliasLinks = []struct {
	name string
	link func(target, alias string) error
}{
	{"symlink", func(target, alias string) error { return os.Symlink(filepath.Base(target), alias) }},
	{"hard link", os.Link},
}

// setupAlias writes the layout with tx.journal in newline's line endings and
// opens the name the test formats.
func setupAlias(t *testing.T, layout aliasLayout, link func(target, alias string) error, named, newline string) (ts *testServer, dir, tx string) {
	t.Helper()
	dir = t.TempDir()
	files := map[string]string{"tx.journal": strings.ReplaceAll(aliasedTx, "\n", newline)}
	for name, content := range layout.files {
		files[name] = content
	}
	writeJournals(t, dir, files)
	tx = filepath.Join(dir, "tx.journal")
	if err := link(tx, filepath.Join(dir, "alias.journal")); err != nil {
		t.Skipf("cannot make the alias: %v", err)
	}
	ts, _ = formatServer(t, dir, filepath.Join(dir, named), aliasedTx)
	return ts, dir, tx
}

// amountsAtBothNames reads text at every occurrence of tx.journal and of
// alias.journal in every tree, matching them by name so the check does not
// depend on how the code under test tells the names are one file.
func amountsAtBothNames(t *testing.T, ts *testServer, dir, text string) [][]string {
	t.Helper()
	names := map[string]bool{filepath.Join(dir, "tx.journal"): true, filepath.Join(dir, "alias.journal"): true}
	var groups [][]string
	for _, tree := range ts.workspace.AllTrees() {
		for _, occurrence := range tree.Resolved.Occurrences {
			if !names[occurrence.Path] {
				continue
			}
			journal, _, ok := tree.Resolved.ParseOccurrence(occurrence.ID, text)
			require.True(t, ok)
			var amounts []string
			for i := range journal.Transactions {
				for j := range journal.Transactions[i].Postings {
					for _, amount := range postingAmounts(&journal.Transactions[i].Postings[j]) {
						if amount != nil {
							amounts = append(amounts, amount.Quantity.String()+" "+amount.Commodity.Symbol)
						}
					}
				}
			}
			groups = append(groups, amounts)
		}
	}
	require.Len(t, groups, 2, "both names must be included")
	return groups
}

func forEachAlias(t *testing.T, test func(t *testing.T, layout aliasLayout, link func(target, alias string) error, named, newline string)) {
	for _, layout := range aliasLayouts {
		for _, kind := range aliasLinks {
			for _, named := range []string{"tx.journal", "alias.journal"} {
				for _, newline := range []string{"\n", "\r\n"} {
					t.Run(fmt.Sprintf("%s/%s/%s/%q", layout.name, kind.name, named, newline), func(t *testing.T) {
						test(t, layout, kind.link, named, newline)
					})
				}
			}
		}
	}
}

// A file reached through another name is read there as well, so formatting
// either name keeps the number that one of the readings would change.
func TestFormat_KeepsValuesAtEveryNameOfTheFile(t *testing.T) {
	forEachAlias(t, func(t *testing.T, layout aliasLayout, link func(target, alias string) error, named, newline string) {
		ts, dir, _ := setupAlias(t, layout, link, named, newline)
		before := amountsAtBothNames(t, ts, dir, aliasedTx)
		docURI := uri.File(filepath.Join(dir, named))

		once := formatted(t, ts, docURI)
		assert.Contains(t, once, "    расходы:食品  1,000 EUR\n")
		assert.Equal(t, before, amountsAtBothNames(t, ts, dir, once))

		ts.documents.Store(docURI, once)
		assert.Equal(t, once, formatted(t, ts, docURI), "formatting must be idempotent")
	})
}

// Enter after a posting reformats it with the same edits as Format.
func TestOnTypeFormatting_KeepsValuesAtEveryNameOfTheFile(t *testing.T) {
	forEachAlias(t, func(t *testing.T, layout aliasLayout, link func(target, alias string) error, named, newline string) {
		ts, dir, _ := setupAlias(t, layout, link, named, newline)
		doc := "2024-01-01 покупка 買い物\n  расходы:食品  1,000 EUR\n    \n"
		docURI := uri.File(filepath.Join(dir, named))
		ts.documents.Store(docURI, doc)

		edits, err := ts.onTypeFormatting(docURI, 2, "\n")
		require.NoError(t, err)
		got := applyTextEdits(doc, edits)
		assert.Contains(t, got, "    расходы:食品  1,000 EUR\n")
		assert.Equal(t, amountsAtBothNames(t, ts, dir, doc), amountsAtBothNames(t, ts, dir, got))
	})
}

// Oracle: hledger reads every root the same after the formatted text is
// written to the file as before.
func TestOracle_FormatKeepsValuesAtEveryNameOfTheFile(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)
	forEachAlias(t, func(t *testing.T, layout aliasLayout, link func(target, alias string) error, named, newline string) {
		ts, dir, tx := setupAlias(t, layout, link, named, newline)
		want := make(map[string][]string)
		for _, root := range layout.roots {
			amounts, ok := testutil.HledgerPostingAmounts(t, hledgerPath, filepath.Join(dir, root))
			require.True(t, ok)
			want[root] = amounts
		}

		once := formatted(t, ts, uri.File(filepath.Join(dir, named)))
		require.NoError(t, os.WriteFile(tx, []byte(strings.ReplaceAll(once, "\n", newline)), 0o644))

		for _, root := range layout.roots {
			got, ok := testutil.HledgerPostingAmounts(t, hledgerPath, filepath.Join(dir, root))
			require.True(t, ok)
			assert.Equal(t, want[root], got, "%s after formatting:\n%s", root, once)
		}
	})
}

// When an included name cannot be compared with the formatted file, it may be
// another name for it, so no edit is offered rather than one that could change
// a number there.
func TestFormat_RefusesWhenAnIncludedNameCannotBeChecked(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can stat any file")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	writeJournals(t, dir, map[string]string{
		"tx.journal":   aliasedTx,
		"main.journal": "commodity 1000.00 EUR\ndecimal-mark .\ninclude tx.journal\ndecimal-mark ,\ninclude sub/alias.journal\n",
	})
	tx := filepath.Join(dir, "tx.journal")
	require.NoError(t, os.Link(tx, filepath.Join(sub, "alias.journal")))
	ts, docURI := formatServer(t, dir, tx, aliasedTx)
	require.NoError(t, os.Chmod(sub, 0))
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })
	if _, err := os.Stat(filepath.Join(sub, "alias.journal")); err == nil {
		t.Skip("the system still lets the file be checked")
	}

	edits, err := ts.Format(context.Background(), &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
	})
	require.NoError(t, err)
	assert.Empty(t, edits)
}

// A name removed since the workspace was read no longer reaches the file, so
// it neither stops formatting nor holds back the numbers.
func TestFormat_IgnoresAnAliasThatIsGone(t *testing.T) {
	dir := t.TempDir()
	writeJournals(t, dir, map[string]string{
		"tx.journal":   aliasedTx,
		"main.journal": "commodity 1000.00 EUR\ndecimal-mark .\ninclude tx.journal\ndecimal-mark ,\ninclude alias.journal\n",
	})
	tx := filepath.Join(dir, "tx.journal")
	require.NoError(t, os.Link(tx, filepath.Join(dir, "alias.journal")))
	ts, docURI := formatServer(t, dir, tx, aliasedTx)
	require.NoError(t, os.Remove(filepath.Join(dir, "alias.journal")))

	assert.Contains(t, formatted(t, ts, docURI), "    расходы:食品  1000.00 EUR\n")
}

// A document with no file behind it has no other name and is formatted with
// the readings it has.
func TestFormat_DocumentWithoutAFile(t *testing.T) {
	dir := t.TempDir()
	writeJournals(t, dir, map[string]string{"main.journal": "commodity 1000.00 EUR\n"})
	ts, docURI := formatServer(t, dir, filepath.Join(dir, "unsaved.journal"), "2024-01-01 x\n  e:x  5 EUR\n  a:y\n")

	assert.Contains(t, formatted(t, ts, docURI), "    e:x  5 EUR\n")
}
