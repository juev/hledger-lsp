// Package document provides an incremental text buffer for LSP editing.
//
// Text is a rope of lines backed by an implicit treap: applying an LSP
// incremental change and accessing a line by index are O(log n) in the number
// of lines, instead of re-splitting the whole document on every keystroke (the
// pathology of building a full position mapper per edit). Materializing the full
// string via String is O(n) and cached until the next edit; the LSP server still
// needs the flat string for the string-based parser, so materialization happens
// once per edit, not per request.
//
// Text assumes LF-only input: NewText normalizes CRLF/CR to LF, matching the
// server's ingestion invariant.
package document

import (
	"math/rand"
	"strings"
	"sync"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/lsputil"
	"github.com/juev/hledger-lsp/internal/textutil"
)

// node is one line in an implicit treap ordered by line index. size is the
// number of lines in the subtree; it drives index-based split and select.
// bytes is the length of those lines plus one for the newline after each,
// which makes the byte offset of a line a subtree sum instead of a walk.
type node struct {
	line     string
	priority uint32
	size     int
	bytes    int
	left     *node
	right    *node
}

func newNode(line string) *node {
	return &node{line: line, priority: rand.Uint32(), size: 1, bytes: len(line) + 1}
}

func size(n *node) int {
	if n == nil {
		return 0
	}
	return n.size
}

func nodeBytes(n *node) int {
	if n == nil {
		return 0
	}
	return n.bytes
}

func update(n *node) {
	if n != nil {
		n.size = 1 + size(n.left) + size(n.right)
		n.bytes = len(n.line) + 1 + nodeBytes(n.left) + nodeBytes(n.right)
	}
}

// split partitions t into (a, b) where a holds the first k lines.
func split(t *node, k int) (*node, *node) {
	if t == nil {
		return nil, nil
	}
	if size(t.left) >= k {
		a, b := split(t.left, k)
		t.left = b
		update(t)
		return a, t
	}
	a, b := split(t.right, k-size(t.left)-1)
	t.right = a
	update(t)
	return t, b
}

// merge concatenates a and b, where every line of a precedes every line of b.
func merge(a, b *node) *node {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if a.priority > b.priority {
		a.right = merge(a.right, b)
		update(a)
		return a
	}
	b.left = merge(a, b.left)
	update(b)
	return b
}

// Text is an editable, line-indexed rope. The zero value is not usable; create
// one with NewText.
//
// A Text is safe for concurrent use. Callbacks passed to EachLine run under the
// read lock, so they must not call back into the same Text.
type Text struct {
	mu      sync.RWMutex
	root    *node
	cached  *string
	version uint64
}

// NewText builds a Text from content, normalizing CRLF/CR line endings to LF.
func NewText(content string) *Text {
	t := &Text{}
	t.Replace(content)
	return t
}

// Replace discards the current content and rebuilds from content, normalizing
// line endings. It backs full-document LSP changes.
func (t *Text) Replace(content string) {
	content = textutil.NormalizeLineEndings(content)

	t.mu.Lock()
	defer t.mu.Unlock()

	t.root = nil
	for _, line := range strings.Split(content, "\n") {
		t.root = merge(t.root, newNode(line))
	}
	t.cached = nil
	t.version++
}

// Version identifies the current content without materializing it, and must
// stay cheap: the workspace reads it on the keystroke path to record which
// revision an edit belongs to.
func (t *Text) Version() uint64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.version
}

// Materialize returns the current content. Together with Version it is what the
// workspace needs from a document it has been handed; read the content first
// and the version second, so a version that matches the recorded one proves the
// content was the recorded content.
func (t *Text) Materialize() string {
	return t.String()
}

// LineCount returns the number of lines.
func (t *Text) LineCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return size(t.root)
}

// Line returns the i-th line (without its trailing newline), or "" if i is out
// of range. O(log n).
func (t *Text) Line(i int) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.line(i)
}

func (t *Text) line(i int) string {
	if i < 0 {
		return ""
	}
	n := t.root
	for n != nil {
		leftSize := size(n.left)
		switch {
		case i < leftSize:
			n = n.left
		case i == leftSize:
			return n.line
		default:
			i -= leftSize + 1
			n = n.right
		}
	}
	return ""
}

// ByteOffsetOf returns the byte offset at which line starts, or the end of the
// document when line is LineCount() or beyond. O(log n).
func (t *Text) ByteOffsetOf(line int) int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.byteOffsetOf(line)
}

