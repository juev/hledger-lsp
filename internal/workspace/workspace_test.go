package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/parser"
)

func TestWorkspace_FindRootJournal_MainJournal(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	err := os.WriteFile(mainPath, []byte(""), 0644)
	require.NoError(t, err)

	otherPath := filepath.Join(tmpDir, "other.journal")
	err = os.WriteFile(otherPath, []byte(""), 0644)
	require.NoError(t, err)

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)

	err = ws.Initialize()
	require.NoError(t, err)

	assert.Equal(t, mainPath, ws.RootJournalPath())
}

func TestWorkspace_FindRootJournal_EnvVarIgnored(t *testing.T) {
	// Workspace mode ignores env vars entirely — they may point to
	// a completely unrelated journal outside the workspace.
	tmpDir := t.TempDir()
	externalDir := t.TempDir()

	externalJournal := filepath.Join(externalDir, "real.journal")
	err := os.WriteFile(externalJournal, []byte("2024-01-01 Real\n    expenses:food  $10\n    assets:cash\n"), 0644)
	require.NoError(t, err)

	mainPath := filepath.Join(tmpDir, "main.journal")
	err = os.WriteFile(mainPath, []byte("2024-01-01 Test\n    expenses:test  $5\n    assets:bank\n"), 0644)
	require.NoError(t, err)

	t.Setenv("LEDGER_FILE", externalJournal)

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)

	err = ws.Initialize()
	require.NoError(t, err)

	assert.Equal(t, mainPath, ws.RootJournalPath(),
		"workspace should use local main.journal, not LEDGER_FILE")

	resolved := ws.GetResolved()
	require.NotNil(t, resolved)
	allTx := resolved.AllTransactions()
	require.Len(t, allTx, 1)
	assert.Equal(t, "Test", allTx[0].Description)
}

func TestWorkspace_FindRootJournal_EnvVarIgnoredNoLocal(t *testing.T) {
	// Even when workspace has no main.journal, env vars pointing outside
	// should be ignored. Root detection falls through to include graph.
	tmpDir := t.TempDir()
	externalDir := t.TempDir()

	externalJournal := filepath.Join(externalDir, "real.journal")
	err := os.WriteFile(externalJournal, []byte(""), 0644)
	require.NoError(t, err)

	// Only a non-standard journal in the workspace
	rootPath := filepath.Join(tmpDir, "ledger.journal")
	err = os.WriteFile(rootPath, []byte("2024-01-01 Local\n    expenses:food  $10\n    assets:cash\n"), 0644)
	require.NoError(t, err)

	t.Setenv("LEDGER_FILE", externalJournal)

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)

	err = ws.Initialize()
	require.NoError(t, err)

	// Should find ledger.journal via include graph scan, not external env var
	assert.Equal(t, rootPath, ws.RootJournalPath(),
		"workspace should find local journal via include graph, not use external LEDGER_FILE")
}

func TestWorkspace_FindRootJournal_NotIncludedFile(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	rootPath := filepath.Join(tmpDir, "root.journal")
	err := os.WriteFile(rootPath, []byte(`include child.journal`), 0644)
	require.NoError(t, err)

	childPath := filepath.Join(tmpDir, "child.journal")
	err = os.WriteFile(childPath, []byte(""), 0644)
	require.NoError(t, err)

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)

	err = ws.Initialize()
	require.NoError(t, err)

	assert.Equal(t, rootPath, ws.RootJournalPath())
}

func TestWorkspace_GetCommodityFormats(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	mainContent := `commodity RUB
  format 1.000,00 RUB

include transactions.journal`
	err := os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	txPath := filepath.Join(tmpDir, "transactions.journal")
	err = os.WriteFile(txPath, []byte(""), 0644)
	require.NoError(t, err)

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)

	err = ws.Initialize()
	require.NoError(t, err)

	formats := ws.GetCommodityFormats()
	require.NotNil(t, formats)

	rubFormat, ok := formats["RUB"]
	assert.True(t, ok, "RUB format should exist")
	assert.Equal(t, ',', rubFormat.DecimalMark)
	assert.Equal(t, ".", rubFormat.ThousandsSep)
	assert.Equal(t, 2, rubFormat.DecimalPlaces)
}

func TestWorkspace_GetCommodityFormats_FromSiblingInclude(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	mainContent := `include common.journal
include 2025.journal`
	err := os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	commonPath := filepath.Join(tmpDir, "common.journal")
	commonContent := `commodity RUB
  format 1.000,00 RUB

commodity EUR
  format 1 000,00 EUR`
	err = os.WriteFile(commonPath, []byte(commonContent), 0644)
	require.NoError(t, err)

	txPath := filepath.Join(tmpDir, "2025.journal")
	err = os.WriteFile(txPath, []byte(""), 0644)
	require.NoError(t, err)

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)

	err = ws.Initialize()
	require.NoError(t, err)

	formats := ws.GetCommodityFormats()
	require.NotNil(t, formats)

	rubFormat, ok := formats["RUB"]
	assert.True(t, ok, "RUB format should exist from sibling include")
	assert.Equal(t, ',', rubFormat.DecimalMark)
	assert.Equal(t, ".", rubFormat.ThousandsSep)

	eurFormat, ok := formats["EUR"]
	assert.True(t, ok, "EUR format should exist from sibling include")
	assert.Equal(t, ',', eurFormat.DecimalMark)
	assert.Equal(t, " ", eurFormat.ThousandsSep)
}

