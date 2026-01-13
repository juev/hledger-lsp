package include

import (
	"fmt"
	"maps"
	"os"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/parser"
)

type Loader struct {
	cache map[string]*ast.Journal
}

func NewLoader() *Loader {
	return &Loader{
		cache: make(map[string]*ast.Journal),
	}
}

func (l *Loader) Load(path string) (*ResolvedJournal, []LoadError) {
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
		includePath := ResolvePath(path, inc.Path)

		if visited[includePath] {
			errors = append(errors, LoadError{
				Kind:    ErrorCycleDetected,
				Path:    includePath,
				Message: fmt.Sprintf("cycle detected: %s includes %s", path, includePath),
				Range:   inc.Range,
			})
			continue
		}

		if cached, ok := l.cache[includePath]; ok {
			result.Files[includePath] = cached
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
			l.cache[includePath] = subResult.Primary
			result.Files[includePath] = subResult.Primary
			maps.Copy(result.Files, subResult.Files)
		}
	}

	return result, errors
}

func (l *Loader) ClearCache() {
	l.cache = make(map[string]*ast.Journal)
}

func (l *Loader) InvalidateFile(path string) {
	delete(l.cache, path)
}
