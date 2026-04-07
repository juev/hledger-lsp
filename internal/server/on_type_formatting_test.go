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

// Regression for issue #21: with default settings, Tab alignment should follow
// the file's natural alignment column (indent + maxAccountLen + 2), not be
// forced to a hardcoded minimum. The previous default MinAlignmentColumn=40
// pushed all amounts to col 39, mismatching files with shorter accounts.
func TestOnTypeFormatting_Tab_DefaultUsesNaturalAlignment(t *testing.T) {
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
