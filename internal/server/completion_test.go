package server

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

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

	assert.Equal(t, "Payee (2)", groceryDetail, "Grocery Store used 2 times")
	assert.Equal(t, "Payee (1)", coffeeDetail, "Coffee Shop used 1 time")
}

func TestCompletion_AccountsByPrefix(t *testing.T) {
	srv := NewServer()
	content := `account expenses:food:groceries
account expenses:food:restaurant
account assets:cash

2024-01-15 test
    expenses:food:groceries  $30
    expenses:food:restaurant  $20
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 5, Character: 14},
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
	content := `2024-01-14 prev
    expenses:food  $40
    expenses:rent  EUR 80
    assets:cash

2024-01-15 test
    expenses:food  $50
    expenses:rent  EUR 100
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 6, Character: 20},
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

func TestCompletion_Date_HistoricalUsesFileFormat(t *testing.T) {
	srv := NewServer()
	content := `2024/01/10 old transaction
    expenses:food  $50
    assets:cash

2024/01/12 another
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

	var historicalItems []protocol.CompletionItem
	for _, item := range result.Items {
		if item.Detail == "from history" {
			historicalItems = append(historicalItems, item)
		}
	}

	require.NotEmpty(t, historicalItems, "should have historical date completions")
	for _, item := range historicalItems {
		assert.Regexp(t, `^\d{4}/\d{2}/\d{2}$`, item.Label, "historical dates should use YYYY/MM/DD format from file")
	}
}

func extractDetails(items []protocol.CompletionItem) []string {
	details := make([]string, len(items))
	for i, item := range items {
		details[i] = item.Detail
	}
	return details
}

func TestCompletion_PayeeInsertsOnlyPayeeName(t *testing.T) {
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
	assert.Empty(t, groceryItem.InsertText, "Payee should insert only the label")
	assert.Equal(t, "Payee (1)", groceryItem.Detail, "Detail should show count")
}

func TestCompletion_MultiplePayeesShowCounts(t *testing.T) {
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
		switch result.Items[i].Label {
		case "Shop EUR":
			eurItem = &result.Items[i]
		case "Dollar Store":
			dollarItem = &result.Items[i]
		case "Euro Shop":
			euroSymItem = &result.Items[i]
		}
	}

	require.NotNil(t, eurItem, "Shop EUR should be in completion items")
	assert.Empty(t, eurItem.InsertText, "Payee should insert only the label")
	assert.Equal(t, "Payee (1)", eurItem.Detail)

	require.NotNil(t, dollarItem, "Dollar Store should be in completion items")
	assert.Empty(t, dollarItem.InsertText, "Payee should insert only the label")
	assert.Equal(t, "Payee (1)", dollarItem.Detail)

	require.NotNil(t, euroSymItem, "Euro Shop should be in completion items")
	assert.Empty(t, euroSymItem.InsertText, "Payee should insert only the label")
	assert.Equal(t, "Payee (1)", euroSymItem.Detail)
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
		RootURI: uri.File(tmpDir),
	}
	_, err = srv.Initialize(context.Background(), initParams)
	require.NoError(t, err)

	err = srv.workspace.Initialize()
	require.NoError(t, err)

	uri := uri.File(txPath)
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
		scored := filterAndScoreFuzzyMatch(items, "альа", true)
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
		scored := filterAndScoreFuzzyMatch(items, "", true)
		assert.Len(t, scored, len(items))
	})

	t.Run("no matches returns empty", func(t *testing.T) {
		scored := filterAndScoreFuzzyMatch(items, "xyz", true)
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

func TestCompletion_AccountMidWord_ReplacesFullToken(t *testing.T) {
	srv := NewServer()
	content := "account expenses:food:supermarket\naccount expenses:food:electronics\n\n2024-01-15 test\n    expenses:food:supermarket"

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	// Cursor right after "food:" at "expenses:food:|supermarket" — UTF-16 col 18
	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 18},
		},
		Context: &protocol.CompletionContext{
			TriggerCharacter: ":",
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	var electronicsItem *protocol.CompletionItem
	for i := range result.Items {
		if result.Items[i].Label == "expenses:food:electronics" {
			electronicsItem = &result.Items[i]
			break
		}
	}

	require.NotNil(t, electronicsItem, "expenses:food:electronics should be in completions")
	require.NotNil(t, electronicsItem.TextEdit, "TextEdit should be set")

	textEdit := electronicsItem.TextEdit
	assert.Equal(t, uint32(4), textEdit.Range.Start.Character, "Start: after indent")
	assert.Equal(t, uint32(29), textEdit.Range.End.Character, "End: covers full existing account token")
	assert.Equal(t, "expenses:food:electronics", textEdit.NewText)
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
		scored := filterAndScoreFuzzyMatch(items, "ал", true)
		labels := make([]string, len(scored))
		for i, s := range scored {
			labels[i] = s.item.Label
		}

		assert.Contains(t, labels, "Активы:Альфа:Текущий", "should match segment 'Альфа'")
		assert.Contains(t, labels, "Расходы:Налоги", "should match segment 'Налоги'")
		assert.NotContains(t, labels, "Расходы:Транспорт", "no segment matches 'ал'")
	})

	t.Run("handles query with trailing colon", func(t *testing.T) {
		scored := filterAndScoreFuzzyMatch(items, "Альфа:", true)
		labels := make([]string, len(scored))
		for i, s := range scored {
			labels[i] = s.item.Label
		}

		assert.Contains(t, labels, "Активы:Альфа:Текущий", "should match even with trailing colon")
	})
}

func TestCompletion_PayeeWithoutTemplate(t *testing.T) {
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

	if groceryItem.InsertText != "" {
		assert.Equal(t, "Grocery Store", groceryItem.InsertText,
			"Payee completion should insert ONLY the payee name, not template")
	}
	assert.NotContains(t, groceryItem.InsertText, "expenses:food",
		"Payee completion should NOT contain template postings")
	assert.NotContains(t, groceryItem.InsertText, "\n",
		"Payee completion should NOT contain newlines (template)")
}

func TestCompletion_FuzzyMatchingDisabled(t *testing.T) {
	srv := NewServer()
	settings := srv.getSettings()
	settings.Completion.FuzzyMatching = false
	srv.setSettings(settings)

	content := `account assets:checking
account expenses:food:groceries

2024-01-15 test
    exp`
	uri := protocol.DocumentURI("file:///test.journal")
	srv.StoreDocument(uri, content)

	result, err := srv.Completion(context.Background(), &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 4, Character: 7},
		},
	})
	require.NoError(t, err)

	hasNonPrefixMatch := false
	for _, item := range result.Items {
		if !strings.HasPrefix(strings.ToLower(item.Label), "exp") {
			hasNonPrefixMatch = true
			break
		}
	}
	assert.False(t, hasNonPrefixMatch, "without fuzzy matching, only prefix matches should appear")
}

func TestCompletion_ShowCountsDisabled(t *testing.T) {
	srv := NewServer()
	settings := srv.getSettings()
	settings.Completion.ShowCounts = false
	srv.setSettings(settings)

	content := `2024-01-15 test
    expenses:food  $50.00
    assets:cash

2024-01-16 test
    expenses:food  $30.00
    assets:cash

2024-01-17 test
    `
	uri := protocol.DocumentURI("file:///test.journal")
	srv.StoreDocument(uri, content)

	result, err := srv.Completion(context.Background(), &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 9, Character: 4},
		},
	})
	require.NoError(t, err)

	for _, item := range result.Items {
		if item.Label == "expenses:food" {
			assert.NotContains(t, item.Detail, "(", "counts should not be shown when disabled")
			return
		}
	}
}

func TestCompletion_Date_UsesNearbyFormat(t *testing.T) {
	srv := NewServer()
	content := `2026-01-01 opening balances
    assets:cash  $1000
    equity:opening

01-05 transaction 1
    expenses:food  $20
    assets:cash

01-06 transaction 2
    expenses:food  $30
    assets:cash

`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 12, Character: 0},
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
	assert.Regexp(t, `^\d{2}-\d{2}$`, todayItem.Label,
		"today should use MM-DD format from nearby transactions, not YYYY-MM-DD from file start")
}

func TestDetermineContext_PartialDate(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		char     uint32
		expected CompletionContextType
	}{
		{"typing year", "2026", 4, ContextDate},
		{"typing year-month-", "2026-01-", 8, ContextDate},
		{"full date no space", "2026-01-15", 10, ContextDate},
		{"full date with space", "2026-01-15 ", 11, ContextPayee},
		{"full date with payee", "2024-01-15 Groc", 15, ContextPayee},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := determineCompletionContext(tt.content, protocol.Position{Line: 0, Character: tt.char}, nil)
			assert.Equal(t, tt.expected, ctx)
		})
	}
}

func TestDetermineContext_IndentedPosting(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		line     uint32
		char     uint32
		expected CompletionContextType
	}{
		{"1 space indent with account", "2024-01-15 test\n exp", 1, 4, ContextAccount},
		{"2 space indent with account", "2024-01-15 test\n  exp", 1, 5, ContextAccount},
		{"3 space indent with account", "2024-01-15 test\n   exp", 1, 6, ContextAccount},
		{"1 space only (empty posting)", "2024-01-15 test\n ", 1, 1, ContextAccount},
		{"4 spaces (existing behavior)", "2024-01-15 test\n    exp", 1, 7, ContextAccount},
		{"tab (existing behavior)", "2024-01-15 test\n\texp", 1, 4, ContextAccount},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := determineCompletionContext(tt.content, protocol.Position{Line: tt.line, Character: tt.char}, nil)
			assert.Equal(t, tt.expected, ctx)
		})
	}
}

func TestExtractQueryText_PartialDate(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		char     uint32
		expected string
	}{
		{"partial date", "2026-01-", 8, "2026-01-"},
		{"empty line", "", 0, ""},
		{"full date", "2026-01-15", 10, "2026-01-15"},
		{"just year", "2026", 4, "2026"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos := protocol.Position{Line: 0, Character: tt.char}
			result := extractQueryText(tt.content, pos, ContextDate)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCalculateTextEditRange_PartialDate(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		char      uint32
		wantStart uint32
		wantEnd   uint32
		wantNil   bool
	}{
		{"partial date replaces from col 0", "2026-01-", 8, 0, 8, false},
		{"full date replaces from col 0", "2026-01-15", 10, 0, 10, false},
		{"empty line returns nil", "", 0, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos := protocol.Position{Line: 0, Character: tt.char}
			r := calculateTextEditRange(tt.content, pos, ContextDate)
			if tt.wantNil {
				assert.Nil(t, r)
			} else {
				require.NotNil(t, r)
				assert.Equal(t, tt.wantStart, r.Start.Character)
				assert.Equal(t, tt.wantEnd, r.End.Character)
			}
		})
	}
}

func TestCompletion_PartialDateReturnsDates(t *testing.T) {
	srv := NewServer()
	content := `2024-01-10 Apple
    Расходы:Транспорт  71,00
    Активы:Сбербанк:Текущий

2026-`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 5},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, len(result.Items) > 0, "should have completion items for partial date")

	details := extractDetails(result.Items)
	assert.Contains(t, details, "today", "partial date should show date completions, not payees")
	assert.Contains(t, details, "yesterday")
	assert.Contains(t, details, "tomorrow")

	labels := extractLabels(result.Items)
	assert.NotContains(t, labels, "Apple", "should NOT show payees when typing partial date")

	for _, item := range result.Items {
		assert.Equal(t, protocol.CompletionItemKindConstant, item.Kind,
			"date items should have Constant kind, got item: %s", item.Label)
		if item.TextEdit != nil {
			assert.Equal(t, uint32(0), item.TextEdit.Range.Start.Character,
				"TextEdit should replace from column 0")
			assert.Equal(t, uint32(5), item.TextEdit.Range.End.Character,
				"TextEdit should replace to cursor position")
		}
	}
}

func TestCompletion_PartialDateOverridesFileFormat(t *testing.T) {
	srv := NewServer()
	content := `01-05 transaction 1
    expenses:food  $20
    assets:cash

2026-`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 5},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, len(result.Items) > 0, "should have completion items when typing year prefix")

	details := extractDetails(result.Items)
	assert.Contains(t, details, "today", "should contain today")
	assert.Contains(t, details, "yesterday", "should contain yesterday")
	assert.Contains(t, details, "tomorrow", "should contain tomorrow")

	for _, item := range result.Items {
		if item.Detail == "today" || item.Detail == "yesterday" || item.Detail == "tomorrow" {
			assert.Regexp(t, `^\d{4}-\d{2}-\d{2}$`, item.Label,
				"when user types '2026-', dates should be YYYY-MM-DD, not MM-DD; got %s", item.Label)
		}
	}
}

func TestCompletion_ShortDateKeepsShortFormat(t *testing.T) {
	srv := NewServer()
	now := time.Now()
	prefix := fmt.Sprintf("%02d-", now.Month())
	histDate := fmt.Sprintf("%02d-05 transaction 1", now.Month())
	content := histDate + `
    expenses:food  $20
    assets:cash

` + prefix

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: uint32(len(prefix))},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, len(result.Items) > 0, "should have completion items for short date prefix")

	for _, item := range result.Items {
		if item.Detail == "today" || item.Detail == "yesterday" || item.Detail == "tomorrow" {
			assert.Regexp(t, `^\d{2}-\d{2}$`, item.Label,
				"when user types '%s' and file uses MM-DD, dates should stay MM-DD; got %s", prefix, item.Label)
		}
	}
}

func TestDetectFormatFromTypedText(t *testing.T) {
	tests := []struct {
		name     string
		typed    string
		wantNil  bool
		wantYear bool
		wantSep  string
	}{
		{"year prefix with dash", "2026-", false, true, "-"},
		{"year prefix with slash", "2026/", false, true, "/"},
		{"year prefix with dot", "2026.", false, true, "."},
		{"four digits only", "2026", false, true, "-"},
		{"full date", "2026-02-06", false, true, "-"},
		{"short two digits", "02-", true, false, ""},
		{"short one digit", "2-", true, false, ""},
		{"empty", "", true, false, ""},
		{"non-digit", "abc", true, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectFormatFromTypedText(tt.typed)
			if tt.wantNil {
				assert.Nil(t, result, "expected nil for typed=%q", tt.typed)
			} else {
				require.NotNil(t, result, "expected non-nil for typed=%q", tt.typed)
				assert.Equal(t, tt.wantYear, result.HasYear)
				assert.Equal(t, tt.wantSep, result.Separator)
				assert.True(t, result.LeadingZeros)
			}
		})
	}
}

func TestCompletion_PrefixCommodity(t *testing.T) {
	srv := NewServer()
	content := `commodity USD
commodity EUR
commodity RUB

2024-01-15 test
    expenses:food  `

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 5, Character: 19},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "USD", "should suggest commodities in prefix position")
	assert.Contains(t, labels, "EUR", "should suggest commodities in prefix position")
	assert.Contains(t, labels, "RUB", "should suggest commodities in prefix position")
	assert.NotContains(t, labels, "expenses:food", "should NOT suggest accounts in commodity position")
}

func TestExtractQueryText_PrefixCommodity(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		line     uint32
		char     uint32
		expected string
	}{
		{
			name:     "prefix $ typed",
			content:  "2024-01-15 test\n    expenses:food  $",
			line:     1,
			char:     20,
			expected: "$",
		},
		{
			name:     "prefix EU typed",
			content:  "2024-01-15 test\n    expenses:food  EU",
			line:     1,
			char:     21,
			expected: "EU",
		},
		{
			name:     "empty prefix position",
			content:  "2024-01-15 test\n    expenses:food  ",
			line:     1,
			char:     19,
			expected: "",
		},
		{
			name:     "suffix US typed",
			content:  "2024-01-15 test\n    expenses:food  100 US",
			line:     1,
			char:     26,
			expected: "US",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos := protocol.Position{Line: tt.line, Character: tt.char}
			result := extractQueryText(tt.content, pos, ContextCommodity)
			assert.Equal(t, tt.expected, result, "extractQueryText for %q at char %d", tt.content, tt.char)
		})
	}
}

func TestFindCommodityStart_PrefixPosition(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		byteCol   int
		wantStart int
	}{
		{
			name:      "prefix $ position",
			line:      "    expenses:food  $50",
			byteCol:   20,
			wantStart: 19,
		},
		{
			name:      "prefix EUR position",
			line:      "    expenses:food  EUR 100",
			byteCol:   21,
			wantStart: 19,
		},
		{
			name:      "suffix USD position",
			line:      "    expenses:food  100 USD",
			byteCol:   25,
			wantStart: 23,
		},
		{
			name:      "empty after separator",
			line:      "    expenses:food  ",
			byteCol:   19,
			wantStart: 19,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findCommodityStart(tt.line, tt.byteCol)
			assert.Equal(t, tt.wantStart, result, "findCommodityStart(%q, %d)", tt.line, tt.byteCol)
		})
	}
}

func TestDetermineContext_PrefixCommodity(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		line     uint32
		char     uint32
		expected CompletionContextType
	}{
		{
			name:     "empty position after separator → commodity",
			content:  "2024-01-15 test\n    expenses:food  ",
			line:     1,
			char:     19,
			expected: ContextCommodity,
		},
		{
			name:     "cursor after $ prefix → commodity",
			content:  "2024-01-15 test\n    expenses:food  $",
			line:     1,
			char:     20,
			expected: ContextCommodity,
		},
		{
			name:     "cursor in EUR prefix → commodity",
			content:  "2024-01-15 test\n    expenses:food  EU",
			line:     1,
			char:     21,
			expected: ContextCommodity,
		},
		{
			name:     "cursor in digits after $ → account (amount area)",
			content:  "2024-01-15 test\n    expenses:food  $50",
			line:     1,
			char:     22,
			expected: ContextAccount,
		},
		{
			name:     "no prefix, cursor in digits → account",
			content:  "2024-01-15 test\n    expenses:food  50",
			line:     1,
			char:     21,
			expected: ContextAccount,
		},
		{
			name:     "suffix commodity position → commodity",
			content:  "2024-01-15 test\n    expenses:food  50.00 ",
			line:     1,
			char:     25,
			expected: ContextCommodity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos := protocol.Position{Line: tt.line, Character: tt.char}
			ctx := determineCompletionContext(tt.content, pos, nil)
			assert.Equal(t, tt.expected, ctx, "context for %q at char %d", tt.content, tt.char)
		})
	}
}

func TestFindPrefixCommodityEnd(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"dollar prefix", "$50", 1},
		{"EUR prefix with space", "EUR 100", 3},
		{"no prefix (number first)", "100 USD", 0},
		{"euro sign prefix", "€100", len("€")},
		{"empty string", "", 0},
		{"negative with dollar prefix", "$-50", 1},
		{"prefix with parens", "$(-50)", 1},
		{"just commodity no number", "USD", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findPrefixCommodityEnd(tt.input)
			assert.Equal(t, tt.expected, result, "findPrefixCommodityEnd(%q)", tt.input)
		})
	}
}

func TestCompletion_PayeeWithTab(t *testing.T) {
	srv := NewServer()
	content := "2024-01-15 Grocery Store\n    expenses:food  $50\n    assets:cash\n\n2024-01-16\t"

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
	assert.Contains(t, labels, "Grocery Store", "should suggest payees when tab separates date and payee")

	for _, item := range result.Items {
		if item.Label == "Grocery Store" {
			require.NotNil(t, item.TextEdit, "TextEdit should be set")
			assert.Equal(t, uint32(11), item.TextEdit.Range.Start.Character, "TextEdit should start after tab")
			assert.Equal(t, uint32(11), item.TextEdit.Range.End.Character, "TextEdit should end at cursor")
		}
	}
}

func TestExtractQueryText_PayeeWithTab(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		char     uint32
		expected string
	}{
		{"tab separator", "2024-01-15\tgrocery", 18, "grocery"},
		{"tab then empty", "2024-01-15\t", 11, ""},
		{"tab with status marker", "2024-01-15\t* grocery", 20, "* grocery"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos := protocol.Position{Line: 0, Character: tt.char}
			result := extractQueryText(tt.content, pos, ContextPayee)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCalculateTextEditRange_PayeeWithTab(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		char      uint32
		wantStart uint32
		wantEnd   uint32
	}{
		{"tab separator", "2024-01-15\tgrocery", 18, 11, 18},
		{"tab then status marker", "2024-01-15\t* grocery", 20, 13, 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos := protocol.Position{Line: 0, Character: tt.char}
			r := calculateTextEditRange(tt.content, pos, ContextPayee)
			require.NotNil(t, r)
			assert.Equal(t, tt.wantStart, r.Start.Character)
			assert.Equal(t, tt.wantEnd, r.End.Character)
		})
	}
}

func TestCalculateTextEditRange_AccountMidWord(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		char      uint32
		wantStart uint32
		wantEnd   uint32
	}{
		{
			name:      "cursor mid-word extends end to doublespace",
			content:   "    expenses:food:grocery  $50",
			char:      9,
			wantStart: 4,
			wantEnd:   25,
		},
		{
			name:      "cursor at end of token (no text after)",
			content:   "    expenses:food",
			char:      17,
			wantStart: 4,
			wantEnd:   17,
		},
		{
			name:      "cursor mid cyrillic account",
			content:   "    Расходы:Продукты:Супермаркет",
			char:      19, // UTF-16 offset inside Расходы:Продукт|ы
			wantStart: 4,
			wantEnd:   32, // UTF-16 len of full line (no terminator, extends to EOL)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos := protocol.Position{Line: 0, Character: tt.char}
			r := calculateTextEditRange(tt.content, pos, ContextAccount)
			require.NotNil(t, r)
			assert.Equal(t, tt.wantStart, r.Start.Character, "Start")
			assert.Equal(t, tt.wantEnd, r.End.Character, "End")
		})
	}
}

func TestDetermineContext_PayeeWithTab(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		char     uint32
		expected CompletionContextType
	}{
		{"tab between date and payee", "2024-01-15\tgrocery", 18, ContextPayee},
		{"tab then space", "2024-01-15\t grocery", 19, ContextPayee},
		{"tab cursor after date", "2024-01-15\t", 11, ContextPayee},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := determineCompletionContext(tt.content, protocol.Position{Line: 0, Character: tt.char}, nil)
			assert.Equal(t, tt.expected, ctx)
		})
	}
}

func TestIndexFirstWhitespace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"space only", "2024-01-15 test", 10},
		{"tab only", "2024-01-15\ttest", 10},
		{"no whitespace", "2024-01-15", -1},
		{"tab before space", "2024-01-15\t test", 10},
		{"space before tab", "2024-01-15 \ttest", 10},
		{"empty string", "", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := indexFirstWhitespace(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDetectDateFormat_FromCursorPosition(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		cursorLine int
		wantYear   bool
	}{
		{
			name: "full date at start, short dates near cursor",
			content: `2026-01-01 opening
    assets:cash  $1000

01-05 transaction
    expenses:food  $20

01-06 transaction
    expenses:food  $30

`,
			cursorLine: 9,
			wantYear:   false,
		},
		{
			name: "full dates throughout",
			content: `2026-01-01 opening
    assets:cash  $1000

2026-01-05 transaction
    expenses:food  $20

`,
			cursorLine: 6,
			wantYear:   true,
		},
		{
			name: "short dates from the start",
			content: `01-01 opening
    assets:cash  $1000

01-05 transaction
    expenses:food  $20

`,
			cursorLine: 5,
			wantYear:   false,
		},
		{
			name: "cursor at beginning, only full dates",
			content: `2026-01-01 opening
    assets:cash  $1000

`,
			cursorLine: 0,
			wantYear:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format := detectDateFormat(tt.content, tt.cursorLine)
			assert.Equal(t, tt.wantYear, format.HasYear,
				"detectDateFormat with cursorLine=%d should have HasYear=%v", tt.cursorLine, tt.wantYear)
		})
	}
}

func TestCompletion_IncludeNotes_True_ShowsFullDescription(t *testing.T) {
	srv := NewServer()
	settings := srv.getSettings()
	settings.Completion.IncludeNotes = true
	srv.setSettings(settings)

	content := `2024-01-15 Grocery Store | weekly shopping
    expenses:food  $50
    assets:cash

2024-01-16 `

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
	assert.Contains(t, labels, "Grocery Store | weekly shopping",
		"with IncludeNotes=true, payee completions should include full description")
	assert.NotContains(t, labels, "Grocery Store",
		"with IncludeNotes=true, payee-only should not appear as separate item")
}

func TestCompletion_IncludeNotes_False_ShowsPayeeOnly(t *testing.T) {
	srv := NewServer()
	settings := srv.getSettings()
	settings.Completion.IncludeNotes = false
	srv.setSettings(settings)

	content := `2024-01-15 Grocery Store | weekly shopping
    expenses:food  $50
    assets:cash

2024-01-16 `

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
	assert.Contains(t, labels, "Grocery Store",
		"with IncludeNotes=false, payee completions should show payee only")
	assert.NotContains(t, labels, "Grocery Store | weekly shopping",
		"with IncludeNotes=false, full description should not appear")
}

func TestCompletion_IncludeNotes_DefaultTrue(t *testing.T) {
	srv := NewServer()

	content := `2024-01-15 Grocery Store | weekly shopping
    expenses:food  $50
    assets:cash

2024-01-16 `

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
	assert.Contains(t, labels, "Grocery Store | weekly shopping",
		"default behavior should include notes (IncludeNotes defaults to true)")
}

func TestCompletion_ExcludesCurrentTransactionAccounts(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 groceries
    expenses:food  $50
    assets:cash

2024-01-16 new
    `

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
	assert.Contains(t, labels, "expenses:food", "accounts from prior transactions remain")
	assert.Contains(t, labels, "assets:cash", "accounts from prior transactions remain")
}

