package server

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func (ts *testServer) onTypeFormatting(uri protocol.DocumentURI, line uint32, ch string) ([]protocol.TextEdit, error) {
	params := &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.Position{Line: line, Character: 0},
		Ch:           ch,
	}
	return ts.OnTypeFormatting(context.Background(), params)
}

func TestOnTypeFormatting_AfterTransactionHeader(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")
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
	uri := protocol.DocumentURI("file:///test.journal")
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

	uri := protocol.DocumentURI("file:///test.journal")
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

	uri := protocol.DocumentURI("file:///test.journal")
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

	uri := protocol.DocumentURI("file:///test.journal")
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

func TestOnTypeFormatting_AfterEmptyLine(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")
	content := "2024-01-15 grocery store\n    expenses:food  $50.00\n    assets:cash\n\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 4, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_AfterWhitespaceOnlyLine(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")
	content := "2024-01-15 grocery store\n    expenses:food  $50.00\n    assets:cash\n    \n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 4, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_FirstLine(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")
	content := "\n2024-01-15 test"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 0, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_AfterDirective(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")
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

			uri := protocol.DocumentURI("file:///test.journal")
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
	uri := protocol.DocumentURI("file:///test.journal")
	content := "2024-01-15 grocery store\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 1, "a")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_DocumentNotFound(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///nonexistent.journal")

	edits, err := ts.onTypeFormatting(uri, 1, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_ReplacesEditorAutoIndent(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")
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
	uri := protocol.DocumentURI("file:///test.journal")
	content := "2024-01-15 grocery store\n    "

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 1, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_SkipsNoopEdit_EmptyIndent(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")
	content := "; this is a comment\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 1, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_AfterComment(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")
	content := "; this is a comment\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 1, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_AfterPeriodicTransaction(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")
	content := "~ monthly\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 1, "\n")
	require.NoError(t, err)
	require.Len(t, edits, 1)
	assert.Equal(t, "    ", edits[0].NewText)
}

func TestOnTypeFormatting_AfterAutoPostingRule(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")
	content := "= expenses\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 1, "\n")
	require.NoError(t, err)
	require.Len(t, edits, 1)
	assert.Equal(t, "    ", edits[0].NewText)
}

func TestOnTypeFormatting_AfterIncludeDirective(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")
	content := "include foo.journal\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 1, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_AfterCommodityDirective(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")
	content := "commodity EUR\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 1, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func (ts *testServer) onTypeFormattingTab(uri protocol.DocumentURI, line, character uint32) ([]protocol.TextEdit, error) {
	params := &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.Position{Line: line, Character: character},
		Ch:           "\t",
	}
	return ts.OnTypeFormatting(context.Background(), params)
}

func TestOnTypeFormatting_Tab_OnPostingLine(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")
	content := "2024-01-15 grocery store\n    expenses:food\t\n    assets:cash\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormattingTab(uri, 1, 18)
	require.NoError(t, err)
	require.Len(t, edits, 1)

	assert.Equal(t, uint32(1), edits[0].Range.Start.Line)
	assert.Equal(t, uint32(18), edits[0].Range.Start.Character)
	assert.Equal(t, uint32(18), edits[0].Range.End.Character)
	assert.True(t, len(edits[0].NewText) > 0)
	assert.True(t, strings.TrimSpace(edits[0].NewText) == "")
}

