package workspace

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/filetype"
	"github.com/juev/hledger-lsp/internal/formatter"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/juev/hledger-lsp/internal/textutil"
)

// IncludeTree represents a single include tree rooted at one journal file.
// Each root file (file with no incoming include edges) gets its own tree
// with an independent ResolvedJournal and caches.
//
// Resolved and LoadErrors are only current as of the last refresh. Callers
// inside this package must reach them through the Workspace accessors, which
// refresh first; reaching into a tree directly can hand back a stale journal.
type IncludeTree struct {
	RootPath   string
	Resolved   *include.ResolvedJournal
	LoadErrors []include.LoadError
	// rootContentRevision is the edit revision this tree was re-resolved from,
	// set only when the tree's own root was the edited file. Zero means the
	// tree does not correspond to any revision a caller could be holding, and
	// ResolvedForRootContent refuses it.
	rootContentRevision uint64
	cachedFormats       map[string]formatter.CommodityFormat
	cachedCommodities   map[string]bool
	cachedAccounts      map[string]bool
}

func (t *IncludeTree) clearCaches() {
	t.cachedFormats = nil
	t.cachedCommodities = nil
	t.cachedAccounts = nil
}

// RevisionUnknown is the revision of content the workspace never recorded. It
// never matches a tree, so a caller passing it falls back to loading content
// directly.
const RevisionUnknown uint64 = 0

// ContentSource is a document whose text can be materialized on demand. Marking
// an edit therefore costs whatever it costs to name the document, not a copy of
// it: a rope is handed over as-is and only flattened inside the recompute, once
// per burst instead of once per keystroke.
//
// The two methods are separate for that reason. Version is read when the edit is
// recorded and must not copy anything; the recompute reads the content and then
// the version again, so a document that moved on in between — the versions
// differ — is discarded rather than stamped with a revision it does not belong
// to. Reading the version last is what makes that safe: versions only increase,
// so a version equal to the recorded one means the content read just before it
// was still the recorded content.
type ContentSource interface {
	Version() uint64
	Materialize() string
}

// StaticContent adapts text that is already materialized and cannot move on,
// such as a file just read from disk. Its version never changes, so it always
// matches the edit it was recorded with.
type StaticContent string

func (StaticContent) Version() uint64       { return 0 }
func (c StaticContent) Materialize() string { return string(c) }

// pendingEdit is an editor edit that has been recorded but not yet applied to
// the include trees and the workspace index.
type pendingEdit struct {
	source        ContentSource
	sourceVersion uint64
	revision      uint64
}

// Workspace holds the include trees and the search index for one workspace
// root. Edits are recorded with MarkFileDirty and applied lazily: the first
// read that needs tree data runs refresh, which re-resolves only the trees
// owning the edited files and updates the index incrementally. A keystroke
// therefore costs O(1) plus one deferred recompute shared by every consumer,
// instead of a full re-parse and index rebuild per edit.
//
// Every public method that hands out tree or index data calls refresh first.
// A consumer that reaches into an IncludeTree without going through one of
// those methods will read a journal that may predate the last edit.
//
// Lock order: refreshMu, then mu. refresh takes both, so no method may call it
// while already holding mu.
type Workspace struct {
	mu         sync.RWMutex
	refreshMu  sync.Mutex
	rootURI    string
	trees      map[string]*IncludeTree // rootPath → tree
	fileTree   map[string][]string     // filePath → sorted owning root paths
	dirty      map[string]pendingEdit  // filePath → edit not yet applied
	dirtyCount atomic.Int64            // len(dirty), readable without mu
	revision   uint64                  // monotonic edit counter, guarded by mu
	// contentRevisions records, per filePath, the revision of the most recent
	// content recorded for it. Unlike dirty, entries outlive refresh, so a
	// caller holding the current buffer can ask whether a tree is up to date
	// without naming a revision itself.
	contentRevisions map[string]uint64
	// includeGraph and reverseGraph are kept for GetIncludedBy and
	// isWorkspaceFileLocked.
	includeGraph map[string][]string
	reverseGraph map[string][]string
	loader       *include.Loader
	parseErrors  []string
	index        *WorkspaceIndex
}

