package server

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/lsputil"
)

func TestIntegration_OpenEditDiagnostics(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")

	validContent := `2024-01-15 grocery store
    expenses:food  $50.00
    assets:cash`

	diagnostics, err := ts.openAndWait(uri, validContent)
	require.NoError(t, err)
	assert.Empty(t, diagnostics, "valid journal should have no diagnostics")

	unbalancedContent := `2024-01-15 grocery store
    expenses:food  $50.00
    assets:cash  $30.00`

	diagnostics, err = ts.replaceAndWait(uri, unbalancedContent)
	require.NoError(t, err)
	require.NotEmpty(t, diagnostics, "unbalanced journal should have diagnostics")
	assert.True(t, hasDiagnosticWithSeverity(diagnostics, protocol.DiagnosticSeverityError))

	diagnostics, err = ts.replaceAndWait(uri, validContent)
	require.NoError(t, err)
	assert.Empty(t, diagnostics, "fixed journal should have no diagnostics")
}

func TestIntegration_IncrementalEditing(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")

	content := `2024-01-15 grocery
    expenses:food  $50.00
    assets:cash`

	_, err := ts.openAndWait(uri, content)
	require.NoError(t, err)

	changes := []protocol.TextDocumentContentChangeEvent{
		&protocol.TextDocumentContentChangePartial{
			Range: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 11},
				End:   protocol.Position{Line: 0, Character: 18},
			},
			Text: "supermarket",
		},
	}

	diagnostics, err := ts.changeAndWait(uri, changes)
	require.NoError(t, err)
	assert.Empty(t, diagnostics)

	doc, ok := ts.GetDocument(uri)
	require.True(t, ok)
	assert.Contains(t, doc, "supermarket")
}

func TestIntegration_CompletionAfterEditing(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")

	content := `2024-01-15 grocery store
    expenses:food  $50.00
    assets:cash

2024-01-16 rent
    `

	_, err := ts.openAndWait(uri, content)
	require.NoError(t, err)

	completions, err := ts.completion(uri, 5, 4)
	require.NoError(t, err)
	require.NotNil(t, completions)

	labels := extractCompletionLabels(completions.Items)
	assert.Contains(t, labels, "expenses:food")
	assert.Contains(t, labels, "assets:cash")
}

func TestIntegration_CompletionContextSwitch(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")

	content := `2024-01-15 grocery store
    expenses:food  $50.00
    assets:cash

2024-01-16 `

	_, err := ts.openAndWait(uri, content)
	require.NoError(t, err)

	completions, err := ts.completion(uri, 4, 11)
	require.NoError(t, err)
	require.NotNil(t, completions)

	labels := extractCompletionLabels(completions.Items)
	assert.Contains(t, labels, "grocery store")
}

func TestIntegration_HoverShowsBalance(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")

	content := `2024-01-15 grocery store
    expenses:food  $50.00
    assets:cash

2024-01-16 restaurant
    expenses:food  $25.00
    assets:cash`

	_, err := ts.openAndWait(uri, content)
	require.NoError(t, err)

	hover, err := ts.hover(uri, 1)
	require.NoError(t, err)
	require.NotNil(t, hover)

	contents := hoverContent(hover)
	assert.Contains(t, contents, "expenses:food")
	assert.Contains(t, contents, "75")
}

func TestIntegration_HoverUpdatesAfterEdit(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")

	content := `2024-01-15 grocery store
    expenses:food  $50.00
    assets:cash`

	_, err := ts.openAndWait(uri, content)
	require.NoError(t, err)

	hover, err := ts.hover(uri, 1)
	require.NoError(t, err)
	require.NotNil(t, hover)
	contents := hoverContent(hover)
	assert.Contains(t, contents, "50")

	updatedContent := `2024-01-15 grocery store
    expenses:food  $50.00
    assets:cash

2024-01-16 restaurant
    expenses:food  $30.00
    assets:cash`

	_, err = ts.replaceAndWait(uri, updatedContent)
	require.NoError(t, err)

	hover, err = ts.hover(uri, 1)
	require.NoError(t, err)
	require.NotNil(t, hover)
	contents = hoverContent(hover)
	assert.Contains(t, contents, "80")
}

