package server

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/include"
)

func TestCompletion_Accounts(t *testing.T) {
	srv := NewServer()
	content := `account assets:cash
account expenses:food

2024-01-15 test
    expenses:food  $50
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 5, Character: 4},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "assets:cash")
	assert.Contains(t, labels, "expenses:food")
}

func TestCompletion_AccountsShowUsageCount(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash

2024-01-16 another
    expenses:food  $30
    assets:cash

2024-01-17 third
    expenses:food  $20
    assets:bank

2024-01-18 new
    `

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 13, Character: 4},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	var foodDetail, cashDetail, bankDetail string
	for _, item := range result.Items {
		switch item.Label {
		case "expenses:food":
			foodDetail = item.Detail
		case "assets:cash":
			cashDetail = item.Detail
		case "assets:bank":
			bankDetail = item.Detail
		}
	}

	assert.Equal(t, "Account (3)", foodDetail, "expenses:food used 3 times")
	assert.Equal(t, "Account (2)", cashDetail, "assets:cash used 2 times")
	assert.Equal(t, "Account (1)", bankDetail, "assets:bank used 1 time")
}

func TestCompletion_PayeesShowUsageCount(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash

2024-01-16 Coffee Shop
    expenses:food  $5
    assets:cash

2024-01-17 Grocery Store
    expenses:food  $30
    assets:cash

2024-01-18 `

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 12, Character: 11},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	var groceryDetail, coffeeDetail string
	for _, item := range result.Items {
		switch item.Label {
		case "Grocery Store":
			groceryDetail = item.Detail
		case "Coffee Shop":
			coffeeDetail = item.Detail
		}
	}

	assert.Equal(t, "Payee (2) + template", groceryDetail, "Grocery Store used 2 times with template")
	assert.Equal(t, "Payee (1) + template", coffeeDetail, "Coffee Shop used 1 time with template")
}

func TestCompletion_AccountsByPrefix(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food:groceries  $30
    expenses:food:restaurant  $20
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 1, Character: 14},
		},
		Context: &protocol.CompletionContext{
			TriggerKind:      protocol.CompletionTriggerKindTriggerCharacter,
			TriggerCharacter: ":",
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "expenses:food:groceries")
	assert.Contains(t, labels, "expenses:food:restaurant")
}

func TestCompletion_Payees(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash

2024-01-16 Coffee Shop
    expenses:food  $5
    assets:cash

2024-01-17 `

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 8, Character: 11},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "Grocery Store")
	assert.Contains(t, labels, "Coffee Shop")
}

func TestCompletion_Commodities(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food  $50
    expenses:rent  EUR 100
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 1, Character: 20},
		},
		Context: &protocol.CompletionContext{
			TriggerKind:      protocol.CompletionTriggerKindTriggerCharacter,
			TriggerCharacter: "@",
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "$")
	assert.Contains(t, labels, "EUR")
}

func TestCompletion_EmptyDocument(t *testing.T) {
	srv := NewServer()
	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), "")

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 0},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	details := extractDetails(result.Items)
	assert.Contains(t, details, "today")
	assert.Contains(t, details, "yesterday")
	assert.Contains(t, details, "tomorrow")
}

func TestCompletion_DocumentNotFound(t *testing.T) {
	srv := NewServer()

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///nonexistent.journal",
			},
			Position: protocol.Position{Line: 0, Character: 0},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Items)
}

func TestCompletion_MaxResults(t *testing.T) {
	srv := NewServer()
	srv.setSettings(serverSettings{
		Completion: completionSettings{MaxResults: 1},
		Limits:     include.DefaultLimits(),
	})
	content := `account assets:cash
account expenses:food

2024-01-15 test
    `

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 4},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Items, 1)
}

func extractLabels(items []protocol.CompletionItem) []string {
	labels := make([]string, len(items))
	for i, item := range items {
		labels[i] = item.Label
	}
	return labels
}

func TestDetermineContext_TagName(t *testing.T) {
	content := `2024-01-15 test  ; `

	ctx := determineCompletionContext(content, protocol.Position{Line: 0, Character: 19}, nil)
	assert.Equal(t, ContextTagName, ctx)
}

func TestDetermineContext_TagName_AfterComma(t *testing.T) {
	content := `2024-01-15 test  ; project:alpha, `

	ctx := determineCompletionContext(content, protocol.Position{Line: 0, Character: 34}, nil)
	assert.Equal(t, ContextTagName, ctx)
}

func TestDetermineContext_TagValue(t *testing.T) {
	content := `2024-01-15 test  ; project:`

	ctx := determineCompletionContext(content, protocol.Position{Line: 0, Character: 27}, nil)
	assert.Equal(t, ContextTagValue, ctx)
}

func TestDetermineContext_TagValue_AfterComma(t *testing.T) {
	content := `2024-01-15 test  ; project:alpha, status:`

	ctx := determineCompletionContext(content, protocol.Position{Line: 0, Character: 41}, nil)
	assert.Equal(t, ContextTagValue, ctx)
}

