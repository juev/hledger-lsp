package server

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func openDocument(t testing.TB, srv *Server, docURI uri.URI, text string) {
	t.Helper()
	require.NoError(t, srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: docURI, Version: 1, Text: text},
	}))
}

func replaceDocument(t testing.TB, srv *Server, docURI uri.URI, version int32, text string) {
	t.Helper()
	require.NoError(t, srv.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: docURI},
			Version:                version,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			&protocol.TextDocumentContentChangeWholeDocument{Text: text},
		},
	}))
}

func requestHintLabels(t testing.TB, srv *Server, docURI uri.URI) []protocol.InlayHintLabel {
	t.Helper()
	hints, err := srv.InlayHint(context.Background(), &protocol.InlayHintParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
		Range:        protocol.Range{End: protocol.Position{Line: 100}},
	})
	require.NoError(t, err)
	labels := make([]protocol.InlayHintLabel, 0, len(hints))
	for _, hint := range hints {
		labels = append(labels, hint.Label)
	}
	return labels
}

func TestInlayHint_RunningBalanceFollowsEditsToIncludedFile(t *testing.T) {
	const doc = "2024-02-01 доход 收入\n    assets:cash  -10 USD\n    expenses:food\n"
	for name, eol := range map[string]string{"LF": "\n", "CRLF": "\r\n"} {
		t.Run(name, func(t *testing.T) {
			srv, dir := newInlayWorkspace(t, map[string]string{
				"all.journal": "include a.journal\ninclude b.journal\n",
				"a.journal":   doc,
				"b.journal":   "2024-01-01 seed\n    assets:cash  100 USD\n    income:job\n",
			})
			enableOnlyRunningBalances(srv)
			aURI := uri.File(filepath.Join(dir, "a.journal"))
			bURI := uri.File(filepath.Join(dir, "b.journal"))
			openDocument(t, srv, aURI, strings.ReplaceAll(doc, "\n", eol))
			openDocument(t, srv, bURI, "2024-01-01 seed\n    assets:cash  100 USD\n    income:job\n")

			require.Equal(t, protocol.String("balance: 90 USD"), requestHintLabels(t, srv, aURI)[0])
			require.Equal(t, protocol.String("balance: 90 USD"), requestHintLabels(t, srv, aURI)[0])

			replaceDocument(t, srv, bURI, 2, "2024-01-01 seed\n    assets:cash  500 USD\n    income:job\n")
			assert.Equal(t, protocol.String("balance: 490 USD"), requestHintLabels(t, srv, aURI)[0])
		})
	}
}

func TestInlayHint_RunningBalanceFollowsEditsToUnopenedIncludedFile(t *testing.T) {
	const doc = "2024-02-01 later\n    assets:cash  -10 USD\n    expenses:food\n"
	srv, dir := newInlayWorkspace(t, map[string]string{
		"all.journal": "include a.journal\ninclude b.journal\n",
		"a.journal":   doc,
		"b.journal":   "2024-01-01 seed\n    assets:cash  100 USD\n    income:job\n",
	})
	enableOnlyRunningBalances(srv)
	aURI := uri.File(filepath.Join(dir, "a.journal"))
	bPath := filepath.Join(dir, "b.journal")
	openDocument(t, srv, aURI, doc)
	require.Equal(t, protocol.String("balance: 90 USD"), requestHintLabels(t, srv, aURI)[0])

	require.NoError(t, os.WriteFile(bPath, []byte("2024-01-01 seed\n    assets:cash  300 USD\n    income:job\n"), 0o644))
	require.NoError(t, srv.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{{URI: uri.File(bPath), Type: protocol.FileChangeTypeChanged}},
	}))
	assert.Equal(t, protocol.String("balance: 290 USD"), requestHintLabels(t, srv, aURI)[0])
}

func TestInlayHint_RunningBalanceFollowsEditsToTheDocument(t *testing.T) {
	srv, dir := newInlayWorkspace(t, map[string]string{
		"all.journal": "include a.journal\ninclude b.journal\n",
		"a.journal":   "2024-02-01 later\n    assets:cash  -10 USD\n    expenses:food\n",
		"b.journal":   "2024-01-01 seed\n    assets:cash  100 USD\n    income:job\n",
	})
	enableOnlyRunningBalances(srv)
	aURI := uri.File(filepath.Join(dir, "a.journal"))
	openDocument(t, srv, aURI, "2024-02-01 later\n    assets:cash  -10 USD\n    expenses:food\n")
	require.Equal(t, protocol.String("balance: 90 USD"), requestHintLabels(t, srv, aURI)[0])

	replaceDocument(t, srv, aURI, 2, "2024-02-01 later\n    assets:cash  -40 USD\n    expenses:food\n")
	assert.Equal(t, protocol.String("balance: 60 USD"), requestHintLabels(t, srv, aURI)[0])
}

