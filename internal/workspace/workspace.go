package workspace

import (
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/formatter"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/parser"
)

type Workspace struct {
	mu              sync.RWMutex
	rootURI         string
	rootJournalPath string
	resolved        *include.ResolvedJournal
	includeGraph    map[string][]string
	reverseGraph    map[string][]string
	loader          *include.Loader
}

func NewWorkspace(rootURI string, loader *include.Loader) *Workspace {
	return &Workspace{
		rootURI:      rootURI,
		loader:       loader,
		includeGraph: make(map[string][]string),
		reverseGraph: make(map[string][]string),
	}
}

func (w *Workspace) Initialize() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	rootPath, err := w.findRootJournal()
	if err != nil {
		return err
	}
	w.rootJournalPath = rootPath

	if rootPath != "" {
		resolved, _ := w.loader.Load(rootPath)
		w.resolved = resolved
	}

	return nil
}

func (w *Workspace) findRootJournal() (string, error) {
	if envPath := os.Getenv("LEDGER_FILE"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath, nil
		}
	}
	if envPath := os.Getenv("HLEDGER_JOURNAL"); envPath != "" {
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

func (w *Workspace) findJournalFiles() ([]string, error) {
	var files []string
	err := filepath.Walk(w.rootURI, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // intentionally skip inaccessible files
		}
		if info.IsDir() {
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
			continue
		}

		journal, _ := parser.Parse(string(content))
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

func (w *Workspace) GetCommodityFormats() map[string]formatter.NumberFormat {
	w.mu.RLock()
	defer w.mu.RUnlock()

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

	return formats
}
