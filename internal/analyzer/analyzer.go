package analyzer

import (
	"fmt"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/include"
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

func (a *Analyzer) AnalyzeResolved(resolved *include.ResolvedJournal) *AnalysisResult {
	result := &AnalysisResult{
		Accounts:    NewAccountIndex(),
		Payees:      []string{},
		Commodities: []string{},
		Tags:        []string{},
		Diagnostics: make([]Diagnostic, 0),
	}

	if resolved == nil || resolved.Primary == nil {
		return result
	}

	result.Accounts = collectAccountsFromResolved(resolved)
	result.Payees = collectPayeesFromResolved(resolved)
	result.Commodities = collectCommoditiesFromResolved(resolved)
	result.Tags = collectTagsFromResolved(resolved)

	declaredAccounts := collectDeclaredAccountsFromResolved(resolved)

	for i := range resolved.Primary.Transactions {
		tx := &resolved.Primary.Transactions[i]
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

func collectAccountsFromResolved(resolved *include.ResolvedJournal) *AccountIndex {
	idx := NewAccountIndex()
	seen := make(map[string]bool)

	if resolved.Primary != nil {
		for _, name := range CollectAccounts(resolved.Primary).All {
			if !seen[name] {
				seen[name] = true
				addAccountToIndex(idx, name)
			}
		}
	}

	for _, journal := range resolved.Files {
		for _, name := range CollectAccounts(journal).All {
			if !seen[name] {
				seen[name] = true
				addAccountToIndex(idx, name)
			}
		}
	}

	return idx
}

func collectPayeesFromResolved(resolved *include.ResolvedJournal) []string {
	seen := make(map[string]bool)
	var payees []string

	if resolved.Primary != nil {
		for _, p := range CollectPayees(resolved.Primary) {
			if !seen[p] {
				seen[p] = true
				payees = append(payees, p)
			}
		}
	}

	for _, journal := range resolved.Files {
		for _, p := range CollectPayees(journal) {
			if !seen[p] {
				seen[p] = true
				payees = append(payees, p)
			}
		}
	}

	return payees
}

func collectCommoditiesFromResolved(resolved *include.ResolvedJournal) []string {
	seen := make(map[string]bool)
	var commodities []string

	if resolved.Primary != nil {
		for _, c := range CollectCommodities(resolved.Primary) {
			if !seen[c] {
				seen[c] = true
				commodities = append(commodities, c)
			}
		}
	}

	for _, journal := range resolved.Files {
		for _, c := range CollectCommodities(journal) {
			if !seen[c] {
				seen[c] = true
				commodities = append(commodities, c)
			}
		}
	}

	return commodities
}

func collectTagsFromResolved(resolved *include.ResolvedJournal) []string {
	seen := make(map[string]bool)
	var tags []string

	if resolved.Primary != nil {
		for _, t := range CollectTags(resolved.Primary) {
			if !seen[t] {
				seen[t] = true
				tags = append(tags, t)
			}
		}
	}

	for _, journal := range resolved.Files {
		for _, t := range CollectTags(journal) {
			if !seen[t] {
				seen[t] = true
				tags = append(tags, t)
			}
		}
	}

	return tags
}

func collectDeclaredAccountsFromResolved(resolved *include.ResolvedJournal) map[string]bool {
	declared := make(map[string]bool)
	if resolved.Primary != nil {
		for k := range collectDeclaredAccounts(resolved.Primary) {
			declared[k] = true
		}
	}
	for _, journal := range resolved.Files {
		for k := range collectDeclaredAccounts(journal) {
			declared[k] = true
		}
	}
	return declared
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
