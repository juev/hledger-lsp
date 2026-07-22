package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/juev/hledger-lsp/internal/ast"
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
// Deprecated: Use LoadOptions with Overlays instead.
type ContentGetter func(path string) (content string, found bool, err error)

// Loader resolves the transitive closure of `include` directives in a
// .rules file by textually expanding includes before parsing, mirroring
// upstream hledger behavior.
//
// Loader is safe for concurrent use.
type Loader struct {
	mu         sync.RWMutex
	cache      map[string]string
	limits     Limits
	getContent ContentGetter
	nextID     RuleOccurrenceID
}

// NewLoader returns a new Loader with default limits and no content getter.
func NewLoader() *Loader {
	return &Loader{
		cache:  make(map[string]string),
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
// an included file from disk. Deprecated: use LoadWithOptions with Overlays.
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

// Load reads path from disk and resolves all transitive includes by textual
// expansion before parsing.
func (l *Loader) Load(path string) (*ResolvedRules, []LoadError) {
	return l.LoadWithOptions(path, LoadOptions{})
}

// LoadFromContent resolves includes starting from the supplied content for
// the root file. Child includes are loaded from disk or overlays.
func (l *Loader) LoadFromContent(path, content string) (*ResolvedRules, []LoadError) {
	path = absoluteClean(path)
	return l.LoadWithOptions(path, LoadOptions{
		Overlays: map[string]OverlayEntry{
			canonicalPath(path): {
				SourcePath: path,
				Content:    content,
				Revision:   1,
			},
		},
	})
}

// LoadWithOptions resolves includes using an immutable per-load overlay snapshot.
func (l *Loader) LoadWithOptions(path string, options LoadOptions) (*ResolvedRules, []LoadError) {
	limits := l.getLimits()
	path = absoluteClean(path)
	rootCanonical := canonicalPath(path)

	result := &ResolvedRules{
		Files:       make(map[string]*RulesFile),
		ByPath:      make(map[string][]RuleOccurrenceID),
		ByCanonical: make(map[string][]RuleOccurrenceID),
		LineOffsets: make(map[string][]int),
	}

	// Snapshot overlays by canonical path.
	snapshot := snapshotOverlays(options)

	// Read root content.
	rootContent, rootErr := l.readContent(path, rootCanonical, snapshot, limits)
	if rootErr != nil {
		return nil, []LoadError{*rootErr}
	}
	rootContent = normalizeLineEndings(rootContent)

	// Expand textually.
	expander := &textExpander{
		loader:    l,
		limits:    limits,
		snapshot:  snapshot,
		result:    result,
		active:    make(map[string]bool),
		sourceMap: nil,
	}

	expanded, errors := expander.expand(path, rootCanonical, rootContent, 0, 0)
	result.Expanded = expanded
	result.SourceMap = expander.sourceMap
	result.Errors = errors

	// Parse the expanded text.
	rf, parseDiags := Parse(expanded)
	result.Primary = rf

	// Remap parse diagnostics to original sources.
	for _, d := range parseDiags {
		mapped := RemapRange(d.Range.Start.Offset, d.Range.End.Offset, result.SourceMap, result.LineOffsets)
		errors = append(errors, LoadError{
			Kind:       ErrorParseError,
			Path:       mapped.Path,
			SourcePath: mapped.Path,
			Message:    d.Message,
			Range:      mapped.Rng,
		})
	}

	// Build legacy projection.
	result.Primary = rf
	l.buildLegacyProjection(result)

	return result, errors
}

// ClearCache drops every cached raw content entry.
func (l *Loader) ClearCache() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cache = make(map[string]string)
}

// InvalidateFile removes the given path from the raw-content cache so the
// next load re-reads from disk.
func (l *Loader) InvalidateFile(path string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.cache, canonicalPath(path))
}

func (l *Loader) allocID() RuleOccurrenceID {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.nextID++
	return l.nextID
}

