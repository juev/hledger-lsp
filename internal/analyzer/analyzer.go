package analyzer

import (
	"fmt"
	"strings"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/include"
)

type Analyzer struct{}

func New() *Analyzer {
	return &Analyzer{}
}

func (a *Analyzer) Analyze(journal *ast.Journal) *AnalysisResult {
	return a.analyzeInternal(journal, ExternalDeclarations{})
}

func (a *Analyzer) AnalyzeWithExternalDeclarations(journal *ast.Journal, external ExternalDeclarations) *AnalysisResult {
	return a.analyzeInternal(journal, external)
}

func (a *Analyzer) analyzeInternal(journal *ast.Journal, external ExternalDeclarations) *AnalysisResult {
	result := &AnalysisResult{
		Accounts:        CollectAccounts(journal),
		Payees:          CollectPayees(journal),
		Commodities:     CollectCommodities(journal),
		Tags:            CollectTags(journal),
		TagValues:       CollectTagValues(journal),
		Dates:           CollectDates(journal),
		PayeeTemplates:  CollectPayeeTemplates(journal),
		Diagnostics:     make([]Diagnostic, 0),
		AccountCounts:   CollectAccountCounts(journal),
		PayeeCounts:     CollectPayeeCounts(journal),
		CommodityCounts: CollectCommodityCounts(journal),
		TagCounts:       CollectTagCounts(journal),
	}

	declaredAccounts := collectDeclaredAccounts(journal)
	for k := range external.Accounts {
		declaredAccounts[k] = true
	}

	declaredCommodities := collectDeclaredCommodities(journal)
	for k := range external.Commodities {
		declaredCommodities[k] = true
	}

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

		if len(declaredCommodities) > 0 {
			undeclaredCommodityDiags := checkUndeclaredCommodities(tx, declaredCommodities)
			result.Diagnostics = append(result.Diagnostics, undeclaredCommodityDiags...)
		}
	}

	return result
}

