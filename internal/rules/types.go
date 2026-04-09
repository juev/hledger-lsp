package rules

import "github.com/juev/hledger-lsp/internal/ast"

// ErrorKind classifies errors emitted by the rules-include Loader.
//
// The set mirrors the journal Loader's ErrorKind (internal/include/types.go)
// but is local to the rules package to avoid a cross-package dependency.
// ErrorNotRules replaces ErrorNotJournal since the rules loader refuses to
// include files that are not themselves .rules files.
type ErrorKind int

const (
	ErrorFileNotFound ErrorKind = iota
	ErrorCycleDetected
	ErrorParseError
	ErrorReadError
	ErrorFileTooLarge
	ErrorPathTraversal
	ErrorNotRules
)

// LoadError is a non-fatal problem encountered while resolving an
// include-chain of .rules files. Loader returns a slice of these alongside
// whatever partial result it managed to produce.
type LoadError struct {
	Kind    ErrorKind
	Path    string
	Message string
	Range   ast.Range
}

func (e LoadError) Error() string {
	return e.Message
}

// ResolvedRules is the transitive closure of a primary .rules file with all
// its included .rules files, loaded from disk (or via a ContentGetter).
//
// Primary is the root file the loader was asked to load. Files holds the
// included files keyed by absolute path, and FileOrder lists them in the
// order they were encountered during depth-first traversal. Errors reports
// problems that did not prevent producing a (partial) result.
//
// The layout mirrors include.ResolvedJournal so that future refactoring can
// share helpers if the shape stabilises.
type ResolvedRules struct {
	Primary   *RulesFile
	Files     map[string]*RulesFile
	FileOrder []string
	Errors    []LoadError
}

// NewResolvedRules creates an empty ResolvedRules with the given primary.
func NewResolvedRules(primary *RulesFile) *ResolvedRules {
	return &ResolvedRules{
		Primary: primary,
		Files:   make(map[string]*RulesFile),
	}
}