func TestDetermineContext_Date(t *testing.T) {
	content := ``

	ctx := determineCompletionContext(content, protocol.Position{Line: 0, Character: 0}, nil)
	assert.Equal(t, ContextDate, ctx)
}

func TestDetermineContext_Date_EmptyLine(t *testing.T) {
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash

`

	ctx := determineCompletionContext(content, protocol.Position{Line: 4, Character: 0}, nil)
	assert.Equal(t, ContextDate, ctx)
}

func TestDetermineContext_Date_EmptyLine_SpaceTrigger(t *testing.T) {
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash

`

	completionCtx := &protocol.CompletionContext{
		TriggerKind:      protocol.CompletionTriggerKindTriggerCharacter,
		TriggerCharacter: " ",
	}

	ctx := determineCompletionContext(content, protocol.Position{Line: 4, Character: 0}, completionCtx)
	assert.Equal(t, ContextDate, ctx, "empty line with space trigger should return ContextDate")
}

func TestDetermineContext_Date_EmptyLine_Invoked(t *testing.T) {
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash

`

	completionCtx := &protocol.CompletionContext{
		TriggerKind: protocol.CompletionTriggerKindInvoked,
	}

	ctx := determineCompletionContext(content, protocol.Position{Line: 4, Character: 0}, completionCtx)
	assert.Equal(t, ContextDate, ctx, "empty line with invoked trigger should return ContextDate")
}

func TestCompletion_TagNames(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test  ; project:alpha, status:done
    expenses:food  $50  ; category:groceries
    assets:cash

2024-01-16 another ; `

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 21},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "project")
	assert.Contains(t, labels, "status")
	assert.Contains(t, labels, "category")
}

func TestCompletion_TagNames_NoDuplicates(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test1  ; project:alpha
    expenses:food  $50
    assets:cash

2024-01-16 test2  ; project:beta
    expenses:rent  $1000
    assets:bank

2024-01-17 new ; `

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 8, Character: 17},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	count := 0
	for _, label := range labels {
		if label == "project" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestCompletion_TagValues(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test1  ; project:alpha
    expenses:food  $50
    assets:cash

2024-01-16 test2  ; project:beta
    expenses:rent  $1000
    assets:bank

2024-01-17 new ; project:`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 8, Character: 26},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "alpha")
	assert.Contains(t, labels, "beta")
}

