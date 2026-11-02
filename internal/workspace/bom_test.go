package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/textutil"
)

func TestWorkspace_JournalsWithLeadingBOM(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	dir := t.TempDir()
	rootPath := filepath.Join(dir, "zzz_root.journal")
	require.NoError(t, os.WriteFile(rootPath, []byte(textutil.BOM+"include child.journal\r\n\r\n2024-01-01 кафе\r\n    расходы:еда  1 ₽\r\n    активы:наличные\r\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "child.journal"), []byte(textutil.BOM+"2024-01-02 食品店\r\n    支出:食品  100 JPY\r\n    資産:現金\r\n"), 0o644))

	ws := NewWorkspace(dir, include.NewLoader())
	require.NoError(t, ws.Initialize())

	assert.Equal(t, rootPath, ws.RootJournalPath())
	resolved := ws.GetResolved()
	require.NotNil(t, resolved)
	assert.Len(t, resolved.AllTransactions(), 2)
}