func TestWorkspace_GetDeclaredCommodities(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	mainContent := `commodity RUB
  format 1.000,00 RUB

commodity EUR

include transactions.journal`
	err := os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	txPath := filepath.Join(tmpDir, "transactions.journal")
	err = os.WriteFile(txPath, []byte(""), 0644)
	require.NoError(t, err)

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)

	err = ws.Initialize()
	require.NoError(t, err)

	declared := ws.GetDeclaredCommodities()
	require.NotNil(t, declared)

	assert.True(t, declared["RUB"], "RUB should be declared")
	assert.True(t, declared["EUR"], "EUR should be declared")
	assert.False(t, declared["USD"], "USD should not be declared")
}

func TestWorkspace_GetDeclaredCommodities_NilResolved(t *testing.T) {
	loader := include.NewLoader()
	ws := NewWorkspace("/nonexistent", loader)

	declared := ws.GetDeclaredCommodities()
	assert.Nil(t, declared)
}

func TestWorkspace_GetDeclaredAccounts(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	mainContent := `account expenses:food
account assets:cash

include transactions.journal`
	err := os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	txPath := filepath.Join(tmpDir, "transactions.journal")
	err = os.WriteFile(txPath, []byte(""), 0644)
	require.NoError(t, err)

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)

	err = ws.Initialize()
	require.NoError(t, err)

	declared := ws.GetDeclaredAccounts()
	require.NotNil(t, declared)

	assert.True(t, declared["expenses:food"], "expenses:food should be declared")
	assert.True(t, declared["assets:cash"], "assets:cash should be declared")
	assert.False(t, declared["liabilities:card"], "liabilities:card should not be declared")
}

func TestWorkspace_GetDeclaredAccounts_NilResolved(t *testing.T) {
	loader := include.NewLoader()
	ws := NewWorkspace("/nonexistent", loader)

	declared := ws.GetDeclaredAccounts()
	assert.Nil(t, declared)
}

func TestWorkspace_IndexSnapshot_FromIncludes(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	mainContent := `include a.journal
include sub/b.journal

2024-02-01 Main Payee
    assets:cash  $10
    income:salary`
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "sub"), 0755))
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0644))

	aPath := filepath.Join(tmpDir, "a.journal")
	aContent := `account expenses:food

2024-02-02 Grocery
    expenses:food  $5
    assets:cash`
	require.NoError(t, os.WriteFile(aPath, []byte(aContent), 0644))

	bPath := filepath.Join(tmpDir, "sub", "b.journal")
	bContent := `commodity EUR

2024-02-03 Cafe
    expenses:food  EUR 3
    assets:cash`
	require.NoError(t, os.WriteFile(bPath, []byte(bContent), 0644))

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)

	require.NoError(t, ws.Initialize())

	snapshot := ws.IndexSnapshot()
	require.NotNil(t, snapshot.Accounts)
	assert.Contains(t, snapshot.Accounts.All, "assets:cash")
	assert.Contains(t, snapshot.Accounts.All, "expenses:food")
	assert.Contains(t, snapshot.Payees, "Grocery")
	assert.Contains(t, snapshot.Payees, "Cafe")
	assert.Contains(t, snapshot.Commodities, "$")
	assert.Contains(t, snapshot.Commodities, "EUR")
}

func TestWorkspace_IndexSnapshot_IncrementalUpdate(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	mainContent := `include child.journal

2024-02-01 Root
    assets:wallet  $10
    income:salary`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0644))

	childPath := filepath.Join(tmpDir, "child.journal")
	childContent := `2024-02-02 Lunch
    expenses:food  $5
    assets:cash`
	require.NoError(t, os.WriteFile(childPath, []byte(childContent), 0644))

	ws := NewWorkspace(tmpDir, include.NewLoader())
	require.NoError(t, ws.Initialize())

	updatedContent := `2024-02-02 Lunch
    expenses:food  $5
    assets:bank`
	ws.UpdateFile(childPath, updatedContent)

	snapshot := ws.IndexSnapshot()
	assert.Contains(t, snapshot.Accounts.All, "assets:bank")
	assert.NotContains(t, snapshot.Accounts.All, "assets:cash")
	assert.Contains(t, snapshot.Accounts.All, "income:salary")
}

