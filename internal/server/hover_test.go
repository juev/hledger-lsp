package server

import (
	"context"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/formatter"
)

func newMarkdownHoverServer() *Server {
	srv := NewServer()
	srv.clientCapabilities.hoverContentFormats = []protocol.MarkupKind{protocol.MarkupKindMarkdown}
	return srv
}

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

	srv.documents.Store(uri.URI("file:///test.journal"), content)

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

	assert.Contains(t, hoverContent(result), "expenses:food")
	assert.Contains(t, hoverContent(result), "80")
}

func TestHover_UsesClientPreferredMarkup(t *testing.T) {
	srv := NewServer()
	srv.clientCapabilities.hoverContentFormats = []protocol.MarkupKind{protocol.MarkupKindPlainText, protocol.MarkupKindMarkdown}
	srv.documents.Store(uri.URI("file:///test.journal"), `2024-01-15 grocery
    expenses:food  $50
    assets:cash  $-50`)

	result, err := srv.Hover(context.Background(), &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: "file:///test.journal"},
			Position:     protocol.Position{Line: 1, Character: 10},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	content := result.Contents.(*protocol.MarkupContent)
	assert.Equal(t, protocol.MarkupKindPlainText, content.Kind)
	assert.NotContains(t, content.Value, "**")
}

func TestHover_Amount(t *testing.T) {
	srv := NewServer()
	//                                        0         1         2
	//                                        0123456789012345678901234
	content := `2024-01-15 test
    expenses:food  $50.00
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

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

	assert.Contains(t, hoverContent(result), "$50")
}

func TestHover_AmountSuffixCommodity(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food  50.00 EUR
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 1, Character: 19},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, hoverContent(result), "50.00 EUR")
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

	srv.documents.Store(uri.URI("file:///test.journal"), content)

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

	assert.Contains(t, hoverContent(result), "Grocery Store")
	assert.Contains(t, hoverContent(result), "2")
}

func TestHover_EmptyPosition(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

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

	srv.documents.Store(uri.URI("file:///test.journal"), content)

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

	content = strings.ToLower(hoverContent(result))
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

	srv.documents.Store(uri.URI("file:///test.journal"), content)

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

	assert.Contains(t, hoverContent(result), "Tag")
	assert.Contains(t, hoverContent(result), "project")
	assert.Contains(t, hoverContent(result), "2") // usage count
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

	srv.documents.Store(uri.URI("file:///test.journal"), content)

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

	assert.Contains(t, hoverContent(result), "Tag")
	assert.Contains(t, hoverContent(result), "project")
	assert.Contains(t, hoverContent(result), "home")
	assert.Contains(t, hoverContent(result), "2") // usage count for project:home
}

func TestHover_PostingTag(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery
    expenses:food  $50 ; category:groceries
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

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

	assert.Contains(t, hoverContent(result), "Tag")
	assert.Contains(t, hoverContent(result), "category")
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

	srv.documents.Store(uri.URI("file:///test.journal"), content)

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
	assert.Contains(t, hoverContent(result), "Values")
}

func TestHover_TagWithEmptyValue(t *testing.T) {
	srv := newMarkdownHoverServer()
	content := `2024-01-15 grocery ; completed:
    expenses:food  $50
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

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

	assert.Contains(t, hoverContent(result), "Tag")
	assert.Contains(t, hoverContent(result), "completed")
	assert.Contains(t, hoverContent(result), "(empty)")
}

