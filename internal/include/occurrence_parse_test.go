package include

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/ast"
)

func writeJournals(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
	return dir
}

func loadedJournal(t *testing.T, resolved *ResolvedJournal, path string) *ast.Journal {
	t.Helper()
	occurrences := resolved.OccurrencesForPath(path)
	require.Len(t, occurrences, 1)
	return occurrences[0].Journal
}

func TestLoader_ReusesParseOfUnchangedLeaf(t *testing.T) {
	for name, leaf := range map[string]string{
		"LF":   "2024-01-02 покупка\n    расходы:食品  10 ₽\n    активы:наличные\n",
		"CRLF": "2024-01-02 покупка\r\n    расходы:食品  10 ₽\r\n    активы:наличные\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := writeJournals(t, map[string]string{
				"root.journal":  "include leaf.journal\ninclude other.journal\n",
				"leaf.journal":  leaf,
				"other.journal": "2024-01-03 rent\n    expenses:rent  1 USD\n    assets:cash\n",
			})
			root := filepath.Join(dir, "root.journal")
			leafPath := filepath.Join(dir, "leaf.journal")
			otherPath := filepath.Join(dir, "other.journal")

			loader := NewLoader()
			first, errs := loader.Load(root)
			require.Empty(t, errs)

			// Editing a sibling must not re-parse the file that did not change.
			second, errs := loader.LoadWithOptions(root, LoadOptions{Overlays: map[string]OverlayEntry{
				otherPath: {SourcePath: otherPath, Content: "2024-01-03 rent\n    expenses:rent  2 USD\n    assets:cash\n"},
			}})
			require.Empty(t, errs)

			assert.Same(t, loadedJournal(t, first, leafPath), loadedJournal(t, second, leafPath))
			assert.NotSame(t, loadedJournal(t, first, otherPath), loadedJournal(t, second, otherPath))
			require.Len(t, second.AllTransactions(), 2)
			assert.Equal(t, "2", second.AllTransactions()[1].Postings[0].Amount.Quantity.String())
		})
	}
}

func TestLoader_ReparsesLeafWhenIncludeContextChanges(t *testing.T) {
	dir := writeJournals(t, map[string]string{
		"root.journal": "commodity 1.000,00 EUR\ninclude leaf.journal\n",
		"leaf.journal": "2024-01-02 tx\n    expenses:a  1.000 EUR\n    assets:b\n",
	})
	root := filepath.Join(dir, "root.journal")

	loader := NewLoader()
	first, errs := loader.Load(root)
	require.Empty(t, errs)
	require.Equal(t, "1000", first.AllTransactions()[0].Postings[0].Amount.Quantity.String())

	second, errs := loader.LoadWithOptions(root, LoadOptions{Overlays: map[string]OverlayEntry{
		root: {SourcePath: root, Content: "commodity 1,000.00 EUR\ninclude leaf.journal\n"},
	}})
	require.Empty(t, errs)
	assert.Equal(t, "1", second.AllTransactions()[0].Postings[0].Amount.Quantity.String(),
		"the same text means something else under the new commodity style")
}

func TestLoader_DoesNotReuseFileWithIncludes(t *testing.T) {
	dir := writeJournals(t, map[string]string{
		"root.journal": "include mid.journal\n",
		"mid.journal":  "include leaf.journal\n",
		"leaf.journal": "2024-01-02 tx\n    expenses:a  1 USD\n    assets:b\n",
	})
	root := filepath.Join(dir, "root.journal")
	leafPath := filepath.Join(dir, "leaf.journal")

	loader := NewLoader()
	_, errs := loader.Load(root)
	require.Empty(t, errs)

	// mid.journal's own text is unchanged, but what it pulls in is not.
	second, errs := loader.LoadWithOptions(root, LoadOptions{Overlays: map[string]OverlayEntry{
		leafPath: {SourcePath: leafPath, Content: "2024-01-02 tx\n    expenses:a  7 USD\n    assets:b\n"},
	}})
	require.Empty(t, errs)
	require.Len(t, second.AllTransactions(), 1)
	assert.Equal(t, "7", second.AllTransactions()[0].Postings[0].Amount.Quantity.String())
}

func TestLoader_ReusedParseKeepsReportingParseErrors(t *testing.T) {
	dir := writeJournals(t, map[string]string{
		"root.journal": "include leaf.journal\n",
		"leaf.journal": "2024-13-45 broken date\n    expenses:a  1 USD\n",
	})
	root := filepath.Join(dir, "root.journal")

	loader := NewLoader()
	_, firstErrs := loader.Load(root)
	require.NotEmpty(t, firstErrs)
	_, secondErrs := loader.Load(root)
	assert.Equal(t, firstErrs, secondErrs)
}

func TestLoader_InvalidateFileDropsReusableParse(t *testing.T) {
	dir := writeJournals(t, map[string]string{
		"root.journal": "include leaf.journal\n",
		"leaf.journal": "2024-01-02 tx\n    expenses:a  1 USD\n    assets:b\n",
	})
	root := filepath.Join(dir, "root.journal")
	leafPath := filepath.Join(dir, "leaf.journal")

	loader := NewLoader()
	first, errs := loader.Load(root)
	require.Empty(t, errs)

	loader.InvalidateFile(leafPath)
	second, errs := loader.Load(root)
	require.Empty(t, errs)
	assert.NotSame(t, loadedJournal(t, first, leafPath), loadedJournal(t, second, leafPath))
}