func (l *Loader) readContent(path, canonical string, snapshot map[string]OverlayEntry, limits Limits) (string, *LoadError) {
	// Check overlay first.
	if entry, ok := snapshot[canonical]; ok {
		return entry.Content, nil
	}

	// Check cache.
	l.mu.RLock()
	cached, ok := l.cache[canonical]
	l.mu.RUnlock()
	if ok {
		return cached, nil
	}

	// Check legacy ContentGetter.
	if getter := l.contentGetter(); getter != nil {
		c, found, err := getter(path)
		if err != nil {
			return "", &LoadError{
				Kind:    ErrorReadError,
				Path:    path,
				Message: fmt.Sprintf("content getter failed: %v", err),
			}
		}
		if found {
			// Populate cache.
			l.mu.Lock()
			l.cache[canonical] = c
			l.mu.Unlock()
			return c, nil
		}
	}

	// Read from disk.
	info, err := os.Stat(path)
	if err != nil {
		return "", &LoadError{
			Kind:    ErrorFileNotFound,
			Path:    path,
			Message: fmt.Sprintf("cannot read file: %v", err),
		}
	}
	if info.Size() > limits.MaxFileSizeBytes {
		return "", &LoadError{
			Kind:    ErrorFileTooLarge,
			Path:    path,
			Message: fmt.Sprintf("file too large: %d bytes (max %d)", info.Size(), limits.MaxFileSizeBytes),
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", &LoadError{
			Kind:    ErrorFileNotFound,
			Path:    path,
			Message: fmt.Sprintf("cannot read file: %v", err),
		}
	}
	content := string(raw)

	// Populate cache.
	l.mu.Lock()
	l.cache[canonical] = content
	l.mu.Unlock()

	return content, nil
}

// buildLegacyProjection populates the legacy Primary/Files/FileOrder from
// the parsed expanded text. Since rules are textually expanded, the primary
// contains everything; Files/FileOrder are populated from occurrences for
// backward compatibility.
func (l *Loader) buildLegacyProjection(result *ResolvedRules) {
	if result.Primary == nil {
		return
	}
	seen := make(map[string]bool)
	for _, occ := range result.Occurrences {
		if occ.Via.ParentID == 0 {
			continue // skip root
		}
		if seen[occ.Path] {
			continue
		}
		seen[occ.Path] = true
		// For legacy consumers, parse the included file standalone.
		rf, _ := Parse(occ.Via.RawPattern) // RawPattern stores the raw content
		result.Files[occ.Path] = rf
		result.FileOrder = append(result.FileOrder, occ.Path)
	}
}

// textExpander recursively expands include directives in rules text.
type textExpander struct {
	loader    *Loader
	limits    Limits
	snapshot  map[string]OverlayEntry
	result    *ResolvedRules
	active    map[string]bool // canonical ancestor stack for cycle detection
	sourceMap []SourceMapping
}

