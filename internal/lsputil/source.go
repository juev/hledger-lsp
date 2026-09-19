package lsputil

import "strings"

// PositionSource is the line access a position lookup needs. It lives here
// rather than in internal/document so that lsputil stays free of that
// dependency; *document.Text satisfies it over a rope, giving O(log n) access
// to a single line, and StringSource adapts a flat string.
type PositionSource interface {
	// LineCount returns the number of lines in the document.
	LineCount() int
	// Line returns the i-th line without its terminator, or "" when out of range.
	Line(i int) string
}

// StringSource adapts a flat string to PositionSource. The split happens once,
// on construction, so callers that already hold a string pay the same cost as
// the strings.Split they used to do inline.
type StringSource struct {
	lines []string
}

func NewStringSource(content string) *StringSource {
	return &StringSource{lines: strings.Split(content, "\n")}
}

func (s *StringSource) LineCount() int { return len(s.lines) }

func (s *StringSource) Line(i int) string {
	if i < 0 || i >= len(s.lines) {
		return ""
	}
	return s.lines[i]
}
