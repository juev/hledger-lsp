package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/include"
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