func TestCompletion_ExcludesCurrentTransactionPayee(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash

2024-01-16 Groc`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 15},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "Grocery Store", "payee from prior transaction remains")
	assert.NotContains(t, labels, "Groc", "partial payee from current transaction excluded")
}

func TestCompletion_CursorBetweenTransactions(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash

2024-01-16 Coffee Shop
    expenses:drinks  $5
    assets:cash
`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 7, Character: 0},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	var hasToday bool
	for _, item := range result.Items {
		if item.Detail == "today" {
			hasToday = true
			break
		}
	}
	assert.True(t, hasToday, "date completions appear between transactions")
	_ = labels
}

func TestCompletion_DirectivesSurviveExclusion(t *testing.T) {
	srv := NewServer()
	content := `account expenses:food
account assets:cash

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

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "expenses:food", "account from directive survives when only transaction excluded")
	assert.Contains(t, labels, "assets:cash", "account from directive survives when only transaction excluded")
}

func TestFindTokenEnd(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		byteCol  int
		ctxType  CompletionContextType
		expected int
	}{
		// ContextAccount: terminates on double-space, tab, semicolon, EOL
		{"account cursor at end", "    expenses:food", 17, ContextAccount, 17},
		{"account mid-word ASCII", "    expenses:food:grocery", 17, ContextAccount, 25},
		{"account before doublespace", "    expenses:food  $50", 17, ContextAccount, 17},
		{"account before tab", "    expenses:food\t$50", 17, ContextAccount, 17},
		{"account before semicolon", "    expenses:food  ; comment", 12, ContextAccount, 17},
		{"account cursor at start", "    expenses:food", 4, ContextAccount, 17},
		{"account cyrillic mid-word", "    Расходы:Продукты:Супермаркет", 39, ContextAccount, 58},
		{"account cyrillic with doublespace", "    Расходы:Продукты  100", 4, ContextAccount, 35},

		// ContextPayee: terminates on |, ;, EOL (trim trailing whitespace)
		{"payee cursor at end", "2024-01-15 grocery store", 24, ContextPayee, 24},
		{"payee mid-word", "2024-01-15 grocery store", 18, ContextPayee, 24},
		{"payee before pipe", "2024-01-15 payee | note", 16, ContextPayee, 16},
		{"payee before semicolon", "2024-01-15 payee ; comment", 16, ContextPayee, 16},
		{"payee trailing whitespace trimmed", "2024-01-15 grocery   ", 18, ContextPayee, 18},

		// ContextCommodity: terminates on space, tab, EOL
		{"commodity cursor at end", "    expenses:food  USD", 22, ContextCommodity, 22},
		{"commodity mid-word", "    expenses:food  USD", 20, ContextCommodity, 22},
		{"commodity before space", "    expenses:food  USD 100", 22, ContextCommodity, 22},
		{"commodity before tab", "    expenses:food  USD\t100", 22, ContextCommodity, 22},

		// ContextDate: terminates on space, tab, EOL
		{"date cursor at end", "2024-01-15", 10, ContextDate, 10},
		{"date mid-word", "2024-01-15 payee", 8, ContextDate, 10},
		{"date before space", "2024-01-15 payee", 10, ContextDate, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findTokenEnd(tt.line, tt.byteCol, tt.ctxType)
			assert.Equal(t, tt.expected, result, "findTokenEnd(%q, %d, %v)", tt.line, tt.byteCol, tt.ctxType)
		})
	}
}

func TestRulesCompletion_HasTextEdit(t *testing.T) {
	srv := NewServer()
	content := "  acco"
	docURI := protocol.DocumentURI("file:///test.rules")
	srv.documents.Store(docURI, content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 0, Character: 6},
		},
	}
	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Items)
	for _, item := range result.Items {
		assert.NotNil(t, item.TextEdit, "item %q should have TextEdit", item.Label)
	}
}

func TestRulesCompletion_TextEditRange(t *testing.T) {
	srv := NewServer()
	content := "  acco"
	docURI := protocol.DocumentURI("file:///test.rules")
	srv.documents.Store(docURI, content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 0, Character: 6},
		},
	}
	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Items)
	item := result.Items[0]
	require.NotNil(t, item.TextEdit)
	assert.Equal(t, uint32(2), item.TextEdit.Range.Start.Character, "Start.Character should be 2 (after indent)")
	assert.Equal(t, uint32(6), item.TextEdit.Range.End.Character, "End.Character should be 6")
}

func TestRulesCompletion_TextEditRange_TopLevel(t *testing.T) {
	srv := NewServer()
	content := "sep"
	docURI := protocol.DocumentURI("file:///test.rules")
	srv.documents.Store(docURI, content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 0, Character: 3},
		},
	}
	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Items)
	item := result.Items[0]
	require.NotNil(t, item.TextEdit)
	assert.Equal(t, uint32(0), item.TextEdit.Range.Start.Character, "Start.Character should be 0")
	assert.Equal(t, uint32(3), item.TextEdit.Range.End.Character, "End.Character should be 3")
}

// Regression: issue #23 — inside an `if` block, the LSP should offer
// %-prefixed field references (from `fields` directive), not top-level
// directive keywords.
func TestRulesCompletion_InsideIfBlock_FieldReferences(t *testing.T) {
	srv := NewServer()
	content := "fields date, description, amount\nif\n%d"
	docURI := protocol.DocumentURI("file:///test.rules")
	srv.documents.Store(docURI, content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 2, Character: 2},
		},
	}
	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Items)

	labels := make([]string, len(result.Items))
	for i, it := range result.Items {
		labels[i] = it.Label
	}

	assert.Contains(t, labels, "%date", "expected %%date inside if block")
	assert.Contains(t, labels, "%description", "expected %%description inside if block")
	assert.Contains(t, labels, "%amount", "expected %%amount inside if block")
	assert.NotContains(t, labels, "date-format", "did NOT expect date-format inside if block")
	assert.NotContains(t, labels, "decimal-mark", "did NOT expect decimal-mark inside if block")

	// TextEdit range should cover the '%d' prefix so the completion replaces it.
	for _, it := range result.Items {
		require.NotNil(t, it.TextEdit, "item %q must have TextEdit", it.Label)
		assert.Equal(t, uint32(0), it.TextEdit.Range.Start.Character, "Start should cover the %% prefix")
		assert.Equal(t, uint32(2), it.TextEdit.Range.End.Character, "End should match cursor col")
	}
}

// Regression: issue #23 — without any declared `fields`, fall back to the
// builtin field names so the user still gets helpful suggestions.
func TestRulesCompletion_InsideIfBlock_NoFieldsDeclared(t *testing.T) {
	srv := NewServer()
	content := "if\n%d"
	docURI := protocol.DocumentURI("file:///test.rules")
	srv.documents.Store(docURI, content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 1, Character: 2},
		},
	}
	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Items)

	labels := make([]string, len(result.Items))
	for i, it := range result.Items {
		labels[i] = it.Label
	}

	assert.Contains(t, labels, "%date", "expected builtin %%date fallback")
	assert.Contains(t, labels, "%description", "expected builtin %%description fallback")
	assert.NotContains(t, labels, "date-format", "did NOT expect date-format inside if block")
}

// Regression: top-level directive completions must still work outside if blocks.
func TestRulesCompletion_TopLevelStillOffersDirectives(t *testing.T) {
	srv := NewServer()
	content := "ski"
	docURI := protocol.DocumentURI("file:///test.rules")
	srv.documents.Store(docURI, content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 0, Character: 3},
		},
	}
	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Items)

	labels := make([]string, len(result.Items))
	for i, it := range result.Items {
		labels[i] = it.Label
	}
	assert.Contains(t, labels, "skip", "expected 'skip' in top-level directive completions")
}

func TestCompletion_DigitTriggerOnEmptyLine(t *testing.T) {
	srv := NewServer()
	content := `2024-01-10 Apple
    expenses:food  $50
    assets:cash

2`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 1},
		},
		Context: &protocol.CompletionContext{
			TriggerKind:      protocol.CompletionTriggerKindTriggerCharacter,
			TriggerCharacter: "2",
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, len(result.Items) > 0, "digit trigger should produce date completions")

	details := extractDetails(result.Items)
	assert.Contains(t, details, "today")
	assert.Contains(t, details, "yesterday")
	assert.Contains(t, details, "tomorrow")
}

func TestCompletion_DigitTriggerPartialYear(t *testing.T) {
	srv := NewServer()
	content := `2024-01-10 Apple
    expenses:food  $50
    assets:cash

202`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 3},
		},
		Context: &protocol.CompletionContext{
			TriggerKind:      protocol.CompletionTriggerKindTriggerCharacter,
			TriggerCharacter: "2",
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, len(result.Items) > 0, "digit trigger on partial year should produce date completions")

	details := extractDetails(result.Items)
	assert.Contains(t, details, "today")

	for _, item := range result.Items {
		assert.Equal(t, protocol.CompletionItemKindConstant, item.Kind,
			"all items should be date (Constant) kind, got: %s", item.Label)
	}
}

func TestCompletion_DigitTriggerInPostingDoesNotReturnDates(t *testing.T) {
	srv := NewServer()
	content := `2024-01-10 Apple
    expenses:food  $5
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
			TriggerCharacter: "5",
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	details := extractDetails(result.Items)
	assert.NotContains(t, details, "today", "digit trigger in posting amount area should not show dates")
}

