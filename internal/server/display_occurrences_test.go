package server

import (
	"context"
	"fmt"
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

func hintsAt(t *testing.T, ts *testServer, docURI uri.URI) []protocol.InlayHint {
	t.Helper()
	hints, err := ts.InlayHint(context.Background(), &protocol.InlayHintParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
		Range:        protocol.Range{End: protocol.Position{Line: 100}},
	})
	require.NoError(t, err)
	return hints
}

func inlayLabelsOf(hints []protocol.InlayHint) []string {
	labels := make([]string, 0, len(hints))
	for _, hint := range hints {
		label, _ := hint.Label.(protocol.String)
		labels = append(labels, string(label))
	}
	return labels
}

func enableHints(ts *testServer, inferred, cost, balances bool) {
	settings := ts.getSettings()
	settings.InlayHints.InferredAmounts = inferred
	settings.InlayHints.CostExpansion = cost
	settings.InlayHints.RunningBalances = balances
	ts.setSettings(settings)
}

func hoverAt(t *testing.T, ts *testServer, docURI uri.URI, line, character uint32) string {
	t.Helper()
	ts.clientCapabilities.hoverContentFormats = []protocol.MarkupKind{protocol.MarkupKindMarkdown}
	hover, err := ts.Hover(context.Background(), &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: line, Character: character},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, hover)
	return hover.Contents.(*protocol.MarkupContent).Value
}

// Hover on an amount lists every value the occurrences read from it, with the
// include it holds for. `1,500` is 1.5 under a comma mark and 1500 under a dot,
// and each is written in the style of its occurrence.
func TestHover_AmountListsEveryReadingOfAnAmbiguousNumber(t *testing.T) {
	samples := map[string]string{
		"ascii":    "2024-01-01 x\n    expenses:food  1,500 USD\n    assets:cash\n",
		"cyrillic": "2024-01-01 x\n    расходы:еда  1,500 РУБ\n    активы:касса\n",
		"cjk":      "2024-01-01 x\n    費用:食費  1,500 元\n    資産:現金\n",
	}
	for _, layout := range conflictingMarkLayouts() {
		for sample, text := range samples {
			for variant, shared := range crlfVariants(text) {
				t.Run(layout.name+"/"+sample+"/"+variant, func(t *testing.T) {
					ts, _, docURI, _ := setupOccurrences(t, layout, shared)
					content := hoverAt(t, ts, docURI, 1, 22)

					symbol := strings.Fields(text)[4]
					// Each reading is written in the style of its own occurrence,
					// like the inlay hints: 1.5 under a comma mark, 1500 under a dot.
					assert.Contains(t, content, "1,5 "+symbol)
					assert.Contains(t, content, "1,500. "+symbol)
					assert.Contains(t, content, "when included from")
				})
			}
		}
	}
}

// Occurrences that read an amount alike produce the hover of a file included once.
func TestHover_AmountIsUnchangedWhenOccurrencesAgree(t *testing.T) {
	shared := "2024-01-01 x\n    expenses:food  10 USD @ 2 EUR\n    assets:cash\n"
	layout := occurrenceLayout{"apply account", includeTwice("", "apply account a\n", "apply account b\n")}
	ts, _, docURI, _ := setupOccurrences(t, layout, shared)
	content := hoverAt(t, ts, docURI, 1, 22)

	assert.Equal(t, "**Amount:** 10 USD\n\n**Unit cost:** @ 2 EUR", content)
}

// An inferred amount that differs between occurrences shows each value, joined
// with " | ", and the tooltip says which include gives which.
func TestInlayHint_InferredAmountJoinsTheValuesOfEveryOccurrence(t *testing.T) {
	shared := "2024-01-01 x\n    a  1,500 USD\n    b\n"
	for _, layout := range conflictingMarkLayouts() {
		t.Run(layout.name, func(t *testing.T) {
			ts, _, docURI, _ := setupOccurrences(t, layout, shared)
			enableHints(ts, true, false, false)

			hints := hintsAt(t, ts, docURI)
			require.Len(t, hints, 1)
			label := inlayLabelsOf(hints)[0]
			require.True(t, strings.HasPrefix(label, "= "), label)
			// Each value is written in the style of the occurrence that reads it.
			assert.ElementsMatch(t, []string{"-1,5 USD", "-1,500. USD"}, strings.Split(strings.TrimPrefix(label, "= "), " | "))

			tooltip := tooltipString(hints[0].Tooltip)
			assert.Contains(t, tooltip, "-1,5 USD")
			assert.Contains(t, tooltip, "-1,500. USD")
			assert.Contains(t, tooltip, "included from")
		})
	}
}

func TestInlayHint_CostExpansionJoinsTheValuesOfEveryOccurrence(t *testing.T) {
	shared := "2024-01-01 x\n    a  1 EUR @ 1,500 USD\n    b\n"
	layout := conflictingMarkLayouts()[0]
	ts, _, docURI, _ := setupOccurrences(t, layout, shared)
	enableHints(ts, false, true, false)

	labels := inlayLabelsOf(hintsAt(t, ts, docURI))
	require.Len(t, labels, 1)
	assert.Equal(t, "cost: 1,5 USD | 1,500. USD", labels[0])
}

