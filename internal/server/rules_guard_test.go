package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// journalLikeContent mimics a hledger journal; handlers must not process it for .rules files.
const journalLikeContent = "2024-01-15 grocery\n    expenses:food  $50\n    assets:cash"

// rulesFileURI is a .rules extension URI that should trigger early-return guards.
const rulesFileURI = uri.URI("file:///bank.rules")

// hoverPos is over "expenses:food" in journalLikeContent (0-indexed line=1, char=4).
var hoverPos = protocol.Position{Line: 1, Character: 4}

func TestRulesFileGuards_Hover(t *testing.T) {
	srv := NewServer()
	srv.documents.Store(rulesFileURI, journalLikeContent)

	result, err := srv.Hover(context.Background(), &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: rulesFileURI},
			Position:     hoverPos,
		},
	})
	require.NoError(t, err)
	assert.Nil(t, result, "Hover must return nil for .rules file")
}

func TestRulesFileGuards_Definition(t *testing.T) {
	srv := NewServer()
	srv.documents.Store(rulesFileURI, journalLikeContent)

	result, err := srv.definition(context.Background(), &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: rulesFileURI},
			Position:     hoverPos,
		},
	})
	require.NoError(t, err)
	assert.Nil(t, result, "Definition must return nil for .rules file")
}

func TestRulesFileGuards_References(t *testing.T) {
	srv := NewServer()
	srv.documents.Store(rulesFileURI, journalLikeContent)

	result, err := srv.References(context.Background(), &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: rulesFileURI},
			Position:     hoverPos,
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: true},
	})
	require.NoError(t, err)
	assert.Nil(t, result, "References must return nil for .rules file")
}

func TestRulesFileGuards_PrepareRename(t *testing.T) {
	srv := NewServer()
	srv.documents.Store(rulesFileURI, journalLikeContent)

	result, err := srv.prepareRename(context.Background(), &protocol.PrepareRenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: rulesFileURI},
			Position:     hoverPos,
		},
	})
	require.NoError(t, err)
	assert.Nil(t, result, "PrepareRename must return nil for .rules file")
}

func TestRulesFileGuards_Rename(t *testing.T) {
	srv := NewServer()
	srv.documents.Store(rulesFileURI, journalLikeContent)

	result, err := srv.Rename(context.Background(), &protocol.RenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: rulesFileURI},
			Position:     hoverPos,
		},
		NewName: "expenses:renamed",
	})
	require.NoError(t, err)
	assert.Nil(t, result, "Rename must return nil for .rules file")
}

func TestRulesFileGuards_CodeAction(t *testing.T) {
	srv := NewServer()
	srv.documents.Store(rulesFileURI, journalLikeContent)

	result, err := srv.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: rulesFileURI},
	})
	require.NoError(t, err)
	assert.Nil(t, result, "CodeAction must return nil for .rules file")
}

func TestRulesFileGuards_DocumentHighlight(t *testing.T) {
	srv := NewServer()
	srv.documents.Store(rulesFileURI, journalLikeContent)

	result, err := srv.DocumentHighlight(context.Background(), &protocol.DocumentHighlightParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: rulesFileURI},
			Position:     hoverPos,
		},
	})
	require.NoError(t, err)
	assert.Nil(t, result, "DocumentHighlight must return nil for .rules file")
}
