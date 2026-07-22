package server

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func TestDiagnostics_RapidEditsCoalesce(t *testing.T) {
	srv := NewServer()
	srv.diagDebounce = 80 * time.Millisecond
	client := newIntegrationMockClient()
	srv.SetClient(client)

	docURI := protocol.DocumentURI("file:///test.journal")

	err := srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     docURI,
			Version: 1,
			Text:    "2024-01-15 test\n    expenses:food  $1\n    assets:cash",
		},
	})
	require.NoError(t, err)

	const edits = 10
	for i := 2; i <= edits+1; i++ {
		content := fmt.Sprintf("2024-01-15 test\n    expenses:food  $%d\n    assets:cash", i)
		err = srv.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
			TextDocument: protocol.VersionedTextDocumentIdentifier{
				TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: docURI},
				Version:                int32(i),
			},
			ContentChanges: []protocol.TextDocumentContentChangeEvent{{Text: content}},
		})
		require.NoError(t, err)
		time.Sleep(2 * time.Millisecond)
	}

	require.True(t, client.waitDiagnostics(), "expected at least one diagnostics publish")
	time.Sleep(50 * time.Millisecond)

	client.mu.Lock()
	count := len(client.diagnostics)
	last := client.diagnostics[count-1]
	client.mu.Unlock()

	assert.Equal(t, 1, count, "rapid edits should coalesce into a single publish")
	assert.Equal(t, uint32(edits+1), last.Version, "published version should match the last edit")
}

func TestDiagnostics_SupersededComputationNeverPublishes(t *testing.T) {
	srv := NewServer()
	srv.diagDebounce = 50 * time.Millisecond
	client := newIntegrationMockClient()
	srv.SetClient(client)

	docURI := protocol.DocumentURI("file:///test.journal")

	err := srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     docURI,
			Version: 1,
			Text:    "2024-01-15 old\n    expenses:food  $999\n    assets:cash  $1",
		},
	})
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	err = srv.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: docURI},
			Version:                2,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{{
			Text: "2024-01-15 new\n    expenses:food  $50\n    assets:cash",
		}},
	})
	require.NoError(t, err)

	require.True(t, client.waitDiagnostics(), "expected diagnostics publish")
	time.Sleep(100 * time.Millisecond)

	client.mu.Lock()
	all := make([]protocol.PublishDiagnosticsParams, len(client.diagnostics))
	copy(all, client.diagnostics)
	client.mu.Unlock()

	for _, pub := range all {
		assert.Equal(t, uint32(2), pub.Version,
			"stale version 1 must never be published after version 2 arrived")
	}

	last := all[len(all)-1]
	assert.Empty(t, last.Diagnostics, "final content is balanced, should have no diagnostics")
}

func TestDiagnostics_PublishedVersionMatchesDocument(t *testing.T) {
	srv := NewServer()
	srv.diagDebounce = 0
	client := newIntegrationMockClient()
	srv.SetClient(client)

	docURI := protocol.DocumentURI("file:///test.journal")

	err := srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     docURI,
			Version: 7,
			Text:    "2024-01-15 test\n    expenses:food  $50\n    assets:cash",
		},
	})
	require.NoError(t, err)

	require.True(t, client.waitDiagnostics())

	last := client.getLastDiagnostics()
	require.NotNil(t, last)
	assert.Equal(t, uint32(7), last.Version)
}

func TestDiagnostics_DidCloseCancelsPending(t *testing.T) {
	srv := NewServer()
	srv.diagDebounce = 200 * time.Millisecond
	client := newIntegrationMockClient()
	srv.SetClient(client)

	docURI := protocol.DocumentURI("file:///test.journal")

	err := srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     docURI,
			Version: 1,
			Text:    "2024-01-15 test\n    expenses:food  $50\n    assets:cash",
		},
	})
	require.NoError(t, err)

	err = srv.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
	})
	require.NoError(t, err)

	time.Sleep(300 * time.Millisecond)

	client.mu.Lock()
	count := len(client.diagnostics)
	client.mu.Unlock()

	assert.Equal(t, 0, count, "no diagnostics should be published after DidClose")
}

func TestDiagnostics_IndependentURIsDoNotInterfere(t *testing.T) {
	srv := NewServer()
	srv.diagDebounce = 50 * time.Millisecond
	client := newIntegrationMockClient()
	srv.SetClient(client)

	uriA := protocol.DocumentURI("file:///a.journal")
	uriB := protocol.DocumentURI("file:///b.journal")

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI: uriA, Version: 1,
				Text: "2024-01-15 a\n    expenses:food  $10\n    assets:cash",
			},
		})
	}()
	go func() {
		defer wg.Done()
		_ = srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI: uriB, Version: 1,
				Text: "2024-01-15 b\n    expenses:rent  $20\n    assets:bank",
			},
		})
	}()
	wg.Wait()

	require.True(t, client.waitDiagnostics())
	require.True(t, client.waitDiagnostics())

	client.mu.Lock()
	count := len(client.diagnostics)
	uris := map[protocol.DocumentURI]bool{}
	for _, d := range client.diagnostics {
		uris[d.URI] = true
	}
	client.mu.Unlock()

	assert.Equal(t, 2, count, "each URI should get exactly one publish")
	assert.True(t, uris[uriA])
	assert.True(t, uris[uriB])
}
