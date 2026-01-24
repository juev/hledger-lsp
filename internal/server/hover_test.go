package server

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/ast"
)

func TestPositionInRange(t *testing.T) {
	tests := []struct {
		name     string
		pos      protocol.Position
		rng      ast.Range
		expected bool
	}{
		{
			name: "position inside range",
			pos:  protocol.Position{Line: 1, Character: 5},
			rng: ast.Range{
				Start: ast.Position{Line: 2, Column: 1},
				End:   ast.Position{Line: 2, Column: 20},
			},
			expected: true,
		},
		{
			name: "position at start",
			pos:  protocol.Position{Line: 1, Character: 0},
			rng: ast.Range{
				Start: ast.Position{Line: 2, Column: 1},
				End:   ast.Position{Line: 2, Column: 20},
			},
			expected: true,
		},
		{
			name: "position at end",
			pos:  protocol.Position{Line: 1, Character: 19},
			rng: ast.Range{
				Start: ast.Position{Line: 2, Column: 1},
				End:   ast.Position{Line: 2, Column: 20},
			},
			expected: true,
		},
		{
			name: "position before range",
			pos:  protocol.Position{Line: 0, Character: 5},
			rng: ast.Range{
				Start: ast.Position{Line: 2, Column: 1},
				End:   ast.Position{Line: 2, Column: 20},
			},
			expected: false,
		},
		{
			name: "position after range",
			pos:  protocol.Position{Line: 2, Character: 5},
			rng: ast.Range{
				Start: ast.Position{Line: 2, Column: 1},
				End:   ast.Position{Line: 2, Column: 20},
			},
			expected: false,
		},
		{
			name: "position on same line but before column",
			pos:  protocol.Position{Line: 1, Character: 0},
			rng: ast.Range{
				Start: ast.Position{Line: 2, Column: 5},
				End:   ast.Position{Line: 2, Column: 20},
			},
			expected: false,
		},
		{
			name: "multiline range - position in middle line",
			pos:  protocol.Position{Line: 2, Character: 5},
			rng: ast.Range{
				Start: ast.Position{Line: 2, Column: 1},
				End:   ast.Position{Line: 4, Column: 10},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := positionInRange(tt.pos, tt.rng)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHover_Account(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery
    expenses:food  $50
    assets:cash  $-50

2024-01-16 restaurant
    expenses:food  $30
    assets:cash  $-30`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 1, Character: 10},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, result.Contents.Value, "expenses:food")
	assert.Contains(t, result.Contents.Value, "80")
}

func TestHover_Amount(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food  $50.00
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 1, Character: 17},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, result.Contents.Value, "50")
}

func TestHover_Payee(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash  $-50

2024-01-16 Grocery Store
    expenses:food  $30
    assets:cash  $-30

2024-01-17 Coffee Shop
    expenses:food  $5
    assets:cash  $-5`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 15},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, result.Contents.Value, "Grocery Store")
	assert.Contains(t, result.Contents.Value, "2")
}

func TestHover_EmptyPosition(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 3, Character: 0},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestHover_DocumentNotFound(t *testing.T) {
	srv := NewServer()

	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///nonexistent.journal",
			},
			Position: protocol.Position{Line: 0, Character: 0},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestHover_AmountWithCost(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 buy stocks
    assets:stocks  10 AAPL @ $150
    assets:cash  $-1500`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 1, Character: 20},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	content = strings.ToLower(result.Contents.Value)
	assert.True(t, strings.Contains(content, "10") || strings.Contains(content, "aapl"))
}

func TestHover_TagName(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery ; project:home
    expenses:food  $50
    assets:cash

2024-01-16 restaurant ; project:work
    expenses:food  $30
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	// Hover over "project" tag name (position right after semicolon and space)
	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 21}, // on "project"
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, result.Contents.Value, "Tag")
	assert.Contains(t, result.Contents.Value, "project")
	assert.Contains(t, result.Contents.Value, "2") // usage count
}

func TestHover_TagValue(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery ; project:home
    expenses:food  $50
    assets:cash

2024-01-16 restaurant ; project:home
    expenses:food  $30
    assets:cash

2024-01-17 office ; project:work
    expenses:food  $20
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	// Hover over "home" tag value
	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 30}, // on "home"
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, result.Contents.Value, "Tag")
	assert.Contains(t, result.Contents.Value, "project")
	assert.Contains(t, result.Contents.Value, "home")
	assert.Contains(t, result.Contents.Value, "2") // usage count for project:home
}

func TestHover_PostingTag(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery
    expenses:food  $50 ; category:groceries
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	// Hover over posting tag
	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 1, Character: 27}, // on "category"
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, result.Contents.Value, "Tag")
	assert.Contains(t, result.Contents.Value, "category")
}

func TestHover_TagWithValuesListed(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery ; project:home
    expenses:food  $50
    assets:cash

2024-01-16 restaurant ; project:work
    expenses:food  $30
    assets:cash

2024-01-17 office ; project:office
    expenses:food  $20
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	// Hover over tag name to see all values
	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 21}, // on "project"
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should show all unique values for this tag
	assert.Contains(t, result.Contents.Value, "Values")
}

func TestHover_TagWithEmptyValue(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery ; completed:
    expenses:food  $50
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	// Hover over tag name with empty value
	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 21}, // on "completed"
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, result.Contents.Value, "Tag")
	assert.Contains(t, result.Contents.Value, "completed")
	assert.Contains(t, result.Contents.Value, "(empty)")
}

func TestHover_TagValueEmpty(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery ; done:
    expenses:food  $50
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	// Hover over empty tag value (position after the colon)
	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 26}, // after "done:"
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, result.Contents.Value, "Tag")
	assert.Contains(t, result.Contents.Value, "done")
	assert.Contains(t, result.Contents.Value, "(empty)")
}

func TestHover_TagWithUnicodeValue(t *testing.T) {
	srv := NewServer()
	// ASCII tag name with Unicode value
	content := `2024-01-15 grocery ; project:дом
    expenses:food  $50
    assets:cash

2024-01-16 restaurant ; project:работа
    expenses:food  $30
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	// Hover over ASCII tag name "project"
	// "2024-01-15 grocery ; " = 21 chars, so "project" starts at char 21
	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 22}, // on "project"
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, result.Contents.Value, "Tag")
	assert.Contains(t, result.Contents.Value, "project")
	assert.Contains(t, result.Contents.Value, "2") // usage count
	// Check that Unicode values are listed
	assert.Contains(t, result.Contents.Value, "дом")
	assert.Contains(t, result.Contents.Value, "работа")
}

