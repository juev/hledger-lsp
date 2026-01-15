package server

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
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
	uri := protocol.DocumentURI(fmt.Sprintf("file://%s", mainPath))

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
	uri := protocol.DocumentURI(fmt.Sprintf("file://%s", mainPath))

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
	uri := protocol.DocumentURI(fmt.Sprintf("file://%s", mainPath))

	_, err = ts.openAndWait(uri, mainContent)
	require.NoError(t, err)

	hover, err := ts.hover(uri, 3)
	require.NoError(t, err)
	require.NotNil(t, hover)

	hoverContent := hover.Contents.Value
	assert.Contains(t, hoverContent, "expenses:food")
	assert.Contains(t, hoverContent, "150")
}

func TestIntegration_IncludeCycleDetection(t *testing.T) {
	tmpDir := t.TempDir()

	file1Content := `include file2.journal

2024-01-15 test
    expenses:food  $50.00
    assets:cash`

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
	uri := protocol.DocumentURI(fmt.Sprintf("file://%s", file1Path))

	_, err = ts.openAndWait(uri, file1Content)
	require.NoError(t, err)

	completions, err := ts.completion(uri, 4, 4)
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
	uri := protocol.DocumentURI(fmt.Sprintf("file://%s", mainPath))

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
	uri := protocol.DocumentURI(fmt.Sprintf("file://%s", mainPath))

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
	uri := protocol.DocumentURI(fmt.Sprintf("file://%s", mainPath))

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
	uri := protocol.DocumentURI(fmt.Sprintf("file://%s", mainPath))

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
	uri := protocol.DocumentURI(fmt.Sprintf("file://%s", mainPath))

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
