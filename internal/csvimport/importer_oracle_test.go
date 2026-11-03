package csvimport

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestPreviewOracle_ImportKeepsCostsAssertionsAndImplicitAmounts(t *testing.T) {
	binary := previewHledger(t)
	dir := t.TempDir()
	data := write(t, dir, "trades.csv", "date,desc,bal,qty,price,total\n2024-01-02,buy,100.00,10,1.5,\n2024-01-03,sell,,-2,,3.25\n")
	write(t, dir, "trades.csv.rules", `skip 1
fields date, description, bal, qty, price, total
account1 assets:bank
account2 assets:stock
account3 equity:x
if %description buy
 balance1 %bal USD
 amount2 %qty AAPL @ %price USD
if %description sell
 amount1 %total USD
 amount2 %qty AAPL @@ %total USD
`)

	result := importFile(t, binary, ReaderCSV, data)

	require.Empty(t, result.Err)
	require.Len(t, result.Transactions, 2)
	// A balance assignment stays an assignment: its amount depends on what
	// precedes the import, which the JSON output computed without.
	assert.Equal(t, []posting{
		{account: "assets:bank", assertion: "100 USD"},
		{account: "assets:stock", amount: "10 AAPL", cost: "@ 1.5 USD"},
		{account: "equity:x"},
	}, postingsOf(result.Transactions[0]))
	assert.Equal(t, int32(-2), result.Transactions[0].Postings[0].BalanceAssertion.Amount.Quantity.Exponent())
	assert.Equal(t, []posting{
		{account: "assets:bank", amount: "3.25 USD"},
		{account: "assets:stock", amount: "-2 AAPL", cost: "@@ 3.25 USD"},
		{account: "equity:x"},
	}, postingsOf(result.Transactions[1]))
}

func TestPreviewOracle_ImportReadsQuotingEncodingAndLineEndings(t *testing.T) {
	binary := previewHledger(t)
	dir := t.TempDir()
	data := write(t, dir, "m.csv", "\ufeffdate,desc,amount\r\n2024-01-02,\"multi\r\nline \"\"q\"\"\",1E3\r\n2024-01-03,日本語 Ёж,-2.5e-1\r\n")
	write(t, dir, "m.csv.rules", "skip 1\nfields date, description, amount\ncurrency EUR\naccount1 расходы:еда\naccount2 b\n")

	result := importFile(t, binary, ReaderCSV, data)

	require.Empty(t, result.Err)
	require.Len(t, result.Transactions, 2)
	assert.Equal(t, `multi line "q"`, result.Transactions[0].Description)
	assert.Equal(t, []posting{{account: "расходы:еда", amount: "1000 EUR"}, {account: "b", amount: "-1000 EUR"}}, postingsOf(result.Transactions[0]))
	assert.Equal(t, "日本語 Ёж", result.Transactions[1].Description)
	assert.Equal(t, []posting{{account: "расходы:еда", amount: "-0.25 EUR"}, {account: "b", amount: "0.25 EUR"}}, postingsOf(result.Transactions[1]))
	// The quoted record spans lines 2 and 3; offsets are in the LF-normalized file.
	first := result.Transactions[0].Range
	assert.Equal(t, 2, first.Start.Line)
	assert.Equal(t, 3, first.End.Line)
	normalized := "\ufeffdate,desc,amount\n2024-01-02,\"multi\nline \"\"q\"\"\",1E3\n"
	assert.Equal(t, len("\ufeffdate,desc,amount\n"), first.Start.Offset)
	assert.Equal(t, len(normalized)-1, first.End.Offset)
	assert.Equal(t, 4, result.Transactions[1].Range.Start.Line)
}

