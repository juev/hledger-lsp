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

// BOM is the UTF-8 byte order mark that some editors write at the start of a
// file. hledger accepts it at the very start of a journal or rules file and
// nowhere else.
const BOM = "\uFEFF"

// StripBOM removes a leading byte order mark. Use it on text read from disk,
// where no client has counted the mark in its positions; text received from an
// editor keeps the mark so that columns on its first line match the client.
func StripBOM(s string) string {
	return strings.TrimPrefix(s, BOM)
}
