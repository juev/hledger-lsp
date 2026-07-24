package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestIntegration_IncludeTransactionsInCompletion(t *testing.T) {
	tmpDir := t.TempDir()

	includedContent := `2024-01-10 paycheck
    income:salary  $3000.00
    assets:bank`

	mainContent := `include included.journal

2024-01-15 grocery store
    expenses:food  $50.00
    assets:cash

2024-01-16 new transaction
    `

	includedPath := filepath.Join(tmpDir, "included.journal")
	mainPath := filepath.Join(tmpDir, "main.journal")

	err := os.WriteFile(includedPath, []byte(includedContent), 0644)
	require.NoError(t, err)
	err = os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	ts := newTestServer()
	uri := uri.URI(fmt.Sprintf("file://%s", mainPath))

	_, err = ts.openAndWait(uri, mainContent)
	require.NoError(t, err)

	completions, err := ts.completion(uri, 7, 4)
	require.NoError(t, err)
	require.NotNil(t, completions)

	labels := extractCompletionLabels(completions.Items)
	assert.Contains(t, labels, "income:salary")
}

func TestIntegration_IncludeFileNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	mainContent := `include nonexistent.journal

2024-01-15 grocery store
    expenses:food  $50.00
    assets:cash`

	mainPath := filepath.Join(tmpDir, "main.journal")
	err := os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	ts := newTestServer()
	uri := uri.URI(fmt.Sprintf("file://%s", mainPath))

	diagnostics, err := ts.openAndWait(uri, mainContent)
	require.NoError(t, err)
	require.NotEmpty(t, diagnostics)

	hasFileError := false
	for _, d := range diagnostics {
		if d.Severity == protocol.DiagnosticSeverityError {
			hasFileError = true
			break
		}
	}
	assert.True(t, hasFileError)
}

func TestIntegration_IncludeHoverShowsAggregatedBalance(t *testing.T) {
	tmpDir := t.TempDir()

	includedContent := `2024-01-10 initial balance
    expenses:food  $100.00
    assets:cash`

	mainContent := `include included.journal

2024-01-15 grocery store
    expenses:food  $50.00
    assets:cash`

	includedPath := filepath.Join(tmpDir, "included.journal")
	mainPath := filepath.Join(tmpDir, "main.journal")

	err := os.WriteFile(includedPath, []byte(includedContent), 0644)
	require.NoError(t, err)
	err = os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	ts := newTestServer()
	uri := uri.URI(fmt.Sprintf("file://%s", mainPath))

	_, err = ts.openAndWait(uri, mainContent)
	require.NoError(t, err)

	hover, err := ts.hover(uri, 3)
	require.NoError(t, err)
	require.NotNil(t, hover)

	hoverContent := hoverContent(hover)
	assert.Contains(t, hoverContent, "expenses:food")
	assert.Contains(t, hoverContent, "150")
}

func TestIntegration_IncludeCycleDetection(t *testing.T) {
	tmpDir := t.TempDir()

	file1Content := `include file2.journal

2024-01-15 test
    expenses:food  $50.00
    assets:cash

2024-01-17 new
    `

	file2Content := `include file1.journal

2024-01-16 test2
    expenses:rent  $100.00
    assets:bank`

	file1Path := filepath.Join(tmpDir, "file1.journal")
	file2Path := filepath.Join(tmpDir, "file2.journal")

	err := os.WriteFile(file1Path, []byte(file1Content), 0644)
	require.NoError(t, err)
	err = os.WriteFile(file2Path, []byte(file2Content), 0644)
	require.NoError(t, err)

	ts := newTestServer()
	uri := uri.URI(fmt.Sprintf("file://%s", file1Path))

	_, err = ts.openAndWait(uri, file1Content)
	require.NoError(t, err)

	completions, err := ts.completion(uri, 7, 4)
	require.NoError(t, err)
	require.NotNil(t, completions)

	labels := extractCompletionLabels(completions.Items)
	assert.Contains(t, labels, "expenses:food")
	assert.Contains(t, labels, "expenses:rent")
}

