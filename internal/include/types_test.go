package include

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/ast"
)

func TestFormatDirectives_FiltersDecimalMarkFromIncludes(t *testing.T) {
	primary := &ast.Journal{
		Directives: []ast.Directive{
			ast.CommodityDirective{Commodity: ast.Commodity{Symbol: "RUB"}, Format: "1.000,00 RUB"},
			ast.DecimalMarkDirective{Mark: ","},
		},
	}

	included := &ast.Journal{
		Directives: []ast.Directive{
			ast.DecimalMarkDirective{Mark: "."},
			ast.DefaultCommodityDirective{Symbol: "$", Format: "$1,000.00"},
		},
	}

	resolved := &ResolvedJournal{
		Primary:   primary,
		Files:     map[string]*ast.Journal{"included.journal": included},
		FileOrder: []string{"included.journal"},
	}

	result := resolved.FormatDirectives()

	require.Len(t, result, 3, "primary commodity + primary decimal-mark + included D directive")

	var hasDecimalMarkComma, hasDecimalMarkDot bool
	var hasCommodityRUB, hasDefaultD bool
	for _, d := range result {
		switch dd := d.(type) {
		case ast.DecimalMarkDirective:
			if dd.Mark == "," {
				hasDecimalMarkComma = true
			}
			if dd.Mark == "." {
				hasDecimalMarkDot = true
			}
		case ast.CommodityDirective:
			hasCommodityRUB = true
		case ast.DefaultCommodityDirective:
			hasDefaultD = true
		}
	}

	assert.True(t, hasDecimalMarkComma, "decimal-mark from primary should be preserved")
	assert.False(t, hasDecimalMarkDot, "decimal-mark from included file should be filtered")
	assert.True(t, hasCommodityRUB, "commodity directive from primary should be preserved")
	assert.True(t, hasDefaultD, "D directive from included file should be preserved")
}

func TestFormatDirectives_PreservesAllDirectivesFromIncludes(t *testing.T) {
	primary := &ast.Journal{}

	included := &ast.Journal{
		Directives: []ast.Directive{
			ast.CommodityDirective{Commodity: ast.Commodity{Symbol: "EUR"}, Format: "1 000,00 EUR"},
			ast.DefaultCommodityDirective{Symbol: "$", Format: "$1,000.00"},
			ast.AccountDirective{Account: ast.Account{Name: "expenses:food"}},
		},
	}

	resolved := &ResolvedJournal{
		Primary:   primary,
		Files:     map[string]*ast.Journal{"included.journal": included},
		FileOrder: []string{"included.journal"},
	}

	result := resolved.FormatDirectives()
	require.Len(t, result, 3, "all non-decimal-mark directives from includes should be preserved")
}

func TestFormatDirectives_NilPrimary(t *testing.T) {
	resolved := &ResolvedJournal{
		Primary:   nil,
		Files:     map[string]*ast.Journal{},
		FileOrder: nil,
	}

	result := resolved.FormatDirectives()
	assert.Empty(t, result)
}

// --- FormatsAt tests ---

func TestFormatsAt_RootDirectiveBeforeOffset(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root.journal")
	writeJournalFile(t, root,
		"commodity $\n    format $1,000.00\n\n2024-01-01 test\n    expenses:food  $50.00\n    assets:cash\n")

	loader := NewLoader()
	result, errs := loader.Load(root)
	require.Empty(t, errs)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Occurrences)

	rootID := result.Occurrences[0].ID
	txOffset := result.Occurrences[0].Journal.Transactions[0].Range.Start.Offset

	formats := result.FormatsAt(rootID, txOffset)
	require.Contains(t, formats, "$", "commodity directive before offset should be included")
}

