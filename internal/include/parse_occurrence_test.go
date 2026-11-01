package include

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOccurrence_AppliesInheritedAndIncludedContext(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
		return path
	}
	write("eur.journal", "commodity 1.000,00 EUR\n")
	write("usd.journal", "commodity 1,000.00 USD\n")
	child := write("child.journal", "include eur.journal\n")
	root := write("main.journal", "include usd.journal\ninclude child.journal\n")

	resolved, errs := NewLoader().Load(root)
	require.Empty(t, errs)
	occurrences := resolved.OccurrencesForPath(child)
	require.Len(t, occurrences, 1)

	journal, ok := resolved.ParseOccurrence(occurrences[0].ID, "include eur.journal\n2024-01-01 x\n    a  1,500 USD\n    b  1.500 EUR\n    c\n")
	require.True(t, ok)
	postings := journal.Transactions[0].Postings
	assert.Equal(t, "1500", postings[0].Amount.Quantity.String(), "inherited from the parent")
	assert.Equal(t, "1500", postings[1].Amount.Quantity.String(), "exported by the file's own include")
}

func TestParseOccurrence_UnknownOccurrence(t *testing.T) {
	_, ok := NewResolvedJournal(nil).ParseOccurrence(99, "")
	assert.False(t, ok)
}
