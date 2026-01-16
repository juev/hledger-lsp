package server

import (
	"context"
	"strings"
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

func TestOnTypeFormatting_NoIndentAfterPostingWithAmount(t *testing.T) {
	srv := NewServer()
	content := "2024-01-15 Grocery Store\n    expenses:food  $50\n    "

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
		Position: protocol.Position{Line: 2, Character: 4},
		Ch:       "\n",
		Options: protocol.FormattingOptions{
			TabSize:      4,
			InsertSpaces: true,
		},
	}

	edits, err := srv.OnTypeFormatting(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, edits)
	require.Len(t, edits, 1, "Should return one edit to remove whitespace")

	assert.Equal(t, uint32(0), edits[0].Range.Start.Character)
	assert.Equal(t, uint32(4), edits[0].Range.End.Character)
	assert.Equal(t, "", edits[0].NewText, "Should remove whitespace after posting with amount")
}

func TestOnTypeFormatting_IndentAfterPostingWithoutAmount(t *testing.T) {
	srv := NewServer()
	content := "2024-01-15 Grocery Store\n    expenses:food\n    "

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
		Position: protocol.Position{Line: 2, Character: 4},
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

	assert.Equal(t, uint32(0), edits[0].Range.Start.Character)
	assert.Equal(t, uint32(4), edits[0].Range.End.Character)
	assert.Equal(t, "    ", edits[0].NewText, "Should replace whitespace with 4 spaces")
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

func TestOnTypeFormatting_TabInsertsSpacesToAmountColumn(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 Grocery Store
    expenses:food	`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
		Position: protocol.Position{Line: 1, Character: 18},
		Ch:       "\t",
		Options: protocol.FormattingOptions{
			TabSize:      4,
			InsertSpaces: true,
		},
	}

	edits, err := srv.OnTypeFormatting(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, edits)
	require.Len(t, edits, 1, "Should return one edit for amount alignment")

	assert.Equal(t, uint32(1), edits[0].Range.Start.Line)
	assert.Equal(t, uint32(17), edits[0].Range.Start.Character)
	assert.Equal(t, uint32(1), edits[0].Range.End.Line)
	assert.Equal(t, uint32(18), edits[0].Range.End.Character)
	assert.True(t, len(edits[0].NewText) >= 2, "Should insert at least 2 spaces for amount separator")
}

func TestOnTypeFormatting_TabUsesFileAmountColumn(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 Previous transaction
    expenses:groceries                       $100.00
    assets:bank

2024-01-16 New transaction
    expenses:food	`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
		Position: protocol.Position{Line: 5, Character: 18},
		Ch:       "\t",
		Options: protocol.FormattingOptions{
			TabSize:      4,
			InsertSpaces: true,
		},
	}

	edits, err := srv.OnTypeFormatting(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, edits)
	require.Len(t, edits, 1)

	expectedSpaces := 45 - 17
	assert.Equal(t, strings.Repeat(" ", expectedSpaces), edits[0].NewText,
		"Should align to column 45 based on existing postings")
}

func TestOnTypeFormatting_TabNoActionWhenAmountExists(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 Grocery Store
    expenses:food  $50.00	`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
		Position: protocol.Position{Line: 1, Character: 26},
		Ch:       "\t",
		Options: protocol.FormattingOptions{
			TabSize:      4,
			InsertSpaces: true,
		},
	}

	edits, err := srv.OnTypeFormatting(context.Background(), params)
	require.NoError(t, err)
	assert.Empty(t, edits, "Should not insert spaces when amount already exists")
}

func TestOnTypeFormatting_TabNoActionOnNonPostingLine(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 Grocery Store	`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
		Position: protocol.Position{Line: 0, Character: 25},
		Ch:       "\t",
		Options: protocol.FormattingOptions{
			TabSize:      4,
			InsertSpaces: true,
		},
	}

	edits, err := srv.OnTypeFormatting(context.Background(), params)
	require.NoError(t, err)
	assert.Empty(t, edits, "Should not insert spaces on transaction header line")
}

func TestOnTypeFormatting_TabNoActionWhenCursorNotAtEnd(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 Grocery Store
    expenses:food`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
		Position: protocol.Position{Line: 1, Character: 10},
		Ch:       "\t",
		Options: protocol.FormattingOptions{
			TabSize:      4,
			InsertSpaces: true,
		},
	}

	edits, err := srv.OnTypeFormatting(context.Background(), params)
	require.NoError(t, err)
	assert.Empty(t, edits, "Should not insert spaces when cursor is not at end of account")
}

func TestOnTypeFormatting_TabWithUnicodeAccount(t *testing.T) {
	srv := NewServer()
	content := "2024-01-15 Покупки\n    расходы:продукты\t"

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	unicodeAccountLen := 4 + 16 + 1
	params := &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
		Position: protocol.Position{Line: 1, Character: uint32(unicodeAccountLen)},
		Ch:       "\t",
		Options: protocol.FormattingOptions{
			TabSize:      4,
			InsertSpaces: true,
		},
	}

	edits, err := srv.OnTypeFormatting(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, edits, "Should handle Unicode account names")
	require.Len(t, edits, 1)

	assert.True(t, len(edits[0].NewText) >= 2, "Should insert at least 2 spaces")
}

func TestOnTypeFormatting_TabWithLongAccount(t *testing.T) {
	srv := NewServer()
	longAccount := "expenses:very:long:account:name:that:exceeds:default:column"
	content := "2024-01-15 Test\n    " + longAccount + "\t"

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	lineLen := 4 + len(longAccount) + 1
	params := &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
		Position: protocol.Position{Line: 1, Character: uint32(lineLen)},
		Ch:       "\t",
		Options: protocol.FormattingOptions{
			TabSize:      4,
			InsertSpaces: true,
		},
	}

	edits, err := srv.OnTypeFormatting(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, edits)
	require.Len(t, edits, 1)

	assert.Equal(t, "  ", edits[0].NewText, "Should insert minimum 2 spaces for long account")
}
