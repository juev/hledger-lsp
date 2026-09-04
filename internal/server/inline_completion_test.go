package server

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func inlineCompletionParams(documentURI uri.URI, line, character uint32) *protocol.InlineCompletionParams {
	return &protocol.InlineCompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
			Position:     protocol.Position{Line: line, Character: character},
		},
		Context: protocol.InlineCompletionContext{TriggerKind: protocol.InlineCompletionTriggerKindAutomatic},
	}
}

type inlineCompletionItem struct {
	InsertText string
	Range      *protocol.Range
}

func inlineCompletionItems(t *testing.T, result protocol.InlineCompletionResult) []inlineCompletionItem {
	t.Helper()
	list, ok := result.(*protocol.InlineCompletionList)
	require.Truef(t, ok, "result type = %T, want *protocol.InlineCompletionList", result)

	items := make([]inlineCompletionItem, 0, len(list.Items))
	for _, item := range list.Items {
		insertText, ok := item.InsertText.(protocol.String)
		require.Truef(t, ok, "insertText type = %T, want protocol.String", item.InsertText)
		items = append(items, inlineCompletionItem{InsertText: string(insertText), Range: item.Range})
	}
	return items
}

func TestInlineCompletion_EnabledByDefault(t *testing.T) {
	srv := NewServer()
	content := `2024-01-10 Grocery Store
    expenses:food  $50.00
    assets:cash

2024-01-15 Grocery Store
`
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.InlineCompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 5, Character: 0},
		},
		Context: protocol.InlineCompletionContext{TriggerKind: protocol.InlineCompletionTriggerKindAutomatic},
	}

	result, err := srv.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.NotEmpty(t, inlineCompletionItems(t, result), "inline completion should be enabled by default")
}

func TestInlineCompletion_EnabledOnEmptyLineAfterPayee(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	srv.setSettings(settings)

	content := `2024-01-10 Grocery Store
    expenses:food  $50.00
    assets:cash

2024-01-15 Grocery Store
`
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := inlineCompletionParams(uri, 5, 0)

	result, err := srv.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, inlineCompletionItems(t, result), 1, "should return one inline completion item")

	item := inlineCompletionItems(t, result)[0]
	assert.Contains(t, item.InsertText, "expenses:food")
	assert.Contains(t, item.InsertText, "assets:cash")
	assert.Contains(t, item.InsertText, "$50.00")
}

func TestInlineCompletion_AlignedGhostTextRightMode(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	settings.Formatting.AmountAlignmentMode = "right"
	settings.Formatting.AmountAlignmentColumn = 40
	srv.setSettings(settings)

	content := `2024-01-10 Grocery Store
    expenses:food  12.00 USD
    assets:cash

2024-01-15 Grocery Store
`
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := inlineCompletionParams(uri, 5, 0)

	result, err := srv.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, inlineCompletionItems(t, result), 1)

	lines := strings.Split(inlineCompletionItems(t, result)[0].InsertText, "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, 40, len(lines[0]))
	assert.Contains(t, lines[0], "12.00 USD")
}

func TestInlineCompletion_AlignedGhostTextDecimalModeDeterministic(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	settings.Formatting.AmountAlignmentMode = "decimal"
	settings.Formatting.AmountAlignmentColumn = 30
	srv.setSettings(settings)

	content := `2024-01-10 Grocery Store
    expenses:food  12.00 USD
    assets:cash

2024-01-15 Grocery Store
`
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := inlineCompletionParams(uri, 5, 0)

	var first string
	for i := range 5 {
		result, err := srv.InlineCompletion(context.Background(), params)
		require.NoError(t, err)
		require.Len(t, inlineCompletionItems(t, result), 1, "iteration %d", i)

		if i == 0 {
			first = inlineCompletionItems(t, result)[0].InsertText
			lines := strings.Split(first, "\n")
			require.Len(t, lines, 2)
			assert.Equal(t, 30, strings.Index(lines[0], "."))
		} else {
			assert.Equal(t, first, inlineCompletionItems(t, result)[0].InsertText, "iteration %d", i)
		}
	}
}