func TestWorkspace_IndexSnapshot_IncludeChange(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	mainContent := `include one.journal`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0644))

	onePath := filepath.Join(tmpDir, "one.journal")
	oneContent := `2024-02-02 One
    expenses:food  $5
    assets:cash`
	require.NoError(t, os.WriteFile(onePath, []byte(oneContent), 0644))

	twoPath := filepath.Join(tmpDir, "two.journal")
	twoContent := `2024-02-03 Two
    expenses:travel  $20
    assets:cash`
	require.NoError(t, os.WriteFile(twoPath, []byte(twoContent), 0644))

	ws := NewWorkspace(tmpDir, include.NewLoader())
	require.NoError(t, ws.Initialize())

	snapshot := ws.IndexSnapshot()
	assert.Contains(t, snapshot.Accounts.All, "expenses:food")
	// In multi-tree mode, two.journal is a standalone tree, so its accounts are visible
	assert.Contains(t, snapshot.Accounts.All, "expenses:travel")

	ws.UpdateFile(mainPath, "include one.journal\ninclude two.journal")
	snapshot = ws.IndexSnapshot()
	assert.Contains(t, snapshot.Accounts.All, "expenses:travel")

	ws.UpdateFile(mainPath, "include two.journal")
	snapshot = ws.IndexSnapshot()
	// one.journal is no longer included by main and has no standalone tree
	assert.NotContains(t, snapshot.Accounts.All, "expenses:food")
}

func TestWorkspace_TransactionIndexKeys(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()
	mainPath := filepath.Join(tmpDir, "main.journal")
	content := `2024-03-01 Coffee Shop
    expenses:food  $3
    assets:cash
`
	require.NoError(t, os.WriteFile(mainPath, []byte(content), 0644))

	ws := NewWorkspace(tmpDir, include.NewLoader())
	require.NoError(t, ws.Initialize())

	journal, errs := parser.Parse(content)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	key := buildTransactionKey(journal.Transactions[0])
	snapshot := ws.IndexSnapshot()
	entries := snapshot.Transactions[key]
	require.Len(t, entries, 1)
	assert.Equal(t, mainPath, entries[0].FilePath)
}

func TestWorkspace_IndexSnapshot_TagValues_SingleFile(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()
	mainPath := filepath.Join(tmpDir, "main.journal")
	content := `2024-03-01 Coffee Shop  ; project:alpha, status:active
    expenses:food  $3  ; category:coffee
    assets:cash
`
	require.NoError(t, os.WriteFile(mainPath, []byte(content), 0644))

	ws := NewWorkspace(tmpDir, include.NewLoader())
	require.NoError(t, ws.Initialize())

	snapshot := ws.IndexSnapshot()
	require.NotNil(t, snapshot.TagValues)

	assert.Contains(t, snapshot.TagValues["project"], "alpha")
	assert.Contains(t, snapshot.TagValues["status"], "active")
	assert.Contains(t, snapshot.TagValues["category"], "coffee")
}

func TestWorkspace_IndexSnapshot_TagValues_MultipleFiles(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	mainContent := `include child.journal

2024-03-01 Main  ; project:alpha
    expenses:food  $10
    assets:cash
`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0644))

	childPath := filepath.Join(tmpDir, "child.journal")
	childContent := `2024-03-02 Child  ; project:beta
    expenses:rent  $100
    assets:bank
`
	require.NoError(t, os.WriteFile(childPath, []byte(childContent), 0644))

	ws := NewWorkspace(tmpDir, include.NewLoader())
	require.NoError(t, ws.Initialize())

	snapshot := ws.IndexSnapshot()
	require.NotNil(t, snapshot.TagValues)

	assert.Contains(t, snapshot.TagValues["project"], "alpha")
	assert.Contains(t, snapshot.TagValues["project"], "beta")
}

func TestWorkspace_IndexSnapshot_TagValues_UpdateOnFileChange(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	content := `2024-03-01 Test  ; project:alpha
    expenses:food  $10
    assets:cash
`
	require.NoError(t, os.WriteFile(mainPath, []byte(content), 0644))

	ws := NewWorkspace(tmpDir, include.NewLoader())
	require.NoError(t, ws.Initialize())

	snapshot := ws.IndexSnapshot()
	assert.Contains(t, snapshot.TagValues["project"], "alpha")

	updatedContent := `2024-03-01 Test  ; project:beta
    expenses:food  $10
    assets:cash
`
	ws.UpdateFile(mainPath, updatedContent)

	snapshot = ws.IndexSnapshot()
	assert.Contains(t, snapshot.TagValues["project"], "beta")
	assert.NotContains(t, snapshot.TagValues["project"], "alpha")
}

