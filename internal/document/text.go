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

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/lsputil"
	"github.com/juev/hledger-lsp/internal/textutil"
)

// node is one line in an implicit treap ordered by line index. size is the
// number of lines in the subtree; it drives index-based split and select.
type node struct {
	line     string
	priority uint32
	size     int
	left     *node
	right    *node
}

func newNode(line string) *node {
	return &node{line: line, priority: rand.Uint32(), size: 1}
}

func size(n *node) int {
	if n == nil {
		return 0
	}
	return n.size
}

func update(n *node) {
	if n != nil {
		n.size = 1 + size(n.left) + size(n.right)
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
type Text struct {
	root   *node
	cached *string
}

// NewText builds a Text from content, normalizing CRLF/CR line endings to LF.
func NewText(content string) *Text {
	content = textutil.NormalizeLineEndings(content)
	t := &Text{}
	for _, line := range strings.Split(content, "\n") {
		t.root = merge(t.root, newNode(line))
	}
	return t
}

// LineCount returns the number of lines.
func (t *Text) LineCount() int {
	return size(t.root)
}

// Line returns the i-th line (without its trailing newline), or "" if i is out
// of range. O(log n).
func (t *Text) Line(i int) string {
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

// String materializes the full document. O(n), cached until the next ApplyChange.
func (t *Text) String() string {
	if t.cached != nil {
		return *t.cached
	}
	var sb strings.Builder
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
	t.cached = nil
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
			last := t.Line(lineCount - 1)
			return lineCount - 1, len(last)
		}
		l := t.Line(int(line))
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

	startLine := t.Line(sl)
	endLine := t.Line(el)

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
