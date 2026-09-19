package server

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/formatter"
	"github.com/juev/hledger-lsp/internal/lsputil"
)

func (ts *testServer) onTypeFormatting(uri uri.URI, line uint32, ch string) ([]protocol.TextEdit, error) {
	params := &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.Position{Line: line, Character: 0},
		Ch:           ch,
	}
	return ts.OnTypeFormatting(context.Background(), params)
}

func TestOnTypeFormatting_AfterTransactionHeader(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 grocery store\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 1, "\n")
	require.NoError(t, err)
	require.Len(t, edits, 1)
	assert.Equal(t, "    ", edits[0].NewText)
	assert.Equal(t, uint32(1), edits[0].Range.Start.Line)
	assert.Equal(t, uint32(0), edits[0].Range.Start.Character)
}

func TestOnTypeFormatting_AfterPosting(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 grocery store\n    expenses:food  $50.00\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 2, "\n")
	require.NoError(t, err)
	require.Len(t, edits, 1)
	assert.Equal(t, "    ", edits[0].NewText)
}

func TestOnTypeFormatting_EnterFormatsPreviousPostingRightMode(t *testing.T) {
	ts := newTestServer()
	settings := ts.getSettings()
	settings.Formatting.AmountAlignmentMode = "right"
	settings.Formatting.AmountAlignmentColumn = 40
	ts.setSettings(settings)

	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 grocery store\n    expenses:food  12.00 USD\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 2, "\n")
	require.NoError(t, err)
	require.Len(t, edits, 2)

	assert.Equal(t, uint32(1), edits[0].Range.Start.Line)
	assert.Equal(t, 40, len(edits[0].NewText))
	assert.Contains(t, edits[0].NewText, "12.00 USD")
	assert.Equal(t, uint32(2), edits[1].Range.Start.Line)
	assert.Equal(t, "    ", edits[1].NewText)
}

func TestOnTypeFormatting_EnterFormatsPreviousPostingRightModeZeroNoCommodity(t *testing.T) {
	ts := newTestServer()
	settings := ts.getSettings()
	settings.Formatting.AmountAlignmentMode = "right"
	settings.Formatting.AmountAlignmentColumn = 40
	ts.setSettings(settings)

	uri := uri.URI("file:///test.journal")
	content := "2026-01-01 * Testpayee\n" +
		"    Asset:Spending  -1,000. USD\n" +
		"    Expenses:Services  1,000. USD\n" +
		"    Expenses:Fees  0\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 4, "\n")
	require.NoError(t, err)
	require.Len(t, edits, 2)

	assert.Equal(t, uint32(3), edits[0].Range.Start.Line)
	assert.Equal(t, 40, len(edits[0].NewText))
	assert.Contains(t, edits[0].NewText, "0")
	assert.Equal(t, uint32(4), edits[1].Range.Start.Line)
	assert.Equal(t, "    ", edits[1].NewText)
}

func TestOnTypeFormatting_EnterFormatsPreviousPostingDecimalMode(t *testing.T) {
	ts := newTestServer()
	settings := ts.getSettings()
	settings.Formatting.AmountAlignmentMode = "decimal"
	settings.Formatting.AmountAlignmentColumn = 30
	ts.setSettings(settings)

	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 grocery store\n    expenses:food  12.00 USD\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 2, "\n")
	require.NoError(t, err)
	require.Len(t, edits, 2)

	assert.Equal(t, uint32(1), edits[0].Range.Start.Line)
	assert.Equal(t, 30, strings.Index(edits[0].NewText, "."))
	assert.Equal(t, uint32(2), edits[1].Range.Start.Line)
	assert.Equal(t, "    ", edits[1].NewText)
}

func TestOnTypeFormatting_EnterFormatsPreviousPostingLeftMode(t *testing.T) {
	ts := newTestServer()
	settings := ts.getSettings()
	settings.Formatting.AmountAlignmentMode = "left"
	settings.Formatting.AmountAlignmentColumn = 30
	ts.setSettings(settings)

	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 grocery store\n    expenses:food  12.00 USD\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 2, "\n")
	require.NoError(t, err)
	require.Len(t, edits, 2)

	assert.Equal(t, uint32(1), edits[0].Range.Start.Line)
	assert.Equal(t, 30, strings.Index(edits[0].NewText, "12.00 USD"))
	assert.NotEqual(t, 30, len(edits[0].NewText))
	assert.Equal(t, uint32(2), edits[1].Range.Start.Line)
	assert.Equal(t, "    ", edits[1].NewText)
}