func TestCompletion_DigitTriggerInPayeeArea(t *testing.T) {
	srv := NewServer()
	content := `2024-01-10 3`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 12},
		},
		Context: &protocol.CompletionContext{
			TriggerKind:      protocol.CompletionTriggerKindTriggerCharacter,
			TriggerCharacter: "3",
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	details := extractDetails(result.Items)
	assert.NotContains(t, details, "today", "digit trigger in payee area should not show dates")
}

func TestCompletion_DigitTriggerOnEmptyDocument(t *testing.T) {
	srv := NewServer()
	content := `2`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 1},
		},
		Context: &protocol.CompletionContext{
			TriggerKind:      protocol.CompletionTriggerKindTriggerCharacter,
			TriggerCharacter: "2",
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, len(result.Items) > 0, "digit trigger on empty document should produce date completions")

	details := extractDetails(result.Items)
	assert.Contains(t, details, "today")
	assert.Contains(t, details, "yesterday")
	assert.Contains(t, details, "tomorrow")
}

func TestDetermineContext_Directive_PartialKeyword(t *testing.T) {
	tests := []struct {
		name    string
		content string
		col     uint32
	}{
		{"acc", "acc", 3},
		{"inc", "inc", 3},
		{"com", "com", 3},
		{"single letter a", "a", 1},
		{"single letter i", "i", 1},
		{"full word account without space", "account", 7},
		{"full word include without space", "include", 7},
		{"D", "D", 1},
		{"Y", "Y", 1},
		{"P", "P", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := determineCompletionContext(tt.content, protocol.Position{Line: 0, Character: tt.col}, nil)
			assert.Equal(t, ContextDirective, ctx)
		})
	}
}