func TestIntegration_NestedIncludesWithTransactions(t *testing.T) {
	tmpDir := t.TempDir()

	level2Content := `2024-01-05 initial
    assets:savings  $1000.00
    income:bonus  $-1000.00`

	level1Content := `include level2.journal

2024-01-08 withdraw
    assets:cash  $200.00
    assets:savings  $-200.00`

	mainContent := `include level1.journal

2024-01-15 test
    `

	level2Path := filepath.Join(tmpDir, "level2.journal")
	level1Path := filepath.Join(tmpDir, "level1.journal")
	mainPath := filepath.Join(tmpDir, "main.journal")

	err := os.WriteFile(level2Path, []byte(level2Content), 0644)
	require.NoError(t, err)
	err = os.WriteFile(level1Path, []byte(level1Content), 0644)
	require.NoError(t, err)
	err = os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	ts := newTestServer()
	uri := uri.URI(fmt.Sprintf("file://%s", mainPath))

	_, err = ts.openAndWait(uri, mainContent)
	require.NoError(t, err)

	completions, err := ts.completion(uri, 3, 4)
	require.NoError(t, err)
	require.NotNil(t, completions)

	labels := extractCompletionLabels(completions.Items)
	assert.Contains(t, labels, "assets:savings")
	assert.Contains(t, labels, "assets:cash")
}

func TestIntegration_IncludeRelativePath(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	err := os.MkdirAll(subDir, 0755)
	require.NoError(t, err)

	subContent := `2024-01-10 netflix
    expenses:subscriptions  $15.00
    assets:bank`

	mainContent := `include subdir/sub.journal

2024-01-15 test
    `

	subPath := filepath.Join(subDir, "sub.journal")
	mainPath := filepath.Join(tmpDir, "main.journal")

	err = os.WriteFile(subPath, []byte(subContent), 0644)
	require.NoError(t, err)
	err = os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	ts := newTestServer()
	uri := uri.URI(fmt.Sprintf("file://%s", mainPath))

	_, err = ts.openAndWait(uri, mainContent)
	require.NoError(t, err)

	completions, err := ts.completion(uri, 3, 4)
	require.NoError(t, err)
	require.NotNil(t, completions)

	labels := extractCompletionLabels(completions.Items)
	assert.Contains(t, labels, "expenses:subscriptions")
	assert.Contains(t, labels, "assets:bank")
}

func TestIntegration_DefinitionAccountInIncludedFile(t *testing.T) {
	tmpDir := t.TempDir()

	accountsContent := `account expenses:food
account assets:cash`

	mainContent := `include accounts.journal

2024-01-15 grocery
    expenses:food  $50
    assets:cash`

	accountsPath := filepath.Join(tmpDir, "accounts.journal")
	mainPath := filepath.Join(tmpDir, "main.journal")

	err := os.WriteFile(accountsPath, []byte(accountsContent), 0644)
	require.NoError(t, err)
	err = os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	ts := newTestServer()
	uri := uri.URI(fmt.Sprintf("file://%s", mainPath))

	_, err = ts.openAndWait(uri, mainContent)
	require.NoError(t, err)

	result, err := ts.definition(uri, 3, 6) // on "expenses:food" in posting
	require.NoError(t, err)
	require.Len(t, result, 1)

	assert.Contains(t, string(result[0].URI), "accounts.journal")
	assert.Equal(t, uint32(0), result[0].Range.Start.Line)
}

func TestIntegration_ReferencesAcrossIncludedFiles(t *testing.T) {
	tmpDir := t.TempDir()

	accountsContent := `account expenses:food`

	transactionsContent := `2024-01-10 paycheck
    expenses:food  $100.00
    assets:cash`

	mainContent := `include accounts.journal
include transactions.journal

2024-01-15 grocery
    expenses:food  $50
    assets:cash`

	accountsPath := filepath.Join(tmpDir, "accounts.journal")
	transactionsPath := filepath.Join(tmpDir, "transactions.journal")
	mainPath := filepath.Join(tmpDir, "main.journal")

	err := os.WriteFile(accountsPath, []byte(accountsContent), 0644)
	require.NoError(t, err)
	err = os.WriteFile(transactionsPath, []byte(transactionsContent), 0644)
	require.NoError(t, err)
	err = os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	ts := newTestServer()
	uri := uri.URI(fmt.Sprintf("file://%s", mainPath))

	_, err = ts.openAndWait(uri, mainContent)
	require.NoError(t, err)

	result, err := ts.references(uri, 4, 6, true) // on "expenses:food", includeDeclaration=true
	require.NoError(t, err)
	require.Len(t, result, 3) // 1 declaration + 2 usages

	uris := make(map[string]int)
	for _, loc := range result {
		uris[string(loc.URI)]++
	}
	assert.Equal(t, 1, uris[fmt.Sprintf("file://%s", accountsPath)])
	assert.Equal(t, 1, uris[fmt.Sprintf("file://%s", transactionsPath)])
	assert.Equal(t, 1, uris[fmt.Sprintf("file://%s", mainPath)])
}

