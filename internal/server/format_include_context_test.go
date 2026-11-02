package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/lsputil"
	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/juev/hledger-lsp/internal/textutil"
	"github.com/juev/hledger-lsp/internal/workspace"
)

// formatServer opens path in a server whose workspace is rooted at dir, the way
// an editor does, with text as the buffer. text may differ from the file on disk.
func formatServer(t *testing.T, dir, path, text string) (*testServer, uri.URI) {
	t.Helper()
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	ts := newTestServer()
	ts.loader = include.NewLoader()
	ts.workspace = workspace.NewWorkspace(dir, ts.loader)
	require.NoError(t, ts.workspace.Initialize())

	docURI := uri.File(path)
	ts.documents.Store(docURI, textutil.NormalizeLineEndings(text))
	return ts, docURI
}

func writeJournals(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
}

func formatted(t *testing.T, ts *testServer, docURI uri.URI) string {
	t.Helper()
	doc, ok := ts.GetDocument(docURI)
	require.True(t, ok)
	edits, err := ts.Format(context.Background(), &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
	})
	require.NoError(t, err)
	return applyTextEdits(doc, edits)
}

// hledger reads `15,000` as fifteen thousand when an earlier include declares a
// `.` decimal mark for the commodity. The formatter rewrites numbers from their
// parsed value, so reading the buffer without that context changed the amount.
func TestFormat_ReadsAmountsWithContextFromSiblingInclude(t *testing.T) {
	tx := "2026-10-01 redemption\n    a  42.67 USD\n    b  -42.670 FDRXX  @@  42.67 USD  =  15,000 FDRXX\n"

	tests := []struct {
		name      string
		directive string
		text      string
		want      string
	}{
		{"balance assertion", "commodity 0,000.000 FDRXX\n", tx, "= 15,000.000 FDRXX"},
		{"crlf buffer", "commodity 0,000.000 FDRXX\n", strings.ReplaceAll(tx, "\n", "\r\n"), "= 15,000.000 FDRXX"},
		{
			"posting amount", "commodity 1,000.000 FDRXX\n",
			"2026-10-01 x\n    a  1,234 FDRXX\n    b\n", "1,234.000 FDRXX",
		},
		{
			"comma decimal", "commodity 1.000,00 EUR\n",
			"2026-10-01 x\n    a  1.234 EUR\n    b\n", "1.234,00 EUR",
		},
		{
			"cyrillic commodity", "commodity 1,000.00 РУБ\n",
			"2026-10-01 x\n    счёт  1,234 РУБ\n    b\n", "1,234.00 РУБ",
		},
		{
			"cjk commodity", "commodity 1,000.00 元\n",
			"2026-10-01 x\n    資産  1,234 元\n    b\n", "1,234.00 元",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeJournals(t, dir, map[string]string{
				"main.journal":        "include commodities.journal\ninclude tx.journal\n",
				"commodities.journal": tt.directive,
				"tx.journal":          tt.text,
			})

			ts, docURI := formatServer(t, dir, filepath.Join(dir, "tx.journal"), tt.text)
			once := formatted(t, ts, docURI)
			assert.Contains(t, once, tt.want)

			ts.documents.Store(docURI, once)
			assert.Equal(t, once, formatted(t, ts, docURI), "formatting must be idempotent")
		})
	}
}

// The context can arrive through an include in the formatted buffer itself, not
// only from the file's parent.
func TestFormat_ReadsAmountsWithContextFromOwnInclude(t *testing.T) {
	root := "include commodities.journal\n\n2026-10-01 x\n    a  1,234 FDRXX\n    b\n"
	dir := t.TempDir()
	writeJournals(t, dir, map[string]string{
		"main.journal":        root,
		"commodities.journal": "include nested.journal\n",
		"nested.journal":      "commodity 1,000.000 FDRXX\n",
	})

	ts, docURI := formatServer(t, dir, filepath.Join(dir, "main.journal"), root)
	assert.Contains(t, formatted(t, ts, docURI), "1,234.000 FDRXX")
}