func TestInlineCompletion_AlignedGhostTextLeftMode(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	settings.Formatting.AmountAlignmentMode = "left"
	settings.Formatting.AmountAlignmentColumn = 30
	srv.setSettings(settings)

	content := `2024-01-10 Grocery Store
    expenses:food  12.00 USD
    assets:cash

2024-01-15 Grocery Store
`
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := inlineCompletionParams(uri, 5, 0)

	result, err := srv.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, inlineCompletionItems(t, result), 1)

	lines := strings.Split(inlineCompletionItems(t, result)[0].InsertText, "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, 30, strings.Index(lines[0], "12.00 USD"))
	assert.NotEqual(t, 30, len(lines[0]))
}

func TestInlineCompletion_AutoDetectColumn_RightMode(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	settings.Formatting.AmountAlignmentMode = "right"
	settings.Formatting.AmountAlignmentColumn = 0
	srv.setSettings(settings)

	// Existing posting "    expenses:food                12.00 USD" ends at
	// column 42 (4 indent + 13 account + 16 padding + 9 amount). Ghost text
	// must reproduce the same end-column.
	content := `2024-01-10 Grocery Store
    expenses:food                12.00 USD
    assets:cash

2024-01-15 Grocery Store
`
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := inlineCompletionParams(uri, 5, 0)

	result, err := srv.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, inlineCompletionItems(t, result), 1)

	lines := strings.Split(inlineCompletionItems(t, result)[0].InsertText, "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, 42, len(lines[0]),
		"ghost text should follow auto-detected end column 42, got: %q", lines[0])
	assert.Contains(t, lines[0], "12.00 USD")
}

func TestInlineCompletion_AutoDetectColumn_DecimalMode(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	settings.Formatting.AmountAlignmentMode = "decimal"
	settings.Formatting.AmountAlignmentColumn = 0
	srv.setSettings(settings)

	// Existing posting "    expenses:food                  12.00 USD":
	// 4 indent + 13 account + 18 padding = 35 (start of "12"). Decimal "."
	// is two chars later → column 37. Ghost text must align decimal at 37.
	content := `2024-01-10 Grocery Store
    expenses:food                  12.00 USD
    assets:cash

2024-01-15 Grocery Store
`
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := inlineCompletionParams(uri, 5, 0)

	result, err := srv.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, inlineCompletionItems(t, result), 1)

	lines := strings.Split(inlineCompletionItems(t, result)[0].InsertText, "\n")
	require.Len(t, lines, 2)
	dotIdx := strings.Index(lines[0], ".")
	assert.Equal(t, 37, dotIdx,
		"decimal point should align to detected column 37, got line: %q (dot at %d)", lines[0], dotIdx)
}

func TestInlineCompletion_AutoDetectColumn_LeftMode(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	settings.Formatting.AmountAlignmentMode = "left"
	settings.Formatting.AmountAlignmentColumn = 0
	srv.setSettings(settings)

	// Existing posting with amount starting at column 30.
	content := `2024-01-10 Grocery Store
    expenses:food             12.00 USD
    assets:cash

2024-01-15 Grocery Store
`
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := inlineCompletionParams(uri, 5, 0)

	result, err := srv.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, inlineCompletionItems(t, result), 1)

	lines := strings.Split(inlineCompletionItems(t, result)[0].InsertText, "\n")
	require.Len(t, lines, 2)
	startIdx := strings.Index(lines[0], "12.00 USD")
	assert.Equal(t, 30, startIdx,
		"amount should start at detected column 30, got line: %q (start at %d)", lines[0], startIdx)
}

func TestInlineCompletion_MixedCommoditiesFallback(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	settings.Formatting.AmountAlignmentMode = "right"
	settings.Formatting.AmountAlignmentColumn = 80
	srv.setSettings(settings)

	// Mix of commodity-left ($10) and commodity-right (12 USD).
	// Per formatter.go:112, mixed positions disable end-column anchoring;
	// ghost text should fall back to start-column alignment, NOT pad to col 80.
	content := `2024-01-10 Grocery Store
    expenses:food  12 USD
    assets:cash    $-10

2024-01-15 Grocery Store
`
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := inlineCompletionParams(uri, 5, 0)

	result, err := srv.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, inlineCompletionItems(t, result), 1)

	lines := strings.Split(inlineCompletionItems(t, result)[0].InsertText, "\n")
	require.GreaterOrEqual(t, len(lines), 1)
	assert.Less(t, len(lines[0]), 80,
		"with mixed commodities formatter falls back to start-col; ghost text must not pad to 80, got: %q", lines[0])
}