func TestWorkspace_IndexSnapshot_FrequencyCounts(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	mainContent := `include child.journal

2024-03-01 Grocery Store  ; project:alpha
    expenses:food  $10
    assets:cash

2024-03-02 Grocery Store  ; project:alpha
    expenses:food  $20
    assets:cash

2024-03-03 Coffee Shop  ; project:beta
    expenses:food  $5
    assets:bank
`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0644))

	childPath := filepath.Join(tmpDir, "child.journal")
	childContent := `2024-03-04 Restaurant  ; project:alpha
    expenses:food  EUR 30
    assets:cash
`
	require.NoError(t, os.WriteFile(childPath, []byte(childContent), 0644))

	ws := NewWorkspace(tmpDir, include.NewLoader())
	require.NoError(t, ws.Initialize())

	snapshot := ws.IndexSnapshot()

	require.NotNil(t, snapshot.AccountCounts)
	assert.Equal(t, 4, snapshot.AccountCounts["expenses:food"])
	assert.Equal(t, 3, snapshot.AccountCounts["assets:cash"])
	assert.Equal(t, 1, snapshot.AccountCounts["assets:bank"])

	require.NotNil(t, snapshot.PayeeCounts)
	assert.Equal(t, 2, snapshot.PayeeCounts["Grocery Store"])
	assert.Equal(t, 1, snapshot.PayeeCounts["Coffee Shop"])
	assert.Equal(t, 1, snapshot.PayeeCounts["Restaurant"])

	require.NotNil(t, snapshot.CommodityCounts)
	assert.Equal(t, 3, snapshot.CommodityCounts["$"])
	assert.Equal(t, 1, snapshot.CommodityCounts["EUR"])

	require.NotNil(t, snapshot.TagCounts)
	assert.Equal(t, 4, snapshot.TagCounts["project"])

	require.NotNil(t, snapshot.TagValueCounts)
	assert.Equal(t, 3, snapshot.TagValueCounts["project"]["alpha"])
	assert.Equal(t, 1, snapshot.TagValueCounts["project"]["beta"])
}

func TestWorkspace_IndexSnapshot_FrequencyCounts_IncrementalUpdate(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	content := `2024-03-01 Shop
    expenses:food  $10
    assets:cash

2024-03-02 Shop
    expenses:food  $20
    assets:cash
`
	require.NoError(t, os.WriteFile(mainPath, []byte(content), 0644))

	ws := NewWorkspace(tmpDir, include.NewLoader())
	require.NoError(t, ws.Initialize())

	snapshot := ws.IndexSnapshot()
	assert.Equal(t, 2, snapshot.PayeeCounts["Shop"])
	assert.Equal(t, 2, snapshot.AccountCounts["expenses:food"])

	updatedContent := `2024-03-01 Cafe
    expenses:drinks  $10
    assets:cash
`
	ws.UpdateFile(mainPath, updatedContent)

	snapshot = ws.IndexSnapshot()
	assert.Equal(t, 0, snapshot.PayeeCounts["Shop"])
	assert.Equal(t, 1, snapshot.PayeeCounts["Cafe"])
	assert.Equal(t, 0, snapshot.AccountCounts["expenses:food"])
	assert.Equal(t, 1, snapshot.AccountCounts["expenses:drinks"])
}

func TestWorkspace_IndexSnapshot_Dates(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	mainContent := `include child.journal

2024-03-01 Shop
    expenses:food  $10
    assets:cash

2024-03-01 Cafe
    expenses:drinks  $5
    assets:cash

2024-03-15 Restaurant
    expenses:food  $30
    assets:cash
`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0644))

	childPath := filepath.Join(tmpDir, "child.journal")
	childContent := `2024-02-20 Market
    expenses:food  $20
    assets:cash
`
	require.NoError(t, os.WriteFile(childPath, []byte(childContent), 0644))

	ws := NewWorkspace(tmpDir, include.NewLoader())
	require.NoError(t, ws.Initialize())

	snapshot := ws.IndexSnapshot()

	require.NotNil(t, snapshot.Dates)
	assert.Contains(t, snapshot.Dates, "2024-03-01")
	assert.Contains(t, snapshot.Dates, "2024-03-15")
	assert.Contains(t, snapshot.Dates, "2024-02-20")
}

func TestWorkspace_IndexSnapshot_PayeeTemplates(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	content := `2024-03-01 Grocery Store
    expenses:food  $50.00
    assets:cash

2024-03-02 Coffee Shop
    expenses:drinks  EUR 5.50
    assets:bank
`
	require.NoError(t, os.WriteFile(mainPath, []byte(content), 0644))

	ws := NewWorkspace(tmpDir, include.NewLoader())
	require.NoError(t, ws.Initialize())

	snapshot := ws.IndexSnapshot()

	require.NotNil(t, snapshot.PayeeTemplates)
	require.Contains(t, snapshot.PayeeTemplates, "Grocery Store")
	require.Contains(t, snapshot.PayeeTemplates, "Coffee Shop")

	groceryPostings := snapshot.PayeeTemplates["Grocery Store"]
	require.Len(t, groceryPostings, 2)
	assert.Equal(t, "expenses:food", groceryPostings[0].Account)
	assert.Equal(t, "assets:cash", groceryPostings[1].Account)
}

