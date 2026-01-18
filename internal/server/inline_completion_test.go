package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/analyzer"
)

func TestFindTransactionContext_FirstPostingLine(t *testing.T) {
	content := `2024-01-15 Grocery Store
    `

	ctx := findTransactionContext(content, 1)

	assert.True(t, ctx.InTransaction, "should be inside a transaction")
	assert.Equal(t, 0, ctx.PayeeLine, "payee should be on line 0")
	assert.Equal(t, 0, ctx.PostingIndex, "should be first posting (index 0)")
	assert.Equal(t, "Grocery Store", ctx.CurrentPayee)
}

func TestFindTransactionContext_SecondPostingLine(t *testing.T) {
	content := `2024-01-15 Grocery Store
    expenses:food  $50.00
    `

	ctx := findTransactionContext(content, 2)

	assert.True(t, ctx.InTransaction, "should be inside a transaction")
	assert.Equal(t, 0, ctx.PayeeLine, "payee should be on line 0")
	assert.Equal(t, 1, ctx.PostingIndex, "should be second posting (index 1)")
	assert.Equal(t, "Grocery Store", ctx.CurrentPayee)
}

func TestFindTransactionContext_NotInTransaction(t *testing.T) {
	content := `2024-01-15 Grocery Store
    expenses:food  $50.00
    assets:cash

`

	ctx := findTransactionContext(content, 4)

	assert.False(t, ctx.InTransaction, "should NOT be inside a transaction")
}

func TestFindTransactionContext_EmptyDocument(t *testing.T) {
	content := ``

	ctx := findTransactionContext(content, 0)

	assert.False(t, ctx.InTransaction, "empty document should not be in transaction")
}

func TestFindTransactionContext_MultipleTransactions(t *testing.T) {
	content := `2024-01-15 First Store
    expenses:food  $50.00
    assets:cash

2024-01-16 Second Store
    `

	ctx := findTransactionContext(content, 5)

	assert.True(t, ctx.InTransaction, "should be inside second transaction")
	assert.Equal(t, 4, ctx.PayeeLine, "payee should be on line 4")
	assert.Equal(t, 0, ctx.PostingIndex, "should be first posting of second transaction")
	assert.Equal(t, "Second Store", ctx.CurrentPayee)
}

func TestFindTransactionContext_WithStatus(t *testing.T) {
	content := `2024-01-15 * Grocery Store
    `

	ctx := findTransactionContext(content, 1)

	assert.True(t, ctx.InTransaction)
	assert.Equal(t, "Grocery Store", ctx.CurrentPayee, "should extract payee without status marker")
}

func TestFindTransactionContext_WithPendingStatus(t *testing.T) {
	content := `2024-01-15 ! Pending Store
    `

	ctx := findTransactionContext(content, 1)

	assert.True(t, ctx.InTransaction)
	assert.Equal(t, "Pending Store", ctx.CurrentPayee, "should extract payee without pending marker")
}

func TestFindTransactionContext_TabIndentation(t *testing.T) {
	content := "2024-01-15 Grocery Store\n\texpenses:food  $50.00\n\t"

	ctx := findTransactionContext(content, 2)

	assert.True(t, ctx.InTransaction, "should be inside transaction with tab indentation")
	assert.Equal(t, 0, ctx.PayeeLine, "payee should be on line 0")
	assert.Equal(t, 1, ctx.PostingIndex, "should be second posting (index 1)")
	assert.Equal(t, "Grocery Store", ctx.CurrentPayee)
}

func TestFindTransactionContext_TwoSpaceIndentation(t *testing.T) {
	content := "2024-01-15 Grocery Store\n  expenses:food  $50.00\n  "

	ctx := findTransactionContext(content, 2)

	assert.True(t, ctx.InTransaction, "should be inside transaction with 2-space indentation")
	assert.Equal(t, 0, ctx.PayeeLine, "payee should be on line 0")
	assert.Equal(t, 1, ctx.PostingIndex, "should be second posting (index 1)")
}

func TestInlineCompletion_FirstPostingLine(t *testing.T) {
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
		Position:     protocol.Position{Line: 5, Character: 4},
		Context: InlineCompletionContext{
			TriggerKind: InlineCompletionTriggerAutomatic,
		},
	}

	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.InlineCompletion(context.Background(), paramsJSON)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, len(result.Items) > 0, "should return inline completion items for first posting")
	if len(result.Items) > 0 {
		assert.Contains(t, result.Items[0].InsertText, "expenses:food", "should suggest template from payee history")
	}
}

func TestInlineCompletion_TabIndentation(t *testing.T) {
	srv := NewServer()
	content := "2024-01-10 Grocery Store\n\texpenses:food  $50.00\n\tassets:cash\n\n2024-01-15 Grocery Store\n\t"

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := InlineCompletionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.Position{Line: 5, Character: 1},
		Context: InlineCompletionContext{
			TriggerKind: InlineCompletionTriggerAutomatic,
		},
	}

	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.InlineCompletion(context.Background(), paramsJSON)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, len(result.Items) > 0, "should return inline completion with tab indentation")
	if len(result.Items) > 0 {
		assert.Equal(t, uint32(1), result.Items[0].Range.Start.Character,
			"Range should start at actual indentation (1 for tab)")
	}
}