func (a *Analyzer) AnalyzeResolved(resolved *include.ResolvedJournal) *AnalysisResult {
	result := &AnalysisResult{
		Accounts:        NewAccountIndex(),
		Payees:          []string{},
		Commodities:     []string{},
		Tags:            []string{},
		TagValues:       make(map[string][]string),
		Dates:           []string{},
		PayeeTemplates:  make(map[string][]PostingTemplate),
		Diagnostics:     make([]Diagnostic, 0),
		AccountCounts:   make(map[string]int),
		PayeeCounts:     make(map[string]int),
		CommodityCounts: make(map[string]int),
		TagCounts:       make(map[string]int),
	}

	if resolved == nil || resolved.Primary == nil {
		return result
	}

	result.Accounts = collectAccountsFromResolved(resolved)
	result.Payees = collectPayeesFromResolved(resolved)
	result.Commodities = collectCommoditiesFromResolved(resolved)
	result.Tags = collectTagsFromResolved(resolved)
	result.TagValues = collectTagValuesFromResolved(resolved)
	result.Dates = collectDatesFromResolved(resolved)
	result.PayeeTemplates = collectPayeeTemplatesFromResolved(resolved)
	result.AccountCounts = collectAccountCountsFromResolved(resolved)
	result.PayeeCounts = collectPayeeCountsFromResolved(resolved)
	result.CommodityCounts = collectCommodityCountsFromResolved(resolved)
	result.TagCounts = collectTagCountsFromResolved(resolved)

	declaredAccounts := collectDeclaredAccountsFromResolved(resolved)
	declaredCommodities := collectDeclaredCommoditiesFromResolved(resolved)

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

		if len(declaredCommodities) > 0 {
			undeclaredCommodityDiags := checkUndeclaredCommodities(tx, declaredCommodities)
			result.Diagnostics = append(result.Diagnostics, undeclaredCommodityDiags...)
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

func collectTagValuesFromResolved(resolved *include.ResolvedJournal) map[string][]string {
	result := make(map[string][]string)
	seen := make(map[string]map[string]bool)

	mergeTagValues := func(journal *ast.Journal) {
		if journal == nil {
			return
		}
		for tagName, values := range CollectTagValues(journal) {
			if seen[tagName] == nil {
				seen[tagName] = make(map[string]bool)
			}
			for _, value := range values {
				if !seen[tagName][value] {
					seen[tagName][value] = true
					result[tagName] = append(result[tagName], value)
				}
			}
		}
	}

	mergeTagValues(resolved.Primary)
	for _, journal := range resolved.Files {
		mergeTagValues(journal)
	}

	return result
}

func collectDatesFromResolved(resolved *include.ResolvedJournal) []string {
	seen := make(map[string]bool)
	var dates []string

	mergeDates := func(journal *ast.Journal) {
		if journal == nil {
			return
		}
		for _, d := range CollectDates(journal) {
			if !seen[d] {
				seen[d] = true
				dates = append(dates, d)
			}
		}
	}

	mergeDates(resolved.Primary)
	for _, journal := range resolved.Files {
		mergeDates(journal)
	}

	return dates
}

// collectPayeeTemplatesFromResolved collects payee templates from all journals.
// When the same payee exists in multiple files, the primary file's template wins.
// Included files are processed first, then primary file overwrites conflicts.
func collectPayeeTemplatesFromResolved(resolved *include.ResolvedJournal) map[string][]PostingTemplate {
	result := make(map[string][]PostingTemplate)

	mergeTemplates := func(journal *ast.Journal) {
		if journal == nil {
			return
		}
		for payee, postings := range CollectPayeeTemplates(journal) {
			result[payee] = postings
		}
	}

	for _, journal := range resolved.Files {
		mergeTemplates(journal)
	}
	mergeTemplates(resolved.Primary)

	return result
}

func collectAccountCountsFromResolved(resolved *include.ResolvedJournal) map[string]int {
	counts := make(map[string]int)
	mergeCounts := func(journal *ast.Journal) {
		if journal == nil {
			return
		}
		for k, v := range CollectAccountCounts(journal) {
			counts[k] += v
		}
	}
	mergeCounts(resolved.Primary)
	for _, journal := range resolved.Files {
		mergeCounts(journal)
	}
	return counts
}

func collectPayeeCountsFromResolved(resolved *include.ResolvedJournal) map[string]int {
	counts := make(map[string]int)
	mergeCounts := func(journal *ast.Journal) {
		if journal == nil {
			return
		}
		for k, v := range CollectPayeeCounts(journal) {
			counts[k] += v
		}
	}
	mergeCounts(resolved.Primary)
	for _, journal := range resolved.Files {
		mergeCounts(journal)
	}
	return counts
}

func collectCommodityCountsFromResolved(resolved *include.ResolvedJournal) map[string]int {
	counts := make(map[string]int)
	mergeCounts := func(journal *ast.Journal) {
		if journal == nil {
			return
		}
		for k, v := range CollectCommodityCounts(journal) {
			counts[k] += v
		}
	}
	mergeCounts(resolved.Primary)
	for _, journal := range resolved.Files {
		mergeCounts(journal)
	}
	return counts
}

func collectTagCountsFromResolved(resolved *include.ResolvedJournal) map[string]int {
	counts := make(map[string]int)
	mergeCounts := func(journal *ast.Journal) {
		if journal == nil {
			return
		}
		for k, v := range CollectTagCounts(journal) {
			counts[k] += v
		}
	}
	mergeCounts(resolved.Primary)
	for _, journal := range resolved.Files {
		mergeCounts(journal)
	}
	return counts
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
		if ad, ok := dir.(ast.AccountDirective); ok {
			declared[ad.Account.Name] = true
		}
	}
	return declared
}

func isAccountDeclared(accountName string, declared map[string]bool) bool {
	if declared[accountName] {
		return true
	}
	for declaredAccount := range declared {
		if strings.HasPrefix(accountName, declaredAccount+":") {
			return true
		}
	}
	return false
}

func checkUndeclaredAccounts(tx *ast.Transaction, declared map[string]bool) []Diagnostic {
	var diags []Diagnostic
	for _, posting := range tx.Postings {
		if !isAccountDeclared(posting.Account.Name, declared) {
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

func collectDeclaredCommodities(journal *ast.Journal) map[string]bool {
	declared := make(map[string]bool)
	for _, dir := range journal.Directives {
		if cd, ok := dir.(ast.CommodityDirective); ok {
			declared[cd.Commodity.Symbol] = true
		}
	}
	return declared
}

func collectDeclaredCommoditiesFromResolved(resolved *include.ResolvedJournal) map[string]bool {
	declared := make(map[string]bool)
	if resolved.Primary != nil {
		for k := range collectDeclaredCommodities(resolved.Primary) {
			declared[k] = true
		}
	}
	for _, journal := range resolved.Files {
		for k := range collectDeclaredCommodities(journal) {
			declared[k] = true
		}
	}
	return declared
}

func checkUndeclaredCommodities(tx *ast.Transaction, declared map[string]bool) []Diagnostic {
	var diags []Diagnostic
	seen := make(map[string]bool)

	checkCommodity := func(symbol string, r ast.Range) {
		if symbol != "" && !declared[symbol] && !seen[symbol] {
			seen[symbol] = true
			diags = append(diags, Diagnostic{
				Range:    r,
				Severity: SeverityWarning,
				Code:     "UNDECLARED_COMMODITY",
				Message:  fmt.Sprintf("commodity '%s' has no directive", symbol),
			})
		}
	}

	for _, posting := range tx.Postings {
		if posting.Amount != nil {
			checkCommodity(posting.Amount.Commodity.Symbol, posting.Amount.Commodity.Range)
		}
		if posting.Cost != nil {
			checkCommodity(posting.Cost.Amount.Commodity.Symbol, posting.Cost.Amount.Commodity.Range)
		}
		if posting.BalanceAssertion != nil {
			checkCommodity(posting.BalanceAssertion.Amount.Commodity.Symbol, posting.BalanceAssertion.Amount.Commodity.Range)
		}
	}
	return diags
}
