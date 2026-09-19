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
	// Data carries structured details for clients that build their own fixes,
	// mirroring the LSP Diagnostic.data payload.
	Data map[string]any
}

type AnalysisResult struct {
	Accounts       *AccountIndex
	Payees         []string
	Descriptions   []string
	Commodities    []string
	Tags           []string
	TagValues      map[string][]string
	Dates          []string
	PayeeTemplates map[string][]PostingTemplate
	Diagnostics    []Diagnostic

	AccountCounts     map[string]int
	PayeeCounts       map[string]int
	DescriptionCounts map[string]int
	CommodityCounts   map[string]int
	TagCounts         map[string]int

	PayeeAccounts         map[string][]string
	PayeeAccountPairUsage map[string]int
	PayeeAccountLastUsed  map[string]string
}

type PostingTemplate struct {
	Account       string
	Amount        string
	Commodity     string
	CommodityLeft bool
	// The remaining posting parts keep ghost text faithful to the transaction the
	// template came from: dropping them would write a different posting than the
	// user's history (a missing cost changes the recorded basis, a missing
	// assertion or comment loses data).
	Status    ast.Status
	Cost      string
	Assertion string
	Comment   string
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
	Balanced bool
	// Differences holds the absolute residual per commodity, keyed by the
	// commodity symbol ("" for commodity-less amounts).
	Differences map[string]decimal.Decimal
	// SignedDifferences holds the same residuals with their sign preserved, so
	// diagnostics can tell the user whether a commodity is over or short.
	SignedDifferences map[string]decimal.Decimal
	InferredIdx       int
}

func NewBalanceResult() *BalanceResult {
	return &BalanceResult{
		Balanced:          true,
		Differences:       make(map[string]decimal.Decimal),
		SignedDifferences: make(map[string]decimal.Decimal),
		InferredIdx:       -1,
	}
}

type ExternalDeclarations struct {
	Accounts    map[string]bool
	Commodities map[string]bool
}