func (e *textExpander) expand(
	path, canonical, content string,
	depth int,
	parentID RuleOccurrenceID,
) (string, []LoadError) {
	var errors []LoadError

	// Build line-offset table for this source file.
	e.result.LineOffsets[path] = buildLineOffsets(content)

	// Record occurrence.
	id := e.loader.allocID()
	occ := RuleOccurrence{
		ID:            id,
		Path:          path,
		CanonicalPath: canonical,
		Via: RuleIncludeProvenance{
			ParentID: parentID,
			Depth:    depth,
		},
	}
	e.result.Occurrences = append(e.result.Occurrences, occ)
	e.result.ByPath[path] = append(e.result.ByPath[path], id)
	e.result.ByCanonical[canonical] = append(e.result.ByCanonical[canonical], id)

	// Push to active stack.
	e.active[canonical] = true
	defer func() { delete(e.active, canonical) }()

	lines := strings.Split(content, "\n")
	var out strings.Builder
	var currentOffset int

	for lineIdx, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "include ") && trimmed != "include" {
			startOffset := currentOffset
			out.WriteString(line)
			if lineIdx < len(lines)-1 {
				out.WriteString("\n")
			}
			e.sourceMap = append(e.sourceMap, SourceMapping{
				ExpandedStart: startOffset,
				ExpandedEnd:   startOffset + len(line),
				OriginalPath:  path,
				OriginalStart: lineStartOffset(content, lineIdx),
				OriginalEnd:   lineStartOffset(content, lineIdx) + len(line),
			})
			currentOffset = startOffset + len(line)
			if lineIdx < len(lines)-1 {
				currentOffset++ // newline
			}
			continue
		}

		// Parse include directive.
		incPath := strings.TrimPrefix(trimmed, "include ")
		if incPath == "" {
			// Bare "include" with no path — pass through.
			out.WriteString(line)
			if lineIdx < len(lines)-1 {
				out.WriteString("\n")
			}
			currentOffset += len(line)
			if lineIdx < len(lines)-1 {
				currentOffset++
			}
			continue
		}

		// Resolve the include path.
		resolved, pathErr := include.ResolvePathSafe(path, incPath)
		if pathErr != nil {
			errors = append(errors, LoadError{
				Kind:    ErrorPathTraversal,
				Path:    incPath,
				Message: fmt.Sprintf("path traversal detected: %s", incPath),
				Range:   lineRange(lineIdx, line),
			})
			// Still emit the line as-is.
			out.WriteString(line)
			if lineIdx < len(lines)-1 {
				out.WriteString("\n")
			}
			currentOffset += len(line)
			if lineIdx < len(lines)-1 {
				currentOffset++
			}
			continue
		}

		childCanonical := canonicalPath(resolved)

		// Self-include: silently ignore.
		if childCanonical == canonical {
			continue
		}

		// Active ancestor check: cycle detection.
		if e.active[childCanonical] {
			errors = append(errors, LoadError{
				Kind:       ErrorCycleDetected,
				Path:       resolved,
				SourcePath: resolved,
				Message:    fmt.Sprintf("cycle detected: %s", resolved),
				Range:      lineRange(lineIdx, line),
			})
			continue
		}

		// Depth check (edges from root; root = 0).
		childDepth := depth + 1
		if childDepth > e.limits.MaxIncludeDepth {
			errors = append(errors, LoadError{
				Kind:       ErrorDepthExceeded,
				Path:       resolved,
				SourcePath: resolved,
				Message:    fmt.Sprintf("include depth limit exceeded (%d)", e.limits.MaxIncludeDepth),
				Range:      lineRange(lineIdx, line),
			})
			continue
		}

		// Read child content.
		childContent, readErr := e.loader.readContent(resolved, childCanonical, e.snapshot, e.limits)
		if readErr != nil {
			readErr.SourcePath = resolved
			readErr.Range = lineRange(lineIdx, line)
			errors = append(errors, *readErr)
			continue
		}
		childContent = normalizeLineEndings(childContent)

		// Recursively expand child.
		expandedStart := out.Len()
		expanded, childErrors := e.expand(resolved, childCanonical, childContent, childDepth, id)
		errors = append(errors, childErrors...)
		out.WriteString(expanded)

		// Record source mapping for the entire expanded child block.
		e.sourceMap = append(e.sourceMap, SourceMapping{
			ExpandedStart: expandedStart,
			ExpandedEnd:   out.Len(),
			OriginalPath:  resolved,
			OriginalStart: 0,
			OriginalEnd:   len(childContent),
		})

		// Update the child occurrence provenance with include site info.
		if len(e.result.Occurrences) > 0 {
			// Find the child occurrence (it was just added by expand).
			for i := len(e.result.Occurrences) - 1; i >= 0; i-- {
				if e.result.Occurrences[i].Path == resolved && e.result.Occurrences[i].Via.ParentID == id {
					e.result.Occurrences[i].Via.SourcePath = path
					e.result.Occurrences[i].Via.IncludeRange = lineRange(lineIdx, line)
					e.result.Occurrences[i].Via.RawPattern = childContent
					break
				}
			}
		}

		currentOffset = out.Len()
	}

	return out.String(), errors
}