func TestOnTypeFormatting_AfterEmptyLine(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 grocery store\n    expenses:food  $50.00\n    assets:cash\n\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 4, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_AfterWhitespaceOnlyLine(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 grocery store\n    expenses:food  $50.00\n    assets:cash\n    \n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 4, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_FirstLine(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "\n2024-01-15 test"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 0, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_AfterDirective(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "account expenses:food\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 1, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_CustomIndentSize(t *testing.T) {
	tests := []struct {
		name       string
		indentSize int
		expected   string
	}{
		{"indent 2", 2, "  "},
		{"indent 8", 8, strings.Repeat(" ", 8)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestServer()
			settings := ts.getSettings()
			settings.Formatting.IndentSize = tt.indentSize
			ts.setSettings(settings)

			uri := uri.URI("file:///test.journal")
			content := "2024-01-15 grocery store\n"

			ts.StoreDocument(uri, content)

			edits, err := ts.onTypeFormatting(uri, 1, "\n")
			require.NoError(t, err)
			require.Len(t, edits, 1)
			assert.Equal(t, tt.expected, edits[0].NewText)
		})
	}
}

func TestOnTypeFormatting_NonNewlineTrigger(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 grocery store\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 1, "a")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_DocumentNotFound(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///nonexistent.journal")

	edits, err := ts.onTypeFormatting(uri, 1, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_ReplacesEditorAutoIndent(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 grocery store\n        "

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 1, "\n")
	require.NoError(t, err)
	require.Len(t, edits, 1)
	assert.Equal(t, "    ", edits[0].NewText)
	assert.Equal(t, uint32(0), edits[0].Range.Start.Character)
	assert.Equal(t, uint32(8), edits[0].Range.End.Character)
}

func TestOnTypeFormatting_SkipsNoopEdit(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 grocery store\n    "

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 1, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_SkipsNoopEdit_EmptyIndent(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "; this is a comment\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 1, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_AfterComment(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "; this is a comment\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 1, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_AfterPeriodicTransaction(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "~ monthly\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 1, "\n")
	require.NoError(t, err)
	require.Len(t, edits, 1)
	assert.Equal(t, "    ", edits[0].NewText)
}

func TestOnTypeFormatting_AfterAutoPostingRule(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "= expenses\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 1, "\n")
	require.NoError(t, err)
	require.Len(t, edits, 1)
	assert.Equal(t, "    ", edits[0].NewText)
}

func TestOnTypeFormatting_AfterIncludeDirective(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "include foo.journal\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 1, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_AfterCommodityDirective(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "commodity EUR\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 1, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

// visualColumnAfterEdit returns the display column the cursor reaches once the
// edit is applied, expanding tabs with the client's tab size. Character counts
// are not comparable on tab-indented lines.
func visualColumnAfterEdit(t *testing.T, content string, edit protocol.TextEdit, tabSize int) int {
	t.Helper()

	lines := strings.Split(content, "\n")
	line := lines[edit.Range.Start.Line]
	byteOffset := lsputil.UTF16OffsetToByteOffset(line, int(edit.Range.Start.Character))
	return formatter.DisplayWidthInLine(line, byteOffset, tabSize) + len(edit.NewText)
}

func (ts *testServer) onTypeFormattingTab(uri uri.URI, line, character uint32) ([]protocol.TextEdit, error) {
	params := &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.Position{Line: line, Character: character},
		Ch:           "\t",
	}
	return ts.OnTypeFormatting(context.Background(), params)
}

func TestOnTypeFormatting_Tab_OnPostingLine(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	// The client does not insert the tab before asking, so the document holds the
	// posting text and the cursor sits right after the account.
	content := "2024-01-15 grocery store\n    expenses:food\n    assets:cash\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormattingTab(uri, 1, 17)
	require.NoError(t, err)
	require.Len(t, edits, 1)

	assert.Equal(t, uint32(1), edits[0].Range.Start.Line)
	assert.Equal(t, uint32(17), edits[0].Range.Start.Character)
	assert.Equal(t, uint32(17), edits[0].Range.End.Character)
	assert.Equal(t, "  ", edits[0].NewText, "two spaces reach the natural column 19 (4+13+2)")
}

func TestOnTypeFormatting_Tab_NotOnPostingLine(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 grocery store\t\n    expenses:food  $50.00\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormattingTab(uri, 0, 25)
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_Tab_PastAlignmentColumn(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 grocery store\n    expenses:food                                      \t\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormattingTab(uri, 1, 55)
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_Tab_UsesGlobalAlignment(t *testing.T) {
	ts := newTestServer()
	settings := ts.getSettings()
	settings.Formatting.MinAlignmentColumn = 0
	ts.setSettings(settings)

	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 grocery store\n    expenses:food:groceries:organic\t\n    assets:cash\n\n2024-01-16 restaurant\n    expenses:food\t\n    assets:cash\n"

	ts.StoreDocument(uri, content)

	edits1, err := ts.onTypeFormattingTab(uri, 1, 36)
	require.NoError(t, err)
	require.Len(t, edits1, 1)

	edits2, err := ts.onTypeFormattingTab(uri, 5, 18)
	require.NoError(t, err)
	require.Len(t, edits2, 1)

	col1 := visualColumnAfterEdit(t, content, edits1[0], 0)
	col2 := visualColumnAfterEdit(t, content, edits2[0], 0)
	assert.Equal(t, col1, col2, "both postings should align to the same display column")
}

func TestOnTypeFormatting_Tab_UsesFixedLeftAlignmentColumn(t *testing.T) {
	ts := newTestServer()
	settings := ts.getSettings()
	settings.Formatting.AmountAlignmentMode = "left"
	settings.Formatting.AmountAlignmentColumn = 30
	ts.setSettings(settings)

	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 grocery store\n    expenses:food\t\n    assets:cash\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormattingTab(uri, 1, 17)
	require.NoError(t, err)
	require.Len(t, edits, 1)

	endCol := int(edits[0].Range.Start.Character) + len(edits[0].NewText)
	assert.Equal(t, 30, endCol)
}

// Emoji- and CJK-bearing accounts: the cursor arrives in UTF-16 units while the
// alignment column counts display cells, so the cursor position must be measured
// in display cells: emoji take two cells (and two UTF-16 units), CJK takes two
// cells but one UTF-16 unit.
func TestOnTypeFormatting_Tab_EmojiAccountAlignment(t *testing.T) {
	ts := newTestServer()
	settings := ts.getSettings()
	settings.Formatting.MinAlignmentColumn = 30 // explicit floor so alignCol is high
	ts.setSettings(settings)

	uri := uri.URI("file:///test.journal")
	// Posting line:    "    🍕:food"
	// Display cells:    4 + 2 + 1 + 4 = 11
	// UTF-16 units:     4 + 2 + 1 + 4 = 11
	// Runes:            4 + 1 + 1 + 4 = 10
	content := "2024-01-15 lunch\n    🍕:food\n    assets:cash\n"

	ts.StoreDocument(uri, content)

	// Cursor at end of "    🍕:food" = UTF-16 char 11 = display cell 11.
	// alignCol with MinAlignmentColumn=30 → floor 29.
	// spacesNeeded = 29 - 11 = 18.
	edits, err := ts.onTypeFormattingTab(uri, 1, 11)
	require.NoError(t, err)
	require.Len(t, edits, 1)

	assert.Equal(t, 18, len(edits[0].NewText),
		"spacesNeeded counts display cells (alignCol 29 − cursor cell 11 = 18)")
}

func TestOnTypeFormatting_Tab_CJKAccountAlignment(t *testing.T) {
	ts := newTestServer()
	settings := ts.getSettings()
	settings.Formatting.MinAlignmentColumn = 30
	ts.setSettings(settings)

	uri := uri.URI("file:///test.journal")
	// Posting line:    "    費用:food"
	// Display cells:    4 + 4 + 1 + 4 = 13 (CJK is two cells wide)
	// UTF-16 units:     4 + 2 + 1 + 4 = 11
	content := "2024-01-15 lunch\n    費用:food\n    assets:cash\n"

	ts.StoreDocument(uri, content)

	// Cursor at end of "    費用:food" = UTF-16 char 11 = display cell 13.
	edits, err := ts.onTypeFormattingTab(uri, 1, 11)
	require.NoError(t, err)
	require.Len(t, edits, 1)

	assert.Equal(t, 16, len(edits[0].NewText),
		"spacesNeeded counts display cells (alignCol 29 − cursor cell 13 = 16), not runes")
}

// Smart alignment detection: when the file already has hand-formatted
// amounts, Tab should respect the most common existing column instead of
// compressing to formula.
func TestOnTypeFormatting_Tab_RespectsExistingAlignment(t *testing.T) {
	ts := newTestServer()
	// Default settings (MinAlignmentColumn=0, no setSettings).

	uri := uri.URI("file:///test.journal")
	lineAt33 := "    expenses:food" + strings.Repeat(" ", 16) + "$50.00"
	lineAt40A := "    assets:cash" + strings.Repeat(" ", 25) + "$-50.00"
	lineAt40B := "    liabilities:card" + strings.Repeat(" ", 20) + "$-5.00"
	content := strings.Join([]string{
		"2024-01-15 * grocery store",
		lineAt33,
		lineAt40A,
		lineAt40B,
	}, "\n")

	ts.StoreDocument(uri, content)

	// Tab at end of "    expenses:food" (cursor char 17, before any amount typed).
	edits, err := ts.onTypeFormattingTab(uri, 1, 17)
	require.NoError(t, err)
	require.Len(t, edits, 1)

	endCol := int(edits[0].Range.Start.Character) + len(edits[0].NewText)
	assert.Equal(t, 40, endCol,
		"Tab should align to modal existing column 40, not MIN column 33")
}

// Regression for the fallback path: when the file has no real amounts (e.g.
// only `\t` placeholders or postings without amounts), DetectExistingAmountColumn
// returns 0 and getAlignmentColumn falls back to the formula-based natural
// calculation. This locks in the issue #21 fix from the previous commit
// (default MinAlignmentColumn=0 → no longer forced to 39).
func TestOnTypeFormatting_Tab_FallbackToFormulaWithoutAmounts(t *testing.T) {
	ts := newTestServer()
	// Intentionally do NOT call setSettings — verify behavior with defaults.

	uri := uri.URI("file:///test.journal")
	// Longest account is "expenses:food:coffee" (20 chars).
	// Natural alignment = indent(4) + maxAccount(20) + minSpaces(2) = 26.
	content := "2024-01-15 grocery store\n    expenses:food\t\n    assets:cash\n\n2024-01-16 coffee shop\n    expenses:food:coffee\t\n    assets:cash\n"

	ts.StoreDocument(uri, content)

	// Tab on the first posting at end of "    expenses:food" (cursor char 17).
	edits, err := ts.onTypeFormattingTab(uri, 1, 17)
	require.NoError(t, err)
	require.Len(t, edits, 1)

	endCol := int(edits[0].Range.Start.Character) + len(edits[0].NewText)
	assert.Equal(t, 26, endCol,
		"with default MinAlignmentColumn=0, alignment should use natural column 26 (4+20+2), not the legacy 39")
}

func TestOnTypeFormatting_Tab_RespectsMinAlignment(t *testing.T) {
	ts := newTestServer()
	settings := ts.getSettings()
	settings.Formatting.MinAlignmentColumn = 50
	ts.setSettings(settings)

	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 grocery store\n    expenses:food\t\n    assets:cash\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormattingTab(uri, 1, 18)
	require.NoError(t, err)
	require.Len(t, edits, 1)

	// The line is tab-indented, so the visual column is the tab-expanded display
	// column of the cursor plus the inserted spaces.
	visualColumn := formatter.DisplayWidthInLine(contentLine(1), 18, 4) + len(edits[0].NewText)
	assert.Equal(t, 49, visualColumn, "the amount must land on the alignment column")
}

// contentLine returns the n-th line (1-based) of the tab-indented fixture used by
// TestOnTypeFormatting_Tab_RespectsMinAlignment.
func contentLine(n int) string {
	lines := strings.Split("2024-01-15 grocery store\n    expenses:food\t\n    assets:cash\n", "\n")
	return lines[n]
}

func TestOnTypeFormatting_Tab_NoTransactions(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "    expenses:food\t\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormattingTab(uri, 0, 18)
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_Tab_DocumentNotFound(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///nonexistent.journal")

	edits, err := ts.onTypeFormattingTab(uri, 1, 10)
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_NewlineBeyondDocumentEnd(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 grocery store\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 100, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_Tab_BeyondDocumentEnd(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")
	content := "2024-01-15 grocery store\n    expenses:food\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormattingTab(uri, 100, 10)
	require.NoError(t, err)
	assert.Nil(t, edits)
}

// onTypeFormattingWithOptions calls on-type formatting with the client's
// formatting options, which carry the indent style.
func (ts *testServer) onTypeFormattingWithOptions(uriValue uri.URI, line uint32, ch string, options protocol.FormattingOptions) ([]protocol.TextEdit, error) {
	params := &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uriValue},
		Position:     protocol.Position{Line: line, Character: 0},
		Ch:           ch,
		Options:      options,
	}
	return ts.OnTypeFormatting(context.Background(), params)
}

// lastIndentEdit returns the edit that writes the new line's indentation.
func lastIndentEdit(t *testing.T, edits []protocol.TextEdit) protocol.TextEdit {
	t.Helper()
	require.NotEmpty(t, edits)
	return edits[len(edits)-1]
}

func TestOnTypeEnter_HonoursClientIndentOptions(t *testing.T) {
	uriValue := uri.URI("file:///test.journal")

	t.Run("tab indentation", func(t *testing.T) {
		ts := newTestServer()
		ts.StoreDocument(uriValue, "2024-01-15 grocery store\n")

		edits, err := ts.onTypeFormattingWithOptions(uriValue, 1, "\n", protocol.FormattingOptions{
			TabSize:      4,
			InsertSpaces: false,
		})
		require.NoError(t, err)
		assert.Equal(t, "\t", lastIndentEdit(t, edits).NewText,
			"a tab-indented document must not be rewritten with spaces")
	})

	t.Run("two-space indentation", func(t *testing.T) {
		ts := newTestServer()
		ts.StoreDocument(uriValue, "2024-01-15 grocery store\n")

		edits, err := ts.onTypeFormattingWithOptions(uriValue, 1, "\n", protocol.FormattingOptions{
			TabSize:      2,
			InsertSpaces: true,
		})
		require.NoError(t, err)
		assert.Equal(t, "  ", lastIndentEdit(t, edits).NewText)
	})

	t.Run("server setting when the client reports nothing", func(t *testing.T) {
		ts := newTestServer()
		ts.StoreDocument(uriValue, "2024-01-15 grocery store\n")

		edits, err := ts.onTypeFormatting(uriValue, 1, "\n")
		require.NoError(t, err)
		assert.Equal(t, "    ", lastIndentEdit(t, edits).NewText)
	})
}

func TestOnTypeEnter_KeepsCommentIndentation(t *testing.T) {
	ts := newTestServer()
	uriValue := uri.URI("file:///comments.journal")
	content := "    ; first comment line\n"

	ts.StoreDocument(uriValue, content)

	edits, err := ts.onTypeFormatting(uriValue, 1, "\n")
	require.NoError(t, err)
	assert.Equal(t, "    ", lastIndentEdit(t, edits).NewText,
		"continuing a comment keeps its indentation instead of collapsing it")
}

func TestOnTypeFormatting_SkipsRulesFiles(t *testing.T) {
	ts := newTestServer()
	rulesURI := uri.URI("file:///import/visa.rules")
	content := "skip 1\nfields date, description, amount\n"

	ts.StoreDocument(rulesURI, content)

	edits, err := ts.onTypeFormatting(rulesURI, 1, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits, "on-type formatting must not touch a CSV rules file")

	tabEdits, err := ts.onTypeFormatting(rulesURI, 1, "\t")
	require.NoError(t, err)
	assert.Nil(t, tabEdits)
}