func TestIntegration_ReferencesDeclarationInIncludedFile(t *testing.T) {
	tmpDir := t.TempDir()

	accountsContent := `account expenses:food`

	mainContent := `include accounts.journal

2024-01-15 grocery
    expenses:food  $50
    assets:cash`

	accountsPath := filepath.Join(tmpDir, "accounts.journal")
	mainPath := filepath.Join(tmpDir, "main.journal")

	err := os.WriteFile(accountsPath, []byte(accountsContent), 0644)
	require.NoError(t, err)
	err = os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	ts := newTestServer()
	uri := uri.URI(fmt.Sprintf("file://%s", mainPath))

	_, err = ts.openAndWait(uri, mainContent)
	require.NoError(t, err)

	resultInclude, err := ts.references(uri, 3, 6, true)
	require.NoError(t, err)
	require.Len(t, resultInclude, 2) // declaration + usage

	resultExclude, err := ts.references(uri, 3, 6, false)
	require.NoError(t, err)
	require.Len(t, resultExclude, 1) // only usage
	assert.Contains(t, string(resultExclude[0].URI), "main.journal")
}

func TestIntegration_Issue18_HoverWithIncludedFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Reproduce exact setup from issue #18
	recordsContent := `2026-01-01 Cafe
    Assets:Cash  -25.70 USD
    Assets:Cash  -9.00 USD
    Expenses:Occasions  25.70 USD
    Expenses:Occasions  9.00 USD

2026-01-01 Payee1
    Assets:Cash  -6.20 USD
    Expenses:Occasions  6.20 USD

2026-03-17 Payee2
    Assets:Cash  -5.00 USD
    Expenses:Food  5.00 USD

2026-03-17 Payee2
    Assets:Cash  -17.70 USD
    Expenses:Food  17.70 USD

2026-03-18 Payee2
    Assets:Cash  -7.80 USD
    Expenses:Food  7.80 USD
`

	mainContent := `include records.journal

account Assets:Cash
account Expenses:Food
account Expenses:Occasions

2026-01-01 Test
    Assets:Cash  -25.70 USD
    Assets:Cash  -18.00 USD
    Expenses:Occasions  25.70 USD
    Expenses:Occasions  9.00 USD
    Expenses:Food  9.00 USD
`

	recordsPath := filepath.Join(tmpDir, "records.journal")
	mainPath := filepath.Join(tmpDir, "main.journal")

	err := os.WriteFile(recordsPath, []byte(recordsContent), 0644)
	require.NoError(t, err)
	err = os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	ts := newTestServer()
	uri := uri.URI(fmt.Sprintf("file://%s", mainPath))

	_, err = ts.openAndWait(uri, mainContent)
	require.NoError(t, err)

	// Hover over Assets:Cash on line 7 (0-indexed), character 6
	hover, err := ts.hover(uri, 7)
	require.NoError(t, err)
	require.NotNil(t, hover)

	hoverContent := hoverContent(hover)
	t.Logf("Hover content:\n%s", hoverContent)

	assert.Contains(t, hoverContent, "Assets:Cash")

	// Total Assets:Cash: main(-25.70 + -18.00) + records(-25.70 + -9.00 + -6.20 + -5.00 + -17.70 + -7.80)
	// = -43.70 + -71.40 = -115.10, 8 postings
	assert.Contains(t, hoverContent, "115.10", "balance should include all postings from both files")
	assert.Contains(t, hoverContent, "Postings: 8", "should count all 8 postings across both files")
}

func TestIntegration_Issue18_CRLFIncludedFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Included file with CRLF line endings (as on Windows)
	recordsContent := "2026-01-01 Cafe\r\n    Assets:Cash  -25.70 USD\r\n    Assets:Cash  -9.00 USD\r\n    Expenses:Occasions  25.70 USD\r\n    Expenses:Occasions  9.00 USD\r\n\r\n2026-01-01 Payee1\r\n    Assets:Cash  -6.20 USD\r\n    Expenses:Occasions  6.20 USD\r\n\r\n2026-03-17 Payee2\r\n    Assets:Cash  -5.00 USD\r\n    Expenses:Food  5.00 USD\r\n\r\n2026-03-17 Payee2\r\n    Assets:Cash  -17.70 USD\r\n    Expenses:Food  17.70 USD\r\n\r\n2026-03-18 Payee2\r\n    Assets:Cash  -7.80 USD\r\n    Expenses:Food  7.80 USD\r\n"

	mainContent := `include records.journal

account Assets:Cash
account Expenses:Food
account Expenses:Occasions

2026-01-01 Test
    Assets:Cash  -25.70 USD
    Assets:Cash  -18.00 USD
    Expenses:Occasions  25.70 USD
    Expenses:Occasions  9.00 USD
    Expenses:Food  9.00 USD
`

	recordsPath := filepath.Join(tmpDir, "records.journal")
	mainPath := filepath.Join(tmpDir, "main.journal")

	err := os.WriteFile(recordsPath, []byte(recordsContent), 0644)
	require.NoError(t, err)
	err = os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	ts := newTestServer()
	uri := uri.URI(fmt.Sprintf("file://%s", mainPath))

	_, err = ts.openAndWait(uri, mainContent)
	require.NoError(t, err)

	hover, err := ts.hover(uri, 7)
	require.NoError(t, err)
	require.NotNil(t, hover)

	hoverContent := hoverContent(hover)
	t.Logf("Hover content:\n%s", hoverContent)

	assert.Contains(t, hoverContent, "Assets:Cash")
	assert.Contains(t, hoverContent, "115.10", "balance should be correct even with CRLF included file")
	assert.Contains(t, hoverContent, "Postings: 8", "should count all 8 postings even with CRLF included file")
}

