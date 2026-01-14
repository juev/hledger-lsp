package include

import "github.com/juev/hledger-lsp/internal/ast"

type ErrorKind int

const (
	ErrorFileNotFound ErrorKind = iota
	ErrorCycleDetected
	ErrorParseError
	ErrorReadError
	ErrorFileTooLarge
	ErrorPathTraversal
)

type LoadError struct {
	Kind    ErrorKind
	Path    string
	Message string
	Range   ast.Range
}

func (e LoadError) Error() string {
	return e.Message
}

type FileSource struct {
	Path    string
	Content string
}

type ResolvedJournal struct {
	Primary *ast.Journal
	Files   map[string]*ast.Journal
	Errors  []LoadError
}

func NewResolvedJournal(primary *ast.Journal) *ResolvedJournal {
	return &ResolvedJournal{
		Primary: primary,
		Files:   make(map[string]*ast.Journal),
	}
}

func (r *ResolvedJournal) AllTransactions() []ast.Transaction {
	var result []ast.Transaction
	if r.Primary != nil {
		result = append(result, r.Primary.Transactions...)
	}
	for _, j := range r.Files {
		result = append(result, j.Transactions...)
	}
	return result
}

func (r *ResolvedJournal) AllDirectives() []ast.Directive {
	var result []ast.Directive
	if r.Primary != nil {
		result = append(result, r.Primary.Directives...)
	}
	for _, j := range r.Files {
		result = append(result, j.Directives...)
	}
	return result
}

func (r *ResolvedJournal) AllIncludes() []ast.Include {
	var result []ast.Include
	if r.Primary != nil {
		result = append(result, r.Primary.Includes...)
	}
	for _, j := range r.Files {
		result = append(result, j.Includes...)
	}
	return result
}
