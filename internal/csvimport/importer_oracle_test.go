package csvimport

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/ast"
)

func importFile(t *testing.T, binary string, reader Reader, path string) Result {
	t.Helper()
	importer := New(Config{Enabled: true, Path: binary, Timeout: 30 * time.Second})
	return importer.Import(context.Background(), Request{Reader: reader, Path: path, MaxFileSize: 10 << 20})
}

type posting struct {
	account   string
	amount    string // "" when the amount is implicit
	cost      string
	assertion string
}

func postingsOf(transaction ast.Transaction) []posting {
	var result []posting
	for _, p := range transaction.Postings {
		got := posting{account: p.Account.Name}
		if p.Amount != nil {
			got.amount = p.Amount.Quantity.String() + " " + p.Amount.Commodity.Symbol
		}
		if p.Cost != nil {
			kind := "@"
			if p.Cost.IsTotal {
				kind = "@@"
			}
			got.cost = kind + " " + p.Cost.Amount.Quantity.String() + " " + p.Cost.Amount.Commodity.Symbol
		}
		if p.BalanceAssertion != nil {
			got.assertion = p.BalanceAssertion.Amount.Quantity.String() + " " + p.BalanceAssertion.Amount.Commodity.Symbol
		}
		result = append(result, got)
	}
	return result
}

func TestPreviewOracle_ImportConvertsCSVRecordsIntoTransactions(t *testing.T) {
	binary := previewHledger(t)
	dir := t.TempDir()
	data := write(t, dir, "bank.csv", "date,desc,amount\n2024-01-02,Кофе ☕,-3.505\n2024-01-03,salary,1000.10\n")
	write(t, dir, "bank.csv.rules", "skip 1\nfields date, description, amount\ncurrency $\naccount1 assets:bank\naccount2 income:other\n")
	before := listing(t, dir)

	result := importFile(t, binary, ReaderCSV, data)

	require.Empty(t, result.Err)
	assert.Equal(t, data, result.DataPath)
	require.Len(t, result.Transactions, 2)
	first, second := result.Transactions[0], result.Transactions[1]
	assert.Equal(t, "Кофе ☕", first.Description)
	assert.Equal(t, ast.Date{Year: 2024, Month: 1, Day: 2}, ast.Date{Year: first.Date.Year, Month: first.Date.Month, Day: first.Date.Day})
	assert.Equal(t, []posting{{account: "assets:bank", amount: "-3.505 $"}, {account: "income:other", amount: "3.505 $"}}, postingsOf(first))
	assert.Equal(t, []posting{{account: "assets:bank", amount: "1000.1 $"}, {account: "income:other", amount: "-1000.1 $"}}, postingsOf(second))
	assert.Equal(t, "1000.10", second.Postings[0].Amount.Quantity.StringFixed(2))
	assert.Equal(t, int32(-2), second.Postings[0].Amount.Quantity.Exponent(), "the decimal places hledger reports are kept")

	// Provenance: each transaction covers its record's line in the data file.
	assert.Equal(t, 2, first.Range.Start.Line)
	assert.Equal(t, len("date,desc,amount\n"), first.Range.Start.Offset)
	assert.Equal(t, 3, second.Range.Start.Line)
	assert.Equal(t, 3, second.Range.End.Line)
	assert.ElementsMatch(t, []string{data, data + ".rules"}, result.Sources)
	assert.Equal(t, before, listing(t, dir), "hledger must not write beside the inputs")
}

// hledger 1.52.4 converts a CSV file given with -f, but refuses to include one
// from a journal, so the server does not import with it either.
func TestStableOracle_ImportRefusesHledgerThatCannotIncludeCSV(t *testing.T) {
	binary := stableHledger(t)
	dir := t.TempDir()
	data := write(t, dir, "bank.csv", "date,amount\n2024-01-02,1\n")
	write(t, dir, "bank.csv.rules", "skip 1\nfields date, amount\naccount1 a\naccount2 b\n")

	result := importFile(t, binary, ReaderCSV, data)

	assert.Regexp(t, `^hledger 1\.\d+\.\d+ cannot include CSV files; hledger 1\.99\.5 or newer is needed$`, result.Err)
	assert.Empty(t, result.Transactions)
	assert.ElementsMatch(t, []string{data, data + ".rules"}, result.Sources)
}
