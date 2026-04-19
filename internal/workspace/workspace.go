package workspace

import (
	"fmt"
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
	RootPath          string
	Resolved          *include.ResolvedJournal
	LoadErrors        []include.LoadError
	cachedFormats     map[string]formatter.CommodityFormat
	cachedCommodities map[string]bool
	cachedAccounts    map[string]bool
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
	fileTree     map[string]string       // filePath → rootPath
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
		fileTree:     make(map[string]string),
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
	w.fileTree = make(map[string]string)
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
		w.fileTree[rootPath] = rootPath
		if resolved != nil {
			for path := range resolved.Files {
				w.fileTree[path] = rootPath
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
// that contains the given file path.
func (w *Workspace) GetResolvedForFile(path string) *include.ResolvedJournal {
	w.mu.RLock()
	defer w.mu.RUnlock()
	rootPath, ok := w.fileTree[path]
	if !ok {
		return nil
	}
	tree, ok := w.trees[rootPath]
	if !ok {
		return nil
	}
	return tree.Resolved
}

// RootForFile returns the root journal path for the tree containing the given file.
func (w *Workspace) RootForFile(path string) string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.fileTree[path]
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
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.trees) == 0 || w.index == nil {
		return
	}
	if !w.isWorkspaceFileLocked(path) {
		return
	}

	rootPath := w.fileTree[path]
	tree := w.trees[rootPath]
	if tree == nil {
		return
	}

	oldIndex := w.index.FileIndex(path)
	oldIncludes := []string(nil)
	if oldIndex != nil {
		oldIncludes = append([]string(nil), oldIndex.Includes...)
	}

	fileIndex, journal, _ := BuildFileIndexFromContent(path, content)
	w.index.SetFileIndex(path, fileIndex)
	w.updateIncludeEdgesLocked(path, oldIncludes, fileIndex.Includes)
	w.updateResolvedForTreeLocked(tree, path, journal)
	tree.clearCaches()

	if !sameStringSlice(oldIncludes, fileIndex.Includes) {
		w.refreshTreeLocked(tree)
	}
}

func (w *Workspace) buildIndexFromResolvedLocked() {
	if w.index == nil {
		w.index = NewWorkspaceIndex()
	}
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

func (w *Workspace) updateResolvedForTreeLocked(tree *IncludeTree, path string, journal *ast.Journal) {
	if tree.Resolved == nil {
		tree.Resolved = include.NewResolvedJournal(nil)
	}
	if path == tree.RootPath {
		tree.Resolved.Primary = journal
		return
	}
	if journal == nil {
		delete(tree.Resolved.Files, path)
		tree.Resolved.FileOrder = removeString(tree.Resolved.FileOrder, path)
		return
	}
	tree.Resolved.Files[path] = journal
	tree.Resolved.FileOrder = addString(tree.Resolved.FileOrder, path)
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

func (w *Workspace) refreshTreeLocked(tree *IncludeTree) {
	if tree.RootPath == "" || w.index == nil {
		return
	}

	for {
		reachable := w.computeReachableFromLocked(tree.RootPath)
		w.removeUnreachableFromTreeLocked(tree, reachable)
		added := w.addMissingReachableToTreeLocked(tree, reachable)
		if !added {
			return
		}
	}
}

func (w *Workspace) computeReachableFromLocked(rootPath string) map[string]bool {
	reachable := make(map[string]bool)
	if rootPath == "" {
		return reachable
	}
	queue := []string{rootPath}
	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]
		if reachable[path] {
			continue
		}
		reachable[path] = true
		for _, inc := range w.includeGraph[path] {
			if !reachable[inc] {
				queue = append(queue, inc)
			}
		}
	}
	return reachable
}

func (w *Workspace) removeUnreachableFromTreeLocked(tree *IncludeTree, reachable map[string]bool) {
	// Only remove files that belong to this tree
	var toRemove []string
	for path, root := range w.fileTree {
		if root == tree.RootPath && !reachable[path] && path != tree.RootPath {
			toRemove = append(toRemove, path)
		}
	}
	for _, path := range toRemove {
		oldIndex := w.index.FileIndex(path)
		if oldIndex != nil {
			w.updateIncludeEdgesLocked(path, oldIndex.Includes, nil)
		}
		w.index.RemoveFile(path)
		delete(w.fileTree, path)
		if tree.Resolved != nil {
			delete(tree.Resolved.Files, path)
			tree.Resolved.FileOrder = removeString(tree.Resolved.FileOrder, path)
		}
	}
}

func (w *Workspace) addMissingReachableToTreeLocked(tree *IncludeTree, reachable map[string]bool) bool {
	added := false
	for path := range reachable {
		// Already in this tree
		if w.fileTree[path] == tree.RootPath {
			continue
		}

		// File is in another tree — move it
		if oldRoot, ok := w.fileTree[path]; ok && oldRoot != tree.RootPath {
			oldTree := w.trees[oldRoot]
			if oldTree != nil {
				// Get journal from old tree to add to new tree
				var journal *ast.Journal
				if path == oldTree.RootPath && oldTree.Resolved != nil {
					journal = oldTree.Resolved.Primary
				} else if oldTree.Resolved != nil {
					journal = oldTree.Resolved.Files[path]
				}
				// Remove from old tree
				if oldTree.Resolved != nil {
					delete(oldTree.Resolved.Files, path)
					oldTree.Resolved.FileOrder = removeString(oldTree.Resolved.FileOrder, path)
					oldTree.clearCaches()
				}
				// If old tree was a standalone (only root, no files), remove it
				if oldRoot == path {
					delete(w.trees, oldRoot)
				}
				// Add to new tree
				if journal != nil {
					w.updateResolvedForTreeLocked(tree, path, journal)
				}
			}
			w.fileTree[path] = tree.RootPath
			added = true
			continue
		}

		// File not in any tree — load from disk
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fileIndex, journal, _ := BuildFileIndexFromContent(path, normalizeLineEndings(string(content)))
		w.index.SetFileIndex(path, fileIndex)
		w.updateIncludeEdgesLocked(path, nil, fileIndex.Includes)
		w.updateResolvedForTreeLocked(tree, path, journal)
		w.fileTree[path] = tree.RootPath
		added = true
	}
	if added {
		tree.clearCaches()
	}
	return added
}

func (w *Workspace) isWorkspaceFileLocked(path string) bool {
	if _, ok := w.fileTree[path]; ok {
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

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func removeString(values []string, target string) []string {
	if len(values) == 0 {
		return values
	}
	result := values[:0]
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
	rootPath := w.fileTree[path]
	tree := w.trees[rootPath]
	if tree == nil {
		w.mu.RUnlock()
		return nil
	}
	if tree.cachedFormats != nil {
		defer w.mu.RUnlock()
		return tree.cachedFormats
	}
	w.mu.RUnlock()

	w.mu.Lock()
	defer w.mu.Unlock()

	tree = w.trees[rootPath]
	if tree == nil {
		return nil
	}
	if tree.cachedFormats != nil {
		return tree.cachedFormats
	}
	if tree.Resolved == nil {
		return nil
	}

	tree.cachedFormats = formatter.ExtractCommodityFormats(tree.Resolved.FormatDirectives())
	return tree.cachedFormats
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
	rootPath := w.fileTree[path]
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
	rootPath := w.fileTree[path]
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