func (t *Text) byteOffsetOf(line int) int {
	if line <= 0 {
		return 0
	}
	n, base := t.root, 0
	for n != nil {
		leftSize := size(n.left)
		lineStart := base + nodeBytes(n.left)
		switch {
		case line < leftSize:
			n = n.left
		case line == leftSize:
			return lineStart
		default:
			line -= leftSize + 1
			base = lineStart + len(n.line) + 1
			n = n.right
		}
	}
	return t.byteLength()
}

// LineAtByte returns the line containing byte offset off and the offset within
// that line. An off past the end of the document resolves to the end of the
// last line. O(log n).
func (t *Text) LineAtByte(off int) (string, int) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	n, base := t.root, 0
	for n != nil {
		lineStart := base + nodeBytes(n.left)
		lineEnd := lineStart + len(n.line)
		switch {
		case off < lineStart:
			n = n.left
		case off <= lineEnd:
			return n.line, off - lineStart
		default:
			base = lineEnd + 1
			n = n.right
		}
	}
	last := t.line(size(t.root) - 1)
	return last, len(last)
}

// EachLine visits every line in order, without materializing the document or
// allocating a line slice. Iteration stops when f returns false.
func (t *Text) EachLine(f func(i int, line string) bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	i := 0
	var walk func(n *node) bool
	walk = func(n *node) bool {
		if n == nil {
			return true
		}
		if !walk(n.left) {
			return false
		}
		if !f(i, n.line) {
			return false
		}
		i++
		return walk(n.right)
	}
	walk(t.root)
}

// byteLength is the length of the materialized document: every line plus the
// newline that follows it, less the one that does not follow the last.
func (t *Text) byteLength() int {
	total := nodeBytes(t.root)
	if total == 0 {
		return 0
	}
	return total - 1
}

// String materializes the full document. O(n), cached until the next ApplyChange.
func (t *Text) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.string()
}

func (t *Text) string() string {
	if t.cached != nil {
		return *t.cached
	}
	var sb strings.Builder
	sb.Grow(t.byteLength())
	first := true
	var walk func(n *node)
	walk = func(n *node) {
		if n == nil {
			return
		}
		walk(n.left)
		if !first {
			sb.WriteByte('\n')
		}
		first = false
		sb.WriteString(n.line)
		walk(n.right)
	}
	walk(t.root)
	s := sb.String()
	t.cached = &s
	return s
}

// ApplyChange applies an LSP incremental edit in place. The replaced range is
// given in UTF-16 line/character coordinates; text may contain newlines. The
// edit itself is O(log n) in the line count plus O(len(text)); the cached
// materialized string is invalidated.
func (t *Text) ApplyChange(r protocol.Range, text string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.cached = nil
	t.version++
	lineCount := size(t.root)
	if lineCount == 0 {
		for _, line := range strings.Split(text, "\n") {
			t.root = merge(t.root, newNode(line))
		}
		return
	}

	// Resolve an endpoint to a (line, byteOffset) within the document, matching
	// lsputil.PositionMapper.LSPToByte: a line beyond the last maps to the end of
	// the document (end of the last line, character offset ignored) rather than
	// clamping into the last line, which would corrupt the buffer.
	resolve := func(line, char uint32) (int, int) {
		if int(line) >= lineCount {
			last := t.line(lineCount - 1)
			return lineCount - 1, len(last)
		}
		l := t.line(int(line))
		b := lsputil.UTF16OffsetToByteOffset(l, int(char))
		if b > len(l) {
			b = len(l)
		}
		return int(line), b
	}

	sl, scBytes := resolve(r.Start.Line, r.Start.Character)
	el, ecBytes := resolve(r.End.Line, r.End.Character)

	// Normalize an inverted range (start after end) to LSP splice semantics,
	// comparing absolute document order (line, then byte offset within the line).
	if sl > el || (sl == el && scBytes > ecBytes) {
		sl, el = el, sl
		scBytes, ecBytes = ecBytes, scBytes
	}

	startLine := t.line(sl)
	endLine := t.line(el)

	prefix := startLine[:scBytes]
	suffix := endLine[ecBytes:]
	newLines := strings.Split(prefix+text+suffix, "\n")

	// A = lines[0..sl-1], drop lines[sl..el], D = lines[el+1..].
	A, rest := split(t.root, sl)
	_, D := split(rest, el-sl+1)

	var M *node
	for _, line := range newLines {
		M = merge(M, newNode(line))
	}

	t.root = merge(A, merge(M, D))
}