// The buffer is what gets formatted, so an amount typed since the last save is
// read with the same context.
func TestFormat_ReadsUnsavedBufferWithIncludeContext(t *testing.T) {
	dir := t.TempDir()
	writeJournals(t, dir, map[string]string{
		"main.journal":        "include commodities.journal\ninclude tx.journal\n",
		"commodities.journal": "commodity 1,000.000 FDRXX\n",
		"tx.journal":          "2026-10-01 x\n    a  1 FDRXX\n    b\n",
	})

	unsaved := "2026-10-01 x\n    a  2,500 FDRXX\n    b\n"
	ts, docURI := formatServer(t, dir, filepath.Join(dir, "tx.journal"), unsaved)
	assert.Contains(t, formatted(t, ts, docURI), "2,500.000 FDRXX")
}

func TestRangeFormat_ReadsAmountsWithIncludeContext(t *testing.T) {
	tx := "2026-10-01 x\n    a  1,234 FDRXX\n    b\n"
	dir := t.TempDir()
	writeJournals(t, dir, map[string]string{
		"main.journal":        "include commodities.journal\ninclude tx.journal\n",
		"commodities.journal": "commodity 1,000.000 FDRXX\n",
		"tx.journal":          tx,
	})

	ts, docURI := formatServer(t, dir, filepath.Join(dir, "tx.journal"), tx)
	edits, err := ts.RangeFormat(context.Background(), &protocol.DocumentRangeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
		Range:        protocol.Range{End: protocol.Position{Line: 3}},
	})
	require.NoError(t, err)
	assert.Contains(t, applyTextEdits(tx, edits), "1,234.000 FDRXX")
}

func TestOnTypeFormatting_ReadsAmountsWithIncludeContext(t *testing.T) {
	tx := "2026-10-01 x\n    a  1,234 FDRXX\n    \n"
	dir := t.TempDir()
	writeJournals(t, dir, map[string]string{
		"main.journal":        "include commodities.journal\ninclude tx.journal\n",
		"commodities.journal": "commodity 1,000.000 FDRXX\n",
		"tx.journal":          tx,
	})

	ts, docURI := formatServer(t, dir, filepath.Join(dir, "tx.journal"), tx)
	edits, err := ts.onTypeFormatting(docURI, 2, "\n")
	require.NoError(t, err)
	assert.Contains(t, applyTextEdits(tx, edits), "1,234.000 FDRXX")
}

// Every feature reads the document through the same parse, so a commodity
// declared in another file settles an ambiguous number for all of them.
func TestFeatures_ReadAmountsWithIncludeContext(t *testing.T) {
	tx := "2026-10-01 inferred\n    a  1,234 FDRXX\n    b\n\n2026-10-02 balanced\n    c  1,234 FDRXX\n    d  -1234 FDRXX\n"
	dir := t.TempDir()
	writeJournals(t, dir, map[string]string{
		"main.journal":        "include commodities.journal\ninclude tx.journal\n",
		"commodities.journal": "commodity 1,000.000 FDRXX\n",
		"tx.journal":          tx,
	})
	path := filepath.Join(dir, "tx.journal")
	ts, docURI := formatServer(t, dir, path, tx)
	id := protocol.TextDocumentIdentifier{URI: docURI}

	t.Run("diagnostics", func(t *testing.T) {
		assert.Empty(t, ts.analyzeWithJournalBalance(docURI, path, tx, true))
	})

	t.Run("inferred amount action", func(t *testing.T) {
		actions := ts.getInferredAmountCodeActions(&protocol.CodeActionParams{
			TextDocument: id,
			Range:        protocol.Range{Start: protocol.Position{Line: 2}, End: protocol.Position{Line: 2, Character: 5}},
		})
		require.Len(t, actions, 1)
		assert.Equal(t, "Insert inferred amount (-1,234.000 FDRXX)", actions[0].Title)
	})

	t.Run("inlay hint", func(t *testing.T) {
		hints, err := ts.InlayHint(context.Background(), &protocol.InlayHintParams{
			TextDocument: id,
			Range:        protocol.Range{End: protocol.Position{Line: 3}},
		})
		require.NoError(t, err)
		require.Len(t, hints, 1)
		assert.Equal(t, protocol.String("= -1,234.000 FDRXX"), hints[0].Label)
	})

	t.Run("hover", func(t *testing.T) {
		hover, err := ts.Hover(context.Background(), &protocol.HoverParams{
			TextDocument: id,
			Position:     protocol.Position{Line: 1, Character: 9},
		})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Contains(t, hoverContent(hover), "1,234.000 FDRXX")
	})
}