func TestHover_TagValueEmpty(t *testing.T) {
	srv := newMarkdownHoverServer()
	content := `2024-01-15 grocery ; done:
    expenses:food  $50
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

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

	assert.Contains(t, hoverContent(result), "Tag")
	assert.Contains(t, hoverContent(result), "done")
	assert.Contains(t, hoverContent(result), "(empty)")
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

	srv.documents.Store(uri.URI("file:///test.journal"), content)

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

	assert.Contains(t, hoverContent(result), "Tag")
	assert.Contains(t, hoverContent(result), "project")
	assert.Contains(t, hoverContent(result), "2") // usage count
	// Check that Unicode values are listed
	assert.Contains(t, hoverContent(result), "дом")
	assert.Contains(t, hoverContent(result), "работа")
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

	srv.documents.Store(uri.URI("file:///test.journal"), content)

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

	assert.Contains(t, hoverContent(result), "Tag")
	assert.Contains(t, hoverContent(result), "project")
	assert.Contains(t, hoverContent(result), "дом")
	assert.Contains(t, hoverContent(result), "2") // usage count for project:дом
}

func TestHover_PayeeWithCode(t *testing.T) {
	srv := NewServer()
	content := `2026-01-01 (test:123) test
    expenses:food  $50
    assets:cash

2026-01-02 test
    expenses:food  $30
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

	// Hover on payee "test" after code (test:123)
	// "2026-01-01 (test:123) test" — "test" starts at column 22 (0-indexed)
	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 23},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result, "hover on payee after code should return result")

	assert.Contains(t, hoverContent(result), "Payee")
	assert.Contains(t, hoverContent(result), "test")
	assert.Contains(t, hoverContent(result), "2")
}

func TestHover_InsideCodeParentheses(t *testing.T) {
	srv := NewServer()
	content := `2026-01-01 (test:123) test
    expenses:food  $50
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

	// Hover inside code parentheses "(test:123)" — should NOT return payee
	// Column 14 (0-indexed) is inside "test:123"
	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 14},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	assert.Nil(t, result, "hover inside code parentheses should NOT return payee hover")
}

func TestHover_PayeeWithStatusAndCode(t *testing.T) {
	srv := NewServer()
	content := `2026-01-01 * (test:123) grocery store
    expenses:food  $50
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

	// Hover on payee "grocery store" after status and code
	// "2026-01-01 * (test:123) grocery store" — "grocery" starts at column 24 (0-indexed)
	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 0, Character: 27},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result, "hover on payee after status+code should return result")

	assert.Contains(t, hoverContent(result), "Payee")
	assert.Contains(t, hoverContent(result), "grocery store")
}

func TestHover_PartialDateWithComment(t *testing.T) {
	srv := NewServer()
	// Y 2024 directive sets default year for partial dates
	content := `Y 2024

01-22 Магазин ; просто текст
    expenses:food  $50
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

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

	srv.documents.Store(uri.URI("file:///test.journal"), content)

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

	assert.Contains(t, hoverContent(result), "Payee")
	assert.Contains(t, hoverContent(result), "Магазин")
	assert.Contains(t, hoverContent(result), "2") // transaction count
}

func TestHover_PartialDateWithStatus(t *testing.T) {
	srv := NewServer()
	content := `Y 2024

01-22 * Магазин
    expenses:food  $50
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

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

	assert.Contains(t, hoverContent(result), "Payee")
	assert.Contains(t, hoverContent(result), "Магазин")
}

func TestDefaultCommoditySymbol(t *testing.T) {
	t.Run("returns symbol from DefaultCommodityDirective", func(t *testing.T) {
		directives := []ast.Directive{
			ast.DefaultCommodityDirective{Symbol: "EUR"},
		}
		assert.Equal(t, "EUR", defaultCommoditySymbol(directives))
	})

	t.Run("returns empty string when no default commodity", func(t *testing.T) {
		directives := []ast.Directive{
			ast.AccountDirective{Account: ast.Account{Name: "expenses:food"}},
		}
		assert.Equal(t, "", defaultCommoditySymbol(directives))
	})

	t.Run("returns empty string for nil directives", func(t *testing.T) {
		assert.Equal(t, "", defaultCommoditySymbol(nil))
	})

	t.Run("returns last directive when multiple exist", func(t *testing.T) {
		directives := []ast.Directive{
			ast.DefaultCommodityDirective{Symbol: "EUR"},
			ast.AccountDirective{Account: ast.Account{Name: "expenses:food"}},
			ast.DefaultCommodityDirective{Symbol: "USD"},
		}
		assert.Equal(t, "USD", defaultCommoditySymbol(directives))
	})
}

