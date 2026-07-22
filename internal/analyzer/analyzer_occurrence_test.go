package analyzer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/include"
)

//nolint:unparam // test helper keeps mainName explicit at call sites
func loadResolved(t *testing.T, files map[string]string, mainName string) *include.ResolvedJournal {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	result, errs := include.NewLoader().Load(filepath.Join(dir, mainName))
	require.NotNil(t, result)
	require.Empty(t, errs)
	return result
}

// A repeated include produces two occurrences of the same source; aggregation
// counts must reflect each occurrence, not a path-deduplicated first occurrence.
func TestAnalyzeResolved_RepeatedIncludeCountsEachOccurrence(t *testing.T) {
	resolved := loadResolved(t, map[string]string{
		"main.journal":  "include child.journal\ninclude child.journal\n",
		"child.journal": "2024-01-01 Child\n    expenses:rent  $100\n    assets:bank\n",
	}, "main.journal")

	require.Len(t, resolved.AllTransactions(), 2)

	a := New()
	result := a.AnalyzeResolved(resolved)

	assert.Equal(t, 2, result.AccountCounts["expenses:rent"], "each occurrence counts")
	assert.Equal(t, 2, result.AccountCounts["assets:bank"], "each occurrence counts")
	assert.Equal(t, 2, result.PayeeCounts["Child"])
	assert.Equal(t, 2, result.DescriptionCounts["Child"])
	assert.Equal(t, 2, result.CommodityCounts["$"])
}

// Unordered set collectors deduplicate, so a repeated include leaves the set
// unchanged while still aggregating from every occurrence.
func TestAnalyzeResolved_RepeatedIncludeKeepsSetsDeduplicated(t *testing.T) {
	resolved := loadResolved(t, map[string]string{
		"main.journal":  "include child.journal\ninclude child.journal\n",
		"child.journal": "2024-01-01 Child\n    expenses:rent  $100\n    assets:bank\n",
	}, "main.journal")

	a := New()
	result := a.AnalyzeResolved(resolved)

	assert.Contains(t, result.Accounts.All, "expenses:rent")
	assert.Contains(t, result.Accounts.All, "assets:bank")
	assert.Contains(t, result.Payees, "Child")
	assert.Contains(t, result.Commodities, "$")
	assert.Contains(t, result.Dates, "2024-01-01")
}

// When the same payee and posting pattern appear in a parent and an included
// child, the conflict is resolved by textual-inline order: the last occurrence
// in inline order wins. Here the include follows the parent transaction, so the
// child is last inline and its template wins (legacy "primary wins" would pick
// the parent's $200).
func TestAnalyzeResolved_TemplateConflictResolvedByInlineOrder(t *testing.T) {
	resolved := loadResolved(t, map[string]string{
		"main.journal":  "2024-01-01 Grocery\n    expenses:food  $200\n    assets:cash\ninclude child.journal\n",
		"child.journal": "2024-01-02 Grocery\n    expenses:food  $100\n    assets:cash\n",
	}, "main.journal")

	a := New()
	result := a.AnalyzeResolved(resolved)

	templates, ok := result.PayeeTemplates["Grocery"]
	require.True(t, ok, "Grocery template should exist")
	require.Len(t, templates, 2)
	assert.Equal(t, "expenses:food", templates[0].Account)
	assert.Equal(t, "100", templates[0].Amount, "child is last in inline order, so its template wins")
}

// Diagnostics (balance/undeclared/date-tag) must cover all occurrences, not
// just Primary.Transactions. An unbalanced transaction in an included child
// must produce a diagnostic.
func TestAnalyzeResolved_DiagnosticsCoverAllOccurrences(t *testing.T) {
	resolved := loadResolved(t, map[string]string{
		"main.journal":  "include child.journal\n\n2024-01-01 Root\n    expenses:food  $50\n    assets:cash\n",
		"child.journal": "2024-01-02 Child\n    expenses:rent  $100\n",
	}, "main.journal")

	a := New()
	result := a.AnalyzeResolved(resolved)

	// child.journal has an unbalanced transaction (only one posting)
	hasChildDiag := false
	for _, d := range result.Diagnostics {
		if d.Message != "" && d.Range.Start.Line == 1 {
			hasChildDiag = true
		}
	}
	assert.True(t, hasChildDiag,
		"unbalanced transaction in included child must produce a diagnostic")
}

// Identical diagnostics from repeated includes are deduped by exact key
// (range, message, code, severity).
func TestAnalyzeResolved_RepeatedIncludeDiagnosticsDeduped(t *testing.T) {
	resolved := loadResolved(t, map[string]string{
		"main.journal":  "include child.journal\ninclude child.journal\n",
		"child.journal": "2024-01-01 Child\n    expenses:rent  $100\n",
	}, "main.journal")

	a := New()
	result := a.AnalyzeResolved(resolved)

	// child.journal included twice → same unbalanced tx appears twice.
	// Diagnostics must be deduped: only one diagnostic for the same range.
	count := 0
	for _, d := range result.Diagnostics {
		if d.Range.Start.Line == 1 && d.Range.Start.Column == 1 {
			count++
		}
	}
	assert.Equal(t, 1, count,
		"identical diagnostics from repeated includes must be deduped")
}
