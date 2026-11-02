package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

type overlayFixture struct {
	ts                     *testServer
	dir                    string
	mainURI, aURI, bURI    uri.URI
	mainPath, aPath, bPath string
}

const (
	overlayMain = "include a.journal\ninclude b.journal\n"
	overlayA    = "2024-01-01 a\n    assets:cash  -10 USD\n    equity:a\n"
	overlayB    = "2024-01-02 b\n    assets:cash  5 USD\n    equity:b\n"
)

func newOverlayFixture(t *testing.T, a string) *overlayFixture {
	t.Helper()
	dir := t.TempDir()
	f := &overlayFixture{dir: dir}
	f.mainPath = filepath.Join(dir, "all.journal")
	f.aPath = filepath.Join(dir, "a.journal")
	f.bPath = filepath.Join(dir, "b.journal")
	require.NoError(t, os.WriteFile(f.mainPath, []byte(overlayMain), 0o644))
	require.NoError(t, os.WriteFile(f.aPath, []byte(a), 0o644))
	require.NoError(t, os.WriteFile(f.bPath, []byte(overlayB), 0o644))
	f.mainURI, f.aURI, f.bURI = uri.File(f.mainPath), uri.File(f.aPath), uri.File(f.bPath)
	f.ts = initWorkspaceTestServer(t, dir)
	settings := f.ts.getSettings()
	settings.InlayHints.InferredAmounts = false
	settings.InlayHints.RunningBalances = true
	f.ts.setSettings(settings)
	return f
}

func (f *overlayFixture) balanceHints(t *testing.T, docURI uri.URI) []string {
	t.Helper()
	hints, err := f.ts.InlayHint(context.Background(), &protocol.InlayHintParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
		Range:        protocol.Range{End: protocol.Position{Line: 100}},
	})
	require.NoError(t, err)
	var labels []string
	for _, hint := range hints {
		if label, ok := hint.Label.(protocol.String); ok {
			labels = append(labels, string(label))
		}
	}
	return labels
}

// Issue #126: a.journal and b.journal are open and unsaved. Editing b and then a
// rebuilds the tree for the second edit, which must still see b's first one.
// hledger -f all.journal register assets:cash on the edited text ends at -17 USD.
func TestWorkspace_OpenFileKeepsUnsavedEditsAcrossRebuilds(t *testing.T) {
	f := newOverlayFixture(t, overlayA)
	_, _ = f.ts.openAndWait(f.mainURI, overlayMain)
	_, _ = f.ts.openAndWait(f.aURI, overlayA)
	_, _ = f.ts.openAndWait(f.bURI, overlayB)

	editedB := "2024-01-02 b\n    assets:cash  3 USD\n    equity:b\n"
	_, err := f.ts.replaceAndWait(f.bURI, editedB)
	require.NoError(t, err)
	assert.Contains(t, f.balanceHints(t, f.bURI), "balance: -7 USD")

	editedA := "2024-01-01 a\n    assets:cash  -20 USD\n    equity:a\n"
	_, err = f.ts.replaceAndWait(f.aURI, editedA)
	require.NoError(t, err)

	assert.Contains(t, f.balanceHints(t, f.bURI), "balance: -17 USD", "b's unsaved edit must count after a is edited")
	assert.NotContains(t, f.balanceHints(t, f.bURI), "balance: -15 USD", "b must not be read from disk")
}

// The same stale read made balance assertions wrong. Here a is the later
// transaction: hledger accepts "assets:cash -20 USD = -17 USD" only when b's
// unsaved 3 USD counts (3 - 20 = -17); read from disk (5 USD) it would fail.
func TestWorkspace_OpenFileKeepsUnsavedEditsInAssertions(t *testing.T) {
	laterA := "2024-01-03 a\n    assets:cash  -10 USD\n    equity:a\n"
	f := newOverlayFixture(t, laterA)
	_, _ = f.ts.openAndWait(f.mainURI, overlayMain)
	_, _ = f.ts.openAndWait(f.aURI, laterA)
	_, _ = f.ts.openAndWait(f.bURI, overlayB)

	_, err := f.ts.replaceAndWait(f.bURI, "2024-01-02 b\n    assets:cash  3 USD\n    equity:b\n")
	require.NoError(t, err)
	_, err = f.ts.replaceAndWait(f.aURI, "2024-01-03 a\n    assets:cash  -20 USD = -17 USD\n    equity:a\n")
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		params := f.ts.client.diagnosticsFor(f.aURI)
		return params != nil && len(params.Diagnostics) == 0
	}, 3*time.Second, 20*time.Millisecond, "the assertion in a holds when b's edit counts")
}

// Closing a document puts the tree back on the disk content.
func TestWorkspace_ClosedFileRevertsToDisk(t *testing.T) {
	f := newOverlayFixture(t, overlayA)
	_, _ = f.ts.openAndWait(f.mainURI, overlayMain)
	_, _ = f.ts.openAndWait(f.aURI, overlayA)
	_, _ = f.ts.openAndWait(f.bURI, overlayB)

	_, err := f.ts.replaceAndWait(f.bURI, "2024-01-02 b\n    assets:cash  3 USD\n    equity:b\n")
	require.NoError(t, err)
	require.NoError(t, f.ts.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: f.bURI},
	}))
	_, err = f.ts.replaceAndWait(f.aURI, "2024-01-01 a\n    assets:cash  -20 USD\n    equity:a\n")
	require.NoError(t, err)

	hints := f.balanceHints(t, f.aURI)
	assert.Contains(t, hints, "balance: -20 USD")
	resolved := f.ts.workspace.GetResolved()
	require.NotNil(t, resolved)
	var total string
	for _, tx := range resolved.AllTransactions() {
		if tx.Description == "b" {
			total = tx.Postings[0].Amount.Quantity.String()
		}
	}
	assert.Equal(t, "5", total, "the closed file is read from disk, where it still holds 5 USD")
}