func TestCompletion_TagValues_OnlyForCurrentTag(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test1  ; project:alpha, status:active
    expenses:food  $50
    assets:cash

2024-01-16 test2  ; project:beta, status:done
    expenses:rent  $1000
    assets:bank

2024-01-17 new ; status:`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 8, Character: 24},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "active")
	assert.Contains(t, labels, "done")
	assert.NotContains(t, labels, "alpha")
	assert.NotContains(t, labels, "beta")
}

func TestExtractCurrentTagName(t *testing.T) {
	tests := []struct {
		line     string
		pos      int
		expected string
	}{
		{"; project:", 10, "project"},
		{"; project: val, status:", 23, "status"},
		{"; no tag here", 13, ""},
		{"; project:alpha, category:", 26, "category"},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			result := extractCurrentTagName(tt.line, tt.pos)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractCurrentTagName_Unicode(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		utf16Pos int
		expected string
	}{
		{"cyrillic tag name", "; проект:", 9, "проект"},
		{"cyrillic with value cursor", "; проект:alpha, статус:", 23, "статус"},
		{"japanese tag name", "; 日本語:", 6, "日本語"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractCurrentTagName(tt.line, tt.utf16Pos)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCompletion_Date_BuiltIn(t *testing.T) {
	srv := NewServer()
	content := ``

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 0},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	details := extractDetails(result.Items)

	assert.True(t, len(labels) >= 3, "should have at least 3 date suggestions")
	assert.Contains(t, details, "today")
	assert.Contains(t, details, "yesterday")
	assert.Contains(t, details, "tomorrow")
}

func TestCompletion_Date_Historical(t *testing.T) {
	srv := NewServer()
	content := `2024-01-10 old transaction
    expenses:food  $50
    assets:cash

2024-01-12 another
    expenses:rent  $1000
    assets:cash

`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 8, Character: 0},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "2024-01-12")
	assert.Contains(t, labels, "2024-01-10")
}

func TestCompletion_Date_UsesFileFormat(t *testing.T) {
	srv := NewServer()
	content := `01-10 old transaction
    expenses:food  $50
    assets:cash

`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 0},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	var todayItem protocol.CompletionItem
	for _, item := range result.Items {
		if item.Detail == "today" {
			todayItem = item
			break
		}
	}

	require.NotEmpty(t, todayItem.Label, "should have today completion")
	assert.Regexp(t, `^\d{2}-\d{2}$`, todayItem.Label, "today should use MM-DD format from file")
}

func TestCompletion_Date_UsesSlashSeparator(t *testing.T) {
	srv := NewServer()
	content := `2024/01/10 transaction
    expenses:food  $50
    assets:cash

`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 0},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	var todayItem protocol.CompletionItem
	for _, item := range result.Items {
		if item.Detail == "today" {
			todayItem = item
			break
		}
	}

	require.NotEmpty(t, todayItem.Label, "should have today completion")
	assert.Regexp(t, `^\d{4}/\d{2}/\d{2}$`, todayItem.Label, "today should use YYYY/MM/DD format from file")
}

func TestCompletion_Date_DefaultFormatWhenNoValidDates(t *testing.T) {
	srv := NewServer()
	content := `; Just a comment
account expenses:food
`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 2, Character: 0},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	var todayItem protocol.CompletionItem
	for _, item := range result.Items {
		if item.Detail == "today" {
			todayItem = item
			break
		}
	}

	require.NotEmpty(t, todayItem.Label, "should have today completion")
	assert.Regexp(t, `^\d{4}-\d{2}-\d{2}$`, todayItem.Label, "should use default YYYY-MM-DD format when no dates in file")
}

func TestCompletion_Date_UsesDotSeparator(t *testing.T) {
	srv := NewServer()
	content := `2024.01.10 transaction
    expenses:food  $50
    assets:cash

`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 0},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	var todayItem protocol.CompletionItem
	for _, item := range result.Items {
		if item.Detail == "today" {
			todayItem = item
			break
		}
	}

	require.NotEmpty(t, todayItem.Label, "should have today completion")
	assert.Regexp(t, `^\d{4}\.\d{2}\.\d{2}$`, todayItem.Label, "today should use YYYY.MM.DD format from file")
}

func TestCompletion_Date_WithoutLeadingZeros(t *testing.T) {
	srv := NewServer()
	content := `2024-1-5 transaction
    expenses:food  $50
    assets:cash

`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 0},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	var todayItem protocol.CompletionItem
	for _, item := range result.Items {
		if item.Detail == "today" {
			todayItem = item
			break
		}
	}

	require.NotEmpty(t, todayItem.Label, "should have today completion")
	assert.Regexp(t, `^\d{4}-\d{1,2}-\d{1,2}$`, todayItem.Label, "should allow single digit month/day when file uses them")
}

func extractDetails(items []protocol.CompletionItem) []string {
	details := make([]string, len(items))
	for i, item := range items {
		details[i] = item.Detail
	}
	return details
}

func TestCompletion_Template_ByPayee(t *testing.T) {
	srv := NewServer()
	content := `2024-01-10 Grocery Store
    expenses:food  $50.00
    assets:cash

2024-01-15 `

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 11},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	var groceryItem *protocol.CompletionItem
	for i := range result.Items {
		if result.Items[i].Label == "Grocery Store" {
			groceryItem = &result.Items[i]
			break
		}
	}

	require.NotNil(t, groceryItem, "Grocery Store should be in completion items")
	require.NotEmpty(t, groceryItem.InsertText, "Should have template text")
	assert.Contains(t, groceryItem.InsertText, "expenses:food")
	assert.Contains(t, groceryItem.InsertText, "assets:cash")
}

func TestCompletion_Template_CommodityPosition(t *testing.T) {
	srv := NewServer()
	content := `2024-01-10 Shop EUR
    expenses:food  100 EUR
    assets:cash

2024-01-11 Dollar Store
    expenses:food  $50.00
    assets:cash

2024-01-12 Euro Shop
    expenses:food  €75.00
    assets:cash

2024-01-15 `

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 12, Character: 11},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	var eurItem, dollarItem, euroSymItem *protocol.CompletionItem
	for i := range result.Items {
		if result.Items[i].Label == "Shop EUR" {
			eurItem = &result.Items[i]
		}
		if result.Items[i].Label == "Dollar Store" {
			dollarItem = &result.Items[i]
		}
		if result.Items[i].Label == "Euro Shop" {
			euroSymItem = &result.Items[i]
		}
	}

	require.NotNil(t, eurItem, "Shop EUR should be in completion items")
	require.NotEmpty(t, eurItem.InsertText)
	assert.Contains(t, eurItem.InsertText, "100 EUR")

	require.NotNil(t, dollarItem, "Dollar Store should be in completion items")
	require.NotEmpty(t, dollarItem.InsertText)
	assert.Contains(t, dollarItem.InsertText, "$50.00")

	require.NotNil(t, euroSymItem, "Euro Shop should be in completion items")
	require.NotEmpty(t, euroSymItem.InsertText)
	assert.Contains(t, euroSymItem.InsertText, "€75.00")
}

func TestCompletion_RankingByFrequency(t *testing.T) {
	srv := NewServer()
	content := `2024-01-01 Rare Shop
    expenses:rare  $10
    assets:cash

2024-01-02 Frequent Store
    expenses:food  $20
    assets:cash

2024-01-03 Frequent Store
    expenses:food  $30
    assets:cash

2024-01-04 Frequent Store
    expenses:food  $40
    assets:cash

2024-01-05 `

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 16, Character: 11},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	var frequentItem, rareItem *protocol.CompletionItem
	for i := range result.Items {
		if result.Items[i].Label == "Frequent Store" {
			frequentItem = &result.Items[i]
		}
		if result.Items[i].Label == "Rare Shop" {
			rareItem = &result.Items[i]
		}
	}

	require.NotNil(t, frequentItem, "Frequent Store should be in completion items")
	require.NotNil(t, rareItem, "Rare Shop should be in completion items")

	assert.NotEmpty(t, frequentItem.SortText, "Frequent item should have SortText")
	assert.NotEmpty(t, rareItem.SortText, "Rare item should have SortText")
	assert.True(t, frequentItem.SortText < rareItem.SortText,
		"Frequent item (SortText=%s) should sort before rare item (SortText=%s)",
		frequentItem.SortText, rareItem.SortText)
}

func TestCompletion_AccountsRankingByFrequency(t *testing.T) {
	srv := NewServer()
	content := `2024-01-01 Test1
    expenses:rare  $10
    assets:cash

2024-01-02 Test2
    expenses:food  $20
    assets:cash

2024-01-03 Test3
    expenses:food  $30
    assets:cash

2024-01-04 Test4
    expenses:food  $40
    assets:cash

2024-01-05 Test5
    `

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 17, Character: 4},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	var foodItem, rareItem, cashItem *protocol.CompletionItem
	for i := range result.Items {
		if result.Items[i].Label == "expenses:food" {
			foodItem = &result.Items[i]
		}
		if result.Items[i].Label == "expenses:rare" {
			rareItem = &result.Items[i]
		}
		if result.Items[i].Label == "assets:cash" {
			cashItem = &result.Items[i]
		}
	}

	require.NotNil(t, foodItem, "expenses:food should be in completion items")
	require.NotNil(t, rareItem, "expenses:rare should be in completion items")
	require.NotNil(t, cashItem, "assets:cash should be in completion items")

	assert.NotEmpty(t, foodItem.SortText, "expenses:food should have SortText")
	assert.NotEmpty(t, rareItem.SortText, "expenses:rare should have SortText")

	assert.True(t, foodItem.SortText < rareItem.SortText,
		"Frequent account expenses:food (SortText=%s) should sort before rare expenses:rare (SortText=%s)",
		foodItem.SortText, rareItem.SortText)

	assert.True(t, cashItem.SortText < rareItem.SortText,
		"assets:cash (used 4 times) should sort before expenses:rare (used 1 time)")
}

func TestCompletion_MaxResultsPreservesFrequent(t *testing.T) {
	srv := NewServer()
	srv.setSettings(serverSettings{
		Completion: completionSettings{MaxResults: 2},
		Limits:     include.DefaultLimits(),
	})

	content := `2024-01-01 Rare Shop
    expenses:rare  $10
    assets:cash

2024-01-02 Another Rare
    expenses:rare  $15
    assets:cash

2024-01-03 Frequent Store
    expenses:food  $20
    assets:cash

2024-01-04 Frequent Store
    expenses:food  $30
    assets:cash

2024-01-05 Frequent Store
    expenses:food  $40
    assets:cash

2024-01-06 `

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 20, Character: 11},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.IsIncomplete, "should be incomplete when truncated")
	assert.Len(t, result.Items, 2, "should respect maxResults limit")

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "Frequent Store", "frequent item should be preserved")
}

func TestCompletion_MaxResultsAccountsPreservesFrequent(t *testing.T) {
	srv := NewServer()
	srv.setSettings(serverSettings{
		Completion: completionSettings{MaxResults: 2},
		Limits:     include.DefaultLimits(),
	})

	content := `2024-01-01 Test1
    expenses:rare  $10
    assets:cash

2024-01-02 Test2
    expenses:food  $20
    assets:frequent

2024-01-03 Test3
    expenses:food  $30
    assets:frequent

2024-01-04 Test4
    expenses:food  $40
    assets:frequent

2024-01-05 Test5
    `

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 17, Character: 4},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.IsIncomplete, "should be incomplete when truncated")
	assert.Len(t, result.Items, 2, "should respect maxResults limit")

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "expenses:food", "most frequent account should be preserved")
	assert.Contains(t, labels, "assets:frequent", "second most frequent account should be preserved")
}

func TestCompletion_WorkspaceUsageCount(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()

	mainPath := tmpDir + "/main.journal"
	mainContent := `2024-01-01 Main Transaction 1
    expenses:food  $10
    assets:cash

2024-01-02 Main Transaction 2
    expenses:food  $20
    assets:cash

2024-01-03 Main Transaction 3
    expenses:food  $30
    assets:cash

include transactions.journal`
	err := os.WriteFile(mainPath, []byte(mainContent), 0644)
	require.NoError(t, err)

	txPath := tmpDir + "/transactions.journal"
	txContent := `2024-01-15 Included Transaction
    expenses:food  $50
    assets:bank

2024-01-16 Another
    `
	err = os.WriteFile(txPath, []byte(txContent), 0644)
	require.NoError(t, err)

	srv := NewServer()
	client := &mockClient{}
	srv.SetClient(client)

	initParams := &protocol.InitializeParams{
		RootURI: protocol.DocumentURI("file://" + tmpDir),
	}
	_, err = srv.Initialize(context.Background(), initParams)
	require.NoError(t, err)

	err = srv.workspace.Initialize()
	require.NoError(t, err)

	uri := protocol.DocumentURI("file://" + txPath)
	srv.documents.Store(uri, txContent)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: uri,
			},
			Position: protocol.Position{Line: 5, Character: 4},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	var foodDetail string
	for _, item := range result.Items {
		if item.Label == "expenses:food" {
			foodDetail = item.Detail
			break
		}
	}

	assert.Equal(t, "Account (4)", foodDetail,
		"expenses:food should show count 4 (3 from main + 1 from included), not just 1 from current file")
}

func TestCompletion_PayeeSnippetWithTabstops(t *testing.T) {
	srv := NewServer()

	initParams := &protocol.InitializeParams{
		Capabilities: protocol.ClientCapabilities{
			TextDocument: &protocol.TextDocumentClientCapabilities{
				Completion: &protocol.CompletionTextDocumentClientCapabilities{
					CompletionItem: &protocol.CompletionTextDocumentClientCapabilitiesItem{
						SnippetSupport: true,
					},
				},
			},
		},
	}
	_, err := srv.Initialize(context.Background(), initParams)
	require.NoError(t, err)

	content := `2024-01-10 Grocery Store
    expenses:food  $50.00
    assets:cash

2024-01-15 `

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 11},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	var groceryItem *protocol.CompletionItem
	for i := range result.Items {
		if result.Items[i].Label == "Grocery Store" {
			groceryItem = &result.Items[i]
			break
		}
	}

	require.NotNil(t, groceryItem, "Grocery Store should be in completion items")
	require.NotEmpty(t, groceryItem.InsertText, "Should have template text")

	assert.Equal(t, protocol.InsertTextFormatSnippet, groceryItem.InsertTextFormat,
		"Should use snippet format when client supports it")
	assert.Contains(t, groceryItem.InsertText, "${1:",
		"Snippet should contain tabstops like ${1:...}")
	assert.Contains(t, groceryItem.InsertText, "$0",
		"Snippet should end with $0 for final cursor position")
}

func TestCompletion_IsIncompleteAlwaysTrue(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 2, Character: 4},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.IsIncomplete, "IsIncomplete should always be true to prevent VSCode from re-sorting")
}

func TestCompletion_FilterTextSameForAllItems(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food  $50
    expenses:rent  $100
    assets:cash

2024-01-16 another
    exp`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 6, Character: 7},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, len(result.Items) >= 2, "should have multiple completion items matching 'exp'")

	firstFilterText := result.Items[0].FilterText
	require.NotEmpty(t, firstFilterText, "FilterText should be set")

	for _, item := range result.Items {
		assert.Equal(t, firstFilterText, item.FilterText,
			"All items should have the same FilterText to make VSCode fuzzy scores equal")
	}
}