func TestInlineCompletion_NonASCIIAccount(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	settings.Formatting.AmountAlignmentMode = "right"
	settings.Formatting.AmountAlignmentColumn = 40
	srv.setSettings(settings)

	// Cyrillic account name. utf8.RuneCountInString must be used (CLAUDE.md Unicode rule).
	content := `2024-01-10 Магазин
    расходы:еда                 12.00 USD
    активы:касса

2024-01-15 Магазин
`
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := inlineCompletionParams(uri, 5, 0)

	result, err := srv.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, inlineCompletionItems(t, result), 1)

	lines := strings.Split(inlineCompletionItems(t, result)[0].InsertText, "\n")
	require.GreaterOrEqual(t, len(lines), 1)
	// Account "расходы:еда" = 11 runes. Amount "12.00 USD" = 9 runes.
	// At column=40 right mode → end at rune column 40.
	endRuneCol := utf8RuneCount(lines[0])
	assert.Equal(t, 40, endRuneCol,
		"non-ASCII account: ghost text rune length should target end column 40, got: %q (runes=%d)", lines[0], endRuneCol)
}

func utf8RuneCount(s string) int {
	count := 0
	for range s {
		count++
	}
	return count
}

func TestInlineCompletion_NotOnNonEmptyLine(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	srv.setSettings(settings)

	content := `2024-01-10 Grocery Store
    expenses:food  $50.00
    assets:cash

2024-01-15 Grocery Store
    exp`
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := inlineCompletionParams(uri, 5, 7)

	result, err := srv.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Empty(t, inlineCompletionItems(t, result), "should not show ghost text on non-empty line")
}

func TestInlineCompletion_NotAfterNonTransactionLine(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	srv.setSettings(settings)

	content := `account expenses:food

`
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := inlineCompletionParams(uri, 2, 0)

	result, err := srv.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Empty(t, inlineCompletionItems(t, result), "should not show ghost text after non-transaction line")
}

func TestInlineCompletion_NoTemplateForPayee(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	srv.setSettings(settings)

	content := `2024-01-15 New Unknown Payee
`
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := inlineCompletionParams(uri, 1, 0)

	result, err := srv.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Empty(t, inlineCompletionItems(t, result), "should not show ghost text when no template exists for payee")
}

func TestInlineCompletion_CorrectInsertText(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	srv.setSettings(settings)

	content := `2024-01-10 Coffee Shop
    expenses:food:coffee  $5.00
    assets:wallet

2024-01-15 Coffee Shop
`
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := inlineCompletionParams(uri, 5, 0)

	result, err := srv.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, inlineCompletionItems(t, result), 1)

	item := inlineCompletionItems(t, result)[0]

	assert.Contains(t, item.InsertText, "    expenses:food:coffee", "should have proper indentation")
	assert.Contains(t, item.InsertText, "$5.00", "should include amount")
	assert.Contains(t, item.InsertText, "    assets:wallet", "should include second posting")
}

func TestInlineCompletion_DocumentNotFound(t *testing.T) {
	srv := NewServer()

	params := inlineCompletionParams("file:///nonexistent.journal", 0, 0)

	result, err := srv.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Empty(t, inlineCompletionItems(t, result), "should return empty for nonexistent document")
}

func TestInlineCompletion_WithStatusMarker(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	srv.setSettings(settings)

	content := `2024-01-10 * Grocery Store
    expenses:food  $50.00
    assets:cash

2024-01-15 * Grocery Store
`
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := inlineCompletionParams(uri, 5, 0)

	result, err := srv.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, inlineCompletionItems(t, result), 1, "should work with status marker (*)")
	assert.Contains(t, inlineCompletionItems(t, result)[0].InsertText, "expenses:food")
}

func TestInlineCompletion_RangeCoversEmptyLine(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	srv.setSettings(settings)

	content := `2024-01-10 Grocery Store
    expenses:food  $50.00
    assets:cash

2024-01-15 Grocery Store
`
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := inlineCompletionParams(uri, 5, 0)

	result, err := srv.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, inlineCompletionItems(t, result), 1)

	item := inlineCompletionItems(t, result)[0]
	require.NotNil(t, item.Range, "should have a range")
	assert.Equal(t, uint32(5), item.Range.Start.Line)
	assert.Equal(t, uint32(0), item.Range.Start.Character)
	assert.Equal(t, uint32(5), item.Range.End.Line)
	assert.Equal(t, uint32(0), item.Range.End.Character)
}

func TestIsTransactionHeaderLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected bool
	}{
		{"simple date with payee", "2024-01-15 Grocery Store", true},
		{"date with status cleared", "2024-01-15 * Grocery Store", true},
		{"date with status pending", "2024-01-15 ! Grocery Store", true},
		{"date with slash separator", "2024/01/15 Grocery Store", true},
		{"date with dot separator", "2024.01.15 Grocery Store", true},
		{"date only no payee", "2024-01-15", false},
		{"posting line", "    expenses:food  $50.00", false},
		{"account directive", "account expenses:food", false},
		{"comment line", "; this is a comment", false},
		{"empty line", "", false},
		{"include directive", "include other.journal", false},
		{"secondary date with payee", "2024-01-15=2024-01-20 Grocery Store", true},
		{"secondary date only", "2024-01-15=2024-01-20", false},
		{"short date MM-DD", "01-15 Grocery Store", true},
		{"short date M-D", "1-5 Coffee Shop", true},
		{"short date only", "01-15", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTransactionHeaderLine(tt.line)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractPayeeFromHeader(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected string
	}{
		{"simple", "2024-01-15 Grocery Store", "Grocery Store"},
		{"with cleared status", "2024-01-15 * Grocery Store", "Grocery Store"},
		{"with pending status", "2024-01-15 ! Grocery Store", "Grocery Store"},
		{"with code", "2024-01-15 (123) Grocery Store", "Grocery Store"},
		{"with status and code", "2024-01-15 * (123) Grocery Store", "Grocery Store"},
		{"slash date", "2024/01/15 Coffee Shop", "Coffee Shop"},
		{"with comment", "2024-01-15 Grocery Store ; comment", "Grocery Store"},
		{"date only", "2024-01-15", ""},
		{"empty", "", ""},
		{"with pipe separator", "2024-01-15 Grocery Store | weekly", "Grocery Store"},
		{"with pipe and comment", "2024-01-15 Payer | note ; tag:value", "Payer"},
		{"with secondary date", "2024-01-15=2024-01-20 Grocery Store", "Grocery Store"},
		{"secondary date with status", "2024-01-15=2024-01-20 * Grocery Store", "Grocery Store"},
		{"short date MM-DD", "01-15 Coffee Shop", "Coffee Shop"},
		{"short date M-D with status", "1-5 * Grocery", "Grocery"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPayeeFromHeader(tt.line)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInlineCompletion_CacheInvalidatedOnSave(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	srv.setSettings(settings)

	uri := uri.URI("file:///test.journal")

	content1 := `2024-01-10 Grocery Store
    expenses:food:groceries  $50.00
    assets:cash

2024-01-15 Grocery Store
`
	srv.documents.Store(uri, content1)

	params := inlineCompletionParams(uri, 5, 0)

	result1, err := srv.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, inlineCompletionItems(t, result1), 1)
	assert.Contains(t, inlineCompletionItems(t, result1)[0].InsertText, "expenses:food:groceries")

	content2 := `2024-01-10 Grocery Store
    expenses:food:supermarket  $100.00
    assets:bank

2024-01-15 Grocery Store
`
	srv.documents.Store(uri, content2)

	err = srv.DidSave(context.Background(), &protocol.DidSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	})
	require.NoError(t, err)

	result2, err := srv.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, inlineCompletionItems(t, result2), 1)
	assert.Contains(t, inlineCompletionItems(t, result2)[0].InsertText, "expenses:food:supermarket",
		"should return updated template after DidSave invalidates cache")
	assert.Contains(t, inlineCompletionItems(t, result2)[0].InsertText, "assets:bank",
		"should return updated second posting after DidSave invalidates cache")
}

func TestInlineCompletion_CacheInvalidatedOnChange(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content1 := `2024-01-10 Grocery Store
    expenses:food:groceries  $50.00
    assets:cash

2024-01-15 Grocery Store
`
	require.NoError(t, ts.openDocument(uri, content1))

	params := inlineCompletionParams(uri, 5, 0)
	result1, err := ts.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, inlineCompletionItems(t, result1), 1)
	assert.Contains(t, inlineCompletionItems(t, result1)[0].InsertText, "expenses:food:groceries")

	content2 := `2024-01-10 Grocery Store
    expenses:food:supermarket  $100.00
    assets:bank

2024-01-15 Grocery Store
`
	content2 = strings.ReplaceAll(content2, "\n", "\r\n")
	require.NoError(t, ts.changeDocument(uri, []protocol.TextDocumentContentChangeEvent{
		&protocol.TextDocumentContentChangeWholeDocument{Text: content2},
	}))

	result2, err := ts.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, inlineCompletionItems(t, result2), 1)
	assert.Contains(t, inlineCompletionItems(t, result2)[0].InsertText, "expenses:food:supermarket")
	assert.Contains(t, inlineCompletionItems(t, result2)[0].InsertText, "assets:bank")
}

