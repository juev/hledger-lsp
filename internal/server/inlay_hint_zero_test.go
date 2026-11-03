package server

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func crlfAll(files map[string]string, newline string) map[string]string {
	out := make(map[string]string, len(files))
	for name, text := range files {
		out[name] = strings.ReplaceAll(text, "\n", newline)
	}
	return out
}

func hintAtLine(t *testing.T, hints []protocol.InlayHint, line uint32, prefix string) protocol.InlayHint {
	t.Helper()
	for _, hint := range hints {
		if hint.Position.Line == line && strings.HasPrefix(inlayLabelsOf([]protocol.InlayHint{hint})[0], prefix) {
			return hint
		}
	}
	require.Failf(t, "no hint", "line %d prefix %q in %v", line, prefix, inlayLabelsOf(hints))
	return protocol.InlayHint{}
}

// An inclusion that infers zero for a posting is one reading of it like any
// other: when another inclusion infers an amount, the hint must say that amount
// holds only there, or it reads as unconditional.
func TestInlayHint_ZeroInferredAmountKeepsItsInclusion(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		t.Run(fmt.Sprintf("assignment %q", newline), func(t *testing.T) {
			child := "2026-02-01 назначение\n    активы:счёт  =500 USD\n    капитал:g\n"
			dir := t.TempDir()
			writeJournals(t, dir, crlfAll(map[string]string{
				"main.journal": "2026-01-01 seed\n    активы:счёт  100 USD\n    капитал:o\n\ninclude child.journal\n\n" +
					"2026-01-20 between\n    активы:счёт  50 USD\n    капитал:o\n\ninclude child.journal\n",
				"child.journal": child,
			}, newline))
			ts, docURI := formatServer(t, dir, filepath.Join(dir, "child.journal"), strings.ReplaceAll(child, "\n", newline))
			enableHints(ts, true, false, false)

			hints := hintsAt(t, ts, docURI)
			assignment := hintAtLine(t, hints, 1, "")
			assert.Equal(t, "350 USD | 0 USD", inlayLabelsOf([]protocol.InlayHint{assignment})[0])

			elided := hintAtLine(t, hints, 2, "= ")
			assert.Equal(t, "= -350 USD | 0 USD", inlayLabelsOf([]protocol.InlayHint{elided})[0])
			tooltip := tooltipString(elided.Tooltip)
			assert.Contains(t, tooltip, "-350 USD (when included from main.journal:5)")
			assert.Contains(t, tooltip, "0 USD (when included from main.journal:11)")
		})

		t.Run(fmt.Sprintf("transaction-local %q", newline), func(t *testing.T) {
			// Under a comma mark 1.000 is a thousand, under a dot mark it is one,
			// so the two inclusions infer zero and 999 EUR for the elided posting
			// (hledger 1.52.4 print -x: 0 EUR and 999,000 EUR).
			child := "2026-02-01 смена\n    расход  1.000 EUR\n    расход  -1000 EUR\n    касса\n"
			dir := t.TempDir()
			writeJournals(t, dir, crlfAll(map[string]string{
				"main.journal":  "decimal-mark ,\ninclude child.journal\ndecimal-mark .\ninclude child.journal\n",
				"child.journal": child,
			}, newline))
			ts, docURI := formatServer(t, dir, filepath.Join(dir, "child.journal"), strings.ReplaceAll(child, "\n", newline))
			enableHints(ts, true, false, false)

			hints := hintsAt(t, ts, docURI)
			elided := hintAtLine(t, hints, 3, "= ")
			assert.Equal(t, "= 999 EUR | 0 EUR", inlayLabelsOf([]protocol.InlayHint{elided})[0])
			tooltip := tooltipString(elided.Tooltip)
			assert.Contains(t, tooltip, "999 EUR (when included from main.journal:4)")
			assert.Contains(t, tooltip, "0 EUR (when included from main.journal:2)")
		})
	}
}

// When every inclusion infers zero there is nothing conditional to show.
func TestInlayHint_ZeroInferredInEveryInclusionShowsNothing(t *testing.T) {
	child := "2026-02-01 x\n    расход  5 USD\n    расход  -5 USD\n    касса\n"
	dir := t.TempDir()
	writeJournals(t, dir, map[string]string{
		"main.journal":  "include child.journal\ninclude child.journal\n",
		"child.journal": child,
	})
	ts, docURI := formatServer(t, dir, filepath.Join(dir, "child.journal"), child)
	enableHints(ts, true, false, false)

	for _, hint := range hintsAt(t, ts, docURI) {
		assert.NotEqual(t, uint32(3), hint.Position.Line, "unexpected hint %v", inlayLabelsOf([]protocol.InlayHint{hint}))
	}
}

// A diamond includes the file twice through one directive chain each; a zero in
// one branch and an amount in the other keep their own provenance.
func TestInlayHint_ZeroInferredAmountInADiamond(t *testing.T) {
	child := "2026-02-01 назначение\n    активы:счёт  =500 USD\n    капитал:g\n"
	dir := t.TempDir()
	writeJournals(t, dir, map[string]string{
		"main.journal":  "2026-01-01 seed\n    активы:счёт  100 USD\n    капитал:o\n\ninclude a.journal\n\n2026-01-20 between\n    активы:счёт  50 USD\n    капитал:o\n\ninclude b.journal\n",
		"a.journal":     "include mid.journal\n",
		"b.journal":     "include mid.journal\n",
		"mid.journal":   "include child.journal\n",
		"child.journal": child,
	})
	ts, docURI := formatServer(t, dir, filepath.Join(dir, "child.journal"), child)
	enableHints(ts, true, false, false)

	elided := hintAtLine(t, hintsAt(t, ts, docURI), 2, "= ")
	assert.Equal(t, "= -350 USD | 0 USD", inlayLabelsOf([]protocol.InlayHint{elided})[0])
	tooltip := tooltipString(elided.Tooltip)
	assert.Contains(t, tooltip, "via a.journal:1")
	assert.Contains(t, tooltip, "via b.journal:1")
}