func TestBalanceFix_ReadsAmountsWithIncludeContext(t *testing.T) {
	tx := "2026-10-01 x\n    a  1,234 FDRXX\n    b  -1,000 FDRXX\n"
	dir := t.TempDir()
	writeJournals(t, dir, map[string]string{
		"main.journal":        "include commodities.journal\ninclude tx.journal\n",
		"commodities.journal": "commodity 1,000.000 FDRXX\n",
		"tx.journal":          tx,
	})
	ts, docURI := formatServer(t, dir, filepath.Join(dir, "tx.journal"), tx)

	journal, _ := ts.cachedJournal(docURI, tx)
	_, title, ok := buildUnbalancedFix(docURI, tx, lsputil.NewPositionMapper(tx), ts.commodityFormatsForDocument(docURI), 0, &journal.Transactions[0])
	require.True(t, ok)
	assert.Equal(t, "Fix final posting amount to -1,234.000 FDRXX", title)
}

// `1,234` looks like a decimal number, but when it was read as a digit group the
// adjusted number must not copy that shape, or it would mean something else.
func TestAdjustedRawQuantity_DoesNotCopyADigitGroupAsADecimalMark(t *testing.T) {
	journal, errs := parser.Parse("commodity 1000.00 X\n2026-10-01 x\n    a  1,234 X\n    b\n")
	require.Empty(t, errs)

	amount := journal.Transactions[0].Postings[0].Amount
	assert.Equal(t, "1000", adjustedRawQuantity(amount, decimal.RequireFromString("1000")))
	assert.Equal(t, "1000.5", adjustedRawQuantity(amount, decimal.RequireFromString("1000.5")))
}

// A cached parse is only good for the tree it was read under: editing an included
// file changes what the numbers in this document mean.
func TestCachedParse_RereadsWhenIncludeTreeChanges(t *testing.T) {
	tx := "2026-10-01 x\n    a  1,234 FDRXX\n    b\n"
	dir := t.TempDir()
	writeJournals(t, dir, map[string]string{
		"main.journal":        "include commodities.journal\ninclude tx.journal\n",
		"commodities.journal": "commodity 1,000.000 FDRXX\n",
		"tx.journal":          tx,
	})
	ts, docURI := formatServer(t, dir, filepath.Join(dir, "tx.journal"), tx)

	journal, _ := ts.cachedJournal(docURI, tx)
	assert.Equal(t, "1234", journal.Transactions[0].Postings[0].Amount.Quantity.String())

	ts.workspace.MarkFileDirty(filepath.Join(dir, "commodities.journal"), workspace.StaticContent("commodity 1.000,00 FDRXX\n"))

	journal, _ = ts.cachedJournal(docURI, tx)
	assert.Equal(t, "1.234", journal.Transactions[0].Postings[0].Amount.Quantity.String())
}

// With no include tree there is no context to apply, and the number stays the
// way it was written.
func TestFormat_FileOutsideAnyTreeKeepsAmbiguousNumber(t *testing.T) {
	tx := "2026-10-01 x\n    a  1,234 FDRXX\n    b\n"
	ts := newTestServer()
	docURI := uri.File("/nonexistent/tx.journal")
	ts.documents.Store(docURI, tx)
	assert.Contains(t, formatted(t, ts, docURI), "1,234 FDRXX")
}