func TestHover_AmountWithDefaultCommodity(t *testing.T) {
	srv := NewServer()
	content := `D 1.000,00 EUR

2024-01-15 test
    expenses:food  1.000,00
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 3, Character: 19},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, hoverContent(result), "1.000,00 EUR")
}

func TestHover_AmountWithDefaultCommodityPrefix(t *testing.T) {
	srv := NewServer()
	content := `D $1,000.00

2024-01-15 test
    expenses:food  1,000.00
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 3, Character: 19},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, hoverContent(result), "$1,000.00")
}

func TestHover_AmountWithDefaultCommodity_ExplicitOverrides(t *testing.T) {
	srv := NewServer()
	content := `D 1.000,00 EUR

2024-01-15 test
    expenses:food  $50.00
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 3, Character: 19},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, hoverContent(result), "$50.00")
	assert.NotContains(t, hoverContent(result), "EUR")
}

func TestHover_AmountWithCommodityDirective(t *testing.T) {
	srv := NewServer()
	content := `commodity 1.000,00 RUB

2024-01-15 test
    expenses:food  10000 RUB
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 3, Character: 19},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, hoverContent(result), "10.000,00 RUB")
}

func TestHover_AmountNoDirectivesFallback(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food  50.00 EUR
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 1, Character: 19},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, hoverContent(result), "50.00 EUR")
}

func TestHover_AccountBalanceWithDefaultCommodity(t *testing.T) {
	srv := NewServer()
	content := `D $1,000.00

2024-01-15 grocery
    expenses:food  $80
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 3, Character: 10},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, hoverContent(result), "$80.00")
	assert.NotContains(t, hoverContent(result), "80 $")
}

func TestHover_AccountBalanceWithCommodityDirective(t *testing.T) {
	srv := NewServer()
	content := `commodity 1.000,00 EUR

2024-01-15 grocery
    expenses:food  500 EUR
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 3, Character: 10},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, hoverContent(result), "500,00 EUR")
}

func TestHover_AccountBalanceWithPrefixCommodity(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery
    expenses:food  $80
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

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

	assert.Contains(t, hoverContent(result), "$80")
	assert.NotContains(t, hoverContent(result), "80 $")
}

func TestHover_AccountBalanceNegativeWithDirective(t *testing.T) {
	srv := NewServer()
	content := `D $1,000.00

2024-01-15 refund
    assets:cash  $80
    expenses:food  $-80`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 10},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, hoverContent(result), "-$80.00")
}

