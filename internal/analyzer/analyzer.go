package analyzer

import (
	"fmt"

	"github.com/juev/hledger-lsp/internal/ast"
)

type Analyzer struct{}

func New() *Analyzer {
	return &Analyzer{}
}

func (a *Analyzer) Analyze(journal *ast.Journal) *AnalysisResult {
	result := &AnalysisResult{
		Accounts:    CollectAccounts(journal),
		Payees:      CollectPayees(journal),
		Commodities: CollectCommodities(journal),
		Tags:        CollectTags(journal),
		Diagnostics: make([]Diagnostic, 0),
	}

	declaredAccounts := collectDeclaredAccounts(journal)

	for i := range journal.Transactions {
		tx := &journal.Transactions[i]
		balanceResult := CheckBalance(tx)

		if !balanceResult.Balanced {
			diag := a.createBalanceDiagnostic(tx, balanceResult)
			result.Diagnostics = append(result.Diagnostics, diag)
		}

		if len(declaredAccounts) > 0 {
			undeclaredDiags := checkUndeclaredAccounts(tx, declaredAccounts)
			result.Diagnostics = append(result.Diagnostics, undeclaredDiags...)
		}
	}

	return result
}

func collectDeclaredAccounts(journal *ast.Journal) map[string]bool {
	declared := make(map[string]bool)
	for _, dir := range journal.Directives {
		switch d := dir.(type) {
		case *ast.AccountDirective:
			declared[d.Account.Name] = true
		case ast.AccountDirective:
			declared[d.Account.Name] = true
		}
	}
	return declared
}

func checkUndeclaredAccounts(tx *ast.Transaction, declared map[string]bool) []Diagnostic {
	var diags []Diagnostic
	for _, posting := range tx.Postings {
		if !declared[posting.Account.Name] {
			diags = append(diags, Diagnostic{
				Range:    posting.Range,
				Severity: SeverityWarning,
				Code:     "UNDECLARED_ACCOUNT",
				Message:  fmt.Sprintf("account '%s' is not declared", posting.Account.Name),
			})
		}
	}
	return diags
}

func (a *Analyzer) createBalanceDiagnostic(tx *ast.Transaction, br *BalanceResult) Diagnostic {
	if br.InferredIdx == -1 && len(br.Differences) == 0 {
		return Diagnostic{
			Range:    tx.Range,
			Severity: SeverityError,
			Code:     "MULTIPLE_INFERRED",
			Message:  "transaction has multiple postings without amounts",
		}
	}

	var msg string
	for commodity, diff := range br.Differences {
		if msg != "" {
			msg += "; "
		}
		msg += fmt.Sprintf("%s off by %s", commodity, diff.String())
	}

	return Diagnostic{
		Range:    tx.Range,
		Severity: SeverityError,
		Code:     "UNBALANCED",
		Message:  fmt.Sprintf("transaction does not balance: %s", msg),
	}
}