func TestWorkspace_GetIncludedBy(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	midPath := filepath.Join(tmpDir, "mid.journal")
	subPath := filepath.Join(tmpDir, "sub.journal")

	mainContent := "include mid.journal\n"
	midContent := "include sub.journal\n"
	subContent := "2024-01-01 test\n    expenses:food  $10\n    assets:cash\n"

	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0644))
	require.NoError(t, os.WriteFile(midPath, []byte(midContent), 0644))
	require.NoError(t, os.WriteFile(subPath, []byte(subContent), 0644))

	ws := NewWorkspace(tmpDir, include.NewLoader())
	require.NoError(t, ws.Initialize())

	parents := ws.GetIncludedBy(subPath)
	assert.Contains(t, parents, midPath)
	assert.Contains(t, parents, mainPath)

	parents2 := ws.GetIncludedBy(midPath)
	assert.Contains(t, parents2, mainPath)
	assert.NotContains(t, parents2, subPath)

	parents3 := ws.GetIncludedBy(mainPath)
	assert.Empty(t, parents3)
}

func TestWorkspace_GetCommodityFormats_WithDefaultDirective(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	mainContent := `commodity RUB
  format 1.000,00 RUB

D 1.000,00 RUB

include transactions.journal`
	err := os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	txPath := filepath.Join(tmpDir, "transactions.journal")
	err = os.WriteFile(txPath, []byte(""), 0644)
	require.NoError(t, err)

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)

	err = ws.Initialize()
	require.NoError(t, err)

	formats := ws.GetCommodityFormats()
	require.NotNil(t, formats)

	// Check RUB format from commodity directive
	rubFormat, ok := formats["RUB"]
	assert.True(t, ok, "RUB format should exist")
	assert.Equal(t, ',', rubFormat.DecimalMark)
	assert.Equal(t, ".", rubFormat.ThousandsSep)

	// Check default format from D directive
	defaultFormat, ok := formats[""]
	assert.True(t, ok, "default format should exist from D directive")
	assert.Equal(t, ',', defaultFormat.DecimalMark)
	assert.Equal(t, ".", defaultFormat.ThousandsSep)
	assert.Equal(t, 2, defaultFormat.DecimalPlaces)
}

func TestWorkspace_GetCommodityFormats_DecimalMarkScopeCurrentFileOnly(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	mainContent := `include accounts.journal`
	err := os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	accountsPath := filepath.Join(tmpDir, "accounts.journal")
	accountsContent := `decimal-mark ,

account expenses:food
account assets:cash`
	err = os.WriteFile(accountsPath, []byte(accountsContent), 0644)
	require.NoError(t, err)

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)

	err = ws.Initialize()
	require.NoError(t, err)

	formats := ws.GetCommodityFormats()

	_, hasDefault := formats[""]
	assert.False(t, hasDefault,
		"decimal-mark from included file should not leak into workspace formats (scope: current file only)")
}

func TestWorkspace_GetCommodityFormats_DecimalMarkFromPrimaryFile(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	mainContent := `decimal-mark ,

include transactions.journal`
	err := os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	txPath := filepath.Join(tmpDir, "transactions.journal")
	err = os.WriteFile(txPath, []byte(""), 0644)
	require.NoError(t, err)

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)

	err = ws.Initialize()
	require.NoError(t, err)

	formats := ws.GetCommodityFormats()
	require.NotNil(t, formats)

	defaultFormat, ok := formats[""]
	assert.True(t, ok,
		"decimal-mark from primary file should be available in workspace formats")
	assert.Equal(t, ',', defaultFormat.DecimalMark)
	assert.Equal(t, ".", defaultFormat.ThousandsSep)
	assert.Equal(t, 0, defaultFormat.DecimalPlaces)
}

func TestWorkspace_GetCommodityFormats_IncludedDDirectivePreservedWithDecimalMark(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	mainContent := `include common.journal`
	err := os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	commonPath := filepath.Join(tmpDir, "common.journal")
	commonContent := `decimal-mark ,

D 1.000,00 EUR

commodity RUB
  format 1.000,00 RUB`
	err = os.WriteFile(commonPath, []byte(commonContent), 0644)
	require.NoError(t, err)

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)

	err = ws.Initialize()
	require.NoError(t, err)

	formats := ws.GetCommodityFormats()
	require.NotNil(t, formats)

	rubFormat, ok := formats["RUB"]
	assert.True(t, ok, "commodity directive from included file should be preserved")
	assert.Equal(t, ',', rubFormat.DecimalMark)

	defaultFormat, ok := formats[""]
	assert.True(t, ok, "D directive from included file should be preserved")
	assert.Equal(t, ',', defaultFormat.DecimalMark)
	assert.Equal(t, 2, defaultFormat.DecimalPlaces,
		"default format should come from D directive, not decimal-mark")
}