func NewWorkspace(rootURI string, loader *include.Loader) *Workspace {
	return &Workspace{
		rootURI:          rootURI,
		loader:           loader,
		trees:            make(map[string]*IncludeTree),
		fileTree:         make(map[string][]string),
		dirty:            make(map[string]pendingEdit),
		contentRevisions: make(map[string]uint64),
		includeGraph:     make(map[string][]string),
		reverseGraph:     make(map[string][]string),
		index:            NewWorkspaceIndex(),
	}
}

func (w *Workspace) Initialize() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.parseErrors = nil
	w.trees = make(map[string]*IncludeTree)
	w.fileTree = make(map[string][]string)
	w.dirty = make(map[string]pendingEdit)
	w.dirtyCount.Store(0)
	w.contentRevisions = make(map[string]uint64)
	w.index = NewWorkspaceIndex()
	w.includeGraph = make(map[string][]string)
	w.reverseGraph = make(map[string][]string)

	roots, err := w.findRootJournals()
	if err != nil {
		return err
	}

	for _, rootPath := range roots {
		resolved, errs := w.loader.Load(rootPath)
		tree := &IncludeTree{
			RootPath:   rootPath,
			Resolved:   resolved,
			LoadErrors: errs,
		}
		w.trees[rootPath] = tree
		w.addOwnerLocked(rootPath, rootPath)
		if resolved != nil {
			for path := range resolved.Files {
				w.addOwnerLocked(path, rootPath)
			}
		}
	}

	w.buildIndexFromResolvedLocked()
	return nil
}

func (w *Workspace) LoadErrors() []include.LoadError {
	w.refresh()
	w.mu.RLock()
	defer w.mu.RUnlock()
	var all []include.LoadError
	for _, tree := range w.trees {
		all = append(all, tree.LoadErrors...)
	}
	return all
}

func (w *Workspace) ParseErrors() []string {
	w.refresh()
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.parseErrors
}

// findRootJournals discovers all root journal files in the workspace.
// A root is a file with no incoming include edges (not included by anyone).
// Environment variables (LEDGER_FILE, HLEDGER_JOURNAL) are intentionally
// ignored — they typically point to the user's primary journal which may
// be completely unrelated to the workspace being edited.
func (w *Workspace) findRootJournals() ([]string, error) {
	journalFiles, err := w.findJournalFiles()
	if err != nil {
		return nil, err
	}

	if len(journalFiles) == 0 {
		return nil, nil
	}

	w.buildIncludeGraph(journalFiles)

	var rootCandidates []string
	for _, file := range journalFiles {
		if len(w.reverseGraph[file]) == 0 {
			rootCandidates = append(rootCandidates, file)
		}
	}

	// All files in a cycle — treat all as roots
	if len(rootCandidates) == 0 {
		rootCandidates = append(rootCandidates, journalFiles...)
	}

	sort.Strings(rootCandidates)
	return rootCandidates, nil
}

var excludedDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true,
	"node_modules": true, "vendor": true, ".cache": true,
}

