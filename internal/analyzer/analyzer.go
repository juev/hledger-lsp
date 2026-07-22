package analyzer

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/include"
)

type Analyzer struct {
	BalanceTolerance decimal.Decimal
}

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
		Accounts:              CollectAccounts(journal),
		Payees:                CollectPayees(journal),
		Descriptions:          CollectDescriptions(journal),
		Commodities:           CollectCommodities(journal),
		Tags:                  CollectTags(journal),
		TagValues:             CollectTagValues(journal),
		Dates:                 CollectDates(journal),
		PayeeTemplates:        CollectPayeeTemplates(journal),
		Diagnostics:           make([]Diagnostic, 0),
		AccountCounts:         CollectAccountCounts(journal),
		PayeeCounts:           CollectPayeeCounts(journal),
		DescriptionCounts:     CollectDescriptionCounts(journal),
		CommodityCounts:       CollectCommodityCounts(journal),
		TagCounts:             CollectTagCounts(journal),
		PayeeAccounts:         CollectPayeeAccounts(journal),
		PayeeAccountPairUsage: CollectPayeeAccountPairUsage(journal),
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
		balanceResult := CheckBalance(tx, a.BalanceTolerance)

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

		dateTagDiags := validateDateTags(tx)
		result.Diagnostics = append(result.Diagnostics, dateTagDiags...)
	}

	return result
}

func (a *Analyzer) AnalyzeResolved(resolved *include.ResolvedJournal) *AnalysisResult {
	result := &AnalysisResult{
		Accounts:              NewAccountIndex(),
		Payees:                []string{},
		Descriptions:          []string{},
		Commodities:           []string{},
		Tags:                  []string{},
		TagValues:             make(map[string][]string),
		Dates:                 []string{},
		PayeeTemplates:        make(map[string][]PostingTemplate),
		Diagnostics:           make([]Diagnostic, 0),
		AccountCounts:         make(map[string]int),
		PayeeCounts:           make(map[string]int),
		DescriptionCounts:     make(map[string]int),
		CommodityCounts:       make(map[string]int),
		TagCounts:             make(map[string]int),
		PayeeAccounts:         make(map[string][]string),
		PayeeAccountPairUsage: make(map[string]int),
	}

	if resolved == nil || resolved.Primary == nil {
		return result
	}

	result.Accounts = collectAccountsFromResolved(resolved)
	result.Payees = collectPayeesFromResolved(resolved)
	result.Descriptions = collectDescriptionsFromResolved(resolved)
	result.Commodities = collectCommoditiesFromResolved(resolved)
	result.Tags = collectTagsFromResolved(resolved)
	result.TagValues = collectTagValuesFromResolved(resolved)
	result.Dates = collectDatesFromResolved(resolved)
	result.PayeeTemplates = collectPayeeTemplatesFromResolved(resolved)
	result.AccountCounts = collectAccountCountsFromResolved(resolved)
	result.PayeeCounts = collectPayeeCountsFromResolved(resolved)
	result.DescriptionCounts = collectDescriptionCountsFromResolved(resolved)
	result.CommodityCounts = collectCommodityCountsFromResolved(resolved)
	result.TagCounts = collectTagCountsFromResolved(resolved)
	result.PayeeAccounts = collectPayeeAccountsFromResolved(resolved)
	result.PayeeAccountPairUsage = collectPayeeAccountPairUsageFromResolved(resolved)

	declaredAccounts := collectDeclaredAccountsFromResolved(resolved)
	declaredCommodities := collectDeclaredCommoditiesFromResolved(resolved)

	// Generate diagnostics for all occurrences, then dedup identical ones
	// (repeated includes produce the same source ranges).
	type diagKey struct {
		line, col int
		msg       string
		code      string
		severity  DiagnosticSeverity
	}
	seen := make(map[diagKey]bool)
	for _, tx := range resolved.AllTransactions() {
		tx := tx
		balanceResult := CheckBalance(&tx, a.BalanceTolerance)

		var txDiags []Diagnostic
		if !balanceResult.Balanced {
			txDiags = append(txDiags, a.createBalanceDiagnostic(&tx, balanceResult))
		}
		if len(declaredAccounts) > 0 {
			txDiags = append(txDiags, checkUndeclaredAccounts(&tx, declaredAccounts)...)
		}
		if len(declaredCommodities) > 0 {
			txDiags = append(txDiags, checkUndeclaredCommodities(&tx, declaredCommodities)...)
		}
		txDiags = append(txDiags, validateDateTags(&tx)...)

		for _, d := range txDiags {
			key := diagKey{d.Range.Start.Line, d.Range.Start.Column, d.Message, d.Code, d.Severity}
			if !seen[key] {
				seen[key] = true
				result.Diagnostics = append(result.Diagnostics, d)
			}
		}
	}

	return result
}