func TestBuildIncludeGraph_GlobExpansion(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainContent := "include *.journal\n"
	mainPath := filepath.Join(tmpDir, "main.journal")
	err := os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	tx1Content := "2024-01-01 payee1\n    expenses:food  10 USD\n    assets:cash\n"
	tx1Path := filepath.Join(tmpDir, "tx1.journal")
	err = os.WriteFile(tx1Path, []byte(tx1Content), 0644)
	require.NoError(t, err)

	tx2Content := "2024-01-02 payee2\n    expenses:food  20 USD\n    assets:cash\n"
	tx2Path := filepath.Join(tmpDir, "tx2.journal")
	err = os.WriteFile(tx2Path, []byte(tx2Content), 0644)
	require.NoError(t, err)

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)

	err = ws.Initialize()
	require.NoError(t, err)

	// Verify both included files are in the reverse graph
	includedBy := ws.GetIncludedBy(tx1Path)
	assert.Contains(t, includedBy, mainPath, "tx1 should be included by main")

	includedBy = ws.GetIncludedBy(tx2Path)
	assert.Contains(t, includedBy, mainPath, "tx2 should be included by main")

	// Verify all transactions are available via resolved journal
	resolved := ws.GetResolved()
	require.NotNil(t, resolved)
	allTx := resolved.AllTransactions()
	assert.Len(t, allTx, 2, "should have transactions from both included files")
}

func TestBuildIncludeGraph_GlobExpansion_FindRoot(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	// No main.journal — root must be found via include graph.
	// Name the root file "zzz_root.journal" so it is alphabetically LAST —
	// without glob expansion in buildIncludeGraph, the included file would be
	// picked as root candidate because reverseGraph misses it.
	rootContent := "include a_*.journal\n"
	rootPath := filepath.Join(tmpDir, "zzz_root.journal")
	err := os.WriteFile(rootPath, []byte(rootContent), 0644)
	require.NoError(t, err)

	tx1Content := "2024-01-01 payee1\n    expenses:food  10 USD\n    assets:cash\n"
	tx1Path := filepath.Join(tmpDir, "a_tx1.journal")
	err = os.WriteFile(tx1Path, []byte(tx1Content), 0644)
	require.NoError(t, err)

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)

	err = ws.Initialize()
	require.NoError(t, err)

	// zzz_root.journal should be the root (it includes others, nothing includes it)
	assert.Equal(t, rootPath, ws.RootJournalPath(),
		"zzz_root.journal should be detected as root even with glob include")
}

func TestBuildIncludeGraph_CRLF(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	// No main.journal — force findRootByIncludeGraph fallback.
	// Root file has CRLF line endings (as on Windows).
	rootContent := "include child.journal\r\n\r\n2024-01-01 Main\r\n    expenses:food  $10\r\n    assets:cash\r\n"
	rootPath := filepath.Join(tmpDir, "zzz_root.journal")
	require.NoError(t, os.WriteFile(rootPath, []byte(rootContent), 0644))

	childContent := "2024-01-02 Child\r\n    expenses:rent  $100\r\n    assets:bank\r\n"
	childPath := filepath.Join(tmpDir, "child.journal")
	require.NoError(t, os.WriteFile(childPath, []byte(childContent), 0644))

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)

	err := ws.Initialize()
	require.NoError(t, err)

	// Without CRLF normalization in buildIncludeGraph, the include path would be
	// "child.journal\r" which doesn't match any file → root detection fails.
	assert.Equal(t, rootPath, ws.RootJournalPath(),
		"should find root even with CRLF line endings in journal files")

	// Verify the included file's transactions are loaded
	resolved := ws.GetResolved()
	require.NotNil(t, resolved)
	allTx := resolved.AllTransactions()
	assert.Len(t, allTx, 2, "should have transactions from both root and child")
}

func TestWorkspace_MultiTree_TwoIndependentRoots(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	// Tree 1: personal.journal → personal-2025.journal
	personalPath := filepath.Join(tmpDir, "personal.journal")
	personalContent := "include personal-2025.journal\n\naccount assets:personal\n"
	require.NoError(t, os.WriteFile(personalPath, []byte(personalContent), 0644))

	personal2025Path := filepath.Join(tmpDir, "personal-2025.journal")
	personal2025Content := "2025-01-01 Salary\n    assets:personal  $1000\n    income:salary\n"
	require.NoError(t, os.WriteFile(personal2025Path, []byte(personal2025Content), 0644))

	// Tree 2: business.journal → business-2025.journal
	businessPath := filepath.Join(tmpDir, "business.journal")
	businessContent := "include business-2025.journal\n\naccount assets:business\n"
	require.NoError(t, os.WriteFile(businessPath, []byte(businessContent), 0644))

	business2025Path := filepath.Join(tmpDir, "business-2025.journal")
	business2025Content := "2025-01-01 Client payment\n    assets:business  $5000\n    income:consulting\n"
	require.NoError(t, os.WriteFile(business2025Path, []byte(business2025Content), 0644))

	ws := NewWorkspace(tmpDir, include.NewLoader())
	require.NoError(t, ws.Initialize())

	// personal.journal tree should have personal transactions
	personalResolved := ws.GetResolvedForFile(personalPath)
	require.NotNil(t, personalResolved)
	personalTx := personalResolved.AllTransactions()
	require.Len(t, personalTx, 1)
	assert.Equal(t, "Salary", personalTx[0].Description)

	// business.journal tree should have business transactions
	businessResolved := ws.GetResolvedForFile(businessPath)
	require.NotNil(t, businessResolved)
	businessTx := businessResolved.AllTransactions()
	require.Len(t, businessTx, 1)
	assert.Equal(t, "Client payment", businessTx[0].Description)

	// Child files should resolve to their parent's tree
	personal2025Resolved := ws.GetResolvedForFile(personal2025Path)
	require.NotNil(t, personal2025Resolved)
	assert.Equal(t, personalResolved, personal2025Resolved)

	business2025Resolved := ws.GetResolvedForFile(business2025Path)
	require.NotNil(t, business2025Resolved)
	assert.Equal(t, businessResolved, business2025Resolved)

	// Trees should be isolated — different resolved objects
	assert.NotEqual(t, personalResolved, businessResolved)
}