func TestIntegration_Issue18_WorkspaceFolderMode(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	// CRLF included file (as on Windows)
	recordsContent := "2026-01-01 Cafe\r\n    Assets:Cash  -25.70 USD\r\n    Assets:Cash  -9.00 USD\r\n    Expenses:Occasions  25.70 USD\r\n    Expenses:Occasions  9.00 USD\r\n\r\n2026-01-01 Payee1\r\n    Assets:Cash  -6.20 USD\r\n    Expenses:Occasions  6.20 USD\r\n"

	mainContent := "include records.journal\r\n\r\naccount Assets:Cash\r\naccount Expenses:Food\r\naccount Expenses:Occasions\r\n\r\n2026-01-01 Test\r\n    Assets:Cash  -25.70 USD\r\n    Assets:Cash  -18.00 USD\r\n    Expenses:Occasions  25.70 USD\r\n    Expenses:Occasions  9.00 USD\r\n    Expenses:Food  9.00 USD\r\n"

	recordsPath := filepath.Join(tmpDir, "records.journal")
	mainPath := filepath.Join(tmpDir, "main.journal")

	err := os.WriteFile(recordsPath, []byte(recordsContent), 0644)
	require.NoError(t, err)
	err = os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	// Simulate workspace folder mode
	ts := newTestServer()
	_, err = ts.Initialize(context.Background(), &protocol.InitializeParams{
		WorkspaceFoldersInitializeParams: protocol.WorkspaceFoldersInitializeParams{
			WorkspaceFolders: protocol.NewNullable([]protocol.WorkspaceFolder{{URI: uri.URI(fmt.Sprintf("file://%s", tmpDir)), Name: "test"}}),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, ts.workspace)

	err = ts.Initialized(context.Background(), &protocol.InitializedParams{})
	require.NoError(t, err)

	uri := uri.URI(fmt.Sprintf("file://%s", mainPath))

	// Normalize main content for DidOpen (as VS Code does)
	mainContentLF := strings.ReplaceAll(mainContent, "\r\n", "\n")
	_, err = ts.openAndWait(uri, mainContentLF)
	require.NoError(t, err)

	hover, err := ts.hover(uri, 7)
	require.NoError(t, err)
	require.NotNil(t, hover, "hover should not be nil in workspace folder mode")

	hoverContent := hoverContent(hover)
	t.Logf("Hover content:\n%s", hoverContent)

	assert.Contains(t, hoverContent, "Assets:Cash")
	// main: -25.70 + -18.00 = -43.70 (2 postings)
	// records: -25.70 + -9.00 + -6.20 = -40.90 (3 postings)
	// Total: -84.60, 5 postings
	assert.Contains(t, hoverContent, "Postings: 5", "workspace mode should aggregate postings from included files")
}

func TestIntegration_Issue18_GlobIncludeWithSubdirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create subdirectory structure matching user's setup: 2026/2026-Records.journal
	subDir := filepath.Join(tmpDir, "2026")
	err := os.MkdirAll(subDir, 0755)
	require.NoError(t, err)

	recordsContent := `2026-01-01 Cafe
    Assets:Cash  -25.70 USD
    Assets:Cash  -9.00 USD
    Expenses:Occasions  25.70 USD
    Expenses:Occasions  9.00 USD

2026-01-01 Payee1
    Assets:Cash  -6.20 USD
    Expenses:Occasions  6.20 USD

2026-03-17 Payee2
    Assets:Cash  -5.00 USD
    Expenses:Food  5.00 USD

2026-03-17 Payee2
    Assets:Cash  -17.70 USD
    Expenses:Food  17.70 USD

2026-03-18 Payee2
    Assets:Cash  -7.80 USD
    Expenses:Food  7.80 USD
`

	// Use the same glob pattern as the user: 20[1-7][0-9]/20[1-7][0-9]-Records.journal
	mainContent := "include 20[1-7][0-9]/20[1-7][0-9]-Records.journal\n\naccount Assets:Cash\naccount Expenses:Food\naccount Expenses:Occasions\n\n2026-01-01 Test\n    Assets:Cash  -25.70 USD\n    Assets:Cash  -18.00 USD\n    Expenses:Occasions  25.70 USD\n    Expenses:Occasions  9.00 USD\n    Expenses:Food  9.00 USD\n"

	recordsPath := filepath.Join(subDir, "2026-Records.journal")
	mainPath := filepath.Join(tmpDir, "main.journal")

	err = os.WriteFile(recordsPath, []byte(recordsContent), 0644)
	require.NoError(t, err)
	err = os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	ts := newTestServer()
	uri := uri.URI(fmt.Sprintf("file://%s", mainPath))

	_, err = ts.openAndWait(uri, mainContent)
	require.NoError(t, err)

	// Hover over Assets:Cash on line 7 (0-indexed), character 6
	hover, err := ts.hover(uri, 7)
	require.NoError(t, err)
	require.NotNil(t, hover)

	hoverContent := hoverContent(hover)
	t.Logf("Hover content:\n%s", hoverContent)

	assert.Contains(t, hoverContent, "Assets:Cash")
	assert.Contains(t, hoverContent, "115.10", "balance should include all postings from both files")
	assert.Contains(t, hoverContent, "Postings: 8", "should count all 8 postings across both files")
}

func TestIntegration_JournalIncludesRulesFile(t *testing.T) {
	tmpDir := t.TempDir()

	rulesContent := `source data.csv
skip 1
fields date,description,amount
`
	mainContent := `include bank.rules

2024-01-15 grocery store
    expenses:food  $50.00
    assets:cash
`
	rulesPath := filepath.Join(tmpDir, "bank.rules")
	mainPath := filepath.Join(tmpDir, "main.journal")

	require.NoError(t, os.WriteFile(rulesPath, []byte(rulesContent), 0644))
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0644))

	ts := newTestServer()
	uri := uri.URI(fmt.Sprintf("file://%s", mainPath))

	diags, err := ts.openAndWait(uri, mainContent)
	require.NoError(t, err)

	// Should NOT have "unexpected content" errors from journal parser
	for _, d := range diags {
		if strings.Contains(tooltipString(d.Message), "unexpected content") {
			t.Errorf("unexpected journal parser error for included rules file: %s", tooltipString(d.Message))
		}
	}

	// Should have one warning about non-journal include
	var warnings []protocol.Diagnostic
	for _, d := range diags {
		if d.Severity == protocol.DiagnosticSeverityWarning &&
			strings.Contains(tooltipString(d.Message), "not a journal file") {
			warnings = append(warnings, d)
		}
	}
	assert.Len(t, warnings, 1, "expected one warning about non-journal include")
	if len(warnings) > 0 {
		assert.Contains(t, tooltipString(warnings[0].Message), "bank.rules")
	}
}

