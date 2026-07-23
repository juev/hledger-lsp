package workspace

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/filetype"
	"github.com/juev/hledger-lsp/internal/formatter"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/parser"
)

// normalizeLineEndings converts \r\n and \r to \n.
// The parser assumes \n-only input. Files read from disk on Windows may have CRLF.
func normalizeLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

// IncludeTree represents a single include tree rooted at one journal file.
// Each root file (file with no incoming include edges) gets its own tree
// with an independent ResolvedJournal and caches.
type IncludeTree struct {
	RootPath                   string
	Resolved                   *include.ResolvedJournal
	LoadErrors                 []include.LoadError
	rootContentSnapshot        string
	rootContentSnapshotIsValid bool
	cachedFormats              map[string]formatter.CommodityFormat
	cachedCommodities          map[string]bool
	cachedAccounts             map[string]bool
}

func (t *IncludeTree) clearCaches() {
	t.cachedFormats = nil
	t.cachedCommodities = nil
	t.cachedAccounts = nil
}

type Workspace struct {
	mu           sync.RWMutex
	rootURI      string
	trees        map[string]*IncludeTree // rootPath → tree
	fileTree     map[string][]string     // filePath → sorted owning root paths
	includeGraph map[string][]string
	reverseGraph map[string][]string
	loader       *include.Loader
	parseErrors  []string
	index        *WorkspaceIndex
}

func NewWorkspace(rootURI string, loader *include.Loader) *Workspace {
	return &Workspace{
		rootURI:      rootURI,
		loader:       loader,
		trees:        make(map[string]*IncludeTree),
		fileTree:     make(map[string][]string),
		includeGraph: make(map[string][]string),
		reverseGraph: make(map[string][]string),
		index:        NewWorkspaceIndex(),
	}
}

func (w *Workspace) Initialize() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.parseErrors = nil
	w.trees = make(map[string]*IncludeTree)
	w.fileTree = make(map[string][]string)
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
	w.mu.RLock()
	defer w.mu.RUnlock()
	var all []include.LoadError
	for _, tree := range w.trees {
		all = append(all, tree.LoadErrors...)
	}
	return all
}

func (w *Workspace) ParseErrors() []string {
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

		journal, errs := parser.Parse(normalizeLineEndings(string(content)))
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

// ResolvedForRootContent returns a root tree only when its resolved state was
// built from content. The caller must pass CRLF-normalized content.
func (w *Workspace) ResolvedForRootContent(path, content string) (*include.ResolvedJournal, []include.LoadError) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	tree := w.trees[path]
	if tree == nil || !tree.rootContentSnapshotIsValid || tree.rootContentSnapshot != content {
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
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.sortedTrees()
}

func (w *Workspace) IndexSnapshot() IndexSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.index == nil {
		return IndexSnapshot{}
	}
	return w.index.Snapshot()
}

func (w *Workspace) UpdateFile(path, content string) {
	if path == "" || !filetype.IsJournalPath(path) {
		return
	}
	journal, _ := parser.Parse(normalizeLineEndings(content))
	w.UpdateFileWithJournal(path, content, journal)
}

// UpdateFileWithJournal updates the workspace index and owning trees for path
// using a pre-parsed journal, avoiding a redundant parse on the hot path
// (DidChange parses once and reuses the cached journal). The journal must be
// parsed from CRLF-normalized content.
func (w *Workspace) UpdateFileWithJournal(path, content string, journal *ast.Journal) {
	if path == "" || !filetype.IsJournalPath(path) {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.trees) == 0 || w.index == nil {
		return
	}
	if !w.isWorkspaceFileLocked(path) {
		return
	}

	oldIndex := w.index.FileIndex(path)
	oldIncludes := []string(nil)
	if oldIndex != nil {
		oldIncludes = append([]string(nil), oldIndex.Includes...)
	}

	fileIndex := BuildFileIndexFromJournal(path, journal)
	w.index.SetFileIndex(path, fileIndex)
	w.updateIncludeEdgesLocked(path, oldIncludes, fileIndex.Includes)

	// Reload all owning roots with the edited content as an overlay so the
	// loader produces fresh occurrences instead of mutating the legacy projection.
	owners := append([]string(nil), w.fileTree[path]...)
	w.loader.InvalidateFile(path)
	for _, rootPath := range owners {
		tree := w.trees[rootPath]
		if tree == nil {
			continue
		}
		opts := include.LoadOptions{
			Overlays: map[string]include.OverlayEntry{
				path: {SourcePath: path, Content: content},
			},
		}
		resolved, errs := w.loader.LoadWithOptions(rootPath, opts)
		tree.Resolved = resolved
		tree.LoadErrors = errs
		tree.rootContentSnapshotIsValid = rootPath == path
		if tree.rootContentSnapshotIsValid {
			tree.rootContentSnapshot = normalizeLineEndings(content)
		}
		tree.clearCaches()
		w.syncOwnershipFromResolvedLocked(tree)
	}
	w.buildIndexFromResolvedLocked()
}

// syncOwnershipFromResolvedLocked updates fileTree ownership from the tree's
// resolved journal occurrences. Paths no longer in the resolved journal lose
// this tree as an owner. Caller must hold the write lock.
func (w *Workspace) syncOwnershipFromResolvedLocked(tree *IncludeTree) {
	if tree.Resolved == nil {
		return
	}
	inResolved := make(map[string]bool)
	inResolved[tree.RootPath] = true
	for i := range tree.Resolved.Occurrences {
		inResolved[tree.Resolved.Occurrences[i].Path] = true
	}
	// Remove this tree as owner from paths no longer in the resolved journal.
	for path := range w.fileTree {
		if w.hasOwnerLocked(path, tree.RootPath) && !inResolved[path] {
			w.removeOwnerLocked(path, tree.RootPath)
		}
	}
	// Add this tree as owner for all paths in the resolved journal.
	for path := range inResolved {
		w.addOwnerLocked(path, tree.RootPath)
	}
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
