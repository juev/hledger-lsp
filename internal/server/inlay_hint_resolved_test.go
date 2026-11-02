package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/workspace"
)

func newInlayWorkspace(t testing.TB, files map[string]string) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
	srv := NewServer()
	srv.workspace = workspace.NewWorkspace(dir, srv.loader)
	require.NoError(t, srv.workspace.Initialize())
	return srv, dir
}

func enableOnlyRunningBalances(srv *Server) {
	settings := srv.getSettings()
	settings.InlayHints.InferredAmounts = false
	settings.InlayHints.RunningBalances = true
	srv.setSettings(settings)
}

func inlayLabels(t *testing.T, srv *Server, docURI uri.URI, content string) []protocol.InlayHintLabel {
	t.Helper()
	srv.StoreDocument(docURI, content)
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

// Two included files each have a transaction at index 0. Balances are looked up
// by index within the open file, so the sibling's transaction must not answer.
func TestInlayHint_RunningBalanceNotTakenFromSiblingInclude(t *testing.T) {
	tests := []struct {
		name     string
		a, b     string
		expected []protocol.InlayHintLabel
	}{
		{
			name: "ascii",
			a:    "2024-01-01 groceries\n    expenses:food  10 USD\n    assets:cash  -10 USD\n",
			b:    "2024-02-01 rent\n    expenses:rent  100 USD\n    assets:cash  -100 USD\n",
			expected: []protocol.InlayHintLabel{
				protocol.String("balance: 10 USD"), protocol.String("balance: -10 USD"),
			},
		},
		{
			name: "non-ascii",
			a:    "2024-01-01 продукты\n    расходы:食品  10 ₽\n    активы:現金  -10 ₽\n",
			b:    "2024-02-01 аренда\n    расходы:жильё  100 ₽\n    активы:現金  -100 ₽\n",
			expected: []protocol.InlayHintLabel{
				protocol.String("balance: 10 ₽"), protocol.String("balance: -10 ₽"),
			},
		},
		{
			name: "crlf",
			a:    "2024-01-01 groceries\r\n    expenses:food  10 USD\r\n    assets:cash  -10 USD\r\n",
			b:    "2024-02-01 rent\r\n    expenses:rent  100 USD\r\n    assets:cash  -100 USD\r\n",
			expected: []protocol.InlayHintLabel{
				protocol.String("balance: 10 USD"), protocol.String("balance: -10 USD"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, dir := newInlayWorkspace(t, map[string]string{
				"all.journal": "include a.journal\ninclude b.journal\n",
				"a.journal":   tt.a,
				"b.journal":   tt.b,
			})
			enableOnlyRunningBalances(srv)
			docURI := uri.File(filepath.Join(dir, "a.journal"))

			labels := inlayLabels(t, srv, docURI, strings.ReplaceAll(tt.a, "\r\n", "\n"))
			assert.Equal(t, tt.expected, labels)
		})
	}
}

func TestInlayHint_RunningBalanceAccumulatesAcrossIncludes(t *testing.T) {
	a := "2024-02-01 later\n    assets:cash  -10 USD\n    expenses:food\n"
	srv, dir := newInlayWorkspace(t, map[string]string{
		"all.journal": "include a.journal\ninclude b.journal\n",
		"a.journal":   a,
		"b.journal":   "2024-01-01 earlier\n    assets:cash  100 USD\n    income:job\n",
	})
	enableOnlyRunningBalances(srv)

	labels := inlayLabels(t, srv, uri.File(filepath.Join(dir, "a.journal")), a)
	require.NotEmpty(t, labels)
	assert.Equal(t, protocol.String("balance: 90 USD"), labels[0])
}

func TestInlayHint_DefaultSettingsDoNotReplayWholeTree(t *testing.T) {
	const accounts, transactions = 400, 1500

	var big strings.Builder
	for i := range transactions {
		fmt.Fprintf(&big, "2024-01-%02d tx\n    expenses:a%d  1 USD\n    assets:b%d\n\n", i%28+1, i%accounts, i%accounts)
	}
	open := "2024-02-01 open\n    expenses:food  1 USD\n    assets:cash\n"
	srv, dir := newInlayWorkspace(t, map[string]string{
		"all.journal":  "include big.journal\ninclude open.journal\n",
		"big.journal":  big.String(),
		"open.journal": open,
	})
	docURI := uri.File(filepath.Join(dir, "open.journal"))

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	labels := inlayLabels(t, srv, docURI, open)
	runtime.ReadMemStats(&after)

	assert.Equal(t, []protocol.InlayHintLabel{protocol.String("= -1 USD")}, labels)
	const budget = 20 << 20
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(budget),
		"running balances are off, so the tree's balances must not be computed")
}

func TestInlayHint_RunningBalanceFollowsBalanceAssignment(t *testing.T) {
	for name, a := range map[string]string{
		"lf":   "2024-02-01 корректировка\n    активы:現金  = 70 USD\n    расходы:разное\n",
		"crlf": "2024-02-01 корректировка\r\n    активы:現金  = 70 USD\r\n    расходы:разное\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			srv, dir := newInlayWorkspace(t, map[string]string{
				"all.journal": "include b.journal\ninclude a.journal\n",
				"a.journal":   a,
				"b.journal":   "2024-01-01 seed\n    активы:現金  100 USD\n    капитал\n",
			})
			enableOnlyRunningBalances(srv)

			labels := inlayLabels(t, srv, uri.File(filepath.Join(dir, "a.journal")), strings.ReplaceAll(a, "\r\n", "\n"))
			assert.Equal(t, []protocol.InlayHintLabel{
				protocol.String("balance: 70 USD"), protocol.String("balance: 30 USD"),
			}, labels)
		})
	}
}
