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

// inferredLayout is a journal whose directives reach the transaction under
// test in a particular way, together with the amount the code action has to
// write for it. Every want is what hledger itself reads as balancing the cost.
type inferredLayout struct {
	name string
	// parent holds the directives and includes tx.journal; "" writes them into
	// the transaction file itself.
	parent string
	// directives are written above the transaction in the transaction file.
	directives string
	// postings are the transaction postings, the one to be inferred first.
	postings string
	want     string
	quantity string
}

func inferredLayouts() []inferredLayout {
	const eurToUsd = "    a  1 EUR @@ %s\n"
	cost := func(amount string) string { return "    b\n" + fmt.Sprintf(eurToUsd, amount) }
	return []inferredLayout{
		{name: "decimal-mark directive in the file, dot commodity format",
			directives: "decimal-mark ,\ncommodity 1,000.00 USD\n", postings: cost("15,00 USD"), want: "-15,00 USD", quantity: "-15"},
		{name: "decimal-mark directive, thousands in the cost",
			directives: "decimal-mark ,\ncommodity 1,000.00 USD\n", postings: cost("1.500,50 USD"), want: "-1.500,50 USD", quantity: "-1500.5"},
		{name: "decimal-mark directive in the parent file",
			parent: "decimal-mark ,\ncommodity 1,000.00 USD\ninclude tx.journal\n", postings: cost("15,00 USD"), want: "-15,00 USD", quantity: "-15"},
		{name: "decimal-mark dot with comma commodity format",
			directives: "decimal-mark .\ncommodity 1.000,00 USD\n", postings: cost("15.00 USD"), want: "-15.00 USD", quantity: "-15"},
		{name: "comma commodity format without directive",
			directives: "commodity 1.000,00 USD\n", postings: cost("15,00 USD"), want: "-15,00 USD", quantity: "-15"},
		{name: "D directive with comma format",
			directives: "D 1.000,00 USD\n", postings: cost("15,00 USD"), want: "-15,00 USD", quantity: "-15"},
		{name: "no directive, dot",
			postings: cost("15.50 USD"), want: "-15.50 USD", quantity: "-15.5"},
		{name: "no directive, comma cost keeps the default mark",
			postings: cost("15,50 USD"), want: "-15.50 USD", quantity: "-15.5"},
		{name: "decimal-mark comma without a commodity format",
			directives: "decimal-mark ,\n", postings: cost("15,50 USD"), want: "-15,50 USD", quantity: "-15.5"},
		{name: "same commodity on another posting under decimal-mark",
			directives: "decimal-mark ,\ncommodity 1,000.00 USD\n", postings: "    b\n    a  10,50 USD\n    c  4,50 USD\n", want: "-15,00 USD", quantity: "-15"},
		{name: "unit cost under decimal-mark",
			directives: "decimal-mark ,\ncommodity 1,000.00 USD\n", postings: "    b\n    a  10 EUR @ 1,50 USD\n", want: "-15,00 USD", quantity: "-15"},
		{name: "bare amounts under a D directive with comma mark",
			directives: "D 1.000,00 USD\n", postings: "    b\n    a  10,50\n    c  4,50\n", want: "-15,00", quantity: "-15"},
		{name: "quoted commodity only in the cost",
			directives: "decimal-mark ,\ncommodity 1,000.00 \"ABC 1\"\n", postings: cost("15,00 \"ABC 1\""), want: "-15,00 \"ABC 1\"", quantity: "-15"},
		{name: "quoted Cyrillic commodity only in the cost",
			directives: "decimal-mark ,\ncommodity 1,000.00 \"руб 1\"\n", postings: cost("15,00 \"руб 1\""), want: "-15,00 \"руб 1\"", quantity: "-15"},
		{name: "Cyrillic account names",
			directives: "decimal-mark ,\ncommodity 1,000.00 USD\n", postings: "    расходы:еда\n    счёт  1 EUR @@ 15,00 USD\n", want: "-15,00 USD", quantity: "-15"},
		{name: "CJK account names",
			directives: "decimal-mark ,\ncommodity 1,000.00 USD\n", postings: "    支出:食品\n    账户  1 EUR @@ 15,00 USD\n", want: "-15,00 USD", quantity: "-15"},
	}
}

// applyInferredAmount opens the layout, asks for the inferred-amount action on
// the first posting line and returns the transaction file after applying it,
// together with the root journal that hledger has to read.
func applyInferredAmount(t *testing.T, layout inferredLayout, crlf bool) (root, applied string) {
	t.Helper()
	tx := layout.directives + "2024-01-01 x\n" + layout.postings
	main := "include tx.journal\n"
	if layout.parent != "" {
		main = layout.parent
	}

	dir := t.TempDir()
	writeJournals(t, dir, map[string]string{"main.journal": main, "tx.journal": tx})
	txPath := filepath.Join(dir, "tx.journal")

	text := tx
	if crlf {
		text = strings.ReplaceAll(tx, "\n", "\r\n")
	}
	ts, docURI := formatServer(t, dir, txPath, text)
	ts.cliClient = nil
	ts.clientCapabilities.supportsCodeActionLiterals = true

	line := uint32(strings.Count(layout.directives, "\n") + 1)
	entries, err := ts.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
		Range:        protocol.Range{Start: protocol.Position{Line: line}, End: protocol.Position{Line: line}},
	})
	require.NoError(t, err)
	var edits []protocol.TextEdit
	for _, action := range codeActionEntries(entries) {
		if action.Kind != nil && *action.Kind == insertInferredAmountKind {
			edits = append(edits, action.Edit.Changes[docURI]...)
		}
	}
	require.Len(t, edits, 1, "one inferred-amount edit")

	doc, ok := ts.GetDocument(docURI)
	require.True(t, ok)
	applied = applyTextEdits(doc, edits)
	require.NoError(t, os.WriteFile(txPath, []byte(applied), 0o644))
	return filepath.Join(dir, "main.journal"), applied
}

func TestCodeAction_InsertInferredAmount_UsesDecimalMarkInForce(t *testing.T) {
	for _, layout := range inferredLayouts() {
		for _, crlf := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s crlf=%v", layout.name, crlf), func(t *testing.T) {
				_, applied := applyInferredAmount(t, layout, crlf)
				assert.Contains(t, applied, "  "+layout.want+"\n")
			})
		}
	}
}
