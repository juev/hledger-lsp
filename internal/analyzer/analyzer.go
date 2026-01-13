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

	for i := range journal.Transactions {
		tx := &journal.Transactions[i]
		balanceResult := CheckBalance(tx)

		if !balanceResult.Balanced {
			diag := a.createBalanceDiagnostic(tx, balanceResult)
			result.Diagnostics = append(result.Diagnostics, diag)
		}
	}

	return result
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