func TestDetermineContext_Directive_DoesNotAffectExistingContexts(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		line     uint32
		col      uint32
		expected CompletionContextType
	}{
		{"account directive with space", "account expenses:food", 0, 10, ContextAccount},
		{"commodity directive with space", "commodity $", 0, 10, ContextCommodity},
		{"empty line", "", 0, 0, ContextDate},
		{"digit prefix", "2024-01-15 test", 0, 3, ContextDate},
		{"indented line", "    expenses:food", 0, 10, ContextAccount},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := determineCompletionContext(tt.content, protocol.Position{Line: tt.line, Character: tt.col}, nil)
			assert.Equal(t, tt.expected, ctx)
		})
	}
}

func TestDetermineContext_Directive_CommentLinesNotDirective(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		col      uint32
		expected CompletionContextType
	}{
		{"semicolon comment", ";comment", 3, ContextTagName},
		{"hash comment", "#comment", 3, ContextDate},
		{"asterisk comment", "*comment", 3, ContextDate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := determineCompletionContext(tt.content, protocol.Position{Line: 0, Character: tt.col}, nil)
			assert.Equal(t, tt.expected, ctx)
			assert.NotEqual(t, ContextDirective, ctx)
		})
	}
}

func TestCompletion_Directive_TypingAccProducesAccount(t *testing.T) {
	srv := NewServer()
	content := "acc"

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 3},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "account")
	assert.NotContains(t, labels, "include")

	for _, item := range result.Items {
		if item.Label == "account" {
			assert.Equal(t, protocol.CompletionItemKindKeyword, item.Kind)
			assert.Equal(t, "account ", item.InsertText)
			assert.Equal(t, "Directive", item.Detail)
			break
		}
	}
}