func TestExtractQueryText_Account(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		line     uint32
		char     uint32
		expected string
	}{
		{
			name:     "partial account name",
			content:  "2024-01-15 test\n    exp",
			line:     1,
			char:     7,
			expected: "exp",
		},
		{
			name:     "empty posting line",
			content:  "2024-01-15 test\n    ",
			line:     1,
			char:     4,
			expected: "",
		},
		{
			name:     "cyrillic partial",
			content:  "2024-01-15 test\n    альа",
			line:     1,
			char:     8,
			expected: "альа",
		},
		{
			name:     "after colon prefix",
			content:  "2024-01-15 test\n    expenses:fo",
			line:     1,
			char:     15,
			expected: "expenses:fo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos := protocol.Position{Line: tt.line, Character: tt.char}
			result := extractQueryText(tt.content, pos, ContextAccount)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractQueryText_Payee(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		line     uint32
		char     uint32
		expected string
	}{
		{
			name:     "partial payee name",
			content:  "2024-01-15 Groc",
			line:     0,
			char:     15,
			expected: "Groc",
		},
		{
			name:     "after date only",
			content:  "2024-01-15 ",
			line:     0,
			char:     11,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos := protocol.Position{Line: tt.line, Character: tt.char}
			result := extractQueryText(tt.content, pos, ContextPayee)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFuzzyMatchScore(t *testing.T) {
	t.Run("returns 0 for no match", func(t *testing.T) {
		score := fuzzyMatchScore("expenses:food", "xyz")
		assert.Equal(t, 0, score)
	})

	t.Run("returns positive for match", func(t *testing.T) {
		score := fuzzyMatchScore("expenses:food", "exp")
		assert.True(t, score > 0)
	})

	t.Run("empty pattern returns high score", func(t *testing.T) {
		score := fuzzyMatchScore("anything", "")
		assert.True(t, score > 0)
	})

	t.Run("consecutive match scores higher than sparse", func(t *testing.T) {
		consecutiveScore := fuzzyMatchScore("Активы:Альфа:Текущий", "альф")
		sparseScore := fuzzyMatchScore("Расходы:Мобильный телефон", "альф")

		assert.True(t, consecutiveScore > sparseScore,
			"consecutive match (%d) should score higher than sparse (%d)",
			consecutiveScore, sparseScore)
	})

	t.Run("word boundary bonus", func(t *testing.T) {
		withBoundary := fuzzyMatchScore("expenses:food", "food")
		withoutBoundary := fuzzyMatchScore("expensesfood", "food")

		assert.True(t, withBoundary > withoutBoundary,
			"word boundary match (%d) should score higher than mid-word (%d)",
			withBoundary, withoutBoundary)
	})

	t.Run("case insensitive", func(t *testing.T) {
		score := fuzzyMatchScore("Expenses:Food", "exp")
		assert.True(t, score > 0)
	})
}

func TestFuzzyMatch_ViaScore(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		pattern     string
		shouldMatch bool
	}{
		{"exact match", "expenses:food", "expenses:food", true},
		{"prefix match", "expenses:food", "exp", true},
		{"fuzzy match latin", "expenses:food", "exfood", true},
		{"fuzzy match cyrillic", "активы:альфа:текущий", "альа", true},
		{"no match", "expenses:food", "xyz", false},
		{"empty pattern matches all", "anything", "", true},
		{"case insensitive", "Expenses:Food", "exp", true},
		{"partial fuzzy", "активы:тинькофф:текущий", "тинт", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := fuzzyMatchScore(tt.text, tt.pattern)
			if tt.shouldMatch {
				assert.True(t, score > 0, "expected match for %q with pattern %q", tt.text, tt.pattern)
			} else {
				assert.Equal(t, 0, score, "expected no match for %q with pattern %q", tt.text, tt.pattern)
			}
		})
	}
}

func TestFilterAndScoreFuzzyMatch(t *testing.T) {
	items := []protocol.CompletionItem{
		{Label: "Активы:Альфа:Текущий"},
		{Label: "Активы:Альфа:Альфа-Счет"},
		{Label: "Активы:Тинькофф:Текущий"},
		{Label: "Расходы:Продукты"},
	}

	t.Run("filters by cyrillic query", func(t *testing.T) {
		scored := filterAndScoreFuzzyMatch(items, "альа")
		filtered := make([]protocol.CompletionItem, len(scored))
		for i, s := range scored {
			filtered[i] = s.item
		}
		labels := extractLabels(filtered)

		assert.Len(t, filtered, 2)
		assert.Contains(t, labels, "Активы:Альфа:Текущий")
		assert.Contains(t, labels, "Активы:Альфа:Альфа-Счет")
	})

	t.Run("empty query returns all", func(t *testing.T) {
		scored := filterAndScoreFuzzyMatch(items, "")
		assert.Len(t, scored, len(items))
	})

	t.Run("no matches returns empty", func(t *testing.T) {
		scored := filterAndScoreFuzzyMatch(items, "xyz")
		assert.Empty(t, scored)
	})
}

func TestCompletion_FiltersAndSortsByFrequency(t *testing.T) {
	srv := NewServer()
	content := `2024-01-01 Test1
    Активы:Альфа:Текущий  100
    Расходы:Продукты

2024-01-02 Test2
    Активы:Альфа:Текущий  200
    Расходы:Продукты

2024-01-03 Test3
    Активы:Альфа:Альфа-Счет  50
    Расходы:Продукты

2024-01-04 Test4
    альа`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 13, Character: 8},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)

	assert.True(t, len(labels) >= 2, "should have at least 2 filtered results")
	assert.Contains(t, labels, "Активы:Альфа:Текущий")
	assert.Contains(t, labels, "Активы:Альфа:Альфа-Счет")
	assert.NotContains(t, labels, "Расходы:Продукты", "should be filtered out")

	var tekushchiyIdx, schetIdx int
	for i, label := range labels {
		if label == "Активы:Альфа:Текущий" {
			tekushchiyIdx = i
		}
		if label == "Активы:Альфа:Альфа-Счет" {
			schetIdx = i
		}
	}
	assert.True(t, tekushchiyIdx < schetIdx,
		"Активы:Альфа:Текущий (2 uses) should come before Активы:Альфа:Альфа-Счет (1 use)")
}

