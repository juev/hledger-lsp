package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestWillSaveWaitUntil_AlwaysReturnsNil(t *testing.T) {
	srv := NewServer()
	content := "2024-01-15 grocery\n    expenses:food  $50\n    assets:cash   $-50"
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.WillSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Reason:       protocol.TextDocumentSaveReasonManual,
	}

	edits, err := srv.WillSaveWaitUntil(context.Background(), params)
	require.NoError(t, err)
	assert.Nil(t, edits, "WillSaveWaitUntil should always return nil to respect editor.formatOnSave")
}

func TestWillSaveWaitUntil_FormattingDisabled(t *testing.T) {
	srv := NewServer()
	settings := srv.getSettings()
	settings.Features.Formatting = false
	srv.setSettings(settings)

	content := "2024-01-15 grocery\n    expenses:food  $50\n    assets:cash"
	uri := uri.URI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.WillSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Reason:       protocol.TextDocumentSaveReasonManual,
	}

	edits, err := srv.WillSaveWaitUntil(context.Background(), params)
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestWillSaveWaitUntil_UnknownDocument(t *testing.T) {
	srv := NewServer()

	params := &protocol.WillSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: "file:///nonexistent.journal"},
		Reason:       protocol.TextDocumentSaveReasonManual,
	}

	edits, err := srv.WillSaveWaitUntil(context.Background(), params)
	require.NoError(t, err)
	assert.Nil(t, edits)
}

func TestWillSaveWaitUntil_RulesFile(t *testing.T) {
	srv := NewServer()
	// Journal-like content that the formatter would modify if not guarded
	content := "2024-01-15 grocery\n    expenses:food  $50\n    assets:cash   $-50"
	uri := uri.URI("file:///bank.rules")
	srv.documents.Store(uri, content)

	params := &protocol.WillSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Reason:       protocol.TextDocumentSaveReasonManual,
	}

	edits, err := srv.WillSaveWaitUntil(context.Background(), params)
	require.NoError(t, err)
	assert.Nil(t, edits, "rules files should not be formatted")
}

func TestFormat_RulesFile(t *testing.T) {
	srv := NewServer()
	// Journal-like content that the formatter would modify if not guarded
	content := "2024-01-15 grocery\n    expenses:food  $50\n    assets:cash   $-50"
	uri := uri.URI("file:///bank.rules")
	srv.documents.Store(uri, content)

	params := &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}

	edits, err := srv.Format(context.Background(), params)
	require.NoError(t, err)
	assert.Nil(t, edits, "rules files should not be formatted")
}