func TestCompletion_Directive_InsertTextHasTrailingSpace(t *testing.T) {
	srv := NewServer()
	content := "inc"

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 3},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	for _, item := range result.Items {
		if item.Label == "include" {
			assert.Equal(t, "include ", item.InsertText)
			return
		}
	}
	t.Fatal("include not found in completion items")
}

func TestCompletion_Directive_TextEditFromColumnZero(t *testing.T) {
	srv := NewServer()
	content := "acc"

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 3},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	for _, item := range result.Items {
		if item.Label == "account" {
			require.NotNil(t, item.TextEdit)
			assert.Equal(t, uint32(0), item.TextEdit.Range.Start.Character)
			assert.Equal(t, uint32(3), item.TextEdit.Range.End.Character)
			return
		}
	}
	t.Fatal("account not found in completion items")
}

func TestCompletion_Directive_FuzzyFiltering(t *testing.T) {
	srv := NewServer()
	content := "com"

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 3},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "commodity")
	assert.Contains(t, labels, "comment")
	assert.NotContains(t, labels, "include")
	assert.NotContains(t, labels, "account")
}

func TestCompletion_Directive_AllDirectivesShownOnSingleLetter(t *testing.T) {
	srv := NewServer()
	content := "2024-01-15 test\n    expenses:food  $50\n    assets:cash\n\na"

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 1},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "account")
	assert.Contains(t, labels, "alias")
	assert.Contains(t, labels, "apply account")
}