func collectAccountsFromResolved(resolved *include.ResolvedJournal) *AccountIndex {
	idx := NewAccountIndex()
	seen := make(map[string]bool)

	for _, journal := range resolved.SourceJournals() {
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

	for _, journal := range resolved.SourceJournals() {
		for _, p := range CollectPayees(journal) {
			if !seen[p] {
				seen[p] = true
				payees = append(payees, p)
			}
		}
	}

	return payees
}

func collectDescriptionsFromResolved(resolved *include.ResolvedJournal) []string {
	seen := make(map[string]bool)
	var descriptions []string

	for _, journal := range resolved.SourceJournals() {
		for _, d := range CollectDescriptions(journal) {
			if !seen[d] {
				seen[d] = true
				descriptions = append(descriptions, d)
			}
		}
	}

	return descriptions
}

func collectCommoditiesFromResolved(resolved *include.ResolvedJournal) []string {
	seen := make(map[string]bool)
	var commodities []string

	for _, journal := range resolved.SourceJournals() {
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

	for _, journal := range resolved.SourceJournals() {
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

	for _, journal := range resolved.SourceJournals() {
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

	for _, journal := range resolved.SourceJournals() {
		mergeDates(journal)
	}

	return dates
}

// collectPayeeTemplatesFromResolved collects payee templates across the resolved
// journal. With occurrences present, transactions are evaluated in textual-inline
// order so an include-site conflict resolves to the last inline occurrence. The
// legacy fallback preserves the historical FileOrder-then-Primary precedence for
// consumers that still mutate the projection directly.
func collectPayeeTemplatesFromResolved(resolved *include.ResolvedJournal) map[string][]PostingTemplate {
	if len(resolved.Occurrences) > 0 {
		combined := &ast.Journal{Transactions: resolved.AllTransactions()}
		return CollectPayeeTemplates(combined)
	}

	result := make(map[string][]PostingTemplate)
	mergeTemplates := func(journal *ast.Journal) {
		if journal == nil {
			return
		}
		for payee, postings := range CollectPayeeTemplates(journal) {
			result[payee] = postings
		}
	}

	for _, path := range resolved.FileOrder {
		if journal, ok := resolved.Files[path]; ok {
			mergeTemplates(journal)
		}
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
	for _, journal := range resolved.SourceJournals() {
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
	for _, journal := range resolved.SourceJournals() {
		mergeCounts(journal)
	}
	return counts
}

func collectDescriptionCountsFromResolved(resolved *include.ResolvedJournal) map[string]int {
	counts := make(map[string]int)
	mergeCounts := func(journal *ast.Journal) {
		if journal == nil {
			return
		}
		for k, v := range CollectDescriptionCounts(journal) {
			counts[k] += v
		}
	}
	for _, journal := range resolved.SourceJournals() {
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
	for _, journal := range resolved.SourceJournals() {
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
	for _, journal := range resolved.SourceJournals() {
		mergeCounts(journal)
	}
	return counts
}

func collectDeclaredAccountsFromResolved(resolved *include.ResolvedJournal) map[string]bool {
	declared := make(map[string]bool)
	for _, journal := range resolved.SourceJournals() {
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
			declared[ad.Account.GetResolvedName()] = true
		}
	}
	return declared
}

var predefinedAccountTypes = map[string]bool{
	"assets":      true,
	"liabilities": true,
	"equity":      true,
	"expenses":    true,
	"revenues":    true,
	"income":      true,
}

func isAccountDeclared(accountName string, declared map[string]bool) bool {
	lowerName := strings.ToLower(accountName)
	colonIdx := strings.Index(lowerName, ":")
	var prefix string
	if colonIdx == -1 {
		prefix = lowerName
	} else {
		prefix = lowerName[:colonIdx]
	}
	if predefinedAccountTypes[prefix] {
		return true
	}

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
		if !isAccountDeclared(posting.Account.GetResolvedName(), declared) {
			diags = append(diags, Diagnostic{
				Range:    posting.Account.Range,
				Severity: SeverityWarning,
				Code:     "UNDECLARED_ACCOUNT",
				Message:  fmt.Sprintf("account '%s' is not declared", posting.Account.GetResolvedName()),
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
	for _, journal := range resolved.SourceJournals() {
		for k := range collectDeclaredCommodities(journal) {
			declared[k] = true
		}
	}
	return declared
}

func checkUndeclaredCommodities(tx *ast.Transaction, declared map[string]bool) []Diagnostic {
	var diags []Diagnostic

	checkCommodity := func(symbol string, r ast.Range) {
		if symbol != "" && !declared[symbol] {
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
			if posting.BalanceAssertion.Cost != nil {
				checkCommodity(posting.BalanceAssertion.Cost.Amount.Commodity.Symbol, posting.BalanceAssertion.Cost.Amount.Commodity.Range)
			}
			if posting.BalanceAssertion.LotPrice != nil && posting.BalanceAssertion.LotPrice.Cost != nil {
				checkCommodity(posting.BalanceAssertion.LotPrice.Cost.Commodity.Symbol, posting.BalanceAssertion.LotPrice.Cost.Commodity.Range)
			}
		}
		if posting.LotPrice != nil && posting.LotPrice.Cost != nil {
			checkCommodity(posting.LotPrice.Cost.Commodity.Symbol, posting.LotPrice.Cost.Commodity.Range)
		}
	}
	return diags
}

func validateDateTags(tx *ast.Transaction) []Diagnostic {
	var diags []Diagnostic

	checkTag := func(tag ast.Tag) {
		name := strings.ToLower(tag.Name)
		if name != "date" && name != "date2" {
			return
		}

		if strings.TrimSpace(tag.Value) == "" {
			diags = append(diags, Diagnostic{
				Range:    tag.Range,
				Severity: SeverityWarning,
				Code:     "EMPTY_DATE_TAG",
				Message:  fmt.Sprintf("tag '%s' requires a date value", tag.Name),
			})
			return
		}

		if !isValidDateValue(tag.Value) {
			diags = append(diags, Diagnostic{
				Range:    tag.Range,
				Severity: SeverityWarning,
				Code:     "INVALID_DATE_TAG",
				Message:  fmt.Sprintf("tag '%s' has invalid date value: %s", tag.Name, tag.Value),
			})
		}
	}

	// Check transaction comments
	for _, comment := range tx.Comments {
		for _, tag := range comment.Tags {
			checkTag(tag)
		}
	}

	// Check posting tags
	for _, posting := range tx.Postings {
		for _, tag := range posting.Tags {
			checkTag(tag)
		}
	}

	return diags
}

func isValidDateValue(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}

	// Simple date validation: YYYY-MM-DD or YYYY/MM/DD or YYYY.MM.DD
	// Also allow partial dates like MM-DD or M-D
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '-' || r == '/' || r == '.'
	})

	if len(parts) < 2 || len(parts) > 3 {
		return false
	}

	for _, part := range parts {
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return false
			}
		}
	}

	return true
}
