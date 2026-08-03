package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

const inferredTestURI = uri.URI("file:///test.journal")

func inferredAmountActions(t *testing.T, ts *testServer, line uint32) []protocol.CodeAction {
	t.Helper()
	docURI := inferredTestURI
	ts.clientCapabilities.supportsCodeActionLiterals = true

	entries, err := ts.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
		Range:        protocol.Range{Start: protocol.Position{Line: line}, End: protocol.Position{Line: line}},
	})
	require.NoError(t, err)

	matched := make([]protocol.CodeAction, 0, 1)
	for _, action := range codeActionEntries(entries) {
		if action.Kind != nil && *action.Kind == insertInferredAmountKind {
			matched = append(matched, action)
		}
	}
	return matched
}

func requireSingleEdit(t *testing.T, action protocol.CodeAction) protocol.TextEdit {
	t.Helper()
	require.NotNil(t, action.Edit)
	edits := action.Edit.Changes[inferredTestURI]
	require.Len(t, edits, 1)
	return edits[0]
}

func TestCodeAction_InsertInferredAmount_AlignsToAmountColumn(t *testing.T) {
	ts := newTestServer()
	ts.cliClient = nil
	docURI := inferredTestURI
	content := "2026-08-03 Good\n    Expenses:Test  -20.50 USD\n    Expenses:Food\n"
	require.NoError(t, ts.openDocument(docURI, content))

	actions := inferredAmountActions(t, ts, 2)
	require.Len(t, actions, 1)
	assert.Equal(t, "Insert inferred amount (20.50 USD)", actions[0].Title)

	edit := requireSingleEdit(t, actions[0])
	assert.Equal(t, protocol.Position{Line: 2, Character: 17}, edit.Range.Start, "replaces from the end of the account name")
	assert.Equal(t, protocol.Position{Line: 2, Character: 17}, edit.Range.End, "up to the end of the line")

	// The sibling posting ends its amount at column 30, so the inserted amount
	// must end there too.
	assert.Equal(t, len("    Expenses:Food")+len(edit.NewText), len("    Expenses:Test  -20.50 USD"))
}

func TestCodeAction_InsertInferredAmount_ReplacesTabAlignmentPadding(t *testing.T) {
	ts := newTestServer()
	ts.cliClient = nil
	docURI := inferredTestURI
	// Trailing spaces are the padding the Tab alignment inserted before the
	// user asked for the amount.
	content := "2026-08-03 Good\n    Expenses:Test  -20.50 USD\n    Expenses:Food            \n"
	require.NoError(t, ts.openDocument(docURI, content))

	actions := inferredAmountActions(t, ts, 2)
	require.Len(t, actions, 1)

	edit := requireSingleEdit(t, actions[0])
	assert.Equal(t, uint32(17), edit.Range.Start.Character)
	assert.Equal(t, uint32(len("    Expenses:Food            ")), edit.Range.End.Character, "the stale padding is replaced, not kept")
	assert.Equal(t, len("    Expenses:Test  -20.50 USD"), len("    Expenses:Food"+edit.NewText), "the amount still ends on the document's amount column")
}

func TestCodeAction_InsertInferredAmount_SupportsAmountsWithoutCommodity(t *testing.T) {
	ts := newTestServer()
	ts.cliClient = nil
	docURI := inferredTestURI
	content := "2024-07-23 Метрополитен\n    Расходы:Транспорт  71.00\n    Активы:Сбербанк\n"
	require.NoError(t, ts.openDocument(docURI, content))

	actions := inferredAmountActions(t, ts, 2)
	require.Len(t, actions, 1)
	assert.Equal(t, "Insert inferred amount (-71.00)", actions[0].Title)
	assert.Contains(t, requireSingleEdit(t, actions[0]).NewText, "-71.00")
}

func TestCodeAction_InsertInferredAmount_NotOfferedWhenAmountPresent(t *testing.T) {
	ts := newTestServer()
	ts.cliClient = nil
	docURI := inferredTestURI
	content := "2026-08-03 Good\n    Expenses:Test  -20.50 USD\n    Expenses:Food   20.50 USD\n"
	require.NoError(t, ts.openDocument(docURI, content))

	assert.Empty(t, inferredAmountActions(t, ts, 2))
	assert.Empty(t, inferredAmountActions(t, ts, 1))
}

func TestCodeAction_InsertInferredAmount_SkipsMultiCommodityInference(t *testing.T) {
	ts := newTestServer()
	ts.cliClient = nil
	docURI := inferredTestURI
	content := "2026-08-03 Good\n    Expenses:One  $10\n    Expenses:Two  10 EUR\n    Assets:Cash\n"
	require.NoError(t, ts.openDocument(docURI, content))

	assert.Empty(t, inferredAmountActions(t, ts, 3), "a single edit cannot express a multi-commodity balance")
}

func TestCodeAction_InsertInferredAmount_OnlyForRequestedRange(t *testing.T) {
	ts := newTestServer()
	ts.cliClient = nil
	docURI := inferredTestURI
	content := "2026-08-03 Good\n    Expenses:Test  -20.50 USD\n    Expenses:Food\n"
	require.NoError(t, ts.openDocument(docURI, content))

	assert.Empty(t, inferredAmountActions(t, ts, 0))
	assert.Len(t, inferredAmountActions(t, ts, 2), 1)
}

func TestCodeAction_InsertInferredAmount_IndependentOfInlayHintSettings(t *testing.T) {
	ts := newTestServer()
	ts.cliClient = nil
	settings := ts.getSettings()
	settings.Features.InlayHints = false
	settings.InlayHints.InferredAmounts = false
	ts.setSettings(settings)

	docURI := inferredTestURI
	content := "2026-08-03 Good\n    Expenses:Test  -20.50 USD\n    Expenses:Food\n"
	require.NoError(t, ts.openDocument(docURI, content))

	assert.Len(t, inferredAmountActions(t, ts, 2), 1)
}