func TestCompletion_Directive_MultiWordDirective(t *testing.T) {
	srv := NewServer()
	content := "app"

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 3},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "apply account")
}

func TestDetermineContext_Directive_PeriodicAndAutoNotDirective(t *testing.T) {
	tests := []struct {
		name    string
		content string
		col     uint32
	}{
		{"periodic transaction", "~ monthly", 3},
		{"auto posting rule", "= expenses", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := determineCompletionContext(tt.content, protocol.Position{Line: 0, Character: tt.col}, nil)
			assert.NotEqual(t, ContextDirective, ctx)
		})
	}
}

func TestDetermineContext_Directive_Unicode(t *testing.T) {
	tests := []struct {
		name    string
		content string
		col     uint32
	}{
		{"CJK character", "日本語", 3},
		{"cyrillic character", "сч", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := determineCompletionContext(tt.content, protocol.Position{Line: 0, Character: tt.col}, nil)
			assert.Equal(t, ContextDirective, ctx)
		})
	}
}

func TestCompletion_Directive_CRLF(t *testing.T) {
	srv := NewServer()
	content := "2024-01-15 test\r\n    expenses:food  $50\r\n    assets:cash\r\n\r\nacc"

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 3},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "account")
}

func TestCompletion_Directive_BlockDirectiveNewlineInsertText(t *testing.T) {
	srv := NewServer()
	content := "com"

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 3},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	for _, item := range result.Items {
		if item.Label == "comment" {
			assert.Equal(t, "comment\n", item.InsertText, "block directive should have newline-terminated insertText")
			return
		}
	}
	t.Fatal("comment not found in completion items")
}