func snapshotOverlays(options LoadOptions) map[string]OverlayEntry {
	if len(options.Overlays) == 0 {
		return nil
	}
	snapshot := make(map[string]OverlayEntry, len(options.Overlays))
	for key, entry := range options.Overlays {
		canonical := canonicalPath(key)
		entry.SourcePath = absoluteClean(entry.SourcePath)
		current, exists := snapshot[canonical]
		if !exists || entry.Revision > current.Revision || (entry.Revision == current.Revision && entry.SourcePath < current.SourcePath) {
			snapshot[canonical] = entry
		}
	}
	return snapshot
}

func lineRange(lineIdx int, line string) ast.Range {
	return ast.Range{
		Start: ast.Position{Line: lineIdx + 1, Column: 1, Offset: 0},
		End:   ast.Position{Line: lineIdx + 1, Column: len(line) + 1, Offset: len(line)},
	}
}

func lineStartOffset(content string, lineIdx int) int {
	offset := 0
	for i := 0; i < lineIdx; i++ {
		idx := strings.Index(content[offset:], "\n")
		if idx < 0 {
			return len(content)
		}
		offset += idx + 1
	}
	return offset
}

// RemappedRange maps a diagnostic range to an original source file.
type RemappedRange struct {
	Path string
	Rng  ast.Range
}

// RemapRange maps expanded-text offsets to the original source file using the source map
// and line-offset tables for accurate line/column computation.
func RemapRange(startOffset, endOffset int, sourceMap []SourceMapping, lineOffsets map[string][]int) RemappedRange {
	// Find the most specific source mapping containing startOffset.
	best := -1
	for i, sm := range sourceMap {
		if sm.ExpandedStart <= startOffset && startOffset < sm.ExpandedEnd {
			if best < 0 || (sm.ExpandedEnd-sm.ExpandedStart) < (sourceMap[best].ExpandedEnd-sourceMap[best].ExpandedStart) {
				best = i
			}
		}
	}
	if best < 0 {
		return RemappedRange{Path: "", Rng: ast.Range{}}
	}
	sm := sourceMap[best]
	localStart := sm.OriginalStart + (startOffset - sm.ExpandedStart)
	localEnd := sm.OriginalStart + (endOffset - sm.ExpandedStart)
	if localEnd > sm.OriginalEnd {
		localEnd = sm.OriginalEnd
	}
	lo := lineOffsets[sm.OriginalPath]
	return RemappedRange{
		Path: sm.OriginalPath,
		Rng: ast.Range{
			Start: offsetToPosition(localStart, lo),
			End:   offsetToPosition(localEnd, lo),
		},
	}
}

// buildLineOffsets returns a table where entry i is the byte offset of the
// start of line i (0-indexed). Used for offset-to-line/column conversion.
func buildLineOffsets(content string) []int {
	offsets := []int{0}
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	return offsets
}

// offsetToPosition converts a byte offset to line/column using a line-offset table.
// lineOffsets[i] is the byte offset where line i+1 starts (1-indexed lines).
func offsetToPosition(offset int, lineOffsets []int) ast.Position {
	if len(lineOffsets) == 0 {
		return ast.Position{Line: 1, Column: offset + 1, Offset: offset}
	}
	line := 1
	for i := len(lineOffsets) - 1; i >= 0; i-- {
		if lineOffsets[i] <= offset {
			line = i + 1
			break
		}
	}
	col := offset - lineOffsets[line-1] + 1
	return ast.Position{Line: line, Column: col, Offset: offset}
}

func absoluteClean(path string) string {
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return filepath.Clean(path)
		}
		return abs
	}
	return filepath.Clean(path)
}

func canonicalPath(path string) string {
	path = absoluteClean(path)
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return absoluteClean(canonical)
}
