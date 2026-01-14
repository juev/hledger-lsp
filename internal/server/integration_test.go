package server

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func TestIntegration_OpenEditDiagnostics(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")

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
	uri := protocol.DocumentURI("file:///test.journal")

	content := `2024-01-15 grocery
    expenses:food  $50.00
    assets:cash`

	_, err := ts.openAndWait(uri, content)
	require.NoError(t, err)

	changes := []protocol.TextDocumentContentChangeEvent{
		{
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
	uri := protocol.DocumentURI("file:///test.journal")

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
	uri := protocol.DocumentURI("file:///test.journal")

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
	uri := protocol.DocumentURI("file:///test.journal")

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

	hoverContent := hover.Contents.Value
	assert.Contains(t, hoverContent, "expenses:food")
	assert.Contains(t, hoverContent, "75")
}

func TestIntegration_HoverUpdatesAfterEdit(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")

	content := `2024-01-15 grocery store
    expenses:food  $50.00
    assets:cash`

	_, err := ts.openAndWait(uri, content)
	require.NoError(t, err)

	hover, err := ts.hover(uri, 1)
	require.NoError(t, err)
	require.NotNil(t, hover)
	hoverContent := hover.Contents.Value
	assert.Contains(t, hoverContent, "50")

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
	hoverContent = hover.Contents.Value
	assert.Contains(t, hoverContent, "80")
}

func TestIntegration_FormatPreservesSemantics(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")

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

func TestIntegration_ErrorRecovery(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")

	content := `2024-01-15 valid transaction
    expenses:food  $50.00
    assets:cash

invalid line here

2024-01-17 another valid
    expenses:rent  $100.00
    assets:bank`

	diagnostics, err := ts.openAndWait(uri, content)
	require.NoError(t, err)
	require.NotEmpty(t, diagnostics, "should have parse error")

	completions, err := ts.completion(uri, 8, 4)
	require.NoError(t, err)
	require.NotNil(t, completions)

	labels := extractCompletionLabels(completions.Items)
	assert.Contains(t, labels, "expenses:food")
	assert.Contains(t, labels, "expenses:rent")
}

func TestIntegration_MultipleErrorTypes(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")

	content := `account expenses:declared
account assets:declared

2024-01-15 transaction with undeclared account
    expenses:undeclared  $50.00
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
	uri := protocol.DocumentURI("file:///test.journal")

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
	require.GreaterOrEqual(t, len(symbols), 3)
}

func TestIntegration_SemanticTokens(t *testing.T) {
	ts := newTestServer()
	uri := protocol.DocumentURI("file:///test.journal")

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
		startChar := int(edit.Range.Start.Character)
		endLine := int(edit.Range.End.Line)
		endChar := int(edit.Range.End.Character)

		if startLine >= len(lines) {
			continue
		}
		if endLine >= len(lines) {
			endLine = len(lines) - 1
			endChar = len(lines[endLine])
		}

		before := ""
		if startLine < len(lines) && startChar <= len(lines[startLine]) {
			before = lines[startLine][:startChar]
		}

		after := ""
		if endLine < len(lines) && endChar <= len(lines[endLine]) {
			after = lines[endLine][endChar:]
		}

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
