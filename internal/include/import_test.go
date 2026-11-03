package include

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/cli"
	"github.com/juev/hledger-lsp/internal/csvimport"
	"github.com/juev/hledger-lsp/internal/testutil"
)

// fakeImporter injects conversion outcomes the real binary cannot be made to
// produce on demand. It says nothing about hledger's semantics.
type fakeImporter struct {
	result   csvimport.Result
	requests []csvimport.Request
}

func (f *fakeImporter) Import(_ context.Context, request csvimport.Request) csvimport.Result {
	f.requests = append(f.requests, request)
	return f.result
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func previewImporter(t *testing.T) *csvimport.Importer {
	t.Helper()
	path := testutil.RequireHledger(t)
	client := cli.NewClient(path, 30*time.Second)
	if !client.Available() || !csvimport.SupportsInclude(client.Version()) {
		t.Skipf("hledger %q on PATH cannot include CSV files; put 1.99.5 first on PATH", client.Version())
	}
	return csvimport.New(csvimport.Config{Enabled: true, Path: path, Timeout: 30 * time.Second})
}

func accountsOf(transaction ast.Transaction) []string {
	var accounts []string
	for _, posting := range transaction.Postings {
		accounts = append(accounts, posting.Account.GetResolvedName())
	}
	return accounts
}

func TestPreviewOracle_IncludedCSVBecomesAnOccurrenceOfTheDataFile(t *testing.T) {
	importer := previewImporter(t)
	dir := t.TempDir()
	data := writeFile(t, dir, "bank.csv", "date,desc,amount\r\n2024-01-03,second,2\r\n2024-01-02,first,1.50\r\n")
	writeFile(t, dir, "bank.csv.rules", "skip 1\nfields date, description, amount\ncurrency EUR \naccount1 assets:bank\naccount2 income\n")
	root := writeFile(t, dir, "main.journal", "alias assets:bank = assets:checking\napply account top\n2024-01-01 opening\n    assets:checking  10 EUR\n    equity\ninclude bank.csv\nend apply account\n2024-01-04 after\n    x  1 EUR\n    y\n")

	loader := NewLoader()
	loader.SetImporter(importer, nil)
	resolved, errs := loader.Load(root)

	require.Empty(t, errs)
	occurrences := resolved.OccurrencesForPath(data)
	require.Len(t, occurrences, 1)
	occurrence := occurrences[0]
	require.NotNil(t, occurrence.Import)
	assert.Equal(t, data, occurrence.Import.Target)
	require.Len(t, occurrence.Journal.Transactions, 2)

	// Textual order with the import inlined at its include; hledger's own order
	// inside the import (the CSV is newest first, hledger reverses it).
	var descriptions []string
	for _, sourced := range resolved.TransactionsWithSource() {
		descriptions = append(descriptions, sourced.Transaction.Description)
	}
	assert.Equal(t, []string{"opening", "first", "second", "after"}, descriptions)

	// The include-site alias applies to imported postings, `apply account`
	// does not, as in hledger 1.99.5.
	first := occurrence.Journal.Transactions[0]
	assert.Equal(t, []string{"assets:checking", "income"}, accountsOf(first))
	assert.Equal(t, "assets:bank", first.Postings[0].Account.Name)
	assert.Equal(t, "1.5", first.Postings[0].Amount.Quantity.String())
	assert.Equal(t, 3, first.Range.Start.Line, "first record read is on line 3")
	assert.Contains(t, resolved.ImportSources, data)
	assert.True(t, loader.IsImportSource(data))
	assert.True(t, loader.IsImportSource(data+".rules"))
	assert.False(t, loader.IsImportSource(root))
}

func TestImport_TargetOneKeepsRefusingCSVIncludes(t *testing.T) {
	dir := t.TempDir()
	data := writeFile(t, dir, "bank.csv", "date,amount\n")
	root := writeFile(t, dir, "main.journal", "include bank.csv\ninclude csv:other.journal\n")
	writeFile(t, dir, "other.journal", "2024-01-01 x\n    a  1\n    b\n")

	loader := NewLoader()
	_, errs := loader.Load(root)

	require.Len(t, errs, 2)
	assert.Equal(t, ErrorNotJournal, errs[0].Kind)
	assert.Equal(t, "included file is not a journal file: bank.csv", errs[0].Message)
	assert.Equal(t, ErrorFileNotFound, errs[1].Kind)
	// The files are data whatever the target, so they are still protected.
	assert.True(t, loader.IsImportSource(data))
	assert.True(t, loader.IsImportSource(filepath.Join(dir, "other.journal")))
}

func TestImport_FailedConversionIsAnErrorAtTheInclude(t *testing.T) {
	dir := t.TempDir()
	data := writeFile(t, dir, "data.txt", "date,amount\n")
	root := writeFile(t, dir, "main.journal", "2024-01-01 x\n    a  1\n    b\ninclude csv:data.txt\n")
	fake := &fakeImporter{result: csvimport.Result{Err: "hledger could not convert the file: boom", Sources: []string{data, data + ".rules"}, Warnings: []string{"data.txt has unsaved changes; the import reads the saved file"}}}

	loader := NewLoader()
	loader.SetImporter(fake, nil)
	resolved, errs := loader.Load(root)

	require.Len(t, fake.requests, 1)
	assert.Equal(t, csvimport.ReaderCSV, fake.requests[0].Reader, "the explicit prefix wins over the extension")
	assert.Equal(t, data, fake.requests[0].Path)
	require.Len(t, errs, 2)
	assert.Equal(t, ErrorImportUnsaved, errs[0].Kind)
	assert.Equal(t, ErrorImport, errs[1].Kind)
	assert.Equal(t, "cannot import data.txt: hledger could not convert the file: boom", errs[1].Message)
	assert.Equal(t, root, errs[1].SourcePath)
	assert.Equal(t, 4, errs[1].Range.Start.Line, "the include is on line 4")
	assert.Empty(t, resolved.OccurrencesForPath(data), "a failed import adds no occurrence")
	assert.Len(t, resolved.AllTransactions(), 1)
	assert.True(t, loader.IsImportSource(data))
}

func TestImport_HardLinkToAnImportSourceIsProtected(t *testing.T) {
	dir := t.TempDir()
	data := writeFile(t, dir, "bank.csv", "date,amount\n")
	root := writeFile(t, dir, "main.journal", "include bank.csv\n")
	link := filepath.Join(dir, "alias.journal")
	require.NoError(t, os.Link(data, link))
	symlink := filepath.Join(dir, "sym.journal")
	require.NoError(t, os.Symlink(data, symlink))

	loader := NewLoader()
	loader.SetImporter(&fakeImporter{result: csvimport.Result{Err: "x", Sources: []string{data}}}, nil)
	loader.Load(root)

	assert.True(t, loader.IsImportSource(link))
	assert.True(t, loader.IsImportSource(symlink))
	assert.False(t, loader.IsImportSource(root))
}

func TestImport_ParseOccurrenceRefusesImportedData(t *testing.T) {
	dir := t.TempDir()
	data := writeFile(t, dir, "bank.csv", "x\n")
	root := writeFile(t, dir, "main.journal", "include bank.csv\n")
	transaction := ast.Transaction{Date: ast.Date{Year: 2024, Month: 1, Day: 1}, Description: "d", Postings: []ast.Posting{{Account: ast.Account{Name: "a"}}}}

	loader := NewLoader()
	loader.SetImporter(&fakeImporter{result: csvimport.Result{DataPath: data, Transactions: []ast.Transaction{transaction}, Sources: []string{data}}}, nil)
	resolved, errs := loader.Load(root)
	require.Empty(t, errs)

	occurrences := resolved.OccurrencesForPath(data)
	require.Len(t, occurrences, 1)
	_, _, ok := resolved.ParseOccurrence(occurrences[0].ID, "2024-01-01 x\n    a  1\n    b\n")
	assert.False(t, ok, "imported data is not journal text")
}
