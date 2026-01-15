package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/formatter"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/parser"
)

type Workspace struct {
	mu                sync.RWMutex
	rootURI           string
	rootJournalPath   string
	resolved          *include.ResolvedJournal
	includeGraph      map[string][]string
	reverseGraph      map[string][]string
	loader            *include.Loader
	loadErrors        []include.LoadError
	parseErrors       []string
	cachedFormats     map[string]formatter.NumberFormat
	cachedCommodities map[string]bool
	cachedAccounts    map[string]bool
	index             *WorkspaceIndex
}

func NewWorkspace(rootURI string, loader *include.Loader) *Workspace {
	return &Workspace{
		rootURI:      rootURI,
		loader:       loader,
		includeGraph: make(map[string][]string),
		reverseGraph: make(map[string][]string),
		index:        NewWorkspaceIndex(),
	}
}

func (w *Workspace) Initialize() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.loadErrors = nil
	w.parseErrors = nil
	w.cachedFormats = nil
	w.cachedCommodities = nil
	w.cachedAccounts = nil
	w.index = NewWorkspaceIndex()
	w.includeGraph = make(map[string][]string)
	w.reverseGraph = make(map[string][]string)

	rootPath, err := w.findRootJournal()
	if err != nil {
		return err
	}
	w.rootJournalPath = rootPath

	if rootPath != "" {
		resolved, errs := w.loader.Load(rootPath)
		w.resolved = resolved
		w.loadErrors = errs
		w.buildIndexFromResolvedLocked()
	}

	return nil
}

func (w *Workspace) LoadErrors() []include.LoadError {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.loadErrors
}

func (w *Workspace) ParseErrors() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.parseErrors
}