func TestFormatsAt_DirectiveAfterOffset_NotIncluded(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root.journal")
	// commodity directive AFTER the transaction
	writeJournalFile(t, root,
		"2024-01-01 test\n    expenses:food  $50.00\n    assets:cash\n\ncommodity $\n    format $1,000.00\n")

	loader := NewLoader()
	result, errs := loader.Load(root)
	require.Empty(t, errs)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Occurrences)

	rootID := result.Occurrences[0].ID
	txOffset := result.Occurrences[0].Journal.Transactions[0].Range.Start.Offset

	formats := result.FormatsAt(rootID, txOffset)
	assert.NotContains(t, formats, "$", "commodity directive after offset should not be included")
}

func TestFormatsAt_ChildInheritsParentFormats(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "child.journal")
	root := filepath.Join(dir, "root.journal")
	writeJournalFile(t, child,
		"2024-01-01 child tx\n    expenses:child  10 EUR\n    assets:cash\n")
	writeJournalFile(t, root,
		"commodity $\n    format $1,000.00\n\ninclude child.journal\n")

	loader := NewLoader()
	result, errs := loader.Load(root)
	require.Empty(t, errs)
	require.NotNil(t, result)
	require.Len(t, result.Occurrences, 2)

	childID := result.Occurrences[1].ID
	childTxOffset := result.Occurrences[1].Journal.Transactions[0].Range.Start.Offset

	formats := result.FormatsAt(childID, childTxOffset)
	assert.Contains(t, formats, "$", "child should inherit parent commodity format at include site")
}

func TestFormatsAt_ChildDecimalMark_LocalOnly(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "child.journal")
	root := filepath.Join(dir, "root.journal")
	writeJournalFile(t, child,
		"decimal-mark ,\n\n2024-01-01 child tx\n    expenses:child  10,50\n    assets:cash\n")
	writeJournalFile(t, root,
		"include child.journal\n\n2024-01-02 root tx\n    expenses:food  50.00\n    assets:cash\n")

	loader := NewLoader()
	result, errs := loader.Load(root)
	require.Empty(t, errs)
	require.NotNil(t, result)
	require.Len(t, result.Occurrences, 2)

	rootID := result.Occurrences[0].ID
	childID := result.Occurrences[1].ID

	// Child at position after decimal-mark: should use comma
	childTxOffset := result.Occurrences[1].Journal.Transactions[0].Range.Start.Offset
	childFormats := result.FormatsAt(childID, childTxOffset)
	if def, ok := childFormats[""]; ok {
		assert.Equal(t, ',', def.DecimalMark, "child should use comma decimal mark locally")
	}

	// Root at position after include: should NOT use comma from child
	rootTxOffset := result.Occurrences[0].Journal.Transactions[0].Range.Start.Offset
	rootFormats := result.FormatsAt(rootID, rootTxOffset)
	if def, ok := rootFormats[""]; ok {
		assert.NotEqual(t, ',', def.DecimalMark, "root must not inherit child decimal-mark")
	}
}

func TestFormatsAt_ChildCommodityMergeForward(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "child.journal")
	root := filepath.Join(dir, "root.journal")
	writeJournalFile(t, child,
		"commodity EUR\n    format 1.000,00 EUR\n\n2024-01-01 child tx\n    expenses:child  10,00 EUR\n    assets:cash\n")
	writeJournalFile(t, root,
		"include child.journal\n\n2024-01-02 root tx\n    expenses:food  50.00 EUR\n    assets:cash\n")

	loader := NewLoader()
	result, errs := loader.Load(root)
	require.Empty(t, errs)
	require.NotNil(t, result)
	require.Len(t, result.Occurrences, 2)

	rootID := result.Occurrences[0].ID
	rootTxOffset := result.Occurrences[0].Journal.Transactions[0].Range.Start.Offset

	// Root after include should see child's commodity EUR declaration (merge-forward)
	rootFormats := result.FormatsAt(rootID, rootTxOffset)
	assert.Contains(t, rootFormats, "EUR", "child commodity declaration should merge forward to root after include")
}
