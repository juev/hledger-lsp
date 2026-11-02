package rules

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/textutil"
)

// hledger reads a rules file that starts with a byte order mark.
func TestRulesLoader_RootFileWithLeadingBOM(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "main.rules")
	writeRulesFile(t, root, textutil.BOM+"skip 1\r\nfields date,description,amount\r\nif кафе\r\n  account2 расходы:еда\r\n")

	result, errs := NewLoader().Load(root)
	require.Empty(t, errs)
	require.NotNil(t, result)
	require.NotNil(t, result.Primary)
	assert.Len(t, result.Primary.FieldsDefs, 1)
	assert.Len(t, result.Primary.IfBlocks, 1)
}