// Occurrences that agree, and files included once, look exactly as before: one
// value, no tooltip.
func TestInlayHint_AgreeingOccurrencesShowOneValueWithoutTooltip(t *testing.T) {
	shared := "2024-01-01 x\n    a  10 USD\n    b\n"
	layout := occurrenceLayout{"apply account", includeTwice("", "apply account a\n", "apply account b\n")}
	ts, _, docURI, _ := setupOccurrences(t, layout, shared)
	enableHints(ts, true, false, false)

	hints := hintsAt(t, ts, docURI)
	require.Len(t, hints, 1)
	assert.Equal(t, "= -10 USD", inlayLabelsOf(hints)[0])
	assert.Nil(t, hints[0].Tooltip)
}

// A file included twice has a running balance at each occurrence: the second
// starts from what the first left.
func TestInlayHint_RunningBalanceIsShownPerOccurrence(t *testing.T) {
	shared := "2024-01-01 seed\n    assets:cash  10 USD\n    income:job\n"
	layout := occurrenceLayout{"equal contexts", includeTwice("", "", "")}
	ts, _, docURI, _ := setupOccurrences(t, layout, shared)
	enableHints(ts, false, false, true)

	hints := hintsAt(t, ts, docURI)
	labels := inlayLabelsOf(hints)
	assert.Equal(t, []string{"balance: 10 USD | 20 USD", "balance: -10 USD | -20 USD"}, labels)
	tooltip := tooltipString(hints[0].Tooltip)
	assert.Contains(t, tooltip, "first.journal:1")
	assert.Contains(t, tooltip, "second.journal:1")
}

func TestInlayHint_RunningBalanceListsAtMostThreeOccurrences(t *testing.T) {
	dir := t.TempDir()
	shared := "2024-01-01 seed\n    assets:cash  10 USD\n    income:job\n"
	var main strings.Builder
	files := map[string]string{"shared.journal": shared}
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		main.WriteString("include " + name + ".journal\n")
		files[name+".journal"] = "include shared.journal\n"
	}
	files["main.journal"] = main.String()
	writeJournals(t, dir, files)
	ts, docURI := formatServer(t, dir, filepath.Join(dir, "shared.journal"), shared)
	enableHints(ts, false, false, true)

	labels := inlayLabelsOf(hintsAt(t, ts, docURI))
	require.NotEmpty(t, labels)
	assert.Equal(t, "balance: 10 USD | 20 USD | 30 USD | …", labels[0])
}

func TestInlayHint_RunningBalanceOfAFileIncludedOnceIsUnchanged(t *testing.T) {
	shared := "2024-01-01 seed\n    assets:cash  10 USD\n    income:job\n"
	dir := t.TempDir()
	writeJournals(t, dir, map[string]string{"main.journal": "include shared.journal\n", "shared.journal": shared})
	ts, docURI := formatServer(t, dir, filepath.Join(dir, "shared.journal"), shared)
	enableHints(ts, false, false, true)

	hints := hintsAt(t, ts, docURI)
	assert.Equal(t, []string{"balance: 10 USD", "balance: -10 USD"}, inlayLabelsOf(hints))
	assert.Nil(t, hints[0].Tooltip)
}

// Two inclusions of a file can sit at the same directive when the file that
// holds it is itself included twice; the tooltip must still tell them apart.
func TestInlayHint_TooltipLabelsOfADiamondAreDistinct(t *testing.T) {
	shared := "2024-01-01 seed\n    assets:cash  10 USD\n    income:job\n"
	dir := t.TempDir()
	writeJournals(t, dir, map[string]string{
		"main.journal":   "include a.journal\ninclude b.journal\n",
		"a.journal":      "include mid.journal\n",
		"b.journal":      "include mid.journal\n",
		"mid.journal":    "include shared.journal\n",
		"shared.journal": shared,
	})
	ts, docURI := formatServer(t, dir, filepath.Join(dir, "shared.journal"), shared)
	enableHints(ts, false, false, true)

	hints := hintsAt(t, ts, docURI)
	require.NotEmpty(t, hints)
	assert.Equal(t,
		"10 USD (when included from mid.journal:1 via a.journal:1)\n20 USD (when included from mid.journal:1 via b.journal:1)",
		tooltipString(hints[0].Tooltip))
}

// More inclusions than any fixed cap must still be replayed once, not on every
// request.
func TestInlayHint_RunningBalancesOfManyInclusionsAreReused(t *testing.T) {
	shared := "2024-01-01 seed\n    assets:cash  10 USD\n    income:job\n"
	dir := t.TempDir()
	var main strings.Builder
	files := map[string]string{"shared.journal": shared}
	const inclusions = 20
	for i := range inclusions {
		name := fmt.Sprintf("inc%02d.journal", i)
		main.WriteString("include " + name + "\n")
		files[name] = "include shared.journal\n"
	}
	files["main.journal"] = main.String()
	writeJournals(t, dir, files)
	ts, docURI := formatServer(t, dir, filepath.Join(dir, "shared.journal"), shared)
	enableHints(ts, false, false, true)

	replays := func() map[readingKey]uintptr {
		hintsAt(t, ts, docURI)
		cached, ok := ts.parseCache.Load(docURI)
		require.True(t, ok)
		doc := cached.(*cachedDoc)
		doc.runningMu.Lock()
		defer doc.runningMu.Unlock()
		pointers := make(map[readingKey]uintptr)
		for key, balances := range doc.running {
			pointers[key] = reflect.ValueOf(balances).Pointer()
		}
		return pointers
	}
	first := replays()
	assert.Len(t, first, inclusions)
	assert.Equal(t, first, replays(), "an unchanged document and tree must reuse every replay")
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