func TestInlineCompletion_SecondPostingLine_NoGhostText(t *testing.T) {
	srv := NewServer()
	content := `2024-01-10 Grocery Store
    expenses:food  $50.00
    assets:cash

2024-01-15 Grocery Store
    expenses:food  $50.00
    `

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := InlineCompletionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.Position{Line: 6, Character: 4},
		Context: InlineCompletionContext{
			TriggerKind: InlineCompletionTriggerAutomatic,
		},
	}

	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.InlineCompletion(context.Background(), paramsJSON)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Empty(t, result.Items, "should NOT show ghost text on second posting line")
}

func TestInlineCompletion_UnknownPayee_NoGhostText(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 Unknown Store
    `

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := InlineCompletionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.Position{Line: 1, Character: 4},
		Context: InlineCompletionContext{
			TriggerKind: InlineCompletionTriggerAutomatic,
		},
	}

	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.InlineCompletion(context.Background(), paramsJSON)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Empty(t, result.Items, "should NOT show ghost text for unknown payee")
}

func TestInlineCompletion_NotInTransaction(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 Grocery Store
    expenses:food  $50.00
    assets:cash

`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := InlineCompletionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.Position{Line: 4, Character: 0},
		Context: InlineCompletionContext{
			TriggerKind: InlineCompletionTriggerAutomatic,
		},
	}

	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.InlineCompletion(context.Background(), paramsJSON)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Empty(t, result.Items, "should NOT show ghost text outside transaction")
}

func TestInlineCompletion_LineWithContent_NoGhostText(t *testing.T) {
	srv := NewServer()
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

	assert.Empty(t, result.Items, "should NOT show ghost text when line has content")
}

func TestInlineCompletion_TemplateFormat(t *testing.T) {
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
		Position:     protocol.Position{Line: 5, Character: 4},
		Context: InlineCompletionContext{
			TriggerKind: InlineCompletionTriggerAutomatic,
		},
	}

	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := srv.InlineCompletion(context.Background(), paramsJSON)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.True(t, len(result.Items) > 0, "should return inline completion")

	template := result.Items[0].InsertText
	assert.Contains(t, template, "expenses:food", "template should contain first account")
	assert.Contains(t, template, "$50.00", "template should contain amount")
	assert.Contains(t, template, "assets:cash", "template should contain second account")
}

func TestBuildInlineTemplate_SinglePosting(t *testing.T) {
	postings := []analyzer.PostingTemplate{
		{Account: "expenses:food", Amount: "$50.00"},
	}

	template := buildInlineTemplate(postings)

	assert.Equal(t, "expenses:food  $50.00", template)
}

func TestBuildInlineTemplate_MultiplePostings(t *testing.T) {
	postings := []analyzer.PostingTemplate{
		{Account: "expenses:food", Amount: "$50.00"},
		{Account: "assets:cash", Amount: ""},
	}

	template := buildInlineTemplate(postings)

	assert.Contains(t, template, "expenses:food  $50.00")
	assert.Contains(t, template, "\n    assets:cash")
}

func TestBuildInlineTemplate_CommodityLeftPosition(t *testing.T) {
	postings := []analyzer.PostingTemplate{
		{Account: "expenses:food", Amount: "50.00", Commodity: "€", CommodityLeft: true},
	}

	template := buildInlineTemplate(postings)

	assert.Contains(t, template, "expenses:food  €50.00")
}

func TestBuildInlineTemplate_CommodityRightPosition(t *testing.T) {
	postings := []analyzer.PostingTemplate{
		{Account: "expenses:food", Amount: "100", Commodity: "EUR", CommodityLeft: false},
	}

	template := buildInlineTemplate(postings)

	assert.Contains(t, template, "expenses:food  100 EUR")
}

func TestIsLineIndented(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected bool
	}{
		{"empty line", "", false},
		{"single space", " ", false},
		{"two spaces", "  ", true},
		{"four spaces", "    ", true},
		{"tab", "\t", true},
		{"tab with content", "\texpenses:food", true},
		{"four spaces with content", "    expenses:food", true},
		{"two spaces with content", "  expenses:food", true},
		{"no indent", "expenses:food", false},
		{"mixed spaces and tab", " \t", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isLineIndented(tt.line)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetLineIndentation(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected int
	}{
		{"empty line", "", 0},
		{"no indent", "expenses:food", 0},
		{"single space", " ", 1},
		{"two spaces", "  ", 2},
		{"four spaces", "    ", 4},
		{"four spaces with content", "    expenses:food", 4},
		{"tab", "\t", 1},
		{"tab with content", "\texpenses:food", 1},
		{"mixed spaces", "  \texpenses:food", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getLineIndentation(tt.line)
			assert.Equal(t, tt.expected, result)
		})
	}
}