func TestIntegration_FormatPreservesSemantics(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")

	content := `2024-01-15 grocery store
    expenses:food  $50.00
    assets:cash`

	_, err := ts.openAndWait(uri, content)
	require.NoError(t, err)

	edits, err := ts.format(uri)
	require.NoError(t, err)
	require.NotNil(t, edits)

	formattedContent := applyTextEdits(content, edits)

	assert.Contains(t, formattedContent, "2024-01-15")
	assert.Contains(t, formattedContent, "grocery store")
	assert.Contains(t, formattedContent, "expenses:food")
	assert.Contains(t, formattedContent, "$50")
	assert.Contains(t, formattedContent, "assets:cash")
}

func TestIntegration_FormatDocumentKeepsBalanceAssertionOnlyIndentation(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")

	content := `2026-05-02 balance check
    资产:微信wx  100 CNY = 100 CNY
    资产:待报销费用bx    = 1800 CNY  ; date:2026-05-02
    equity:opening`

	_, err := ts.openAndWait(uri, content)
	require.NoError(t, err)

	edits, err := ts.format(uri)
	require.NoError(t, err)

	formattedContent := applyTextEdits(content, edits)
	lines := strings.Split(formattedContent, "\n")
	require.Len(t, lines, 4)

	assert.Contains(t, lines[1], "100 CNY = 100 CNY")
	assert.NotContains(t, lines[1], "100 CNY  = 100 CNY")

	account := "资产:待报销费用bx"
	accountIdx := strings.Index(lines[2], account)
	require.NotEqual(t, -1, accountIdx, "formatted line should keep the original account name: %q", lines[2])
	afterAccount := lines[2][accountIdx+len(account):]
	eqIdx := strings.Index(afterAccount, "=")
	require.NotEqual(t, -1, eqIdx, "formatted line should keep the balance assertion: %q", lines[2])
	assert.GreaterOrEqual(t, eqIdx, 2, "balance assertion must remain separated from account by 2+ spaces: %q", lines[2])
	assert.Contains(t, lines[2], "= 1800 CNY  ; date:2026-05-02")
}

func TestIntegration_ErrorRecovery(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")

	content := `2024-01-15 valid transaction
    expenses:food  $50.00
    assets:cash

invalid line here

2024-01-17 another valid
    expenses:rent  $100.00
    assets:bank

2024-01-18 new
    `

	diagnostics, err := ts.openAndWait(uri, content)
	require.NoError(t, err)
	require.NotEmpty(t, diagnostics, "should have parse error")

	completions, err := ts.completion(uri, 11, 4)
	require.NoError(t, err)
	require.NotNil(t, completions)

	labels := extractCompletionLabels(completions.Items)
	assert.Contains(t, labels, "expenses:food")
	assert.Contains(t, labels, "expenses:rent")
}

func TestIntegration_MultipleErrorTypes(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")

	content := `account expenses:declared
account assets:declared

2024-01-15 transaction with undeclared account
    custom:undeclared  $50.00
    assets:declared

2024-01-16 unbalanced transaction
    expenses:declared  $100.00
    assets:declared  $50.00`

	diagnostics, err := ts.openAndWait(uri, content)
	require.NoError(t, err)
	require.NotEmpty(t, diagnostics, "should have diagnostics")

	hasError := false
	hasWarning := false
	for _, d := range diagnostics {
		if d.Severity == protocol.DiagnosticSeverityError {
			hasError = true
		}
		if d.Severity == protocol.DiagnosticSeverityWarning {
			hasWarning = true
		}
	}
	assert.True(t, hasError, "should have balance error")
	assert.True(t, hasWarning, "should have undeclared account warning")
}

func TestIntegration_DocumentSymbols(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")

	content := `account expenses:food

2024-01-15 grocery store
    expenses:food  $50.00
    assets:cash

2024-01-16 restaurant
    expenses:food  $25.00
    assets:cash`

	_, err := ts.openAndWait(uri, content)
	require.NoError(t, err)

	params := &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}
	symbols, err := ts.DocumentSymbol(context.Background(), params)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(symbols), 2, "month group + account directive")
}