func TestCompletion_ConsecutiveMatchBeforeSparse(t *testing.T) {
	srv := NewServer()
	content := `2024-01-01 Test1
    Расходы:Мобильный телефон  100
    Активы:Банк

2024-01-02 Test2
    Расходы:Мобильный телефон  200
    Активы:Банк

2024-01-03 Test3
    Расходы:Мобильный телефон  300
    Активы:Банк

2024-01-04 Test4
    Активы:Альфа:Текущий  50
    Расходы:Продукты

2024-01-05 Test5
    альф`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 17, Character: 8},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	require.True(t, len(labels) >= 2, "should have at least 2 results")

	alfaIdx, mobileIdx := -1, -1
	for i, label := range labels {
		if label == "Активы:Альфа:Текущий" {
			alfaIdx = i
		}
		if label == "Расходы:Мобильный телефон" {
			mobileIdx = i
		}
	}

	require.NotEqual(t, -1, alfaIdx, "Активы:Альфа:Текущий should be in results")
	require.NotEqual(t, -1, mobileIdx, "Расходы:Мобильный телефон should be in results")

	assert.True(t, alfaIdx < mobileIdx,
		"Активы:Альфа:Текущий (consecutive 'альф') should come before Расходы:Мобильный телефон (sparse match, even with 3x frequency)")
}

