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

func TestWorkspace_FindRootJournal_EnvVariable(t *testing.T) {
	tmpDir := t.TempDir()

	customPath := filepath.Join(tmpDir, "custom.journal")
	err := os.WriteFile(customPath, []byte(""), 0644)
	require.NoError(t, err)

	mainPath := filepath.Join(tmpDir, "main.journal")
	err = os.WriteFile(mainPath, []byte(""), 0644)
	require.NoError(t, err)

	t.Setenv("LEDGER_FILE", customPath)

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)

	err = ws.Initialize()
	require.NoError(t, err)

	assert.Equal(t, customPath, ws.RootJournalPath())
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

func TestExpandTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	tests := []struct {
		input    string
		expected string
	}{
		{"~/test.journal", filepath.Join(home, "test.journal")},
		{"~/.hledger/main.journal", filepath.Join(home, ".hledger/main.journal")},
		{"/absolute/path.journal", "/absolute/path.journal"},
		{"relative/path.journal", "relative/path.journal"},
		{"~", "~"},
		{"~user/path", "~user/path"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := expandTilde(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWorkspace_FindRootJournal_EnvWithTilde(t *testing.T) {
	tmpDir := t.TempDir()

	customPath := filepath.Join(tmpDir, "custom.journal")
	err := os.WriteFile(customPath, []byte(""), 0644)
	require.NoError(t, err)

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	relPath, err := filepath.Rel(home, customPath)
	if err != nil {
		t.Skip("temp dir is not under home directory")
	}

	t.Setenv("LEDGER_FILE", "~/"+relPath)

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)

	err = ws.Initialize()
	require.NoError(t, err)

	assert.Equal(t, customPath, ws.RootJournalPath())
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
	assert.NotContains(t, snapshot.Accounts.All, "expenses:travel")

	ws.UpdateFile(mainPath, "include one.journal\ninclude two.journal")
	snapshot = ws.IndexSnapshot()
	assert.Contains(t, snapshot.Accounts.All, "expenses:travel")

	ws.UpdateFile(mainPath, "include two.journal")
	snapshot = ws.IndexSnapshot()
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