func TestCompletion_Directive_TextEditCoversFullLine(t *testing.T) {
	srv := NewServer()
	content := "apply a"

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 7},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	for _, item := range result.Items {
		if item.Label == "apply account" {
			require.NotNil(t, item.TextEdit)
			assert.Equal(t, uint32(0), item.TextEdit.Range.Start.Character)
			assert.Equal(t, uint32(7), item.TextEdit.Range.End.Character,
				"TextEdit should cover full typed text including spaces")
			return
		}
	}
	t.Fatal("apply account not found in completion items")
}

func TestCompletion_Directive_FindTokenEndScansToEndOfLine(t *testing.T) {
	end := findTokenEnd("apply account", 5, ContextDirective)
	assert.Equal(t, len("apply account"), end,
		"ContextDirective findTokenEnd should scan to end of line, not stop at space")
}

func TestCompletion_TagName_TextEditRange(t *testing.T) {
	srv := NewServer()
	// line 4: "2024-01-16 another ; proj" (25 chars)
	// ';' at 19, ' ' at 20, 'p' at 21
	content := `2024-01-15 test  ; project:alpha, status:done
    expenses:food  $50
    assets:cash

2024-01-16 another ; proj`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 25},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	for _, item := range result.Items {
		if item.Label == "project" {
			require.NotNil(t, item.TextEdit, "tag name completion should have TextEdit")
			assert.Equal(t, uint32(4), item.TextEdit.Range.Start.Line)
			assert.Equal(t, uint32(21), item.TextEdit.Range.Start.Character,
				"TextEdit should start after '; '")
			return
		}
	}
	t.Fatal("project not found in tag name completion items")
}

func TestCompletion_TagValue_TextEditRange(t *testing.T) {
	srv := NewServer()
	// line 8: "2024-01-17 new ; project:al" (27 chars)
	// ';' at 15, ':' at 24, 'a' at 25, 'l' at 26
	content := `2024-01-15 test1  ; project:alpha
    expenses:food  $50
    assets:cash

2024-01-16 test2  ; project:beta
    expenses:rent  $1000
    assets:bank

2024-01-17 new ; project:al`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 8, Character: 27},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	for _, item := range result.Items {
		if item.Label == "alpha" {
			require.NotNil(t, item.TextEdit, "tag value completion should have TextEdit")
			assert.Equal(t, uint32(8), item.TextEdit.Range.Start.Line)
			assert.Equal(t, uint32(25), item.TextEdit.Range.Start.Character,
				"TextEdit should start after 'project:'")
			return
		}
	}
	t.Fatal("alpha not found in tag value completion items")
}

