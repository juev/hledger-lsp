package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestHover_EmojiPayeeRangeIsUTF16(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///hover-emoji.journal")
	content := "2024-01-15 grocery 🛒 store\n    expenses:food  $50.00\n    assets:cash\n"

	ts.StoreDocument(docURI, content)

	// The payee occupies UTF-16 columns 11..27 (11 + 7 letters + space + 2 units
	// for the emoji + space + 5 letters). A rune-based range would end at 26.
	hover, err := ts.Hover(context.Background(), &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 0, Character: 24},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, hover)
	require.NotNil(t, hover.Range)
	assert.Equal(t, uint32(11), hover.Range.Start.Character)
	assert.Equal(t, uint32(27), hover.Range.End.Character)
	assert.NotEmpty(t, hoverContent(hover))
}

func TestHover_ReachesTextAfterAstralCharacter(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///hover-tail.journal")
	content := "2024-01-15 🛒 grocery store\n    expenses:food  $50.00\n    assets:cash\n"

	ts.StoreDocument(docURI, content)

	// The cursor sits in the word "store", which follows the emoji.
	hover, err := ts.Hover(context.Background(), &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 0, Character: 22},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, hover, "hit testing must convert UTF-16 columns to runes before comparing")
	assert.Contains(t, hoverContent(hover), "grocery store")
}

func TestDefinition_ReachesAccountAfterAstralCharacter(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///definition-emoji.journal")
	content := `account Активы:🍜

2024-01-15 lunch
    Расходы:🍜  $10.00
    Активы:🍜
`

	ts.StoreDocument(docURI, content)

	// "    Активы:🍜" — the emoji occupies two UTF-16 units, so the account ends
	// at column 12 while a rune-based comparison would stop at 11.
	locations, err := ts.definition(docURI, 3, 12)
	require.NoError(t, err)
	require.NotEmpty(t, locations, "definition must reach the account behind the emoji")
}

func TestDocumentHighlight_ReachesAccountAfterAstralCharacter(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///highlight-emoji.journal")
	content := `account Активы:🍜

2024-01-15 lunch
    Активы:🍜  $50.00
    Расходы:🍜
`

	ts.StoreDocument(docURI, content)

	highlights, err := ts.DocumentHighlight(context.Background(), &protocol.DocumentHighlightParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 3, Character: 12},
		},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, highlights, "highlight must reach the account behind the emoji")
}
