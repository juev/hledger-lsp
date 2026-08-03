package include

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/filetype"
	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/juev/hledger-lsp/internal/textutil"
)

const (
	defaultMaxFileSizeBytes = 10 * 1024 * 1024
	defaultMaxIncludeDepth  = 50
)

type Limits struct {
	MaxFileSizeBytes int64
	MaxIncludeDepth  int
}

// Loader caches normalized source text. Parsed journals are occurrence-specific
// because parser context depends on the include site.
type Loader struct {
	mu     sync.RWMutex
	cache  map[string]string
	limits Limits
}

func NewLoader() *Loader {
	return &Loader{cache: make(map[string]string), limits: DefaultLimits()}
}

func DefaultLimits() Limits {
	return Limits{MaxFileSizeBytes: defaultMaxFileSizeBytes, MaxIncludeDepth: defaultMaxIncludeDepth}
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

func (l *Loader) SetLimits(limits Limits) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.limits = normalizeLimits(limits)
}

func (l *Loader) getLimits() Limits {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.limits
}

func (l *Loader) Load(path string) (*ResolvedJournal, []LoadError) {
	return l.LoadWithOptions(path, LoadOptions{})
}

func (l *Loader) LoadWithOptions(path string, options LoadOptions) (*ResolvedJournal, []LoadError) {
	path = absoluteClean(path)
	options = snapshotLoadOptions(options)
	content, err := l.readRoot(path, options)
	if err != nil {
		return nil, []LoadError{*err}
	}
	return l.load(path, content, options)
}

func (l *Loader) LoadFromContent(path, content string) (*ResolvedJournal, []LoadError) {
	path = absoluteClean(path)
	limits := l.getLimits()
	if int64(len(content)) > limits.MaxFileSizeBytes {
		return nil, []LoadError{{Kind: ErrorFileTooLarge, Path: path, SourcePath: path, Message: fmt.Sprintf("file too large: %d bytes (max %d)", len(content), limits.MaxFileSizeBytes)}}
	}
	return l.load(path, textutil.NormalizeLineEndings(content), LoadOptions{Overlays: map[string]OverlayEntry{
		canonicalPath(path): {SourcePath: path, Content: content},
	}})
}

// FileSizeError returns a file-too-large LoadError when content exceeds the
// configured MaxFileSizeBytes, or nil when it fits. It lets callers guard the
// analyze path before parsing.
func (l *Loader) FileSizeError(content string) *LoadError {
	limits := l.getLimits()
	if int64(len(content)) > limits.MaxFileSizeBytes {
		return &LoadError{Kind: ErrorFileTooLarge, Message: fmt.Sprintf("file too large: %d bytes (max %d)", len(content), limits.MaxFileSizeBytes)}
	}
	return nil
}

func (l *Loader) load(path, content string, options LoadOptions) (*ResolvedJournal, []LoadError) {
	result := NewResolvedJournal(nil)
	result.Items = make([]ResolvedItem, 0)
	state := loadState{loader: l, options: options, result: result, limits: l.getLimits(), children: make(map[OccurrenceID]map[int][]OccurrenceID)}
	state.loadOccurrence(path, canonicalPath(path), content, parser.Context{}, IncludeProvenance{SourcePath: path}, 0)
	result.Items = state.inlineItems(1)
	result.Errors = state.errors
	return result, state.errors
}

type loadState struct {
	loader   *Loader
	options  LoadOptions
	result   *ResolvedJournal
	limits   Limits
	errors   []LoadError
	active   []string
	children map[OccurrenceID]map[int][]OccurrenceID
}

