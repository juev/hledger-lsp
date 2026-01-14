package include

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/parser"
)

const (
	MaxFileSize     = 10 * 1024 * 1024
	MaxIncludeDepth = 50
)

type Loader struct {
	mu    sync.RWMutex
	cache map[string]*ast.Journal
}

func NewLoader() *Loader {
	return &Loader{
		cache: make(map[string]*ast.Journal),
	}
}

func (l *Loader) Load(path string) (*ResolvedJournal, []LoadError) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, []LoadError{{
			Kind:    ErrorFileNotFound,
			Path:    path,
			Message: fmt.Sprintf("cannot read file: %v", err),
		}}
	}

	if info.Size() > MaxFileSize {
		return nil, []LoadError{{
			Kind:    ErrorFileTooLarge,
			Path:    path,
			Message: fmt.Sprintf("file too large: %d bytes (max %d)", info.Size(), MaxFileSize),
		}}
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, []LoadError{{
			Kind:    ErrorFileNotFound,
			Path:    path,
			Message: fmt.Sprintf("cannot read file: %v", err),
		}}
	}

	return l.loadWithContent(path, string(content), make(map[string]bool))
}

func (l *Loader) LoadFromContent(path, content string) (*ResolvedJournal, []LoadError) {
	return l.loadWithContent(path, content, make(map[string]bool))
}

func (l *Loader) loadWithContent(path, content string, visited map[string]bool) (*ResolvedJournal, []LoadError) {
	var errors []LoadError

	if len(visited) >= MaxIncludeDepth {
		return nil, []LoadError{{
			Kind:    ErrorCycleDetected,
			Path:    path,
			Message: fmt.Sprintf("include depth limit exceeded (%d)", MaxIncludeDepth),
		}}
	}

	journal, parseErrs := parser.Parse(content)
	for _, e := range parseErrs {
		pos := ast.Position{
			Line:   e.Pos.Line,
			Column: e.Pos.Column,
			Offset: e.Pos.Offset,
		}
		errors = append(errors, LoadError{
			Kind:    ErrorParseError,
			Path:    path,
			Message: e.Message,
			Range:   ast.Range{Start: pos, End: pos},
		})
	}

	result := NewResolvedJournal(journal)
	visited[path] = true

	for _, inc := range journal.Includes {
		if IsGlobPattern(inc.Path) {
			matches, err := l.expandGlob(path, inc.Path)
			if err != nil {
				errors = append(errors, LoadError{
					Kind:    ErrorFileNotFound,
					Path:    inc.Path,
					Message: err.Error(),
					Range:   inc.Range,
				})
				continue
			}

			for _, matchPath := range matches {
				subErrors := l.loadSingleInclude(path, matchPath, inc.Range, visited, result)
				errors = append(errors, subErrors...)
			}
			continue
		}

		includePath, pathErr := ResolvePathSafe(path, inc.Path)
		if pathErr != nil {
			errors = append(errors, LoadError{
				Kind:    ErrorPathTraversal,
				Path:    inc.Path,
				Message: fmt.Sprintf("path traversal detected: %s", inc.Path),
				Range:   inc.Range,
			})
			continue
		}

		subErrors := l.loadSingleInclude(path, includePath, inc.Range, visited, result)
		errors = append(errors, subErrors...)
	}

	return result, errors
}

func (l *Loader) loadSingleInclude(
	basePath, includePath string,
	incRange ast.Range,
	visited map[string]bool,
	result *ResolvedJournal,
) []LoadError {
	var errors []LoadError

	if visited[includePath] {
		errors = append(errors, LoadError{
			Kind:    ErrorCycleDetected,
			Path:    includePath,
			Message: fmt.Sprintf("cycle detected: %s includes %s", basePath, includePath),
			Range:   incRange,
		})
		return errors
	}

	l.mu.RLock()
	cached, ok := l.cache[includePath]
	l.mu.RUnlock()
	if ok {
		result.Files[includePath] = cached
		return errors
	}

	info, err := os.Stat(includePath)
	if err != nil {
		errors = append(errors, LoadError{
			Kind:    ErrorFileNotFound,
			Path:    includePath,
			Message: fmt.Sprintf("cannot read included file: %v", err),
			Range:   incRange,
		})
		return errors
	}

	if info.Size() > MaxFileSize {
		errors = append(errors, LoadError{
			Kind:    ErrorFileTooLarge,
			Path:    includePath,
			Message: fmt.Sprintf("included file too large: %d bytes (max %d)", info.Size(), MaxFileSize),
			Range:   incRange,
		})
		return errors
	}

	incContent, err := os.ReadFile(includePath)
	if err != nil {
		errors = append(errors, LoadError{
			Kind:    ErrorFileNotFound,
			Path:    includePath,
			Message: fmt.Sprintf("cannot read included file: %v", err),
			Range:   incRange,
		})
		return errors
	}

	subResult, subErrors := l.loadWithContent(includePath, string(incContent), visited)
	errors = append(errors, subErrors...)

	if subResult != nil && subResult.Primary != nil {
		l.mu.Lock()
		l.cache[includePath] = subResult.Primary
		l.mu.Unlock()
		result.Files[includePath] = subResult.Primary
		maps.Copy(result.Files, subResult.Files)
	}

	return errors
}

func (l *Loader) expandGlob(basePath, pattern string) ([]string, error) {
	dir := filepath.Dir(basePath)

	pattern = ConvertHledgerGlob(pattern)

	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(dir, pattern)
	}

	allMatches, err := doublestar.FilepathGlob(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid glob pattern: %w", err)
	}

	absBasePath, _ := filepath.Abs(basePath)
	var matches []string
	for _, m := range allMatches {
		absM, _ := filepath.Abs(m)
		if absM != absBasePath {
			matches = append(matches, m)
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no files match pattern: %s", pattern)
	}

	sort.Strings(matches)
	return matches, nil
}

func (l *Loader) ClearCache() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cache = make(map[string]*ast.Journal)
}

func (l *Loader) InvalidateFile(path string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.cache, path)
}
