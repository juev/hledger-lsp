// Package csvimport turns CSV, SSV and TSV data included from a journal into
// transactions by running the configured hledger binary on it. hledger's own
// CSV rules engine does the conversion; this package only decides whether a run
// is safe, runs it with bounded resources, and reads the result back into AST
// transactions with exact quantities.
package csvimport

import (
	"path/filepath"
	"strings"
)

// Reader names the hledger reader that converts an included file.
type Reader string

const (
	ReaderCSV Reader = "csv"
	ReaderSSV Reader = "ssv"
	ReaderTSV Reader = "tsv"
	// ReaderRules reads a rules file whose `source` rule names the data. hledger
	// selects it by the .rules extension only: a `rules:` prefix is not a reader
	// prefix, neither in -f nor in include (checked on 1.99.5).
	ReaderRules Reader = "rules"
)

// SplitReader separates an explicit reader prefix from an include argument:
// "csv:bank.txt" reads bank.txt as CSV whatever its extension. Only the
// prefixes hledger accepts for separated values are recognised; anything else
// is returned unchanged.
func SplitReader(argument string) (Reader, string) {
	prefix, rest, ok := strings.Cut(argument, ":")
	if !ok || rest == "" {
		return "", argument
	}
	switch Reader(prefix) {
	case ReaderCSV, ReaderSSV, ReaderTSV:
		return Reader(prefix), rest
	}
	return "", argument
}

// ReaderForPath returns the reader hledger picks for path by its extension, or
// "" for a file that is not separated data or rules.
func ReaderForPath(path string) Reader {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv":
		return ReaderCSV
	case ".ssv":
		return ReaderSSV
	case ".tsv":
		return ReaderTSV
	case ".rules":
		return ReaderRules
	}
	return ""
}

// IsDataPath reports whether path has a separated-values extension. Such a file
// is never a journal, whether or not anything includes it.
func IsDataPath(path string) bool {
	switch ReaderForPath(path) {
	case ReaderCSV, ReaderSSV, ReaderTSV:
		return true
	}
	return false
}