func (s *loadState) loadOccurrence(path, canonical, content string, context parser.Context, via IncludeProvenance, depth int) parser.ContextExports {
	id := OccurrenceID(len(s.result.Occurrences) + 1)
	occurrence := Occurrence{ID: id, Path: path, CanonicalPath: canonical, InitialContext: context, Via: via}
	s.result.Occurrences = append(s.result.Occurrences, occurrence)
	s.result.ByPath[path] = append(s.result.ByPath[path], id)
	s.result.ByCanonical[canonical] = append(s.result.ByCanonical[canonical], id)
	if id > 1 {
		if _, exists := s.result.Files[path]; !exists {
			s.result.FileOrder = append(s.result.FileOrder, path)
		}
	}

	s.active = append(s.active, canonical)
	defer func() { s.active = s.active[:len(s.active)-1] }()

	children := make(map[int][]OccurrenceID)
	s.children[id] = children
	includeIndex := 0
	parseResult, parseErrs := parser.ParseWithContext(content, context, func(site parser.IncludeSite) parser.ContextExports {
		currentIndex := includeIndex
		includeIndex++
		return s.resolveInclude(id, path, site, depth, children, currentIndex)
	})
	for _, parseErr := range parseErrs {
		s.errors = append(s.errors, LoadError{
			Kind: ErrorParseError, Path: path, SourcePath: path, Message: parseErr.Message,
			Range:      ast.Range{Start: ast.Position{Line: parseErr.Pos.Line, Column: parseErr.Pos.Column, Offset: parseErr.Pos.Offset}, End: ast.Position{Line: parseErr.End.Line, Column: parseErr.End.Column, Offset: parseErr.End.Offset}},
			Provenance: via,
		})
	}

	occurrence.Journal = parseResult.Journal
	occurrence.Checkpoints = parseResult.Checkpoints
	for _, local := range parseResult.Items {
		item := Item{OccurrenceID: id, Index: local.Index}
		switch local.Kind {
		case parser.LocalItemTransaction:
			item.Kind = ItemTransaction
		case parser.LocalItemPeriodicTransaction:
			item.Kind = ItemPeriodicTransaction
		case parser.LocalItemAutoPostingRule:
			item.Kind = ItemAutoPostingRule
		case parser.LocalItemDirective:
			item.Kind = ItemDirective
		case parser.LocalItemComment:
			item.Kind = ItemComment
		case parser.LocalItemInclude:
			item.Kind = ItemInclude
		}
		occurrence.Items = append(occurrence.Items, item)
	}
	s.result.Occurrences[id-1] = occurrence
	if id == 1 {
		s.result.Primary = occurrence.Journal
	} else if _, exists := s.result.Files[path]; !exists {
		s.result.Files[path] = occurrence.Journal
	}
	return parseResult.Exports
}

func (s *loadState) inlineItems(id OccurrenceID) []ResolvedItem {
	occurrence := s.result.Occurrence(id)
	if occurrence == nil {
		return nil
	}
	localItems := occurrence.Items
	items := make([]ResolvedItem, 0, len(localItems))
	for _, item := range localItems {
		items = append(items, item)
		if item.Kind != ResolvedItemInclude {
			continue
		}
		for _, childID := range s.children[id][item.Index] {
			items = append(items, s.inlineItems(childID)...)
		}
	}
	occurrence.Items = items
	return items
}

func (s *loadState) resolveInclude(parentID OccurrenceID, basePath string, site parser.IncludeSite, depth int, children map[int][]OccurrenceID, includeIndex int) parser.ContextExports {
	provenance := IncludeProvenance{ParentID: parentID, SourcePath: basePath, IncludeRange: site.Include.Range, Range: site.Include.Range, RawPattern: site.Include.Path, Depth: depth + 1}
	matches, err := s.includePaths(basePath, site.Include.Path)
	if err != nil {
		kind := ErrorFileNotFound
		if strings.HasPrefix(err.Error(), "path traversal detected:") {
			kind = ErrorPathTraversal
		}
		s.errors = append(s.errors, LoadError{Kind: kind, Path: site.Include.Path, SourcePath: basePath, Message: err.Error(), Range: site.Include.Range, Provenance: provenance})
		return parser.ContextExports{}
	}
	var exports []parser.ContextExports
	// Each glob match inherits the declared commodity styles of earlier matches,
	// mirroring hledger's sequential processing of include matches. Other parse
	// state stays local to each match because exports carry commodity styles only.
	runningContext := site.Context
	for matchIndex, path := range matches {
		provenance.MatchIndex = matchIndex
		if !filetype.IsJournalPath(path) {
			s.errors = append(s.errors, LoadError{Kind: ErrorNotJournal, Path: path, SourcePath: basePath, Message: fmt.Sprintf("included file is not a journal file: %s", filepath.Base(path)), Range: site.Include.Range, Provenance: provenance})
			continue
		}
		canonical := canonicalPath(path)
		if canonical == s.active[len(s.active)-1] {
			continue
		}
		if contains(s.active, canonical) {
			s.errors = append(s.errors, LoadError{Kind: ErrorCycleDetected, Path: path, SourcePath: basePath, Message: fmt.Sprintf("cycle detected: %s includes %s", basePath, path), Range: site.Include.Range, Provenance: provenance})
			continue
		}
		if depth+1 > s.limits.MaxIncludeDepth {
			s.errors = append(s.errors, LoadError{Kind: ErrorDepthExceeded, Path: path, SourcePath: basePath, Message: fmt.Sprintf("include depth limit exceeded (%d)", s.limits.MaxIncludeDepth), Range: site.Include.Range, Provenance: provenance})
			continue
		}
		content, readErr := s.readIncluded(path)
		if readErr != nil {
			readErr.Range, readErr.Provenance = site.Include.Range, provenance
			readErr.SourcePath = basePath
			s.errors = append(s.errors, *readErr)
			continue
		}
		childID := OccurrenceID(len(s.result.Occurrences) + 1)
		children[includeIndex] = append(children[includeIndex], childID)
		matchExports := s.loadOccurrence(path, canonical, content, runningContext, provenance, depth+1)
		exports = append(exports, matchExports)
		runningContext = parser.ApplyContextExports(runningContext, matchExports)
	}
	return parser.MergeContextExports(exports...)
}

