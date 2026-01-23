package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func TestInlineCompletion_DisabledByDefault(t *testing.T) {
	srv := NewServer()
	content := `2024-01-10 Grocery Store
    expenses:food  $50.00
    assets:cash

2024-01-15 Grocery Store
`
	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := InlineCompletionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.Position{Line: 5, Character: 0},
		Context: InlineCompletionContext{
			TriggerKind: InlineCompletionTriggerAutomatic,
		},
	}

	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.InlineCompletion(context.Background(), paramsJSON)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Empty(t, result.Items, "inline completion should be disabled by default")
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
	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := InlineCompletionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.Position{Line: 5, Character: 0},
		Context: InlineCompletionContext{
			TriggerKind: InlineCompletionTriggerAutomatic,
		},
	}

	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.InlineCompletion(context.Background(), paramsJSON)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, result.Items, 1, "should return one inline completion item")

	item := result.Items[0]
	assert.Contains(t, item.InsertText, "expenses:food")
	assert.Contains(t, item.InsertText, "assets:cash")
	assert.Contains(t, item.InsertText, "$50.00")
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
	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := InlineCompletionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.Position{Line: 5, Character: 7},
		Context: InlineCompletionContext{
			TriggerKind: InlineCompletionTriggerAutomatic,
		},
	}

	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.InlineCompletion(context.Background(), paramsJSON)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Empty(t, result.Items, "should not show ghost text on non-empty line")
}

func TestInlineCompletion_NotAfterNonTransactionLine(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	srv.setSettings(settings)

	content := `account expenses:food

`
	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := InlineCompletionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.Position{Line: 2, Character: 0},
		Context: InlineCompletionContext{
			TriggerKind: InlineCompletionTriggerAutomatic,
		},
	}

	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.InlineCompletion(context.Background(), paramsJSON)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Empty(t, result.Items, "should not show ghost text after non-transaction line")
}

func TestInlineCompletion_NoTemplateForPayee(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.Features.InlineCompletion = true
	srv.setSettings(settings)

	content := `2024-01-15 New Unknown Payee
`
	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := InlineCompletionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.Position{Line: 1, Character: 0},
		Context: InlineCompletionContext{
			TriggerKind: InlineCompletionTriggerAutomatic,
		},
	}

	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.InlineCompletion(context.Background(), paramsJSON)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Empty(t, result.Items, "should not show ghost text when no template exists for payee")
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
	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := InlineCompletionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.Position{Line: 5, Character: 0},
		Context: InlineCompletionContext{
			TriggerKind: InlineCompletionTriggerAutomatic,
		},
	}

	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.InlineCompletion(context.Background(), paramsJSON)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Items, 1)

	item := result.Items[0]

	assert.Contains(t, item.InsertText, "    expenses:food:coffee", "should have proper indentation")
	assert.Contains(t, item.InsertText, "$5.00", "should include amount")
	assert.Contains(t, item.InsertText, "    assets:wallet", "should include second posting")
}

func TestInlineCompletion_DocumentNotFound(t *testing.T) {
	srv := NewServer()

	params := InlineCompletionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: "file:///nonexistent.journal"},
		Position:     protocol.Position{Line: 0, Character: 0},
		Context: InlineCompletionContext{
			TriggerKind: InlineCompletionTriggerAutomatic,
		},
	}

	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.InlineCompletion(context.Background(), paramsJSON)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Empty(t, result.Items, "should return empty for nonexistent document")
}

func TestInlineCompletion_InvalidParams(t *testing.T) {
	srv := NewServer()

	result, err := srv.InlineCompletion(context.Background(), []byte("invalid json"))

	assert.Error(t, err)
	assert.Nil(t, result)
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
	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := InlineCompletionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.Position{Line: 5, Character: 0},
		Context: InlineCompletionContext{
			TriggerKind: InlineCompletionTriggerAutomatic,
		},
	}

	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.InlineCompletion(context.Background(), paramsJSON)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, result.Items, 1, "should work with status marker (*)")
	assert.Contains(t, result.Items[0].InsertText, "expenses:food")
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
	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := InlineCompletionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.Position{Line: 5, Character: 0},
		Context: InlineCompletionContext{
			TriggerKind: InlineCompletionTriggerAutomatic,
		},
	}

	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.InlineCompletion(context.Background(), paramsJSON)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Items, 1)

	item := result.Items[0]
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