func TestPreviewOracle_ImportHonoursReaderPrefixSeparatorsAndMerge(t *testing.T) {
	binary := previewHledger(t)
	dir := t.TempDir()
	ssv := write(t, dir, "semi.dat", "date;desc;amount\n2024-01-02;a;1,5\n")
	write(t, dir, "semi.dat.rules", "skip 1\nfields date, description, amount\ndecimal-mark ,\ncurrency EUR \naccount1 a\naccount2 b\n")
	tsv := write(t, dir, "tab.tsv", "date\tdesc\tamount\n2024-01-02\tt\t7\n")
	write(t, dir, "tab.tsv.rules", "skip 1\nfields date, description, amount\naccount1 a\naccount2 b\n")
	merged := write(t, dir, "conv.csv", "2026-09-01,PAYMENT,coffee,-3.50,USD\n2026-09-02,CONVERT,converted,-100.00,USD\n2026-09-02,CONVERT,converted,90.00,EUR\n")
	write(t, dir, "conv.csv.rules", `fields date, type, desc, amt, cur
description %desc
account1 assets:bank:%cur
amount1  %amt %cur
if %type CONVERT
 merge
 account2 assets:bank:%cur_2
 amount2  %amt_2 %cur_2
if %type PAYMENT
 account2 expenses:unknown
`)

	ssvResult := importFile(t, binary, ReaderSSV, ssv)
	require.Empty(t, ssvResult.Err)
	require.Len(t, ssvResult.Transactions, 1)
	assert.Equal(t, []posting{{account: "a", amount: "1.5 EUR"}, {account: "b", amount: "-1.5 EUR"}}, postingsOf(ssvResult.Transactions[0]))

	tsvResult := importFile(t, binary, ReaderTSV, tsv)
	require.Empty(t, tsvResult.Err)
	require.Len(t, tsvResult.Transactions, 1)
	assert.Equal(t, []posting{{account: "a", amount: "7 "}, {account: "b", amount: "-7 "}}, postingsOf(tsvResult.Transactions[0]))

	mergeResult := importFile(t, binary, ReaderCSV, merged)
	require.Empty(t, mergeResult.Err)
	require.Len(t, mergeResult.Transactions, 2, "two rows merge into one transaction")
	assert.Equal(t, []posting{{account: "assets:bank:USD", amount: "-100 USD"}, {account: "assets:bank:EUR", amount: "90 EUR"}}, postingsOf(mergeResult.Transactions[1]))
	assert.Equal(t, 2, mergeResult.Transactions[1].Range.Start.Line)
	assert.Equal(t, 3, mergeResult.Transactions[1].Range.End.Line)
}

func TestPreviewOracle_ImportFollowsRulesIncludesAndSourceRules(t *testing.T) {
	binary := previewHledger(t)
	dir := t.TempDir()
	write(t, dir, "rules/common.rules", "skip 1\nfields date, description, amount\naccount1 assets:bank\naccount2 expenses:misc\n")
	rules := write(t, dir, "rules/bank.rules", "source ../downloads/bank-*.csv\ninclude common.rules\n")
	older := write(t, dir, "downloads/bank-1.csv", "date,desc,amount\n2024-01-01,old,1\n")
	newer := write(t, dir, "downloads/bank-2.csv", "date,desc,amount\n2024-02-01,new,2\n2024-02-02,newer,3\n")
	past := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(older, past, past))
	before := listing(t, dir)

	result := importFile(t, binary, ReaderRules, rules)

	require.Empty(t, result.Err)
	assert.Equal(t, newer, result.DataPath, "hledger reads the newest match")
	require.Len(t, result.Transactions, 2)
	assert.Equal(t, "new", result.Transactions[0].Description)
	assert.ElementsMatch(t, []string{rules, filepath.Join(dir, "rules/common.rules"), filepath.Join(dir, "downloads/bank-*.csv"), older, newer}, result.Sources)
	assert.Equal(t, before, listing(t, dir))

	// A change to an included rules file changes the conversion.
	write(t, dir, "rules/common.rules", "skip 1\nfields date, description, amount\naccount1 assets:bank\naccount2 expenses:food\n")
	changed := importFile(t, binary, ReaderRules, rules)
	require.Empty(t, changed.Err)
	assert.Equal(t, "expenses:food", changed.Transactions[0].Postings[1].Account.Name)
}