func (s *loadState) includePaths(basePath, pattern string) ([]string, error) {
	if IsGlobPattern(pattern) {
		return s.loader.expandGlob(basePath, pattern)
	}
	path, err := ResolvePathSafe(basePath, pattern)
	if err != nil {
		return nil, fmt.Errorf("path traversal detected: %s", pattern)
	}
	return []string{absoluteClean(path)}, nil
}

func (s *loadState) readIncluded(path string) (string, *LoadError) {
	path = absoluteClean(path)
	if content, ok := s.options.overlay(path); ok {
		if int64(len(content)) > s.limits.MaxFileSizeBytes {
			return "", &LoadError{Kind: ErrorFileTooLarge, Path: path, Message: fmt.Sprintf("included file too large: %d bytes (max %d)", len(content), s.limits.MaxFileSizeBytes)}
		}
		return textutil.NormalizeLineEndings(content), nil
	}
	canonical := canonicalPath(path)
	s.loader.mu.RLock()
	content, ok := s.loader.cache[canonical]
	s.loader.mu.RUnlock()
	if ok {
		return content, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", &LoadError{Kind: ErrorFileNotFound, Path: path, Message: fmt.Sprintf("cannot read included file: %v", err)}
	}
	if info.Size() > s.limits.MaxFileSizeBytes {
		return "", &LoadError{Kind: ErrorFileTooLarge, Path: path, Message: fmt.Sprintf("included file too large: %d bytes (max %d)", info.Size(), s.limits.MaxFileSizeBytes)}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", &LoadError{Kind: ErrorFileNotFound, Path: path, Message: fmt.Sprintf("cannot read included file: %v", err)}
	}
	content = textutil.NormalizeLineEndings(string(raw))
	s.loader.mu.Lock()
	s.loader.cache[canonical] = content
	s.loader.mu.Unlock()
	return content, nil
}

func (l *Loader) readRoot(path string, options LoadOptions) (string, *LoadError) {
	if content, ok := options.overlay(path); ok {
		if int64(len(content)) > l.getLimits().MaxFileSizeBytes {
			return "", &LoadError{Kind: ErrorFileTooLarge, Path: path, SourcePath: path, Message: fmt.Sprintf("file too large: %d bytes (max %d)", len(content), l.getLimits().MaxFileSizeBytes)}
		}
		return textutil.NormalizeLineEndings(content), nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", &LoadError{Kind: ErrorFileNotFound, Path: path, SourcePath: path, Message: fmt.Sprintf("cannot read file: %v", err)}
	}
	if info.Size() > l.getLimits().MaxFileSizeBytes {
		return "", &LoadError{Kind: ErrorFileTooLarge, Path: path, SourcePath: path, Message: fmt.Sprintf("file too large: %d bytes (max %d)", info.Size(), l.getLimits().MaxFileSizeBytes)}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", &LoadError{Kind: ErrorFileNotFound, Path: path, SourcePath: path, Message: fmt.Sprintf("cannot read file: %v", err)}
	}
	return textutil.NormalizeLineEndings(string(raw)), nil
}

func (l *Loader) expandGlob(basePath, pattern string) ([]string, error) {
	pattern = ConvertHledgerGlob(pattern)
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(filepath.Dir(basePath), pattern)
	}
	matches, err := doublestar.FilepathGlob(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid glob pattern: %w", err)
	}
	basePath = absoluteClean(basePath)
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		match = absoluteClean(match)
		if match != basePath {
			result = append(result, match)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no files match pattern: %s", pattern)
	}
	sort.Strings(result)
	return result, nil
}

func (l *Loader) ExpandGlob(basePath, pattern string) ([]string, error) {
	return l.expandGlob(basePath, pattern)
}

func (l *Loader) ClearCache() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cache = make(map[string]string)
}

func (l *Loader) InvalidateFile(path string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.cache, canonicalPath(path))
}

func absoluteClean(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func canonicalPath(path string) string {
	path = absoluteClean(path)
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return absoluteClean(canonical)
}

func snapshotLoadOptions(options LoadOptions) LoadOptions {
	if len(options.Overlays) == 0 {
		return LoadOptions{}
	}

	snapshot := LoadOptions{Overlays: make(map[string]OverlayEntry, len(options.Overlays))}
	for key, entry := range options.Overlays {
		canonical := canonicalPath(key)
		entry.SourcePath = absoluteClean(entry.SourcePath)
		current, exists := snapshot.Overlays[canonical]
		if !exists || entry.Revision > current.Revision || (entry.Revision == current.Revision && entry.SourcePath < current.SourcePath) {
			snapshot.Overlays[canonical] = entry
		}
	}
	return snapshot
}

func contains(paths []string, target string) bool {
	for _, path := range paths {
		if path == target {
			return true
		}
	}
	return false
}