func (w *Workspace) findJournalFiles() ([]string, error) {
	var files []string
	err := filepath.Walk(w.rootURI, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // intentionally skip inaccessible files
		}
		if info.IsDir() {
			if excludedDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if filetype.IsJournalPath(path) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func (w *Workspace) buildIncludeGraph(files []string) {
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			w.parseErrors = append(w.parseErrors, fmt.Sprintf("%s: %v", file, err))
			continue
		}

		journal, errs := parser.Parse(textutil.NormalizeLineEndings(string(content)))
		if len(errs) > 0 {
			for _, e := range errs {
				w.parseErrors = append(w.parseErrors, fmt.Sprintf("%s: %s", file, e.Message))
			}
		}
		if journal == nil {
			continue
		}

		dir := filepath.Dir(file)
		for _, inc := range journal.Includes {
			if include.IsGlobPattern(inc.Path) {
				matches, err := w.loader.ExpandGlob(file, inc.Path)
				if err != nil {
					continue
				}
				for _, match := range matches {
					absMatch, _ := filepath.Abs(match)
					if absMatch == "" {
						absMatch = match
					}
					w.includeGraph[file] = append(w.includeGraph[file], absMatch)
					w.reverseGraph[absMatch] = append(w.reverseGraph[absMatch], file)
				}
				continue
			}

			incPath := inc.Path
			if !filepath.IsAbs(incPath) {
				incPath = filepath.Join(dir, incPath)
			}
			incPath = filepath.Clean(incPath)

			w.includeGraph[file] = append(w.includeGraph[file], incPath)
			w.reverseGraph[incPath] = append(w.reverseGraph[incPath], file)
		}
	}
}

// RootJournalPath returns the root path of the first tree (for backward compatibility).
func (w *Workspace) RootJournalPath() string {
	w.refresh()
	w.mu.RLock()
	defer w.mu.RUnlock()
	// Return the first tree root in sorted order for determinism
	for _, tree := range w.sortedTrees() {
		return tree.RootPath
	}
	return ""
}

// GetResolved returns the resolved journal of the first tree (for backward compatibility).
func (w *Workspace) GetResolved() *include.ResolvedJournal {
	w.refresh()
	w.mu.RLock()
	defer w.mu.RUnlock()
	for _, tree := range w.sortedTrees() {
		return tree.Resolved
	}
	return nil
}

// GetResolvedForFile returns the resolved journal for the include tree
// that contains the given file path. When multiple roots own the file,
// the lexicographically smallest root is selected (deterministic policy).
func (w *Workspace) GetResolvedForFile(path string) *include.ResolvedJournal {
	w.refresh()
	w.mu.RLock()
	defer w.mu.RUnlock()
	rootPath := w.primaryRootLocked(path)
	if rootPath == "" {
		return nil
	}
	tree, ok := w.trees[rootPath]
	if !ok {
		return nil
	}
	return tree.Resolved
}

// GetIncludeTreesForFile returns all include trees that own path, sorted by
// root path. Callers that depend on include context must use this instead of
// GetResolvedForFile, which keeps its primary-root behavior for compatibility.
func (w *Workspace) GetIncludeTreesForFile(path string) []*IncludeTree {
	w.refresh()
	w.mu.RLock()
	defer w.mu.RUnlock()

	roots := w.fileTree[path]
	trees := make([]*IncludeTree, 0, len(roots))
	for _, rootPath := range roots {
		if tree := w.trees[rootPath]; tree != nil {
			trees = append(trees, tree)
		}
	}
	return trees
}

// GetUniqueResolvedForFile returns the resolved journal only when exactly one
// include-tree root owns path. Callers that need an unambiguous transaction
// context, such as running balance calculations, must use this method instead
// of the deterministic primary-root fallback.
func (w *Workspace) GetUniqueResolvedForFile(path string) (*include.ResolvedJournal, bool) {
	w.refresh()
	w.mu.RLock()
	defer w.mu.RUnlock()

	roots := w.fileTree[path]
	if len(roots) != 1 {
		return nil, false
	}
	tree := w.trees[roots[0]]
	if tree == nil || tree.Resolved == nil {
		return nil, false
	}
	return tree.Resolved, true
}

// ResolvedForRootContent returns a root tree only when its resolved state was
// built from the edit revision the caller holds, so a request carrying older
// content is never answered from a newer tree. revision is the value
// MarkFileDirty returned for that content; RevisionUnknown never matches.
func (w *Workspace) ResolvedForRootContent(path string, revision uint64) (*include.ResolvedJournal, []include.LoadError) {
	w.refresh()
	w.mu.RLock()
	defer w.mu.RUnlock()

	tree := w.trees[path]
	if tree == nil || revision == RevisionUnknown || tree.rootContentRevision != revision {
		return nil, nil
	}
	return tree.Resolved, tree.LoadErrors
}