func TestWorkspace_MultiTree_Standalone(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	// Standalone file — no includes, not included by anyone
	standalonePath := filepath.Join(tmpDir, "standalone.journal")
	standaloneContent := "2025-01-01 Solo\n    expenses:food  $10\n    assets:cash\n"
	require.NoError(t, os.WriteFile(standalonePath, []byte(standaloneContent), 0644))

	ws := NewWorkspace(tmpDir, include.NewLoader())
	require.NoError(t, ws.Initialize())

	resolved := ws.GetResolvedForFile(standalonePath)
	require.NotNil(t, resolved)
	allTx := resolved.AllTransactions()
	require.Len(t, allTx, 1)
	assert.Equal(t, "Solo", allTx[0].Description)
}

func TestWorkspace_MultiTree_SingleRoot(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	mainContent := "include child.journal\n\n2024-01-01 Main\n    expenses:food  $10\n    assets:cash\n"
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0644))

	childPath := filepath.Join(tmpDir, "child.journal")
	childContent := "2024-01-02 Child\n    expenses:rent  $100\n    assets:bank\n"
	require.NoError(t, os.WriteFile(childPath, []byte(childContent), 0644))

	ws := NewWorkspace(tmpDir, include.NewLoader())
	require.NoError(t, ws.Initialize())

	// Both files should resolve to the same tree
	mainResolved := ws.GetResolvedForFile(mainPath)
	require.NotNil(t, mainResolved)
	childResolved := ws.GetResolvedForFile(childPath)
	require.NotNil(t, childResolved)
	assert.Equal(t, mainResolved, childResolved)

	// Should contain both transactions
	allTx := mainResolved.AllTransactions()
	assert.Len(t, allTx, 2)
}

func TestWorkspace_GetResolvedForFile_UnknownFile(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	require.NoError(t, os.WriteFile(mainPath, []byte(""), 0644))

	ws := NewWorkspace(tmpDir, include.NewLoader())
	require.NoError(t, ws.Initialize())

	resolved := ws.GetResolvedForFile("/nonexistent/file.journal")
	assert.Nil(t, resolved)
}

func TestWorkspace_AddMissingReachable_CRLF(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	// Start with main.journal that has no includes
	mainContent := "2024-01-01 Main\n    expenses:food  $10\n    assets:cash\n"
	mainPath := filepath.Join(tmpDir, "main.journal")
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0644))

	// Child file with CRLF on disk
	childContent := "2024-01-02 Child\r\n    expenses:rent  $100\r\n    assets:bank\r\n"
	childPath := filepath.Join(tmpDir, "child.journal")
	require.NoError(t, os.WriteFile(childPath, []byte(childContent), 0644))

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)
	require.NoError(t, ws.Initialize())

	// Now add include directive — this triggers refreshIncludeTreeLocked → addMissingReachableLocked
	ws.UpdateFile(mainPath, "include child.journal\n\n2024-01-01 Main\n    expenses:food  $10\n    assets:cash\n")

	resolved := ws.GetResolvedForFile(mainPath)
	require.NotNil(t, resolved)

	allTx := resolved.AllTransactions()
	assert.Len(t, allTx, 2, "should have transactions from both files after include added")

	// Verify the child was parsed correctly (CRLF normalized)
	snapshot := ws.IndexSnapshot()
	assert.Contains(t, snapshot.Accounts.All, "expenses:rent",
		"child file accounts should be available (CRLF must be normalized)")
	assert.Contains(t, snapshot.Accounts.All, "assets:bank")
}

