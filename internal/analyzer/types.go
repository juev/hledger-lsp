package analyzer

import (
	"github.com/shopspring/decimal"

	"github.com/juev/hledger-lsp/internal/ast"
)

type DiagnosticSeverity int

const (
	SeverityError DiagnosticSeverity = iota
	SeverityWarning
	SeverityInfo
	SeverityHint
)

type Diagnostic struct {
	Range    ast.Range
	Severity DiagnosticSeverity
	Message  string
	Code     string
}

type AnalysisResult struct {
	Accounts    *AccountIndex
	Payees      []string
	Commodities []string
	Tags        []string
	Diagnostics []Diagnostic
}

type AccountIndex struct {
	All      []string
	ByPrefix map[string][]string
}

func NewAccountIndex() *AccountIndex {
	return &AccountIndex{
		All:      make([]string, 0),
		ByPrefix: make(map[string][]string),
	}
}

type BalanceResult struct {
	Balanced    bool
	Differences map[string]decimal.Decimal
	InferredIdx int
}

func NewBalanceResult() *BalanceResult {
	return &BalanceResult{
		Balanced:    true,
		Differences: make(map[string]decimal.Decimal),
		InferredIdx: -1,
	}
}

type ExternalDeclarations struct {
	Accounts    map[string]bool
	Commodities map[string]bool
}
