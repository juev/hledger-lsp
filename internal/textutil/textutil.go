// Package textutil holds text helpers shared by every layer that accepts
// journal or rules content, without depending on any of them.
package textutil

import "strings"

// NormalizeLineEndings converts CRLF and bare CR to LF.
//
// Every document the server keeps in memory is normalized on the way in, and
// the rest of the server relies on it: AST offsets, PositionMapper and the
// rope in internal/document all assume LF-only text, so a single unnormalized
// entry point would silently shift positions.
func NormalizeLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}
