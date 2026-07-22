package rules

import "github.com/juev/hledger-lsp/internal/ast"

// ErrorKind classifies errors emitted by the rules-include Loader.
//
// The set mirrors the journal Loader's ErrorKind (internal/include/types.go)
// but is local to the rules package to avoid a cross-package dependency.
// ErrorNotRules replaces ErrorNotJournal since the rules loader refuses to
// include files that are not themselves .rules files. ErrorDepthExceeded is
// added at the end to preserve existing numeric values.
type ErrorKind int

const (
	ErrorFileNotFound ErrorKind = iota
	ErrorCycleDetected
	ErrorParseError
	ErrorReadError
	ErrorFileTooLarge
	ErrorPathTraversal
	ErrorNotRules
	ErrorDepthExceeded
)

// LoadError is a non-fatal problem encountered while resolving an
// include-chain of .rules files. Loader returns a slice of these alongside
// whatever partial result it managed to produce.
type LoadError struct {
	Kind       ErrorKind
	Path       string
	SourcePath string
	Message    string
	Range      ast.Range
}

func (e LoadError) Error() string {
	return e.Message
}

// OverlayEntry provides immutable editor content for a canonical path.
type OverlayEntry struct {
	SourcePath string
	Content    string
	Revision   uint64
}

// LoadOptions provides a per-load immutable editor-content snapshot.
type LoadOptions struct {
	Overlays map[string]OverlayEntry
}

// RuleOccurrenceID uniquely identifies a rules occurrence within one resolved rules.
type RuleOccurrenceID uint64

// RuleIncludeProvenance identifies the include site that created an occurrence.
type RuleIncludeProvenance struct {
	ParentID     RuleOccurrenceID
	SourcePath   string
	IncludeRange ast.Range
	RawPattern   string
	Depth        int
}

// RuleOccurrence is one inclusion of a rules source.
type RuleOccurrence struct {
	ID            RuleOccurrenceID
	Path          string
	CanonicalPath string
	Via           RuleIncludeProvenance
}

// SourceMapping maps a range in the expanded text to the original source.
type SourceMapping struct {
	ExpandedStart int
	ExpandedEnd   int
	OriginalPath  string
	OriginalStart int
	OriginalEnd   int
}

// ResolvedRules is the result of loading a .rules file with all its includes
// textually expanded and parsed.
//
// Occurrences tracks every inclusion of a source (root + included files),
// supporting repeated and diamond includes. ByPath and ByCanonical index
// occurrences for lookup. SourceMap maps expanded-text positions back to
// original file/line for diagnostic remapping. LineOffsets maps each original
// source path to a table of byte offsets where each line starts, enabling
// offset-to-line/column conversion for diagnostic remapping.
//
// Legacy fields (Primary, Files, FileOrder) are retained as a compile-only
// first-occurrence projection until all consumers migrate.
type ResolvedRules struct {
	Occurrences []RuleOccurrence
	ByPath      map[string][]RuleOccurrenceID
	ByCanonical map[string][]RuleOccurrenceID
	SourceMap   []SourceMapping
	LineOffsets map[string][]int // original path → line-start byte offsets
	Expanded    string

	// Legacy fields — compile-only projection for un-migrated consumers.
	Primary   *RulesFile
	Files     map[string]*RulesFile
	FileOrder []string
	Errors    []LoadError
}

// NewResolvedRules creates an empty ResolvedRules with the given primary.
func NewResolvedRules(primary *RulesFile) *ResolvedRules {
	return &ResolvedRules{
		Primary:     primary,
		Files:       make(map[string]*RulesFile),
		ByPath:      make(map[string][]RuleOccurrenceID),
		ByCanonical: make(map[string][]RuleOccurrenceID),
		LineOffsets: make(map[string][]int),
	}
}

// Occurrence returns the occurrence with the given ID, or nil.
func (r *ResolvedRules) Occurrence(id RuleOccurrenceID) *RuleOccurrence {
	for i := range r.Occurrences {
		if r.Occurrences[i].ID == id {
			return &r.Occurrences[i]
		}
	}
	return nil
}

// OccurrencesForPath returns all occurrences for an exact source path.
func (r *ResolvedRules) OccurrencesForPath(path string) []RuleOccurrence {
	return r.occurrencesForIDs(r.ByPath[path])
}

// OccurrencesForCanonical returns all occurrences for a canonical filesystem path.
func (r *ResolvedRules) OccurrencesForCanonical(path string) []RuleOccurrence {
	return r.occurrencesForIDs(r.ByCanonical[canonicalPath(path)])
}

func (r *ResolvedRules) occurrencesForIDs(ids []RuleOccurrenceID) []RuleOccurrence {
	if len(ids) == 0 {
		return nil
	}
	result := make([]RuleOccurrence, 0, len(ids))
	for _, id := range ids {
		if occ := r.Occurrence(id); occ != nil {
			result = append(result, *occ)
		}
	}
	return result
}
