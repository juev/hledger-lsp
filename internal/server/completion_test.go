package server

import (
	"context"
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
