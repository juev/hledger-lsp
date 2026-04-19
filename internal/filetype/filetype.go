// Package filetype detects hledger file types from URI extensions.
package filetype

import (
	"path/filepath"
	"strings"
)

type FileType int

const (
	Unknown FileType = iota
	Journal
	Rules
)

func (f FileType) String() string {
	switch f {
	case Journal:
		return "journal"
	case Rules:
		return "rules"
	default:
		return "unknown"
	}
}

var journalExtensions = map[string]struct{}{
	".journal": {},
	".hledger": {},
	".j":       {},
	".ledger":  {},
	".prices":  {},
}

func isJournalExt(ext string) bool {
	_, ok := journalExtensions[strings.ToLower(ext)]
	return ok
}

// Detect returns the FileType for the given URI based on its file extension.
// The uri parameter is a file path or LSP file URI (file://...); query parameters are not present in LSP URIs.
func Detect(uri string) FileType {
	if uri == "" {
		return Unknown
	}
	ext := filepath.Ext(uri)
	switch {
	case isJournalExt(ext):
		return Journal
	case strings.HasSuffix(strings.ToLower(uri), ".rules"):
		return Rules
	default:
		return Unknown
	}
}

// IsJournal reports whether the URI refers to an hledger journal file.
func IsJournal(uri string) bool { return Detect(uri) == Journal }

// IsRules reports whether the URI refers to an hledger rules file.
func IsRules(uri string) bool { return Detect(uri) == Rules }

// IsJournalPath reports whether a filesystem path has a journal file extension.
func IsJournalPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return isJournalExt(ext)
}
