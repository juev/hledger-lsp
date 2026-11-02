package server

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// Handlers run concurrently with the edits that make the workspace re-resolve
// its include trees. Readers of a tree must not see it half replaced: run with
// -race, which is what makes this test meaningful.
func TestOccurrenceReaders_DoNotRaceWithEditsToAnIncludingFile(t *testing.T) {
	shared := "2024-01-01 x\n    expenses:food   1,500 EUR\n    assets:cash\n"
	dir := t.TempDir()
	mainText := func(n int) string {
		return "commodity 1.000,00 EUR\n\ninclude first.journal\ninclude second.journal\n; edit " + string(rune('a'+n%26)) + "\n"
	}
	writeJournals(t, dir, map[string]string{
		"main.journal":   mainText(0),
		"first.journal":  "decimal-mark ,\ninclude shared.journal\n",
		"second.journal": "decimal-mark .\ninclude shared.journal\n",
		"shared.journal": shared,
	})
	ts, sharedURI := formatServer(t, dir, filepath.Join(dir, "shared.journal"), shared)
	settings := ts.getSettings()
	settings.InlayHints.InferredAmounts = true
	settings.InlayHints.RunningBalances = true
	ts.setSettings(settings)
	mainURI := uri.File(filepath.Join(dir, "main.journal"))
	require.NoError(t, ts.openDocument(mainURI, mainText(0)))

	ctx := context.Background()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	run := func(work func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					work()
				}
			}
		}()
	}
	run(func() {
		_, _ = ts.InlayHint(ctx, &protocol.InlayHintParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: sharedURI},
			Range:        protocol.Range{End: protocol.Position{Line: 10}},
		})
	})
	run(func() {
		_, _ = ts.Format(ctx, &protocol.DocumentFormattingParams{TextDocument: protocol.TextDocumentIdentifier{URI: sharedURI}})
	})
	run(func() {
		doc, _ := ts.GetDocument(sharedURI)
		_ = ts.analyze(sharedURI, uriToPath(sharedURI), doc)
	})
	run(func() {
		_, _ = ts.WorkspaceSymbol(ctx, &protocol.WorkspaceSymbolParams{Query: "x"})
	})

	for n := 1; n <= 40; n++ {
		require.NoError(t, ts.changeDocument(mainURI, []protocol.TextDocumentContentChangeEvent{
			&protocol.TextDocumentContentChangeWholeDocument{Text: mainText(n)},
		}))
		time.Sleep(2 * time.Millisecond)
	}
	close(stop)
	wg.Wait()
}