func TestPreviewOracle_ImportRefusesWhatItCannotRunSafely(t *testing.T) {
	binary := previewHledger(t)
	cases := []struct {
		name  string
		rules string
		data  string
		want  string
	}{
		{"source command", "source | echo x\nfields date, amount\n", "", "a source rule that runs a command is not run by the language server"},
		{"cleaning command", "source ./x.csv | sort\nfields date, amount\n", "", "a source rule that runs a command"},
		{"bare source name", "source bank.csv\nfields date, amount\n", "", `source "bank.csv" is searched for in data/ and ~/Downloads`},
		{"home source", "source ~/bank.csv\nfields date, amount\n", "", "is searched for in data/ and ~/Downloads"},
		{"no source", "fields date, amount\n", "", "r.rules has no source rule naming its data file"},
		{"huge exponent", "source ./x.csv\nfields date, amount\n", "date,amount\n2024-01-01,1E100\n", "a number with an exponent above 99 is not imported"},
		{"long digits", "source ./x.csv\nfields date, amount\n", "date,amount\n2024-01-01," + strings.Repeat("9", 300) + "\n", "a number longer than 255 digits is not imported"},
		{"rules include cycle", "source ./x.csv\ninclude r.rules\n", "", "rules file r.rules includes itself"},
		{"missing rules include", "source ./x.csv\ninclude nope.rules\n", "", "rules file nope.rules not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			rules := write(t, dir, "r.rules", tc.rules)
			if tc.data != "" {
				write(t, dir, "x.csv", tc.data)
			}
			before := listing(t, dir)
			result := importFile(t, binary, ReaderRules, rules)
			assert.Contains(t, result.Err, tc.want)
			assert.Empty(t, result.Transactions)
			assert.Contains(t, result.Sources, rules)
			assert.Equal(t, before, listing(t, dir))
		})
	}
}

func TestPreviewOracle_ImportReportsHledgerErrors(t *testing.T) {
	binary := previewHledger(t)
	dir := t.TempDir()
	data := write(t, dir, "bad.csv", "date,amount\nnot-a-date,1\n")
	write(t, dir, "bad.csv.rules", "skip 1\nfields date, amount\naccount1 a\naccount2 b\n")
	missingRules := write(t, dir, "plain.csv", "date,amount\n")

	result := importFile(t, binary, ReaderCSV, data)
	assert.Contains(t, result.Err, "hledger could not convert the file:")
	assert.Contains(t, result.Err, "not-a-date")
	assert.NotContains(t, result.Err, "hledger: Error:")

	noRules := importFile(t, binary, ReaderCSV, missingRules)
	assert.Equal(t, "rules file plain.csv.rules not found", noRules.Err)
	assert.Equal(t, []string{missingRules, missingRules + ".rules"}, noRules.Sources)
}

func TestPreviewOracle_ImportCachesByContentAndNoticesChanges(t *testing.T) {
	binary := previewHledger(t)
	dir := t.TempDir()
	// A wrapper that logs each run and then executes the real binary unchanged.
	runs := filepath.Join(dir, "runs.log")
	wrapper := write(t, dir, "bin/hledger", "#!/bin/sh\necho \"$*\" >> "+runs+"\nexec "+binary+" \"$@\"\n")
	require.NoError(t, os.Chmod(wrapper, 0o700))
	data := write(t, dir, "in/bank.csv", "date,amount\n2024-01-02,1\n")
	write(t, dir, "in/bank.csv.rules", "skip 1\nfields date, amount\naccount1 a\naccount2 b\n")
	importer := New(Config{Enabled: true, Path: wrapper, Timeout: 30 * time.Second})
	request := Request{Reader: ReaderCSV, Path: data, MaxFileSize: 1 << 20}
	countRuns := func() int {
		content, err := os.ReadFile(runs)
		if os.IsNotExist(err) {
			return 0
		}
		require.NoError(t, err)
		return strings.Count(string(content), "print")
	}

	first := importer.Import(context.Background(), request)
	require.Empty(t, first.Err)
	require.Equal(t, 2, countRuns(), "one JSON and one text run")

	again := importer.Import(context.Background(), request)
	require.Empty(t, again.Err)
	assert.Equal(t, 2, countRuns(), "unchanged inputs are served from the cache")

	write(t, dir, "in/bank.csv", "date,amount\n2024-01-02,1\n2024-01-03,2.25\n")
	changed := importer.Import(context.Background(), request)
	require.Empty(t, changed.Err)
	assert.Equal(t, 4, countRuns())
	require.Len(t, changed.Transactions, 2)
	assert.Equal(t, "2.25", changed.Transactions[1].Postings[0].Amount.Quantity.String())

	// A different size limit is a different request: the cached result must
	// not bypass the check.
	small := importer.Import(context.Background(), Request{Reader: ReaderCSV, Path: data, MaxFileSize: 8})
	assert.Equal(t, "data file bank.csv is too large: 41 bytes (max 8)", small.Err)
}