// === NEW TESTS FOR COMPLETION FIXES ===

func TestDetermineContext_CommodityInPosting(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		line     uint32
		char     uint32
		expected CompletionContextType
	}{
		{
			name:     "cursor after amount - should be commodity",
			content:  "2024-01-15 test\n    expenses:food  100 ",
			line:     1,
			char:     24,
			expected: ContextCommodity,
		},
		{
			name:     "cursor in commodity",
			content:  "2024-01-15 test\n    expenses:food  100 US",
			line:     1,
			char:     26,
			expected: ContextCommodity,
		},
		{
			name:     "cursor in account - should be account",
			content:  "2024-01-15 test\n    expenses:fo",
			line:     1,
			char:     15,
			expected: ContextAccount,
		},
		{
			name:     "cursor at start of posting - should be account",
			content:  "2024-01-15 test\n    ",
			line:     1,
			char:     4,
			expected: ContextAccount,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos := protocol.Position{Line: tt.line, Character: tt.char}
			ctx := determineCompletionContext(tt.content, pos, nil)
			assert.Equal(t, tt.expected, ctx, "context should be %v but got %v", tt.expected, ctx)
		})
	}
}

func TestDetermineContext_Directive_Account(t *testing.T) {
	content := `account assets:b`

	ctx := determineCompletionContext(content, protocol.Position{Line: 0, Character: 16}, nil)
	assert.Equal(t, ContextAccount, ctx, "directive 'account' should return ContextAccount")
}