func expandTilde(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func (w *Workspace) findRootJournal() (string, error) {
	if envPath := os.Getenv("LEDGER_FILE"); envPath != "" {
		envPath = expandTilde(envPath)
		if _, err := os.Stat(envPath); err == nil {
			return envPath, nil
		}
	}
	if envPath := os.Getenv("HLEDGER_JOURNAL"); envPath != "" {
		envPath = expandTilde(envPath)
		if _, err := os.Stat(envPath); err == nil {
			return envPath, nil
		}
	}

	mainPath := filepath.Join(w.rootURI, "main.journal")
	if _, err := os.Stat(mainPath); err == nil {
		return mainPath, nil
	}

	hledgerPath := filepath.Join(w.rootURI, ".hledger.journal")
	if _, err := os.Stat(hledgerPath); err == nil {
		return hledgerPath, nil
	}

	return w.findRootByIncludeGraph()
}

func (w *Workspace) findRootByIncludeGraph() (string, error) {
	journalFiles, err := w.findJournalFiles()
	if err != nil {
		return "", err
	}

	if len(journalFiles) == 0 {
		return "", nil
	}

	w.buildIncludeGraph(journalFiles)

	var rootCandidates []string
	for _, file := range journalFiles {
		if len(w.reverseGraph[file]) == 0 {
			rootCandidates = append(rootCandidates, file)
		}
	}

	if len(rootCandidates) == 0 {
		return journalFiles[0], nil
	}

	sort.Strings(rootCandidates)
	return rootCandidates[0], nil
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
		ext := filepath.Ext(path)
		if ext == ".journal" || ext == ".j" || ext == ".hledger" || ext == ".ledger" {
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

		journal, errs := parser.Parse(string(content))
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

func (w *Workspace) RootJournalPath() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.rootJournalPath
}

func (w *Workspace) GetResolved() *include.ResolvedJournal {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.resolved
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
	if path == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.rootJournalPath == "" || w.index == nil {
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

	fileIndex, journal, _ := BuildFileIndexFromContent(path, content)
	w.index.SetFileIndex(path, fileIndex)
	w.updateIncludeEdgesLocked(path, oldIncludes, fileIndex.Includes)
	w.updateResolvedLocked(path, journal)
	w.clearCachesLocked()

	if !sameStringSlice(oldIncludes, fileIndex.Includes) {
		w.refreshIncludeTreeLocked()
	}
}

func (w *Workspace) buildIndexFromResolvedLocked() {
	if w.index == nil {
		w.index = NewWorkspaceIndex()
	}
	if w.resolved == nil || w.resolved.Primary == nil {
		return
	}

	w.index.SetFileIndex(w.rootJournalPath, BuildFileIndexFromJournal(w.rootJournalPath, w.resolved.Primary))
	w.updateIncludeEdgesLocked(w.rootJournalPath, nil, w.index.FileIndex(w.rootJournalPath).Includes)

	for path, journal := range w.resolved.Files {
		w.index.SetFileIndex(path, BuildFileIndexFromJournal(path, journal))
		w.updateIncludeEdgesLocked(path, nil, w.index.FileIndex(path).Includes)
	}
}

func (w *Workspace) updateResolvedLocked(path string, journal *ast.Journal) {
	if w.resolved == nil {
		w.resolved = include.NewResolvedJournal(nil)
	}
	if path == w.rootJournalPath {
		w.resolved.Primary = journal
		return
	}
	if journal == nil {
		delete(w.resolved.Files, path)
		return
	}
	w.resolved.Files[path] = journal
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

func (w *Workspace) refreshIncludeTreeLocked() {
	if w.rootJournalPath == "" || w.index == nil {
		return
	}

	for {
		reachable := w.computeReachableLocked()
		w.removeUnreachableLocked(reachable)
		added := w.addMissingReachableLocked(reachable)
		if !added {
			return
		}
	}
}

func (w *Workspace) computeReachableLocked() map[string]bool {
	reachable := make(map[string]bool)
	if w.rootJournalPath == "" {
		return reachable
	}
	queue := []string{w.rootJournalPath}
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

func (w *Workspace) removeUnreachableLocked(reachable map[string]bool) {
	var toRemove []string
	for path := range w.index.fileIndexes {
		if !reachable[path] {
			toRemove = append(toRemove, path)
		}
	}
	for _, path := range toRemove {
		oldIndex := w.index.FileIndex(path)
		if oldIndex != nil {
			w.updateIncludeEdgesLocked(path, oldIndex.Includes, nil)
		}
		w.index.RemoveFile(path)
		delete(w.includeGraph, path)
		delete(w.reverseGraph, path)
		if w.resolved != nil {
			delete(w.resolved.Files, path)
		}
	}
}

func (w *Workspace) addMissingReachableLocked(reachable map[string]bool) bool {
	added := false
	for path := range reachable {
		if w.index.FileIndex(path) != nil {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fileIndex, journal, _ := BuildFileIndexFromContent(path, string(content))
		w.index.SetFileIndex(path, fileIndex)
		w.updateIncludeEdgesLocked(path, nil, fileIndex.Includes)
		w.updateResolvedLocked(path, journal)
		added = true
	}
	if added {
		w.clearCachesLocked()
	}
	return added
}

func (w *Workspace) isWorkspaceFileLocked(path string) bool {
	if path == w.rootJournalPath {
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

func (w *Workspace) clearCachesLocked() {
	w.cachedFormats = nil
	w.cachedCommodities = nil
	w.cachedAccounts = nil
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

func (w *Workspace) GetCommodityFormats() map[string]formatter.NumberFormat {
	w.mu.RLock()
	if w.cachedFormats != nil {
		defer w.mu.RUnlock()
		return w.cachedFormats
	}
	w.mu.RUnlock()

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.cachedFormats != nil {
		return w.cachedFormats
	}

	if w.resolved == nil {
		return nil
	}

	formats := make(map[string]formatter.NumberFormat)
	for _, dir := range w.resolved.AllDirectives() {
		if cd, ok := dir.(ast.CommodityDirective); ok {
			if cd.Format != "" {
				formats[cd.Commodity.Symbol] = formatter.ParseNumberFormat(cd.Format)
			}
		}
	}

	w.cachedFormats = formats
	return formats
}

func (w *Workspace) GetDeclaredCommodities() map[string]bool {
	w.mu.RLock()
	if w.cachedCommodities != nil {
		defer w.mu.RUnlock()
		return w.cachedCommodities
	}
	w.mu.RUnlock()

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.cachedCommodities != nil {
		return w.cachedCommodities
	}

	if w.resolved == nil {
		return nil
	}

	declared := make(map[string]bool)
	for _, dir := range w.resolved.AllDirectives() {
		if cd, ok := dir.(ast.CommodityDirective); ok {
			declared[cd.Commodity.Symbol] = true
		}
	}
	w.cachedCommodities = declared
	return declared
}

func (w *Workspace) GetDeclaredAccounts() map[string]bool {
	w.mu.RLock()
	if w.cachedAccounts != nil {
		defer w.mu.RUnlock()
		return w.cachedAccounts
	}
	w.mu.RUnlock()

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.cachedAccounts != nil {
		return w.cachedAccounts
	}

	if w.resolved == nil {
		return nil
	}

	declared := make(map[string]bool)
	for _, dir := range w.resolved.AllDirectives() {
		if ad, ok := dir.(ast.AccountDirective); ok {
			declared[ad.Account.Name] = true
		}
	}
	w.cachedAccounts = declared
	return declared
}