func TestCompletion_TagName_FuzzyMatch(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test  ; project:alpha, status:done
    expenses:food  $50
    assets:cash

2024-01-16 another ; proj`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 25},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "project", "fuzzy match should find 'project' from 'proj'")
	assert.NotContains(t, labels, "status", "fuzzy match should filter out 'status'")
}

func TestCompletion_TagName_FindTokenEnd(t *testing.T) {
	end := findTokenEnd("; project:alpha, status:done", 2, ContextTagName)
	assert.Equal(t, 9, end, "ContextTagName findTokenEnd should stop at ':'")
}

func TestCompletion_TagValue_FindTokenEnd(t *testing.T) {
	end := findTokenEnd("; project:alpha, status:done", 11, ContextTagValue)
	assert.Equal(t, 15, end, "ContextTagValue findTokenEnd should stop at ','")
}

func TestCompletion_TagValue_FindTokenEnd_DoubleSpace(t *testing.T) {
	end := findTokenEnd("; project:some  value", 10, ContextTagValue)
	assert.Equal(t, 21, end,
		"ContextTagValue should NOT stop at double spaces (tag values can contain spaces)")
}

func TestCompletion_TagName_ExtractQuery(t *testing.T) {
	content := "2024-01-16 another ; proj"
	pos := protocol.Position{Line: 0, Character: 25}
	query := extractQueryText(content, pos, ContextTagName)
	assert.Equal(t, "proj", query)
}

func TestCompletion_TagValue_ExtractQuery(t *testing.T) {
	// "2024-01-16 another ; project:bet" — len=32, ':' at 28, 'b' at 29
	content := "2024-01-16 another ; project:bet"
	pos := protocol.Position{Line: 0, Character: 32}
	query := extractQueryText(content, pos, ContextTagValue)
	assert.Equal(t, "bet", query)
}

func TestCompletion_TagName_AfterComma_ExtractQuery(t *testing.T) {
	content := "2024-01-16 another ; project:alpha, sta"
	pos := protocol.Position{Line: 0, Character: 39}
	query := extractQueryText(content, pos, ContextTagName)
	assert.Equal(t, "sta", query)
}

func TestCompletion_TagName_CalculateRange(t *testing.T) {
	// "2024-01-16 another ; proj" — ';' at 19, ' ' at 20, 'p' at 21
	content := "2024-01-16 another ; proj"
	pos := protocol.Position{Line: 0, Character: 25}
	r := calculateTextEditRange(content, pos, ContextTagName)
	require.NotNil(t, r, "calculateTextEditRange should not return nil for ContextTagName")
	assert.Equal(t, uint32(21), r.Start.Character, "should start after '; '")
	assert.Equal(t, uint32(25), r.End.Character, "should end at end of partial tag name")
}

func TestCompletion_TagValue_CalculateRange(t *testing.T) {
	// "2024-01-16 another ; project:bet" — ':' at 28, 'b' at 29, len=32
	content := "2024-01-16 another ; project:bet"
	pos := protocol.Position{Line: 0, Character: 32}
	r := calculateTextEditRange(content, pos, ContextTagValue)
	require.NotNil(t, r, "calculateTextEditRange should not return nil for ContextTagValue")
	assert.Equal(t, uint32(29), r.Start.Character, "should start after 'project:'")
	assert.Equal(t, uint32(32), r.End.Character, "should end at end of partial value")
}

func TestCompletion_TagName_AfterComma_CalculateRange(t *testing.T) {
	// "2024-01-16 another ; project:alpha, sta" — ',' at 34, ' ' at 35, 's' at 36, len=39
	content := "2024-01-16 another ; project:alpha, sta"
	pos := protocol.Position{Line: 0, Character: 39}
	r := calculateTextEditRange(content, pos, ContextTagName)
	require.NotNil(t, r, "calculateTextEditRange should not return nil for ContextTagName after comma")
	assert.Equal(t, uint32(36), r.Start.Character, "should start after ', '")
	assert.Equal(t, uint32(39), r.End.Character, "should end at end of partial tag name")
}

func TestCompletion_TagName_CRLF(t *testing.T) {
	srv := NewServer()
	content := "2024-01-15 test  ; project:alpha, status:done\r\n    expenses:food  $50\r\n    assets:cash\r\n\r\n2024-01-16 another ; proj"

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 25},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "project", "CRLF: tag name fuzzy match should work")
}

func TestCompletion_TagValue_CRLF(t *testing.T) {
	srv := NewServer()
	content := "2024-01-15 test1  ; project:alpha\r\n    expenses:food  $50\r\n    assets:cash\r\n\r\n2024-01-16 test2  ; project:beta\r\n    expenses:rent  $1000\r\n    assets:bank\r\n\r\n2024-01-17 new ; project:"

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 8, Character: 25},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "alpha", "CRLF: tag value completion should work")
}

func TestCompletion_TagName_PostingComment(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test  ; project:alpha
    expenses:food  $50  ; category:groceries
    assets:cash

2024-01-16 another
    expenses:rent  $1000  ; `

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 5, Character: 27},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "project", "posting comment: should suggest tag names")
	assert.Contains(t, labels, "category", "posting comment: should suggest tag names")
}

func TestCompletion_TagValue_PostingComment(t *testing.T) {
	srv := NewServer()
	// line 5: "    expenses:rent  $1000  ; project:" (36 chars)
	// ';' at 26, ':' at 35, cursor at 36 = end of line
	content := `2024-01-15 test  ; project:alpha
    expenses:food  $50  ; category:groceries
    assets:cash

2024-01-16 another
    expenses:rent  $1000  ; project:`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 5, Character: 36},
		},
	}

	result, err := srv.Completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "alpha", "posting comment: should suggest tag values")
}

func TestCompletion_TagName_Unicode_ExtractQuery(t *testing.T) {
	// Verify extractQueryText handles Cyrillic in tag name context
	// "2024-01-16 описание ; про" = 25 UTF-16 units (Cyrillic = 1 UTF-16 unit each)
	content := "2024-01-16 описание ; про"
	pos := protocol.Position{Line: 0, Character: 25}
	query := extractQueryText(content, pos, ContextTagName)
	assert.Equal(t, "про", query, "Unicode: Cyrillic query text should be extracted")
}

func TestCompletion_TagValue_Unicode_ExtractQuery(t *testing.T) {
	// "2024-01-16 описание ; project:аль" = 33 UTF-16 units
	content := "2024-01-16 описание ; project:аль"
	pos := protocol.Position{Line: 0, Character: 33}
	query := extractQueryText(content, pos, ContextTagValue)
	assert.Equal(t, "аль", query, "Unicode: Cyrillic value query should be extracted")
}

func TestCompletion_TagName_Unicode_CalculateRange(t *testing.T) {
	// "2024-01-16 описание ; про" — ';' at byte 22, ' ' at 23, 'п' starts at 24
	// UTF-16: ';' at offset 17, ' ' at 18, 'п' at 19
	content := "2024-01-16 описание ; про"
	pos := protocol.Position{Line: 0, Character: 25}
	r := calculateTextEditRange(content, pos, ContextTagName)
	require.NotNil(t, r)
	assert.Equal(t, uint32(22), r.Start.Character, "Unicode: start after '; '")
	assert.Equal(t, uint32(25), r.End.Character, "Unicode: end at cursor")
}