func TestIntegration_JournalIncludesPricesFile(t *testing.T) {
	tmpDir := t.TempDir()

	pricesContent := `P 2024-01-01 USD  1.25 EUR
P 2024-01-02 EUR  0.85 GBP
`
	mainContent := `include 2026.prices

2024-01-15 grocery store
    expenses:food  $50.00
    assets:cash
`
	pricesPath := filepath.Join(tmpDir, "2026.prices")
	mainPath := filepath.Join(tmpDir, "main.journal")

	require.NoError(t, os.WriteFile(pricesPath, []byte(pricesContent), 0644))
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0644))

	ts := newTestServer()
	uri := uri.URI(fmt.Sprintf("file://%s", mainPath))

	diags, err := ts.openAndWait(uri, mainContent)
	require.NoError(t, err)

	// Should NOT have non-journal include warning
	for _, d := range diags {
		if d.Severity == protocol.DiagnosticSeverityWarning && strings.Contains(tooltipString(d.Message), "not a journal file") {
			t.Errorf("unexpected non-journal warning: %s", tooltipString(d.Message))
		}
	}

	// Should NOT have unexpected parser errors from included prices directives
	for _, d := range diags {
		if strings.Contains(tooltipString(d.Message), "unexpected content") {
			t.Errorf("unexpected content error from included prices file: %s", tooltipString(d.Message))
		}
	}
}