func TestOnTypeFormatting_Tab_NotOnPostingLine(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")
	content := "2024-01-15 grocery store\t\n    expenses:food  $50.00\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormattingTab(uri, 0, 25)
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_Tab_PastAlignmentColumn(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")
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

	uri := protocol.DocumentURI("file:///test.journal")
	content := "2024-01-15 grocery store\n    expenses:food:groceries:organic\t\n    assets:cash\n\n2024-01-16 restaurant\n    expenses:food\t\n    assets:cash\n"

	ts.StoreDocument(uri, content)

	edits1, err := ts.onTypeFormattingTab(uri, 1, 36)
	require.NoError(t, err)
	require.Len(t, edits1, 1)

	edits2, err := ts.onTypeFormattingTab(uri, 5, 18)
	require.NoError(t, err)
	require.Len(t, edits2, 1)

	col1 := int(edits1[0].Range.Start.Character) + len(edits1[0].NewText)
	col2 := int(edits2[0].Range.Start.Character) + len(edits2[0].NewText)
	assert.Equal(t, col1, col2, "both postings should align to the same global column")
}

// Emoji-bearing accounts: cursor position from LSP is in UTF-16 units, where
// emoji (codepoint > 0xFFFF) take 2 units. The server's alignment column is
// rune-based. Without conversion, mixing units causes off-by-one for every
// emoji on the line. This test pins down the conversion behavior.
func TestOnTypeFormatting_Tab_EmojiAccountAlignment(t *testing.T) {
	ts := newTestServer()
	settings := ts.getSettings()
	settings.Formatting.MinAlignmentColumn = 30 // explicit floor so alignCol is high
	ts.setSettings(settings)

	uri := protocol.DocumentURI("file:///test.journal")
	// Posting line:    "    🍕:food"
	// Runes:            4 + 1 + 1 + 4 = 10
	// UTF-16 units:     4 + 2 + 1 + 4 = 11 (🍕 = surrogate pair)
	// Bytes:            4 + 4 + 1 + 4 = 13
	content := "2024-01-15 lunch\n    🍕:food\n    assets:cash\n"

	ts.StoreDocument(uri, content)

	// Cursor at end of "    🍕:food" in UTF-16 units = char 11.
	// In runes that's col 10. alignCol with MinAlignmentColumn=30 → floor 29.
	// spacesNeeded = 29 - 10 = 19 (in chars/runes/utf16 since spaces are ASCII).
	edits, err := ts.onTypeFormattingTab(uri, 1, 11)
	require.NoError(t, err)
	require.Len(t, edits, 1)

	assert.Equal(t, 19, len(edits[0].NewText),
		"spacesNeeded must be computed in runes (alignCol=29 - cursorRune=10 = 19), not in mixed units")
}

// Smart alignment detection: when the file already has hand-formatted amounts,
// Tab should respect the existing widest column instead of compressing to formula.
// This is the issue #21 v2 case (after smart detection was added on top of the
// initial default change).
func TestOnTypeFormatting_Tab_RespectsExistingAlignment(t *testing.T) {
	ts := newTestServer()
	// Default settings (MinAlignmentColumn=0, no setSettings).

	uri := protocol.DocumentURI("file:///test.journal")
	// Hand-formatted journal:
	//   "    expenses:food                $50.00"
	//    0123456789012345678901234567890123
	//                                    ^ $ at col 33 (0-indexed)
	//   "    expenses:food:coffee          $5.00"
	//    01234567890123456789012345678901234
	//                                     ^ $ at col 34 (0-indexed)
	// MIN detected = 33 (leftmost = widest amount; both end at col 39 right-aligned).
	// Formula natural = 4 + 20 ("expenses:food:coffee") + 2 = 26.
	// alignCol should be max(detected=33, formula=26) = 33.
	content := "2024-01-15 * grocery store\n    expenses:food                $50.00\n    assets:cash\n\n" +
		"2024-01-16 * coffee shop\n    expenses:food:coffee          $5.00\n    assets:cash\n"

	ts.StoreDocument(uri, content)

	// Tab at end of "    expenses:food" (cursor char 17, before any amount typed).
	edits, err := ts.onTypeFormattingTab(uri, 1, 17)
	require.NoError(t, err)
	require.Len(t, edits, 1)

	endCol := int(edits[0].Range.Start.Character) + len(edits[0].NewText)
	assert.Equal(t, 33, endCol,
		"Tab should align to detected MIN column (33) from existing $50.00, preserving the file's right-edge alignment at col 39")
}

// Regression for the fallback path: when the file has no real amounts (e.g.
// only `\t` placeholders or postings without amounts), DetectExistingAmountColumn
// returns 0 and getAlignmentColumn falls back to the formula-based natural
// calculation. This locks in the issue #21 fix from the previous commit
// (default MinAlignmentColumn=0 → no longer forced to 39).
func TestOnTypeFormatting_Tab_FallbackToFormulaWithoutAmounts(t *testing.T) {
	ts := newTestServer()
	// Intentionally do NOT call setSettings — verify behavior with defaults.

	uri := protocol.DocumentURI("file:///test.journal")
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

	uri := protocol.DocumentURI("file:///test.journal")
	content := "2024-01-15 grocery store\n    expenses:food\t\n    assets:cash\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormattingTab(uri, 1, 18)
	require.NoError(t, err)
	require.Len(t, edits, 1)

	endCol := int(edits[0].Range.Start.Character) + len(edits[0].NewText)
	assert.Equal(t, 49, endCol)
}

func TestOnTypeFormatting_Tab_NoTransactions(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")
	content := "    expenses:food\t\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormattingTab(uri, 0, 18)
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_Tab_DocumentNotFound(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///nonexistent.journal")

	edits, err := ts.onTypeFormattingTab(uri, 1, 10)
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_NewlineBeyondDocumentEnd(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")
	content := "2024-01-15 grocery store\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormatting(uri, 100, "\n")
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestOnTypeFormatting_Tab_BeyondDocumentEnd(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")
	content := "2024-01-15 grocery store\n    expenses:food\n"

	ts.StoreDocument(uri, content)

	edits, err := ts.onTypeFormattingTab(uri, 100, 10)
	require.NoError(t, err)
	assert.Nil(t, edits)
}