func TestDetermineContext_Directive_Commodity(t *testing.T) {
	content := `commodity U`

	ctx := determineCompletionContext(content, protocol.Position{Line: 0, Character: 11}, nil)
	assert.Equal(t, ContextCommodity, ctx, "directive 'commodity' should return ContextCommodity")
}

func TestDetermineContext_Directive_ApplyAccount(t *testing.T) {
	content := `apply account expenses:`

	ctx := determineCompletionContext(content, protocol.Position{Line: 0, Character: 23}, nil)
	assert.Equal(t, ContextAccount, ctx, "directive 'apply account' should return ContextAccount")
}

func TestCompletion_CommodityAfterAmount(t *testing.T) {
	srv := NewServer()
	content := `commodity USD
commodity EUR
commodity RUB

2024-01-15 test
    expenses:food  100 `

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 5, Character: 23},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)

	assert.Contains(t, labels, "USD", "should suggest commodities")
	assert.Contains(t, labels, "EUR", "should suggest commodities")
	assert.Contains(t, labels, "RUB", "should suggest commodities")
	assert.NotContains(t, labels, "expenses:food", "should NOT suggest accounts when in commodity position")
}

func TestCompletion_DirectiveAccount(t *testing.T) {
	srv := NewServer()
	content := `account assets:cash
account expenses:food

account `

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 3, Character: 8},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "assets:cash", "directive 'account' should suggest accounts")
	assert.Contains(t, labels, "expenses:food", "directive 'account' should suggest accounts")
}