// An included file deleted on disk while an editor holds it open keeps its
// buffer; once it is closed the include is missing, as in hledger.
func TestWorkspace_DeletedIncludeWhileOpenAndAfterClose(t *testing.T) {
	f := newOverlayFixture(t, overlayA)
	_, _ = f.ts.openAndWait(f.mainURI, overlayMain)
	_, _ = f.ts.openAndWait(f.aURI, overlayA)
	_, _ = f.ts.openAndWait(f.bURI, overlayB)
	_, err := f.ts.replaceAndWait(f.bURI, "2024-01-02 b\n    assets:cash  3 USD\n    equity:b\n")
	require.NoError(t, err)

	require.NoError(t, os.Remove(f.bPath))
	require.NoError(t, f.ts.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{{URI: f.bURI, Type: protocol.FileChangeTypeDeleted}},
	}))
	_, err = f.ts.replaceAndWait(f.aURI, "2024-01-01 a\n    assets:cash  -20 USD\n    equity:a\n")
	require.NoError(t, err)
	assert.Equal(t, "3", amountOfTransaction(t, f.ts, "b"), "the open buffer of b counts although its file is gone")

	require.NoError(t, f.ts.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: f.bURI},
	}))
	assert.Equal(t, "", amountOfTransaction(t, f.ts, "b"), "b is gone from disk and closed")
	assert.NotEmpty(t, f.ts.workspace.LoadErrors(), "the missing include is reported")
}

func amountOfTransaction(t *testing.T, ts *testServer, description string) string {
	t.Helper()
	resolved := ts.workspace.GetResolved()
	require.NotNil(t, resolved)
	for _, tx := range resolved.AllTransactions() {
		if tx.Description == description {
			return tx.Postings[0].Amount.Quantity.String()
		}
	}
	return ""
}

// A file deleted on disk and not open drops out of the tree at once.
func TestWorkspace_DeletedClosedIncludeIsMissing(t *testing.T) {
	f := newOverlayFixture(t, overlayA)
	_, _ = f.ts.openAndWait(f.mainURI, overlayMain)
	_, _ = f.ts.openAndWait(f.aURI, overlayA)

	require.NoError(t, os.Remove(f.bPath))
	require.NoError(t, f.ts.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{{URI: f.bURI, Type: protocol.FileChangeTypeDeleted}},
	}))

	resolved := f.ts.workspace.GetResolved()
	require.NotNil(t, resolved)
	for _, tx := range resolved.AllTransactions() {
		assert.NotEqual(t, "b", tx.Description)
	}
	assert.NotEmpty(t, f.ts.workspace.LoadErrors(), "the missing include is reported")
}

// A closed include that is renamed away and back arrives as Deleted and then
// Created in separate batches. The first rebuild leaves the file without
// owners; the second must still find the trees that include it.
func TestWorkspace_DeletedThenCreatedClosedIncludeComesBack(t *testing.T) {
	f := newOverlayFixture(t, overlayA)
	_, _ = f.ts.openAndWait(f.mainURI, overlayMain)
	_, _ = f.ts.openAndWait(f.aURI, overlayA)
	moved := filepath.Join(f.dir, "b-moved.journal")

	require.NoError(t, os.Rename(f.bPath, moved))
	require.NoError(t, f.ts.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{{URI: f.bURI, Type: protocol.FileChangeTypeDeleted}},
	}))
	assert.Equal(t, "", amountOfTransaction(t, f.ts, "b"))
	assert.NotEmpty(t, f.ts.workspace.LoadErrors(), "b is missing while it is renamed away")

	require.NoError(t, os.Rename(moved, f.bPath))
	require.NoError(t, f.ts.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{{URI: f.bURI, Type: protocol.FileChangeTypeCreated}},
	}))
	assert.Equal(t, "5", amountOfTransaction(t, f.ts, "b"), "b is back in the tree")
	assert.Empty(t, f.ts.workspace.LoadErrors(), "the include no longer fails")
}

// A file that cannot be read when its document closes is not in the tree, and
// its last buffer must stop counting, as hledger fails on the include.
func TestWorkspace_CloseOfUnreadableFileDropsItsBuffer(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("file permissions do not restrict root")
	}
	f := newOverlayFixture(t, overlayA)
	_, _ = f.ts.openAndWait(f.mainURI, overlayMain)
	_, _ = f.ts.openAndWait(f.aURI, overlayA)
	_, _ = f.ts.openAndWait(f.bURI, overlayB)
	_, err := f.ts.replaceAndWait(f.bURI, "2024-01-02 b\n    assets:cash  3 USD\n    equity:b\n")
	require.NoError(t, err)

	require.NoError(t, os.Chmod(f.bPath, 0o000))
	t.Cleanup(func() { _ = os.Chmod(f.bPath, 0o644) })
	require.NoError(t, f.ts.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: f.bURI},
	}))
	_, err = f.ts.replaceAndWait(f.aURI, "2024-01-01 a\n    assets:cash  -20 USD\n    equity:a\n")
	require.NoError(t, err)

	assert.Equal(t, "", amountOfTransaction(t, f.ts, "b"), "the closed buffer of b no longer counts")
}