func TestHover_TagValueWithUnicodeContent(t *testing.T) {
	srv := NewServer()
	// ASCII tag name with Unicode value
	content := `2024-01-15 grocery ; project:дом
    expenses:food  $50
    assets:cash

2024-01-16 restaurant ; project:дом
    expenses:food  $30
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	// Hover over Unicode tag value "дом"
	// "2024-01-15 grocery ; project:" = 30 chars, "дом" starts at char 30
	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 30}, // on "дом"
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, result.Contents.Value, "Tag")
	assert.Contains(t, result.Contents.Value, "project")
	assert.Contains(t, result.Contents.Value, "дом")
	assert.Contains(t, result.Contents.Value, "2") // usage count for project:дом
}

func TestHover_PartialDateWithComment(t *testing.T) {
	srv := NewServer()
	// Y 2024 directive sets default year for partial dates
	content := `Y 2024

01-22 Магазин ; просто текст
    expenses:food  $50
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	// Hover over plain comment text (no tags) - should return nil
	// "01-22 Магазин ; просто текст"
	// Position on "просто" which is in the comment area
	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 2, Character: 18}, // on "просто"
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	// Comment without tags should not show any hover
	assert.Nil(t, result)
}

func TestHover_PartialDatePayee(t *testing.T) {
	srv := NewServer()
	content := `Y 2024

01-22 Магазин
    expenses:food  $50
    assets:cash

01-23 Магазин
    expenses:food  $30
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	// Hover over "Магазин" payee with partial date
	// "01-22 Магазин" - payee starts at column 6 (0-indexed: 6)
	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 2, Character: 8}, // on "Магазин"
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, result.Contents.Value, "Payee")
	assert.Contains(t, result.Contents.Value, "Магазин")
	assert.Contains(t, result.Contents.Value, "2") // transaction count
}

func TestHover_PartialDateWithStatus(t *testing.T) {
	srv := NewServer()
	content := `Y 2024

01-22 * Магазин
    expenses:food  $50
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	// Hover over "Магазин" payee with partial date and status
	// "01-22 * Магазин" - payee starts at column 8 (0-indexed: 8)
	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 2, Character: 10}, // on "Магазин"
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, result.Contents.Value, "Payee")
	assert.Contains(t, result.Contents.Value, "Магазин")
}
