package server

import (
	"context"
	"fmt"
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

// Hint requests running while the buffers of the journal are edited must leave
// the workspace describing the final text: after the burst, the next request
// sees both edits.
func TestInlayHint_ConcurrentRequestsDoNotLoseEdits(t *testing.T) {
	const seedB = "2024-01-01 seed\n    assets:cash  %d USD\n    income:job\n"
	const spendA = "2024-02-01 x\n    assets:cash  -%d USD\n    expenses:food\n"
	srv, dir := newInlayWorkspace(t, map[string]string{
		"all.journal": "include b.journal\ninclude a.journal\n",
		"a.journal":   fmt.Sprintf(spendA, 1),
		"b.journal":   fmt.Sprintf(seedB, 100),
	})
	enableOnlyRunningBalances(srv)
	aURI := uri.File(filepath.Join(dir, "a.journal"))
	bURI := uri.File(filepath.Join(dir, "b.journal"))
	openDocument(t, srv, aURI, fmt.Sprintf(spendA, 1))
	openDocument(t, srv, bURI, fmt.Sprintf(seedB, 100))

	for round := range 20 {
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				requestHintLabels(t, srv, aURI)
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			replaceDocument(t, srv, bURI, int32(round*10+2), fmt.Sprintf(seedB, round+1000))
			replaceDocument(t, srv, aURI, int32(round*10+2), fmt.Sprintf(spendA, round+1))
		}()
		wg.Wait()

		want := []protocol.InlayHintLabel{
			protocol.String(fmt.Sprintf("balance: %d USD", round+1000-(round+1))),
			protocol.String(fmt.Sprintf("balance: %d USD", round+1)),
		}
		require.Equal(t, want, requestHintLabels(t, srv, aURI), "round %d", round)
	}
}