func TestWorkspace_UpdateFile_SkipsNonJournal(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := filepath.Join(tmpDir, "main.journal")
	mainContent := `include bank.rules

2024-01-01 Test
    expenses:food  $10
    assets:cash
`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0644))

	rulesPath := filepath.Join(tmpDir, "bank.rules")
	rulesContent := `source data.csv
skip 1
fields date,description,amount
`
	require.NoError(t, os.WriteFile(rulesPath, []byte(rulesContent), 0644))

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)
	require.NoError(t, ws.Initialize())

	// UpdateFile on .rules path should not panic and should return early
	ws.UpdateFile(rulesPath, rulesContent)

	// Main journal data should still be intact
	snapshot := ws.IndexSnapshot()
	assert.Contains(t, snapshot.Accounts.All, "expenses:food")
	assert.Contains(t, snapshot.Accounts.All, "assets:cash")
}

// TestWorkspace_UpdateFile_OccurrencesPreserved verifies that after UpdateFile
// the resolved journal still has occurrence data (not just the legacy projection).
func TestWorkspace_UpdateFile_OccurrencesPreserved(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	childContent := "2024-01-01 child tx\n    expenses:child  $10\n    assets:cash\n"
	rootContent := "include child.journal\n\n2024-01-02 root tx\n    expenses:food  $20\n    assets:cash\n"

	childPath := filepath.Join(tmpDir, "child.journal")
	rootPath := filepath.Join(tmpDir, "root.journal")
	require.NoError(t, os.WriteFile(childPath, []byte(childContent), 0644))
	require.NoError(t, os.WriteFile(rootPath, []byte(rootContent), 0644))

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)
	require.NoError(t, ws.Initialize())

	// Edit root without changing includes
	ws.UpdateFile(rootPath, "include child.journal\n\n2024-01-02 root tx edited\n    expenses:food  $25\n    assets:cash\n")

	resolved := ws.GetResolvedForFile(rootPath)
	require.NotNil(t, resolved)
	assert.NotEmpty(t, resolved.Occurrences,
		"UpdateFile must preserve occurrences in the resolved journal")
	assert.NotEmpty(t, resolved.Items,
		"UpdateFile must preserve items in the resolved journal")
}

// TestWorkspace_UpdateFile_ReloadsAllOwners verifies that editing a child file
// included by two roots updates both roots.
func TestWorkspace_UpdateFile_ReloadsAllOwners(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	childContent := "2024-01-01 child tx\n    expenses:child  $10\n    assets:cash\n"
	root1Content := "include child.journal\n\n2024-01-02 root1 tx\n    expenses:food  $20\n    assets:cash\n"
	root2Content := "include child.journal\n\n2024-01-03 root2 tx\n    expenses:rent  $30\n    assets:bank\n"

	childPath := filepath.Join(tmpDir, "child.journal")
	root1Path := filepath.Join(tmpDir, "root1.journal")
	root2Path := filepath.Join(tmpDir, "root2.journal")
	require.NoError(t, os.WriteFile(childPath, []byte(childContent), 0644))
	require.NoError(t, os.WriteFile(root1Path, []byte(root1Content), 0644))
	require.NoError(t, os.WriteFile(root2Path, []byte(root2Content), 0644))

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)
	require.NoError(t, ws.Initialize())

	// Edit child — both roots must be reloaded
	ws.UpdateFile(childPath, "2024-01-01 child tx edited\n    expenses:child  $99\n    assets:cash\n")

	resolved1 := ws.GetResolvedForFile(root1Path)
	require.NotNil(t, resolved1)
	allTx1 := resolved1.AllTransactions()
	found := false
	for _, tx := range allTx1 {
		if tx.Description == "child tx edited" {
			found = true
		}
	}
	assert.True(t, found, "root1 must see edited child transaction")

	resolved2 := ws.GetResolvedForFile(root2Path)
	require.NotNil(t, resolved2)
	allTx2 := resolved2.AllTransactions()
	found = false
	for _, tx := range allTx2 {
		if tx.Description == "child tx edited" {
			found = true
		}
	}
	assert.True(t, found, "root2 must see edited child transaction")
}

// TestWorkspace_GetResolvedForFile_RepeatedInclude verifies that a file included
// twice in one root has two occurrences in the resolved journal.
func TestWorkspace_GetResolvedForFile_RepeatedInclude(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	childContent := "2024-01-01 child tx\n    expenses:child  $10\n    assets:cash\n"
	rootContent := "include child.journal\n\ninclude child.journal\n\n2024-01-02 root tx\n    expenses:food  $20\n    assets:cash\n"

	childPath := filepath.Join(tmpDir, "child.journal")
	rootPath := filepath.Join(tmpDir, "root.journal")
	require.NoError(t, os.WriteFile(childPath, []byte(childContent), 0644))
	require.NoError(t, os.WriteFile(rootPath, []byte(rootContent), 0644))

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)
	require.NoError(t, ws.Initialize())

	resolved := ws.GetResolvedForFile(rootPath)
	require.NotNil(t, resolved)

	// child.journal included twice → 2 child occurrences + 1 root = 3 total
	assert.Len(t, resolved.Occurrences, 3,
		"repeated include must produce two child occurrences")

	// AllTransactions counts each occurrence
	allTx := resolved.AllTransactions()
	assert.Len(t, allTx, 3, "child tx counted twice + root tx once")
}
