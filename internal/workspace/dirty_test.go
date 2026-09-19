package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/include"
)

// newDirtyWorkspace writes files into a fresh directory and initializes a
// workspace over it. Files are named relative to the returned directory.
func newDirtyWorkspace(t *testing.T, files map[string]string) (*Workspace, string) {
	t.Helper()
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	dir := t.TempDir()
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
	ws := NewWorkspace(dir, include.NewLoader())
	require.NoError(t, ws.Initialize())
	return ws, dir
}

// resolvedOf returns the tree's current resolved journal. Read directly on
// purpose: these tests assert on the internal pointer, which the accessors
// would refresh before handing back.
func resolvedOf(ws *Workspace, rootPath string) *include.ResolvedJournal {
	tree := ws.trees[rootPath]
	if tree == nil {
		return nil
	}
	return tree.Resolved
}

func TestWorkspace_MarkFileDirty_DefersUntilRead(t *testing.T) {
	ws, dir := newDirtyWorkspace(t, map[string]string{
		"main.journal": "2024-01-01 a\n    expenses:food  $1\n    assets:cash\n",
	})
	root := filepath.Join(dir, "main.journal")

	before := resolvedOf(ws, root)
	require.NotNil(t, before)

	ws.MarkFileDirty(root, StaticContent("2024-01-02 b\n    expenses:rent  $2\n    assets:cash\n"))

	assert.Same(t, before, resolvedOf(ws, root),
		"MarkFileDirty must not re-resolve: the O(n) work belongs to the first read")
	assert.Len(t, ws.dirty, 1, "the edit must be recorded as pending")

	// Any accessor that hands out tree data forces the recompute.
	_ = ws.IndexSnapshot()

	assert.NotSame(t, before, resolvedOf(ws, root),
		"the first read after MarkFileDirty must apply the edit")
	assert.Empty(t, ws.dirty)
}

func TestWorkspace_RefreshScopedToOwningRoots(t *testing.T) {
	ws, dir := newDirtyWorkspace(t, map[string]string{
		"a.journal":     "include child.journal\n\n2024-01-01 a\n    expenses:food  $1\n    assets:cash\n",
		"b.journal":     "2024-01-01 b\n    expenses:rent  $2\n    assets:cash\n",
		"child.journal": "2024-01-03 c\n    expenses:books  $3\n    assets:cash\n",
	})
	aRoot := filepath.Join(dir, "a.journal")
	bRoot := filepath.Join(dir, "b.journal")
	child := filepath.Join(dir, "child.journal")

	require.NotNil(t, resolvedOf(ws, aRoot), "a.journal must be a root tree")
	require.NotNil(t, resolvedOf(ws, bRoot), "b.journal must be a root tree")

	aBefore := resolvedOf(ws, aRoot)
	bBefore := resolvedOf(ws, bRoot)

	ws.MarkFileDirty(child, StaticContent("2024-01-03 c\n    expenses:books  $9\n    assets:cash\n"))
	_ = ws.IndexSnapshot()

	assert.NotSame(t, aBefore, resolvedOf(ws, aRoot),
		"the tree owning the edited file must be re-resolved")
	assert.Same(t, bBefore, resolvedOf(ws, bRoot),
		"a tree that does not own the edited file must not be touched")
}

func TestWorkspace_RefreshCoalescesBurst(t *testing.T) {
	ws, dir := newDirtyWorkspace(t, map[string]string{
		"main.journal": "2024-01-01 a\n    expenses:food  $1\n    assets:cash\n",
	})
	root := filepath.Join(dir, "main.journal")

	before := resolvedOf(ws, root)
	for i := range 20 {
		ws.MarkFileDirty(root, StaticContent("2024-01-0"+string(rune('1'+i%9))+" burst\n    expenses:food  $1\n    assets:cash\n"))
	}

	assert.Len(t, ws.dirty, 1, "a burst on one file collapses into a single pending edit")
	assert.Same(t, before, resolvedOf(ws, root), "nothing is applied before a read")

	_ = ws.IndexSnapshot()

	assert.Empty(t, ws.dirty, "one read applies the whole burst")
	assert.Equal(t, ws.revision, ws.trees[root].rootContentRevision,
		"the tree must be built from the last edit in the burst, not an earlier one")
}

// TestWorkspace_MarkFileDirty_UpdatesIndexAndResolved covers the contract the
// removed UpdateFileWithJournal test asserted: an edit lands in both the
// aggregate index and the owning root's resolved journal.
func TestWorkspace_MarkFileDirty_UpdatesIndexAndResolved(t *testing.T) {
	ws, dir := newDirtyWorkspace(t, map[string]string{
		"main.journal": "2024-01-01 store\n    expenses:food  $20\n    assets:cash\n",
	})
	root := filepath.Join(dir, "main.journal")

	ws.MarkFileDirty(root, StaticContent("2024-01-01 store\n    expenses:food  $20\n    assets:cash\n\n2024-01-02 extra\n    expenses:rent  $5\n    assets:bank\n"))

	snap := ws.IndexSnapshot()
	assert.Contains(t, snap.Accounts.All, "expenses:rent")

	resolved := ws.GetResolvedForFile(root)
	require.NotNil(t, resolved)
	found := false
	for _, tx := range resolved.AllTransactions() {
		if tx.Description == "extra" {
			found = true
			break
		}
	}
	assert.True(t, found, "the owning root's resolved journal must include the edit")
}