func TestInlayHint_RunningBalanceOfLoneDocumentFollowsEdits(t *testing.T) {
	srv := NewServer()
	enableOnlyRunningBalances(srv)
	docURI := uri.URI("file:///lone.journal")
	openDocument(t, srv, docURI, "2024-01-01 a\n    assets:cash  10 USD\n    income\n")
	require.Equal(t, protocol.String("balance: 10 USD"), requestHintLabels(t, srv, docURI)[0])
	require.Equal(t, protocol.String("balance: 10 USD"), requestHintLabels(t, srv, docURI)[0])

	replaceDocument(t, srv, docURI, 2, "2024-01-01 a\n    assets:cash  25 USD\n    income\n")
	assert.Equal(t, protocol.String("balance: 25 USD"), requestHintLabels(t, srv, docURI)[0])
}

func TestInlayHint_RunningBalancesAreReusedWhileNothingChanges(t *testing.T) {
	const doc = "2024-02-01 later\n    assets:cash  -10 USD\n    expenses:food\n"
	srv, dir := newInlayWorkspace(t, map[string]string{
		"all.journal": "include a.journal\ninclude b.journal\n",
		"a.journal":   doc,
		"b.journal":   "2024-01-01 seed\n    assets:cash  100 USD\n    income:job\n",
	})
	enableOnlyRunningBalances(srv)
	aURI := uri.File(filepath.Join(dir, "a.journal"))
	bURI := uri.File(filepath.Join(dir, "b.journal"))
	openDocument(t, srv, aURI, doc)
	openDocument(t, srv, bURI, "2024-01-01 seed\n    assets:cash  100 USD\n    income:job\n")

	replayed := func() uintptr {
		requestHintLabels(t, srv, aURI)
		cached, ok := srv.parseCache.Load(aURI)
		require.True(t, ok)
		entry := cached.(*cachedDoc).running
		require.NotNil(t, entry)
		return reflect.ValueOf(entry.balances).Pointer()
	}
	first := replayed()
	assert.Equal(t, first, replayed(), "unchanged document and tree must reuse the replay")

	replaceDocument(t, srv, bURI, 2, "2024-01-01 seed\n    assets:cash  1 USD\n    income:job\n")
	assert.NotEqual(t, first, replayed(), "an edit to the tree must replay")
}

func TestInlayHint_RunningBalancesDroppedWithTheDocument(t *testing.T) {
	srv := NewServer()
	enableOnlyRunningBalances(srv)
	docURI := uri.URI("file:///lone.journal")
	openDocument(t, srv, docURI, "2024-01-01 a\n    assets:cash  10 USD\n    income\n")
	requestHintLabels(t, srv, docURI)
	_, ok := srv.parseCache.Load(docURI)
	require.True(t, ok)

	require.NoError(t, srv.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
	}))
	_, ok = srv.parseCache.Load(docURI)
	assert.False(t, ok)
}

func TestInlayHint_RunningBalancesUnderConcurrentEdits(t *testing.T) {
	srv, dir := newInlayWorkspace(t, map[string]string{
		"all.journal": "include a.journal\ninclude b.journal\n",
		"a.journal":   "2024-02-01 later\n    assets:cash  -10 USD\n    expenses:food\n",
		"b.journal":   "2024-01-01 seed\n    assets:cash  100 USD\n    income:job\n",
	})
	enableOnlyRunningBalances(srv)
	aURI := uri.File(filepath.Join(dir, "a.journal"))
	bURI := uri.File(filepath.Join(dir, "b.journal"))
	openDocument(t, srv, aURI, "2024-02-01 later\n    assets:cash  -10 USD\n    expenses:food\n")
	openDocument(t, srv, bURI, "2024-01-01 seed\n    assets:cash  100 USD\n    income:job\n")

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				requestHintLabels(t, srv, aURI)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 25 {
			text := "2024-01-01 seed\n    assets:cash  " + string(rune('1'+i%9)) + "00 USD\n    income:job\n"
			replaceDocument(t, srv, bURI, int32(i+2), text)
		}
	}()
	wg.Wait()

	replaceDocument(t, srv, bURI, 100, "2024-01-01 seed\n    assets:cash  700 USD\n    income:job\n")
	assert.Equal(t, protocol.String("balance: 690 USD"), requestHintLabels(t, srv, aURI)[0])
}
