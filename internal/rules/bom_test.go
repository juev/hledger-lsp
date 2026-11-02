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

func TestRulesParse_LeadingBOMIsSkipped(t *testing.T) {
	body := "skip 1\nfields date,description,amount\nif кафе\n  account2 расходы:еда\n"
	plain, plainDiags := Parse(body)
	require.Empty(t, plainDiags)

	rf, diags := Parse(textutil.BOM + body)
	require.Empty(t, diags)
	assert.Equal(t, len(plain.FieldsDefs), len(rf.FieldsDefs))
	assert.Equal(t, len(plain.IfBlocks), len(rf.IfBlocks))
	require.NotEmpty(t, rf.IfBlocks)
	// Offsets count the mark's bytes so they stay valid in the buffer.
	assert.Equal(t, plain.IfBlocks[0].Range.Start.Offset+len(textutil.BOM), rf.IfBlocks[0].Range.Start.Offset)
}

func TestRulesParse_SecondBOMIsADiagnostic(t *testing.T) {
	_, diags := Parse(textutil.BOM + textutil.BOM + "skip 1\n")
	assert.NotEmpty(t, diags)
}

// hledger rejects a mark at the start of an included rules file; only the root
// file may carry one.
func TestRulesLoader_IncludedRulesFileWithBOMIsRejected(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "main.rules")
	writeRulesFile(t, root, textutil.BOM+"skip 1\ninclude inc.rules\n")
	writeRulesFile(t, filepath.Join(dir, "inc.rules"), textutil.BOM+"fields date,description,amount\n")

	_, errs := NewLoader().Load(root)
	assert.NotEmpty(t, errs)
}

func TestRulesLoader_IncludeOnFirstLineOfRootWithBOM(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "main.rules")
	writeRulesFile(t, root, textutil.BOM+"include inc.rules\n")
	writeRulesFile(t, filepath.Join(dir, "inc.rules"), "fields date,description,amount\n")

	result, errs := NewLoader().Load(root)
	require.Empty(t, errs)
	require.NotNil(t, result.Primary)
	assert.Len(t, result.Primary.FieldsDefs, 1)
}

func TestRulesLoader_DoubleBOMInRootIsRejected(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "main.rules")
	writeRulesFile(t, root, textutil.BOM+textutil.BOM+"skip 1\n")

	_, errs := NewLoader().Load(root)
	assert.NotEmpty(t, errs)
}