// TestWorkspace_MarkFileDirty_DropsOrphanedIndex covers a file falling out of
// every include tree: its index entry must go with it, otherwise the workspace
// keeps offering accounts and payees from a file nothing includes any more.
func TestWorkspace_MarkFileDirty_DropsOrphanedIndex(t *testing.T) {
	ws, dir := newDirtyWorkspace(t, map[string]string{
		"main.journal": "include one.journal\n\n2024-01-01 main\n    income:salary  $10\n    assets:cash\n",
		"one.journal":  "2024-01-02 one\n    expenses:books  $3\n    assets:cash\n",
	})
	root := filepath.Join(dir, "main.journal")
	one := filepath.Join(dir, "one.journal")

	require.Contains(t, ws.IndexSnapshot().Accounts.All, "expenses:books")

	ws.MarkFileDirty(root, StaticContent("2024-01-01 main\n    income:salary  $10\n    assets:cash\n"))

	snap := ws.IndexSnapshot()
	assert.NotContains(t, snap.Accounts.All, "expenses:books",
		"one.journal is no longer included by anything, so its accounts must go")
	assert.Equal(t, []string(nil), ws.fileTree[one])
	assert.Nil(t, ws.index.FileIndex(one))
}

func TestWorkspace_ResolvedForRootContent_StaleRevisionReturnsNil(t *testing.T) {
	ws, dir := newDirtyWorkspace(t, map[string]string{
		"main.journal": "2024-01-01 a\n    expenses:food  $1\n    assets:cash\n",
	})
	root := filepath.Join(dir, "main.journal")

	oldRevision := ws.MarkFileDirty(root, StaticContent("2024-01-02 b\n    expenses:rent  $2\n    assets:cash\n"))
	newRevision := ws.MarkFileDirty(root, StaticContent("2024-01-03 c\n    expenses:books  $3\n    assets:cash\n"))
	require.NotEqual(t, oldRevision, newRevision)

	// A request still carrying the older content must not be answered from a
	// tree built out of the newer one.
	resolved, loadErrors := ws.ResolvedForRootContent(root, oldRevision)
	assert.Nil(t, resolved)
	assert.Nil(t, loadErrors)

	resolved, _ = ws.ResolvedForRootContent(root, newRevision)
	require.NotNil(t, resolved, "the revision matching the newest content must be served")
	found := false
	for _, tx := range resolved.AllTransactions() {
		if tx.Description == "c" {
			found = true
			break
		}
	}
	assert.True(t, found, "the served tree must be built from the newest content")
}

func TestWorkspace_ResolvedForRootContent_UnknownRevisionReturnsNil(t *testing.T) {
	ws, dir := newDirtyWorkspace(t, map[string]string{
		"main.journal": "2024-01-01 a\n    expenses:food  $1\n    assets:cash\n",
	})
	root := filepath.Join(dir, "main.journal")

	ws.MarkFileDirty(root, StaticContent("2024-01-02 b\n    expenses:rent  $2\n    assets:cash\n"))

	resolved, _ := ws.ResolvedForRootContent(root, RevisionUnknown)
	assert.Nil(t, resolved, "a caller that cannot name its content must not be served a tree")

	assert.Equal(t, RevisionUnknown, ws.ContentRevision(filepath.Join(dir, "other.journal")))
	assert.NotEqual(t, RevisionUnknown, ws.ContentRevision(root))
}

// TestWorkspace_ResolvedForRootContent_IncludedEditInvalidatesRoot covers the
// case the snapshot flag used to handle: editing an included file re-resolves
// the root's tree, so the tree no longer corresponds to any content the root's
// buffer can name.
func TestWorkspace_ResolvedForRootContent_IncludedEditInvalidatesRoot(t *testing.T) {
	ws, dir := newDirtyWorkspace(t, map[string]string{
		"main.journal":  "include child.journal\n\n2024-01-01 main\n    income:salary  $10\n    assets:cash\n",
		"child.journal": "2024-01-02 child\n    expenses:books  $3\n    assets:cash\n",
	})
	root := filepath.Join(dir, "main.journal")
	child := filepath.Join(dir, "child.journal")

	rootRevision := ws.MarkFileDirty(root, StaticContent("include child.journal\n\n2024-01-01 main\n    income:salary  $10\n    assets:cash\n"))
	resolved, _ := ws.ResolvedForRootContent(root, rootRevision)
	require.NotNil(t, resolved)

	ws.MarkFileDirty(child, StaticContent("2024-01-02 child\n    expenses:books  $9\n    assets:cash\n"))

	resolved, _ = ws.ResolvedForRootContent(root, rootRevision)
	assert.Nil(t, resolved,
		"the root tree now reflects an edit the buffer cannot name, so it must not be served")
}
