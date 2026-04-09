package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/filetype"
	"github.com/juev/hledger-lsp/internal/include"
)

// normalizeLineEndings converts \r\n and \r to \n.
// The rules lexer assumes \n-only input. Files read from disk on Windows may
// have CRLF. (Mirrors the copy in internal/include/loader.go; kept local to
// avoid a cross-package dependency for a trivial two-line helper.)
func normalizeLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

const (
	defaultMaxFileSizeBytes = 10 * 1024 * 1024
	defaultMaxIncludeDepth  = 50
)

// Limits bounds the rules loader to prevent runaway include chains from
// exhausting memory or stack space.
type Limits struct {
	MaxFileSizeBytes int64
	MaxIncludeDepth  int
}

// DefaultLimits returns sensible defaults: 10 MB per file, 50 includes deep.
func DefaultLimits() Limits {
	return Limits{
		MaxFileSizeBytes: defaultMaxFileSizeBytes,
		MaxIncludeDepth:  defaultMaxIncludeDepth,
	}
}

func normalizeLimits(limits Limits) Limits {
	if limits.MaxFileSizeBytes <= 0 {
		limits.MaxFileSizeBytes = defaultMaxFileSizeBytes
	}
	if limits.MaxIncludeDepth <= 0 {
		limits.MaxIncludeDepth = defaultMaxIncludeDepth
	}
	return limits
}

// ContentGetter lets callers override file-system reads for child includes.
// If it returns found=true, the returned content is parsed in place of
// os.ReadFile. The server uses this to prefer editor content (from the
// documents map) over the on-disk copy of an included .rules file that is
// also open in the editor.
//
// The primary file is never read through ContentGetter — it is either passed
// via LoadFromContent or read from disk by Load.
type ContentGetter func(path string) (content string, found bool, err error)

// Loader resolves the transitive closure of `include` directives in a
// .rules file. It mirrors the architecture of internal/include.Loader but
// targets *RulesFile instead of *ast.Journal.
//
// Loader is safe for concurrent use.
type Loader struct {
	mu         sync.RWMutex
	cache      map[string]*RulesFile
	limits     Limits
	getContent ContentGetter
}

// NewLoader returns a new Loader with default limits and no content getter.
func NewLoader() *Loader {
	return &Loader{
		cache:  make(map[string]*RulesFile),
		limits: DefaultLimits(),
	}
}

// SetLimits updates file size and depth limits. Non-positive values are
// replaced with defaults.
func (l *Loader) SetLimits(limits Limits) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.limits = normalizeLimits(limits)
}

// SetContentGetter installs a callback that loader consults before reading
// an included file from disk. Passing nil removes any previously installed
// getter. Safe to call concurrently with Load / LoadFromContent.
func (l *Loader) SetContentGetter(g ContentGetter) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.getContent = g
}

func (l *Loader) getLimits() Limits {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.limits
}

func (l *Loader) contentGetter() ContentGetter {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.getContent
}

