package include

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/textutil"
)

func writeBOMFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestLoader_FilesWithLeadingBOM(t *testing.T) {
	bodies := map[string]string{
		"ASCII":         "2024-01-01 shop\n    expenses:food  1 USD\n    assets:cash\n",
		"Cyrillic CRLF": "2024-01-01 кафе\r\n    расходы:еда  100 ₽\r\n    активы:наличные\r\n",
		"CJK":           "2024-01-01 食品店\n    支出:食品  100 JPY\n    資産:現金\n",
	}
	for name, body := range bodies {
		t.Run(name+"/included file", func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, "main.journal")
			writeBOMFile(t, root, "include child.journal\n")
			writeBOMFile(t, filepath.Join(dir, "child.journal"), textutil.BOM+body)

			result, errs := NewLoader().Load(root)
			require.Empty(t, errs)
			require.NotNil(t, result)
			assert.Len(t, result.AllTransactions(), 1)
		})
		t.Run(name+"/root file", func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, "main.journal")
			writeBOMFile(t, root, textutil.BOM+body)

			result, errs := NewLoader().Load(root)
			require.Empty(t, errs)
			require.NotNil(t, result)
			assert.Len(t, result.AllTransactions(), 1)
		})
	}
}

func TestLoader_BOMInRootAndIncludedFile(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "main.journal")
	writeBOMFile(t, root, textutil.BOM+"; header\r\ninclude child.journal\r\n")
	writeBOMFile(t, filepath.Join(dir, "child.journal"), textutil.BOM+"account assets:cash\r\n2024-01-01 кафе\r\n    расходы:еда  1 ₽\r\n    assets:cash\r\n")

	result, errs := NewLoader().Load(root)
	require.Empty(t, errs)
	require.NotNil(t, result)
	assert.Len(t, result.AllTransactions(), 1)
	assert.Len(t, result.AllDirectives(), 1)
}