func TestHover_CostFormattedWithCommodityDirective(t *testing.T) {
	srv := NewServer()
	content := `commodity 1.000,00 EUR

2024-01-15 buy stocks
    assets:stocks  10 AAPL @ 15000 EUR
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 3, Character: 20},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, hoverContent(result), "15.000,00 EUR")
}

func TestHover_AccountBalanceMergesEmptyCommodityWithDefault(t *testing.T) {
	srv := NewServer()
	// Default commodity is RUB. One posting on the account has explicit "RUB",
	// another has no commodity (stored as key ""). Both should be merged into
	// a single "RUB" balance line on hover.
	content := `D 1.000,00 RUB

2024-01-15 Магазин
    Расходы:Еда  500,00 RUB
    Активы:Банк  -500,00 RUB

2024-01-16 Транспорт
    Расходы:Транспорт  71,00
    Активы:Банк  -71,00`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

	// Hover over Активы:Банк — should show single consolidated RUB balance
	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 4, Character: 6},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should show exactly one balance line, not two separate "RUB" lines
	balanceSection := hoverContent(result)
	assert.Contains(t, balanceSection, "RUB")
	// Count occurrences of "RUB" in balance lines — should be exactly 1
	rubCount := strings.Count(balanceSection, "RUB")
	assert.Equal(t, 1, rubCount, "expected single consolidated RUB balance line, got %d occurrences:\n%s", rubCount, balanceSection)
}

func TestHover_AccountBalanceNoDefaultCommodityKeepsSeparate(t *testing.T) {
	srv := NewServer()
	// No default commodity directive. Postings with explicit commodity and without
	// should remain separate — two balance lines.
	content := `2024-01-15 Магазин
    Расходы:Еда  500,00 RUB
    Активы:Банк  -500,00 RUB

2024-01-16 Транспорт
    Расходы:Транспорт  71,00
    Активы:Банк  -71,00`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

	// Hover over Активы:Банк
	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 2, Character: 6},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Without default commodity, we can't merge — should have two balance lines
	assert.Contains(t, hoverContent(result), "Balance")
	assert.Contains(t, hoverContent(result), "RUB")
	assert.Contains(t, hoverContent(result), "-71")
}

func TestHover_AccountBalanceDecimalPrecision(t *testing.T) {
	srv := newMarkdownHoverServer()
	content := `2024-01-15 grocery
    expenses:food  25.70 USD
    assets:cash  -25.70 USD

2024-01-16 restaurant
    expenses:food  9.00 USD
    assets:cash  -9.00 USD`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

	params := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: "file:///test.journal",
			},
			Position: protocol.Position{Line: 1, Character: 6},
		},
	}

	result, err := srv.Hover(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, hoverContent(result), "expenses:food")
	assert.Contains(t, hoverContent(result), "34.70")
	assert.Contains(t, hoverContent(result), "Postings:** 2")
}

func TestEnrichCommodityFormatsFromTransactions_DecimalPlaces(t *testing.T) {
	transactions := []ast.Transaction{
		{
			Postings: []ast.Posting{
				{
					Amount: &ast.Amount{
						Quantity:    decimal.NewFromFloat(25.70),
						RawQuantity: "25.70",
						Commodity:   ast.Commodity{Symbol: "USD", Position: ast.CommodityRight},
					},
				},
				{
					Amount: &ast.Amount{
						Quantity:    decimal.NewFromFloat(9.00),
						RawQuantity: "9.00",
						Commodity:   ast.Commodity{Symbol: "USD", Position: ast.CommodityRight},
					},
				},
			},
		},
	}

	formats := make(map[string]formatter.CommodityFormat)
	enrichCommodityFormatsFromTransactions(formats, transactions)

	usdFormat, ok := formats["USD"]
	require.True(t, ok, "USD format should be present")
	assert.True(t, usdFormat.HasDecimal, "HasDecimal should be true")
	assert.Equal(t, 2, usdFormat.DecimalPlaces, "DecimalPlaces should be 2")
}

func TestHover_AccountBalanceSpecificCommodityFormatRoundsAggregate(t *testing.T) {
	srv := NewServer()
	content := `commodity 1.000,00 RUB

2024-01-15 grocery
    expenses:food  1000,123 RUB
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

	result, err := srv.Hover(context.Background(), &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: "file:///test.journal"},
			Position:     protocol.Position{Line: 3, Character: 10},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, hoverContent(result), "1.000,12 RUB")
	assert.NotContains(t, hoverContent(result), "1.000,123 RUB")
}

func TestHover_AccountBalanceDefaultFormatRoundsOtherCommodityAggregate(t *testing.T) {
	srv := NewServer()
	content := `D 1.000,00 RUB

2024-01-15 grocery
    expenses:food  1000.123 EUR
    assets:cash`

	srv.documents.Store(uri.URI("file:///test.journal"), content)

	result, err := srv.Hover(context.Background(), &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: "file:///test.journal"},
			Position:     protocol.Position{Line: 3, Character: 10},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, hoverContent(result), "1.000,12 EUR")
	assert.NotContains(t, hoverContent(result), "1.000.123 EUR")
}

func TestEnrichCommodityFormatsFromTransactionsDoesNotMutateConfiguredFormat(t *testing.T) {
	configured := map[string]formatter.CommodityFormat{
		"RUB": {
			NumberFormat: formatter.NumberFormat{
				DecimalMark:   ',',
				ThousandsSep:  ".",
				DecimalPlaces: 2,
				HasDecimal:    true,
			},
			Position:     ast.CommodityRight,
			SpaceBetween: true,
		},
	}
	want := configured["RUB"]
	transactions := []ast.Transaction{{
		Postings: []ast.Posting{{
			Amount: &ast.Amount{
				Quantity:    decimal.RequireFromString("1000.123"),
				RawQuantity: "1000.123",
				Commodity:   ast.Commodity{Symbol: "RUB", Position: ast.CommodityRight},
			},
		}},
	}}

	enrichCommodityFormatsFromTransactions(configured, transactions)

	assert.Equal(t, want, configured["RUB"])
}