// Load reads path from disk and resolves all transitive .rules includes.
// The returned *ResolvedRules has Primary set to the parsed root file and
// Files populated with every included .rules file reachable from it.
func (l *Loader) Load(path string) (*ResolvedRules, []LoadError) {
	limits := l.getLimits()

	info, err := os.Stat(path)
	if err != nil {
		return nil, []LoadError{{
			Kind:    ErrorFileNotFound,
			Path:    path,
			Message: fmt.Sprintf("cannot read file: %v", err),
		}}
	}

	if info.Size() > limits.MaxFileSizeBytes {
		return nil, []LoadError{{
			Kind:    ErrorFileTooLarge,
			Path:    path,
			Message: fmt.Sprintf("file too large: %d bytes (max %d)", info.Size(), limits.MaxFileSizeBytes),
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

	return l.loadWithContent(path, normalizeLineEndings(string(content)), make(map[string]bool))
}

// LoadFromContent resolves the transitive .rules include closure starting
// from the supplied content (treated as the primary file's body). Child
// includes are loaded from disk or via the ContentGetter, as usual.
//
// This entry point exists so the server can honour unsaved editor changes
// for the primary .rules file.
func (l *Loader) LoadFromContent(path, content string) (*ResolvedRules, []LoadError) {
	limits := l.getLimits()
	if int64(len(content)) > limits.MaxFileSizeBytes {
		return nil, []LoadError{{
			Kind:    ErrorFileTooLarge,
			Path:    path,
			Message: fmt.Sprintf("file too large: %d bytes (max %d)", len(content), limits.MaxFileSizeBytes),
		}}
	}
	return l.loadWithContent(path, content, make(map[string]bool))
}

func (l *Loader) loadWithContent(path, content string, visited map[string]bool) (*ResolvedRules, []LoadError) {
	var errors []LoadError
	limits := l.getLimits()

	if len(visited) >= limits.MaxIncludeDepth {
		return nil, []LoadError{{
			Kind:    ErrorCycleDetected,
			Path:    path,
			Message: fmt.Sprintf("include depth limit exceeded (%d)", limits.MaxIncludeDepth),
		}}
	}

	rf, parseDiags := Parse(content)
	for _, d := range parseDiags {
		errors = append(errors, LoadError{
			Kind:    ErrorParseError,
			Path:    path,
			Message: d.Message,
			Range:   d.Range,
		})
	}

	result := NewResolvedRules(rf)
	visited[path] = true

	for _, inc := range rf.Includes {
		includePath, pathErr := include.ResolvePathSafe(path, inc.Path)
		if pathErr != nil {
			errors = append(errors, LoadError{
				Kind:    ErrorPathTraversal,
				Path:    inc.Path,
				Message: fmt.Sprintf("path traversal detected: %s", inc.Path),
				Range:   inc.Range,
			})
			continue
		}

		subErrors := l.loadSingleInclude(includePath, inc.Range, visited, result)
		errors = append(errors, subErrors...)
	}

	return result, errors
}

func (l *Loader) loadSingleInclude(
	includePath string,
	incRange ast.Range,
	visited map[string]bool,
	result *ResolvedRules,
) []LoadError {
	var errors []LoadError
	limits := l.getLimits()

	if visited[includePath] {
		errors = append(errors, LoadError{
			Kind:    ErrorCycleDetected,
			Path:    includePath,
			Message: fmt.Sprintf("cycle detected: %s", includePath),
			Range:   incRange,
		})
		return errors
	}

	if !filetype.IsRules(includePath) {
		errors = append(errors, LoadError{
			Kind:    ErrorNotRules,
			Path:    includePath,
			Message: fmt.Sprintf("included file is not a rules file: %s", filepath.Base(includePath)),
			Range:   incRange,
		})
		return errors
	}

	// Cache lookup first.
	l.mu.RLock()
	cached, ok := l.cache[includePath]
	l.mu.RUnlock()
	if ok {
		result.Files[includePath] = cached
		result.FileOrder = append(result.FileOrder, includePath)
		// Still mark as visited so deeper cycles are detected.
		visited[includePath] = true
		return errors
	}

	// Resolve content: prefer the ContentGetter (open editor docs), fall back
	// to disk.
	var content string
	var gotFromGetter bool
	if getter := l.contentGetter(); getter != nil {
		c, ok, err := getter(includePath)
		if err != nil {
			errors = append(errors, LoadError{
				Kind:    ErrorReadError,
				Path:    includePath,
				Message: fmt.Sprintf("content getter failed: %v", err),
				Range:   incRange,
			})
			return errors
		}
		if ok {
			content = c
			gotFromGetter = true
		}
	}

	if !gotFromGetter {
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
		if info.Size() > limits.MaxFileSizeBytes {
			errors = append(errors, LoadError{
				Kind:    ErrorFileTooLarge,
				Path:    includePath,
				Message: fmt.Sprintf("included file too large: %d bytes (max %d)", info.Size(), limits.MaxFileSizeBytes),
				Range:   incRange,
			})
			return errors
		}
		raw, err := os.ReadFile(includePath)
		if err != nil {
			errors = append(errors, LoadError{
				Kind:    ErrorFileNotFound,
				Path:    includePath,
				Message: fmt.Sprintf("cannot read included file: %v", err),
				Range:   incRange,
			})
			return errors
		}
		content = string(raw)
	}

	subResult, subErrors := l.loadWithContent(includePath, normalizeLineEndings(content), visited)
	errors = append(errors, subErrors...)

	if subResult != nil && subResult.Primary != nil {
		l.mu.Lock()
		l.cache[includePath] = subResult.Primary
		l.mu.Unlock()
		result.Files[includePath] = subResult.Primary
		result.FileOrder = append(result.FileOrder, includePath)
		for _, p := range subResult.FileOrder {
			if _, exists := result.Files[p]; !exists {
				result.Files[p] = subResult.Files[p]
				result.FileOrder = append(result.FileOrder, p)
			}
		}
	}

	return errors
}

// ClearCache drops every cached parsed file.
func (l *Loader) ClearCache() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cache = make(map[string]*RulesFile)
}

// InvalidateFile removes the given path from the parsed-file cache so the
// next Load / LoadFromContent for it re-reads from disk. Safe to call for
// paths that are not in the cache.
func (l *Loader) InvalidateFile(path string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.cache, path)
}