func TestIntegration_SemanticTokens(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///test.journal")

	content := `2024-01-15 grocery store
    expenses:food  $50.00
    assets:cash`

	_, err := ts.openAndWait(uri, content)
	require.NoError(t, err)

	params := &protocol.SemanticTokensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}
	tokens, err := ts.SemanticTokensFull(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, tokens)
	assert.NotEmpty(t, tokens.Data)
}

func applyTextEdits(content string, edits []protocol.TextEdit) string {
	if len(edits) == 0 {
		return content
	}

	for i := len(edits) - 1; i >= 0; i-- {
		edit := edits[i]
		lines := strings.Split(content, "\n")

		startLine := int(edit.Range.Start.Line)
		endLine := int(edit.Range.End.Line)

		if startLine >= len(lines) {
			continue
		}
		if endLine >= len(lines) {
			endLine = len(lines) - 1
		}

		startByte := lsputil.UTF16OffsetToByteOffset(lines[startLine], int(edit.Range.Start.Character))
		if startByte > len(lines[startLine]) {
			startByte = len(lines[startLine])
		}
		endChar := int(edit.Range.End.Character)
		if endLine != int(edit.Range.End.Line) {
			// end clamped above; take whole line
			endChar = lsputil.UTF16Len(lines[endLine])
		}
		endByte := lsputil.UTF16OffsetToByteOffset(lines[endLine], endChar)
		if endByte > len(lines[endLine]) {
			endByte = len(lines[endLine])
		}

		before := lines[startLine][:startByte]
		after := lines[endLine][endByte:]

		newLines := strings.Split(edit.NewText, "\n")
		newLines[0] = before + newLines[0]
		newLines[len(newLines)-1] = newLines[len(newLines)-1] + after

		result := make([]string, 0, startLine+len(newLines)+(len(lines)-endLine-1))
		result = append(result, lines[:startLine]...)
		result = append(result, newLines...)
		if endLine+1 < len(lines) {
			result = append(result, lines[endLine+1:]...)
		}

		content = strings.Join(result, "\n")
	}

	return content
}

// TestIntegration_QuotedCommodityDiagnostics_Issue199 pins the end-to-end
// behavior reported in hledger-vscode issue #199: through the full server
// pipeline (DidOpen -> normalize -> analyze -> publish), a declared quoted
// commodity with a dot ("TEST.A") produces no diagnostic, while an undeclared
// quoted commodity with a space ("TEST B") is reported. It also guards the
// diagnostic identity (source "hledger-lsp", code "UNDECLARED_COMMODITY"),
// which distinguishes our LSP from the hledger CLI's own check output.
func TestIntegration_QuotedCommodityDiagnostics_Issue199(t *testing.T) {
	ts := newTestServer()
	uri := uri.URI("file:///issue199.journal")

	content := `account monies
account assets:broker:TEST.A
account assets:broker:TEST.B

commodity $
commodity "TEST.A"
; commodity "TEST B"

2026-01-01 Broker | Test
    assets:broker:TEST.A    +  0.000250 "TEST.A" @@ +$104.24
    monies                  -$104.24

2026-01-01 Broker | Test
    assets:broker:TEST.B    +  0.000250 "TEST B" @@ +$104.24
    monies                  -$104.24`

	diagnostics, err := ts.openAndWait(uri, content)
	require.NoError(t, err)

	var testBDiag *protocol.Diagnostic
	for i, d := range diagnostics {
		assert.NotContains(t, tooltipString(d.Message), "TEST.A",
			"declared quoted commodity TEST.A must not be flagged")
		if strings.Contains(tooltipString(d.Message), "TEST B") {
			testBDiag = &diagnostics[i]
		}
	}

	require.NotNil(t, testBDiag, "undeclared quoted commodity TEST B must be reported")
	assert.Equal(t, "commodity 'TEST B' has no directive", tooltipString(testBDiag.Message))
	assert.Equal(t, "hledger-lsp", optionalString(testBDiag.Source),
		"diagnostic must originate from the LSP, not the hledger CLI")
	assert.Equal(t, "UNDECLARED_COMMODITY", diagnosticCodeString(testBDiag.Code))
}
