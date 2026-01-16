package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func TestOnTypeFormatting_IndentAfterTransactionHeader(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 Grocery Store
`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
		Position: protocol.Position{Line: 1, Character: 0},
		Ch:       "\n",
		Options: protocol.FormattingOptions{
			TabSize:      4,
			InsertSpaces: true,
		},
	}

	edits, err := srv.OnTypeFormatting(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, edits)
	require.Len(t, edits, 1, "Should return one edit for indentation")

	assert.Equal(t, "    ", edits[0].NewText, "Should indent with 4 spaces after transaction header")
}

func TestOnTypeFormatting_IndentAfterPosting(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 Grocery Store
    expenses:food  $50
`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
		Position: protocol.Position{Line: 2, Character: 0},
		Ch:       "\n",
		Options: protocol.FormattingOptions{
			TabSize:      4,
			InsertSpaces: true,
		},
	}

	edits, err := srv.OnTypeFormatting(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, edits)
	require.Len(t, edits, 1, "Should return one edit for indentation")

	assert.Equal(t, "    ", edits[0].NewText, "Should indent with 4 spaces after posting")
}

func TestOnTypeFormatting_NoIndentAfterEmptyLine(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash

`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
		Position: protocol.Position{Line: 4, Character: 0},
		Ch:       "\n",
		Options: protocol.FormattingOptions{
			TabSize:      4,
			InsertSpaces: true,
		},
	}

	edits, err := srv.OnTypeFormatting(context.Background(), params)
	require.NoError(t, err)

	assert.Empty(t, edits, "Should not indent after empty line (end of transaction)")
}

func TestOnTypeFormatting_UsesTabWhenConfigured(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 Grocery Store
`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
		Position: protocol.Position{Line: 1, Character: 0},
		Ch:       "\n",
		Options: protocol.FormattingOptions{
			TabSize:      4,
			InsertSpaces: false,
		},
	}

	edits, err := srv.OnTypeFormatting(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, edits)
	require.Len(t, edits, 1)

	assert.Equal(t, "\t", edits[0].NewText, "Should use tab when InsertSpaces is false")
}

func TestOnTypeFormatting_IgnoresNonNewlineCharacter(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 Grocery Store
`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
		Position: protocol.Position{Line: 1, Character: 0},
		Ch:       "a",
		Options: protocol.FormattingOptions{
			TabSize:      4,
			InsertSpaces: true,
		},
	}

	edits, err := srv.OnTypeFormatting(context.Background(), params)
	require.NoError(t, err)
	assert.Nil(t, edits, "Should return nil for non-newline characters")
}
