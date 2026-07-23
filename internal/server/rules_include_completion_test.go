package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

// TestCompletion_RulesIncludedFields is the end-to-end verification for
// issue #24: when main.rules transitively includes common.rules which
// declares the CSV field layout, completion inside main.rules must surface
// those field names as %-prefixed references.
func TestCompletion_RulesIncludedFields(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.rules")
	commonPath := filepath.Join(dir, "common.rules")

	// common.rules declares the fields; main.rules does NOT, but includes
	// common.rules transitively.
	require.NoError(t, os.WriteFile(commonPath, []byte("fields date, payee, amount\n"), 0o644))

	// main.rules has an if block; the cursor will be on a non-indented
	// pattern line where field-reference completions fire.
	mainContent := "include common.rules\nif\n%pa"
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o644))

	srv := NewServer()
	mainURI := pathToURI(mainPath)
	srv.documents.Store(mainURI, mainContent)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: mainURI},
			// Cursor at the end of "%pa" on line 2 (0-based).
			Position: protocol.Position{Line: 2, Character: 3},
		},
	}

	result, err := srv.completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "%payee",
		"expected %%payee from included common.rules, got %v", labels)
	assert.Contains(t, labels, "%date",
		"expected %%date from included common.rules, got %v", labels)
	assert.Contains(t, labels, "%amount",
		"expected %%amount from included common.rules, got %v", labels)
}

// TestCompletion_RulesIncludedFields_InvalidationAfterDidChange verifies
// that editing a transitively-included .rules file (via DidChange) causes
// the next completion on the parent file to observe the updated fields,
// i.e. the rules loader cache is actually evicted by the DidChange hook.
func TestCompletion_RulesIncludedFields_InvalidationAfterDidChange(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.rules")
	commonPath := filepath.Join(dir, "common.rules")

	// Initial on-disk common.rules has only `date`.
	require.NoError(t, os.WriteFile(commonPath, []byte("fields date\n"), 0o644))

	mainContent := "include common.rules\nif\n%pa"
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o644))

	srv := NewServer()
	mainURI := pathToURI(mainPath)
	commonURI := pathToURI(commonPath)

	// Open both files in the editor.
	require.NoError(t, srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: mainURI, Text: mainContent},
	}))
	require.NoError(t, srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: commonURI, Text: "fields date\n"},
	}))

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: mainURI},
			Position:     protocol.Position{Line: 2, Character: 3},
		},
	}

	// First completion populates the loader cache with the v1 common.rules.
	first, err := srv.completion(context.Background(), params)
	require.NoError(t, err)
	firstLabels := extractLabels(first.Items)
	assert.Contains(t, firstLabels, "%date", "first completion should see %%date from v1")
	assert.NotContains(t, firstLabels, "%payee", "v1 common.rules has no payee yet")

	// User edits common.rules in the editor — a full-document change that
	// adds `payee`. The DidChange hook must invalidate the cache entry for
	// common.rules so the next completion reads the new editor content.
	require.NoError(t, srv.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: commonURI},
			Version:                2,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			&protocol.TextDocumentContentChangeWholeDocument{Text: "fields date, payee, amount\n"},
		},
	}))

	second, err := srv.completion(context.Background(), params)
	require.NoError(t, err)
	secondLabels := extractLabels(second.Items)
	assert.Contains(t, secondLabels, "%payee",
		"after DidChange invalidation, completion should see %%payee, got %v", secondLabels)
}

// TestCompletion_RulesIncludedFields_EditorContentPreferred verifies that
// when both main.rules and common.rules are open in the editor, unsaved
// edits to common.rules (via s.documents) are honoured by the rules loader
// ahead of the on-disk copy.
func TestCompletion_RulesIncludedFields_EditorContentPreferred(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.rules")
	commonPath := filepath.Join(dir, "common.rules")

	// Disk copy of common.rules has only one field.
	require.NoError(t, os.WriteFile(commonPath, []byte("fields date\n"), 0o644))

	mainContent := "include common.rules\nif\n%pa"
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o644))

	srv := NewServer()
	mainURI := pathToURI(mainPath)
	commonURI := pathToURI(commonPath)
	srv.documents.Store(mainURI, mainContent)
	// Open common.rules with unsaved edits that add `payee` field.
	srv.documents.Store(commonURI, "fields date, payee, amount\n")

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: mainURI},
			Position:     protocol.Position{Line: 2, Character: 3},
		},
	}

	result, err := srv.completion(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)

	labels := extractLabels(result.Items)
	assert.Contains(t, labels, "%payee",
		"expected %%payee from unsaved editor content of common.rules, got %v", labels)
}