func TestInlineCompletion_DeterministicResults(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	srv.setSettings(settings)

	content := `2024-01-10 Grocery Store
    expenses:food  $50.00
    assets:cash

2024-01-15 Grocery Store
`
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := inlineCompletionParams(uri, 5, 0)

	var firstResult string
	for i := range 10 {
		result, err := srv.InlineCompletion(context.Background(), params)
		require.NoError(t, err)
		require.Len(t, inlineCompletionItems(t, result), 1)

		if i == 0 {
			firstResult = inlineCompletionItems(t, result)[0].InsertText
		} else {
			assert.Equal(t, firstResult, inlineCompletionItems(t, result)[0].InsertText,
				"iteration %d: inline completion should return deterministic results", i)
		}
	}
}

func TestInlineCompletion_DeterministicWithMultiplePatterns(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	srv.setSettings(settings)

	// Three transactions for "Store" with different patterns (each pattern appears once)
	// This tests that when multiple patterns have the same count, results are deterministic
	content := `2024-01-10 Store
    expenses:food  $50.00
    assets:cash

2024-01-12 Store
    expenses:food  $100.00
    assets:bank

2024-01-14 Store
    expenses:household  $30.00
    assets:wallet

2024-01-20 Store
`
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := inlineCompletionParams(uri, 13, 0)

	var firstResult string
	for i := range 10 {
		result, err := srv.InlineCompletion(context.Background(), params)
		require.NoError(t, err)
		require.Len(t, inlineCompletionItems(t, result), 1, "iteration %d: should return one completion item", i)

		if i == 0 {
			firstResult = inlineCompletionItems(t, result)[0].InsertText
		} else {
			assert.Equal(t, firstResult, inlineCompletionItems(t, result)[0].InsertText,
				"iteration %d: inline completion should return deterministic results with multiple patterns", i)
		}
	}

	// Verify the last pattern (expenses:household, assets:wallet) is selected
	// because when counts are equal, we pick the one with highest lastIdx
	assert.Contains(t, firstResult, "expenses:household",
		"Should select pattern from last transaction when counts are equal")
	assert.Contains(t, firstResult, "assets:wallet")
}

func TestInlineCompletion_FuzzyPayeeMatch(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	srv.setSettings(settings)

	content := `2024-01-10 Grocery Store
    expenses:food  $50.00
    assets:cash

2024-01-15 Grcery Store
`
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := inlineCompletionParams(uri, 5, 0)

	result, err := srv.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, inlineCompletionItems(t, result), 1, "fuzzy match should find template for misspelled payee")

	assert.Contains(t, inlineCompletionItems(t, result)[0].InsertText, "expenses:food")
	assert.Contains(t, inlineCompletionItems(t, result)[0].InsertText, "assets:cash")
}

func TestInlineCompletion_FuzzyPayeeMatch_NoMatchForUnrelated(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	srv.setSettings(settings)

	content := `2024-01-10 Grocery Store
    expenses:food  $50.00
    assets:cash

2024-01-15 xyz
`
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := inlineCompletionParams(uri, 5, 0)

	result, err := srv.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Empty(t, inlineCompletionItems(t, result), "should not match unrelated short payee")
}

func TestInlineCompletion_FuzzyPayeeMatch_Unicode(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	srv.setSettings(settings)

	content := `2024-01-10 Пятёрочка
    expenses:food  $50.00
    assets:cash

2024-01-15 Пятёрочка
`
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := inlineCompletionParams(uri, 5, 0)

	result, err := srv.InlineCompletion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, inlineCompletionItems(t, result), 1, "exact unicode payee match should work")

	assert.Contains(t, inlineCompletionItems(t, result)[0].InsertText, "expenses:food")
}