// primaryRootLocked returns the lexicographically smallest owning root for path.
// Caller must hold at least a read lock.
func (w *Workspace) primaryRootLocked(path string) string {
	roots := w.fileTree[path]
	if len(roots) == 0 {
		return ""
	}
	return roots[0]
}

// addOwnerLocked adds root to the sorted owner list for path.
// Caller must hold the write lock.
func (w *Workspace) addOwnerLocked(path, root string) {
	roots := w.fileTree[path]
	for _, r := range roots {
		if r == root {
			return
		}
	}
	w.fileTree[path] = append(roots, root)
	sort.Strings(w.fileTree[path])
}

// removeOwnerLocked removes root from the owner list for path.
// Caller must hold the write lock.
func (w *Workspace) removeOwnerLocked(path, root string) {
	roots := w.fileTree[path]
	for i, r := range roots {
		if r == root {
			w.fileTree[path] = append(roots[:i], roots[i+1:]...)
			break
		}
	}
	if len(w.fileTree[path]) == 0 {
		delete(w.fileTree, path)
	}
}

// hasOwnerLocked reports whether root is in the owner list for path.
// Caller must hold at least a read lock.
func (w *Workspace) hasOwnerLocked(path, root string) bool {
	for _, r := range w.fileTree[path] {
		if r == root {
			return true
		}
	}
	return false
}