func TestCompletion_DirectiveCommodity(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food  100 USD
    assets:cash

commodity U`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 11},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "USD", "directive 'commodity' should suggest commodities")
}

func TestExtractQueryText_Commodity(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		line     uint32
		char     uint32
		expected string
	}{
		{
			name:     "partial commodity after amount",
			content:  "2024-01-15 test\n    expenses:food  100 US",
			line:     1,
			char:     26,
			expected: "US",
		},
		{
			name:     "empty after amount",
			content:  "2024-01-15 test\n    expenses:food  100 ",
			line:     1,
			char:     24,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos := protocol.Position{Line: tt.line, Character: tt.char}
			result := extractQueryText(tt.content, pos, ContextCommodity)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCompletion_TextEditForAccount(t *testing.T) {
	srv := NewServer()
	content := `account assets:cash
account expenses:food

2024-01-15 test
    exp`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 7},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, len(result.Items) > 0, "should have completion items")

	var foodItem *protocol.CompletionItem
	for i := range result.Items {
		if result.Items[i].Label == "expenses:food" {
			foodItem = &result.Items[i]
			break
		}
	}

	require.NotNil(t, foodItem, "expenses:food should be in completion items")
	require.NotNil(t, foodItem.TextEdit, "TextEdit should be set for proper replacement")

	textEdit := foodItem.TextEdit

	assert.Equal(t, uint32(4), textEdit.Range.Start.Line)
	assert.Equal(t, uint32(4), textEdit.Range.Start.Character, "TextEdit should start at column 4 (after indent)")
	assert.Equal(t, uint32(4), textEdit.Range.End.Line)
	assert.Equal(t, uint32(7), textEdit.Range.End.Character, "TextEdit should end at cursor position")
}

// === REFACTORING TESTS ===

func TestFindAmountEnd_Parentheses(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"simple number", "100", 3},
		{"negative in parentheses", "(-50)", 5},
		{"currency prefix with parens", "$(-50)", 6},
		{"positive with currency", "$100", 4},
		{"number with decimals", "100.50", 6},
		{"number with comma", "1,000", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findAmountEnd(tt.input)
			assert.Equal(t, tt.expected, result, "findAmountEnd(%q) should return %d", tt.input, tt.expected)
		})
	}
}

func TestParsePosting(t *testing.T) {
	tests := []struct {
		name            string
		line            string
		expectedIndent  int
		expectedAccount string
		expectedSepIdx  int
		expectedAmount  string
	}{
		{
			name:            "simple posting with amount",
			line:            "    expenses:food  100 USD",
			expectedIndent:  4,
			expectedAccount: "expenses:food",
			expectedSepIdx:  13,
			expectedAmount:  "100 USD",
		},
		{
			name:            "posting without amount",
			line:            "    assets:cash",
			expectedIndent:  4,
			expectedAccount: "assets:cash",
			expectedSepIdx:  -1,
			expectedAmount:  "",
		},
		{
			name:            "tab indent",
			line:            "\texpenses:rent  500",
			expectedIndent:  1,
			expectedAccount: "expenses:rent",
			expectedSepIdx:  13,
			expectedAmount:  "500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := parsePosting(tt.line)
			assert.Equal(t, tt.expectedIndent, parts.indent)
			assert.Equal(t, tt.expectedAccount, parts.account)
			assert.Equal(t, tt.expectedSepIdx, parts.separatorIdx)
			if tt.expectedSepIdx != -1 {
				assert.NotEmpty(t, parts.afterAccount, "afterAccount should be set when separator found")
			}
		})
	}
}

func TestFuzzyMatchScoreBySegments(t *testing.T) {
	tests := []struct {
		name        string
		accountName string
		pattern     string
		shouldMatch bool
	}{
		{
			name:        "matches segment exactly",
			accountName: "Активы:Альфа:Текущий",
			pattern:     "Альфа",
			shouldMatch: true,
		},
		{
			name:        "matches segment fuzzy",
			accountName: "Активы:Альфа:Текущий",
			pattern:     "ал",
			shouldMatch: true,
		},
		{
			name:        "no segment matches",
			accountName: "Расходы:Транспорт",
			pattern:     "ал",
			shouldMatch: false,
		},
		{
			name:        "empty pattern matches all",
			accountName: "anything:here",
			pattern:     "",
			shouldMatch: true,
		},
		{
			name:        "matches in middle segment",
			accountName: "expenses:food:groceries",
			pattern:     "foo",
			shouldMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := fuzzyMatchScoreBySegments(tt.accountName, tt.pattern)
			if tt.shouldMatch {
				assert.True(t, score > 0, "should match: fuzzyMatchScoreBySegments(%q, %q)", tt.accountName, tt.pattern)
			} else {
				assert.Equal(t, 0, score, "should not match: fuzzyMatchScoreBySegments(%q, %q)", tt.accountName, tt.pattern)
			}
		})
	}
}

func TestFilterAndScoreFuzzyMatch_SegmentBased(t *testing.T) {
	items := []protocol.CompletionItem{
		{Label: "Активы:Альфа:Текущий"},
		{Label: "Расходы:Налоги"},
		{Label: "Расходы:Транспорт"},
		{Label: "expenses:food"},
	}

	t.Run("filters by segment matching cyrillic", func(t *testing.T) {
		scored := filterAndScoreFuzzyMatch(items, "ал")
		labels := make([]string, len(scored))
		for i, s := range scored {
			labels[i] = s.item.Label
		}

		assert.Contains(t, labels, "Активы:Альфа:Текущий", "should match segment 'Альфа'")
		assert.Contains(t, labels, "Расходы:Налоги", "should match segment 'Налоги'")
		assert.NotContains(t, labels, "Расходы:Транспорт", "no segment matches 'ал'")
	})

	t.Run("handles query with trailing colon", func(t *testing.T) {
		scored := filterAndScoreFuzzyMatch(items, "Альфа:")
		labels := make([]string, len(scored))
		for i, s := range scored {
			labels[i] = s.item.Label
		}

		assert.Contains(t, labels, "Активы:Альфа:Текущий", "should match even with trailing colon")
	})
}
