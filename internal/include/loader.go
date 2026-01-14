package include

import (
	"fmt"
	"maps"
	"os"
	"sync"

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

		if visited[includePath] {
			errors = append(errors, LoadError{
				Kind:    ErrorCycleDetected,
				Path:    includePath,
				Message: fmt.Sprintf("cycle detected: %s includes %s", path, includePath),
				Range:   inc.Range,
			})
			continue
		}

		l.mu.RLock()
		cached, ok := l.cache[includePath]
		l.mu.RUnlock()
		if ok {
			result.Files[includePath] = cached
			continue
		}

		info, err := os.Stat(includePath)
		if err != nil {
			errors = append(errors, LoadError{
				Kind:    ErrorFileNotFound,
				Path:    includePath,
				Message: fmt.Sprintf("cannot read included file: %v", err),
				Range:   inc.Range,
			})
			continue
		}

		if info.Size() > MaxFileSize {
			errors = append(errors, LoadError{
				Kind:    ErrorFileTooLarge,
				Path:    includePath,
				Message: fmt.Sprintf("included file too large: %d bytes (max %d)", info.Size(), MaxFileSize),
				Range:   inc.Range,
			})
			continue
		}

		incContent, err := os.ReadFile(includePath)
		if err != nil {
			errors = append(errors, LoadError{
				Kind:    ErrorFileNotFound,
				Path:    includePath,
				Message: fmt.Sprintf("cannot read included file: %v", err),
				Range:   inc.Range,
			})
			continue
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
	}

	return result, errors
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