func (w *Workspace) sortedTrees() []*IncludeTree {
	keys := make([]string, 0, len(w.trees))
	for k := range w.trees {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	result := make([]*IncludeTree, 0, len(keys))
	for _, k := range keys {
		result = append(result, w.trees[k])
	}
	return result
}

// AllTrees returns all include trees in deterministic order (sorted by root path).
func (w *Workspace) AllTrees() []*IncludeTree {
	w.refresh()
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.sortedTrees()
}

func (w *Workspace) IndexSnapshot() IndexSnapshot {
	w.refresh()
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.index == nil {
		return IndexSnapshot{}
	}
	return w.index.Snapshot()
}

// MarkFileDirty records that path now holds the content of source, without
// re-resolving the include trees or rebuilding the index. The work happens on
// the first read that needs tree data, so a burst of keystrokes collapses into
// one recompute instead of one per edit.
//
// The returned revision identifies this content and is what a caller passes
// back to ResolvedForRootContent. It is a workspace-local counter, not the LSP
// document version: two edits can carry the same client version (or none at
// all, as in tests), which would make the comparison match the wrong content.
//
// Returns RevisionUnknown when path is not a journal file, and so was not
// recorded.
//
// source must yield LF-only content, matching the invariant the rest of the
// pipeline holds (see the line-endings note in CLAUDE.md). Normalizing here
// would put a full scan of the document on the keystroke path, where the
// editor content is LF by construction.
func (w *Workspace) MarkFileDirty(path string, source ContentSource) uint64 {
	if path == "" || !filetype.IsJournalPath(path) || source == nil {
		return RevisionUnknown
	}
	sourceVersion := source.Version()

	w.mu.Lock()
	defer w.mu.Unlock()

	w.revision++
	w.dirty[path] = pendingEdit{source: source, sourceVersion: sourceVersion, revision: w.revision}
	w.contentRevisions[path] = w.revision
	w.dirtyCount.Store(int64(len(w.dirty)))
	return w.revision
}

// ContentRevision returns the revision of the content most recently recorded
// for path, or RevisionUnknown when the workspace holds none. A caller that
// has just read the current buffer for path can pass the result to
// ResolvedForRootContent to accept the tree only when it is built from that
// same content.
func (w *Workspace) ContentRevision(path string) uint64 {
	w.refresh()
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.contentRevisions[path]
}

// refresh applies every pending edit. It runs at the top of each public method
// that hands out tree or index data, so no consumer can observe a stale tree.
//
// The atomic counter keeps the common case — nothing pending — to a single
// load. When something is pending, refreshMu serialises the recompute so
// concurrent readers collapse onto one pass rather than queueing behind mu.
func (w *Workspace) refresh() {
	if w.dirtyCount.Load() == 0 {
		return
	}
	w.refreshMu.Lock()
	defer w.refreshMu.Unlock()
	// Another goroutine may have applied the edits while we waited for
	// refreshMu; the counter is the only thing that needs re-checking.
	if w.dirtyCount.Load() == 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.applyDirtyLocked()
}

// applyDirtyLocked re-resolves the trees owning the edited files and updates
// the index for those files only. Caller must hold the write lock.
func (w *Workspace) applyDirtyLocked() {
	if len(w.dirty) == 0 {
		w.dirtyCount.Store(0)
		return
	}
	pending := w.dirty
	w.dirty = make(map[string]pendingEdit, len(pending))
	w.dirtyCount.Store(0)

	if len(w.trees) == 0 || w.index == nil {
		return
	}

	// Each owning tree is re-resolved once, with an overlay holding every edit
	// that belongs to it, so a burst touching several files of one journal
	// still costs a single load.
	overlays := make(map[string]map[string]include.OverlayEntry)
	rootRevision := make(map[string]uint64)
	for path, edit := range pending {
		if !w.isWorkspaceFileLocked(path) {
			continue
		}

		// Read the content, then the version. A document that was edited again
		// since the mark carries a newer version, and this entry describes
		// content no caller can name any more: the newer edit has already
		// replaced it in the dirty map, so the recompute it belongs to is still
		// to come. Applying this one would stamp a tree with a revision that
		// does not match what it was built from.
		content := edit.source.Materialize()
		if edit.source.Version() != edit.sourceVersion {
			continue
		}

		oldIndex := w.index.FileIndex(path)
		oldIncludes := []string(nil)
		if oldIndex != nil {
			oldIncludes = append([]string(nil), oldIndex.Includes...)
		}
		journal, _ := parser.Parse(content)
		fileIndex := BuildFileIndexFromJournal(path, journal)
		w.index.SetFileIndex(path, fileIndex)
		w.updateIncludeEdgesLocked(path, oldIncludes, fileIndex.Includes)

		for _, rootPath := range w.fileTree[path] {
			if overlays[rootPath] == nil {
				overlays[rootPath] = make(map[string]include.OverlayEntry)
			}
			overlays[rootPath][path] = include.OverlayEntry{
				SourcePath: path,
				Content:    content,
				Revision:   edit.revision,
			}
			// Only the tree rooted at the edited file can be said to correspond
			// to the revision the editor holds; a tree that merely includes an
			// edited file was built partly from content the caller never saw.
			if rootPath == path {
				rootRevision[rootPath] = edit.revision
			}
		}
	}

	var orphaned []string
	for rootPath, entries := range overlays {
		tree := w.trees[rootPath]
		if tree == nil {
			continue
		}
		resolved, errs := w.loader.LoadWithOptions(rootPath, include.LoadOptions{Overlays: entries})
		tree.Resolved = resolved
		tree.LoadErrors = errs
		tree.rootContentRevision = rootRevision[rootPath]
		tree.clearCaches()
		orphaned = append(orphaned, w.syncOwnershipFromResolvedLocked(tree)...)

		// A file that entered this tree with the edit (a new include) has no
		// index entry yet. Already-known files keep theirs: their content did
		// not change, so re-deriving it would only repeat the O(n) walk this
		// refresh exists to avoid.
		for path, journal := range tree.Resolved.Files {
			if w.index.FileIndex(path) == nil {
				w.index.SetFileIndex(path, BuildFileIndexFromJournal(path, journal))
				w.updateIncludeEdgesLocked(path, nil, w.index.FileIndex(path).Includes)
			}
		}
	}

	// A path that lost its last owning tree is no longer part of the workspace;
	// leaving its index entry behind would keep reporting its accounts and
	// payees. Ownership was collected across every refreshed tree first, so a
	// path another tree picked up in the same batch is not dropped here.
	for _, path := range orphaned {
		if len(w.fileTree[path]) == 0 {
			w.index.RemoveFile(path)
		}
	}

	w.index.RefreshDerived()
}

// syncOwnershipFromResolvedLocked updates fileTree ownership from the tree's
// resolved journal occurrences. Paths no longer in the resolved journal lose
// this tree as an owner; the paths that lose their last owner are returned, so
// the caller can drop their index entries. Caller must hold the write lock.
func (w *Workspace) syncOwnershipFromResolvedLocked(tree *IncludeTree) []string {
	if tree.Resolved == nil {
		return nil
	}
	inResolved := make(map[string]bool)
	inResolved[tree.RootPath] = true
	for i := range tree.Resolved.Occurrences {
		inResolved[tree.Resolved.Occurrences[i].Path] = true
	}
	// Remove this tree as owner from paths no longer in the resolved journal.
	var orphaned []string
	for path := range w.fileTree {
		if w.hasOwnerLocked(path, tree.RootPath) && !inResolved[path] {
			w.removeOwnerLocked(path, tree.RootPath)
			if len(w.fileTree[path]) == 0 {
				orphaned = append(orphaned, path)
			}
		}
	}
	// Add this tree as owner for all paths in the resolved journal.
	for path := range inResolved {
		w.addOwnerLocked(path, tree.RootPath)
	}
	return orphaned
}

func (w *Workspace) buildIndexFromResolvedLocked() {
	w.index = NewWorkspaceIndex()
	for _, tree := range w.trees {
		if tree.Resolved == nil || tree.Resolved.Primary == nil {
			continue
		}
		w.index.SetFileIndex(tree.RootPath, BuildFileIndexFromJournal(tree.RootPath, tree.Resolved.Primary))
		w.updateIncludeEdgesLocked(tree.RootPath, nil, w.index.FileIndex(tree.RootPath).Includes)

		for path, journal := range tree.Resolved.Files {
			w.index.SetFileIndex(path, BuildFileIndexFromJournal(path, journal))
			w.updateIncludeEdgesLocked(path, nil, w.index.FileIndex(path).Includes)
		}
	}
	w.index.RefreshDerived()
}

func (w *Workspace) updateIncludeEdgesLocked(path string, oldIncludes, newIncludes []string) {
	if len(oldIncludes) > 0 {
		for _, inc := range oldIncludes {
			w.reverseGraph[inc] = removeString(w.reverseGraph[inc], path)
		}
	}
	w.includeGraph[path] = append([]string(nil), newIncludes...)
	for _, inc := range newIncludes {
		w.reverseGraph[inc] = addString(w.reverseGraph[inc], path)
	}
}

func (w *Workspace) isWorkspaceFileLocked(path string) bool {
	if len(w.fileTree[path]) > 0 {
		return true
	}
	if w.index.FileIndex(path) != nil {
		return true
	}
	if len(w.reverseGraph[path]) > 0 {
		return true
	}
	return false
}

func removeString(values []string, target string) []string {
	if len(values) == 0 {
		return values
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != target {
			result = append(result, value)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func addString(values []string, target string) []string {
	for _, value := range values {
		if value == target {
			return values
		}
	}
	return append(values, target)
}

func (w *Workspace) GetIncludedBy(path string) []string {
	// The refresh matters: DidChangeWatchedFiles asks who includes a changed
	// file and republishes diagnostics for those open buffers, which is wrong
	// if the pending edits have not been applied yet.
	w.refresh()
	w.mu.RLock()
	defer w.mu.RUnlock()

	visited := make(map[string]bool)
	var result []string

	queue := []string{path}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if visited[current] {
			continue
		}
		visited[current] = true

		for _, parent := range w.reverseGraph[current] {
			if !visited[parent] {
				result = append(result, parent)
				queue = append(queue, parent)
			}
		}
	}

	return result
}

// GetCommodityFormatsForFile returns commodity formats for the tree containing the given file.
func (w *Workspace) GetCommodityFormatsForFile(path string) map[string]formatter.CommodityFormat {
	w.refresh()
	w.mu.RLock()
	rootPath := w.primaryRootLocked(path)
	tree := w.trees[rootPath]
	if tree == nil {
		w.mu.RUnlock()
		return nil
	}
	if tree.cachedFormats != nil {
		defer w.mu.RUnlock()
		return maps.Clone(tree.cachedFormats)
	}
	w.mu.RUnlock()

	w.mu.Lock()
	defer w.mu.Unlock()

	tree = w.trees[rootPath]
	if tree == nil {
		return nil
	}
	if tree.cachedFormats != nil {
		return maps.Clone(tree.cachedFormats)
	}
	if tree.Resolved == nil {
		return nil
	}

	tree.cachedFormats = formatter.ExtractCommodityFormats(tree.Resolved.FormatDirectives())
	return maps.Clone(tree.cachedFormats)
}

// GetCommodityFormats returns commodity formats for the first tree (backward compatibility).
func (w *Workspace) GetCommodityFormats() map[string]formatter.CommodityFormat {
	root := w.RootJournalPath()
	if root == "" {
		return nil
	}
	return w.GetCommodityFormatsForFile(root)
}

// GetDeclaredCommoditiesForFile returns declared commodities for the tree containing the given file.
func (w *Workspace) GetDeclaredCommoditiesForFile(path string) map[string]bool {
	w.refresh()
	w.mu.RLock()
	rootPath := w.primaryRootLocked(path)
	tree := w.trees[rootPath]
	if tree == nil {
		w.mu.RUnlock()
		return nil
	}
	if tree.cachedCommodities != nil {
		defer w.mu.RUnlock()
		return tree.cachedCommodities
	}
	w.mu.RUnlock()

	w.mu.Lock()
	defer w.mu.Unlock()

	tree = w.trees[rootPath]
	if tree == nil {
		return nil
	}
	if tree.cachedCommodities != nil {
		return tree.cachedCommodities
	}
	if tree.Resolved == nil {
		return nil
	}

	declared := make(map[string]bool)
	for _, dir := range tree.Resolved.AllDirectives() {
		if cd, ok := dir.(ast.CommodityDirective); ok {
			declared[cd.Commodity.Symbol] = true
		}
	}
	tree.cachedCommodities = declared
	return declared
}

// GetDeclaredCommodities returns declared commodities for the first tree (backward compatibility).
func (w *Workspace) GetDeclaredCommodities() map[string]bool {
	root := w.RootJournalPath()
	if root == "" {
		return nil
	}
	return w.GetDeclaredCommoditiesForFile(root)
}

// GetDeclaredAccountsForFile returns declared accounts for the tree containing the given file.
func (w *Workspace) GetDeclaredAccountsForFile(path string) map[string]bool {
	w.refresh()
	w.mu.RLock()
	rootPath := w.primaryRootLocked(path)
	tree := w.trees[rootPath]
	if tree == nil {
		w.mu.RUnlock()
		return nil
	}
	if tree.cachedAccounts != nil {
		defer w.mu.RUnlock()
		return tree.cachedAccounts
	}
	w.mu.RUnlock()

	w.mu.Lock()
	defer w.mu.Unlock()

	tree = w.trees[rootPath]
	if tree == nil {
		return nil
	}
	if tree.cachedAccounts != nil {
		return tree.cachedAccounts
	}
	if tree.Resolved == nil {
		return nil
	}

	declared := make(map[string]bool)
	for _, dir := range tree.Resolved.AllDirectives() {
		if ad, ok := dir.(ast.AccountDirective); ok {
			declared[ad.Account.Name] = true
		}
	}
	tree.cachedAccounts = declared
	return declared
}

// GetDeclaredAccounts returns declared accounts for the first tree (backward compatibility).
func (w *Workspace) GetDeclaredAccounts() map[string]bool {
	root := w.RootJournalPath()
	if root == "" {
		return nil
	}
	return w.GetDeclaredAccountsForFile(root)
}
