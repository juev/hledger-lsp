package analyzer

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/parser"
)

// AccountCheckMode selects how strictly posting accounts must be declared.
type AccountCheckMode int

const (
	// AccountCheckLint keeps the historical soft check: warn about accounts no
	// account directive covers, while at least one directive exists.
	AccountCheckLint AccountCheckMode = iota
	// AccountCheckOff disables the check entirely, matching plain hledger.
	AccountCheckOff
	// AccountCheckStrict mirrors `hledger --strict`: every posting account needs
	// its own account directive, and subaccounts are not covered by a parent.
	AccountCheckStrict
)

type Analyzer struct {
	// mu guards the settings a running server can change (tolerance, account
	// check mode) against concurrent analysis. Read them through
	// balanceTolerance/accountCheckMode rather than directly.
	mu sync.RWMutex
	// BalanceTolerance and AccountCheck are the configured settings. Direct
	// field access is fine for single-goroutine callers; concurrent callers use
	// SetBalanceTolerance/SetAccountCheckMode and the accessors below.
	BalanceTolerance decimal.Decimal
	AccountCheck     AccountCheckMode
}

// SetBalanceTolerance updates the user tolerance for balance checks.
func (a *Analyzer) SetBalanceTolerance(tolerance decimal.Decimal) {
	a.mu.Lock()
	a.BalanceTolerance = tolerance
	a.mu.Unlock()
}

// SetAccountCheckMode updates how strictly posting accounts must be declared.
func (a *Analyzer) SetAccountCheckMode(mode AccountCheckMode) {
	a.mu.Lock()
	a.AccountCheck = mode
	a.mu.Unlock()
}

func (a *Analyzer) balanceTolerance() decimal.Decimal {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.BalanceTolerance
}

func (a *Analyzer) accountCheckMode() AccountCheckMode {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.AccountCheck
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
		PayeeAccountLastUsed:  CollectPayeeAccountLastUsed(journal),
	}

	declaredAccounts := collectDeclaredAccounts(journal)
	for k := range external.Accounts {
		declaredAccounts[k] = true
	}
	sortedDeclared := sortedKeys(declaredAccounts)

	declaredCommodities := collectDeclaredCommodities(journal)
	for k := range external.Commodities {
		declaredCommodities[k] = true
	}

	for i := range journal.Transactions {
		tx := &journal.Transactions[i]
		balanceResult := CheckBalance(tx, a.balanceTolerance())

		// A balance assignment takes its amount from the running balance, which
		// one buffer cannot see. CheckJournalBalance has the verdict on those.
		if !balanceResult.Balanced && !HasBalanceAssignment(tx) {
			diag := a.createBalanceDiagnostic(tx, balanceResult)
			result.Diagnostics = append(result.Diagnostics, diag)
		}

		if mode := a.accountCheckMode(); shouldCheckAccounts(mode, sortedDeclared) {
			undeclaredDiags := checkUndeclaredAccounts(tx, sortedDeclared, mode == AccountCheckStrict)
			result.Diagnostics = append(result.Diagnostics, undeclaredDiags...)
		}

		if len(declaredCommodities) > 0 {
			undeclaredCommodityDiags := checkUndeclaredCommodities(tx, declaredCommodities)
			result.Diagnostics = append(result.Diagnostics, undeclaredCommodityDiags...)
		}

		dateTagDiags := validateDateTags(tx)
		result.Diagnostics = append(result.Diagnostics, dateTagDiags...)
		result.Diagnostics = append(result.Diagnostics, checkAssignmentDates(tx)...)
		result.Diagnostics = append(result.Diagnostics, validateBracketDates(tx)...)
	}
	result.Diagnostics = append(result.Diagnostics, validateRuleBracketDates(journal)...)
	result.Diagnostics = append(result.Diagnostics, validateRuleDateTags(journal)...)

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
		PayeeAccountLastUsed:  make(map[string]string),
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
	result.PayeeTemplates = CollectPayeeTemplatesFromResolved(resolved)
	result.AccountCounts = collectAccountCountsFromResolved(resolved)
	result.PayeeCounts = collectPayeeCountsFromResolved(resolved)
	result.DescriptionCounts = collectDescriptionCountsFromResolved(resolved)
	result.CommodityCounts = collectCommodityCountsFromResolved(resolved)
	result.TagCounts = collectTagCountsFromResolved(resolved)
	result.PayeeAccounts = collectPayeeAccountsFromResolved(resolved)
	result.PayeeAccountPairUsage = collectPayeeAccountPairUsageFromResolved(resolved)
	result.PayeeAccountLastUsed = collectPayeeAccountLastUsedFromResolved(resolved)

	declaredAccounts := collectDeclaredAccountsFromResolved(resolved)
	sortedDeclared := sortedKeys(declaredAccounts)
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
		balanceResult := CheckBalance(&tx, a.balanceTolerance())

		var txDiags []Diagnostic
		if !balanceResult.Balanced && !HasBalanceAssignment(&tx) {
			txDiags = append(txDiags, a.createBalanceDiagnostic(&tx, balanceResult))
		}
		if mode := a.accountCheckMode(); shouldCheckAccounts(mode, sortedDeclared) {
			txDiags = append(txDiags, checkUndeclaredAccounts(&tx, sortedDeclared, mode == AccountCheckStrict)...)
		}
		if len(declaredCommodities) > 0 {
			txDiags = append(txDiags, checkUndeclaredCommodities(&tx, declaredCommodities)...)
		}
		txDiags = append(txDiags, validateDateTags(&tx)...)
		txDiags = append(txDiags, checkAssignmentDates(&tx)...)
		txDiags = append(txDiags, validateBracketDates(&tx)...)

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

// CollectPayeeTemplatesFromResolved collects payee templates across the resolved
// journal. With occurrences present, transactions are evaluated in textual-inline
// order so an include-site conflict resolves to the last inline occurrence. The
// legacy fallback preserves the historical FileOrder-then-Primary precedence for
// consumers that still mutate the projection directly.
func CollectPayeeTemplatesFromResolved(resolved *include.ResolvedJournal) map[string][]PostingTemplate {
	if resolved == nil || resolved.Primary == nil {
		return make(map[string][]PostingTemplate)
	}
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

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func isAccountDeclared(accountName string, sortedDeclared []string) bool {
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

	// Exact match via binary search
	i := sort.SearchStrings(sortedDeclared, accountName)
	if i < len(sortedDeclared) && sortedDeclared[i] == accountName {
		return true
	}

	// Prefix match: check each ancestor (e.g. "a:b:c" → "a:b", "a")
	name := accountName
	for {
		idx := strings.LastIndex(name, ":")
		if idx == -1 {
			break
		}
		name = name[:idx]
		i := sort.SearchStrings(sortedDeclared, name)
		if i < len(sortedDeclared) && sortedDeclared[i] == name {
			return true
		}
	}
	return false
}

// shouldCheckAccounts reports whether the account declaration check runs. The
// soft mode needs at least one directive to be meaningful; `hledger --strict`
// requires every account to be declared, so a journal without any directive is
// exactly the case it must report.
func shouldCheckAccounts(mode AccountCheckMode, declared []string) bool {
	switch mode {
	case AccountCheckOff:
		return false
	case AccountCheckStrict:
		return true
	default:
		return len(declared) > 0
	}
}

// checkUndeclaredAccounts reports posting accounts that no account directive
// covers. strict mirrors `hledger --strict`: the account needs its own
// declaration (an ancestor's declaration does not count and the built-in account
// type names are not exempt), and hledger refuses to load the journal, so the
// diagnostic is an error. The soft mode warns and keeps the historical
// exemptions.
func checkUndeclaredAccounts(tx *ast.Transaction, sortedDeclared []string, strict bool) []Diagnostic {
	var diags []Diagnostic
	for i := range tx.Postings {
		posting := &tx.Postings[i]
		name := posting.Account.GetResolvedName()

		declared := isAccountDeclared(name, sortedDeclared)
		severity := SeverityWarning
		if strict {
			declared = isAccountDeclaredStrict(name, sortedDeclared)
			severity = SeverityError
		}
		if declared {
			continue
		}

		message := fmt.Sprintf("account '%s' is not declared", name)
		if strict {
			message = fmt.Sprintf("account '%s' has not been declared (hledger --strict)", name)
		}

		diags = append(diags, Diagnostic{
			Range:    posting.Account.Range,
			Severity: severity,
			Code:     "UNDECLARED_ACCOUNT",
			Message:  message,
			Data:     map[string]any{"kind": "undeclaredAccount", "account": name, "strict": strict},
		})
	}
	return diags
}

// isAccountDeclaredStrict reports whether the account has its own account
// directive, mirroring `hledger --strict`.
func isAccountDeclaredStrict(accountName string, sortedDeclared []string) bool {
	i := sort.SearchStrings(sortedDeclared, accountName)
	return i < len(sortedDeclared) && sortedDeclared[i] == accountName
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
				Data:     map[string]any{"kind": "undeclaredCommodity", "commodity": symbol},
			})
		}
	}

	// An amount written without a symbol inherits the default commodity, but has
	// no commodity text to point at: report the amount itself, the way hledger
	// underlines the bare number.
	checkAmountCommodity := func(amount *ast.Amount) {
		if amount == nil {
			return
		}
		r := amount.Commodity.Range
		if amount.Commodity.Inferred {
			r = amount.Range
		}
		checkCommodity(amount.Commodity.Symbol, r)
	}

	for _, posting := range tx.Postings {
		checkAmountCommodity(posting.Amount)
		if posting.Cost != nil {
			checkAmountCommodity(&posting.Cost.Amount)
		}
		if posting.BalanceAssertion != nil {
			checkAmountCommodity(&posting.BalanceAssertion.Amount)
			if posting.BalanceAssertion.Cost != nil {
				checkAmountCommodity(&posting.BalanceAssertion.Cost.Amount)
			}
		}
		if posting.LotPrice != nil && posting.LotPrice.Cost != nil {
			checkAmountCommodity(posting.LotPrice.Cost)
		}
	}
	return diags
}

func validateDateTags(tx *ast.Transaction) []Diagnostic {
	return postingDateTagDiagnostics(tx.Postings, tx.Comments, tx.Date.Year)
}

// postingDateTagDiagnostics reports the date: and date2: tags hledger refuses on
// postings and on the comment lines below them. A value without a year is read
// in the given year; with unknownYear, the year of an auto posting rule, hledger
// cannot read it at all.
func postingDateTagDiagnostics(postings []ast.Posting, comments []ast.Comment, year int) []Diagnostic {
	var diags []Diagnostic

	checkTag := func(tag ast.Tag) {
		name := tag.Name
		if name != "date" && name != "date2" {
			return
		}

		if strings.TrimSpace(tag.Value) == "" {
			diags = append(diags, Diagnostic{
				Range:    tag.Range,
				Severity: SeverityError,
				Code:     "EMPTY_DATE_TAG",
				Message:  fmt.Sprintf("tag '%s' requires a date value", tag.Name),
				Data:     map[string]any{"kind": "dateTag", "tag": tag.Name, "value": tag.Value},
			})
			return
		}

		value := leadingDateText(tag.Value)
		if year == unknownYear && isValidDateValueIn(value, 0) && strings.Count(value, "-")+strings.Count(value, "/")+strings.Count(value, ".") == 1 {
			diags = append(diags, Diagnostic{
				Range:    tag.Range,
				Severity: SeverityError,
				Code:     "INVALID_DATE_TAG",
				Message:  fmt.Sprintf("tag '%s' needs a year in an auto posting rule, which has no year to read %s in", tag.Name, tag.Value),
				Data:     map[string]any{"kind": "dateTag", "tag": tag.Name, "value": tag.Value},
			})
			return
		}
		if !isValidDateValueIn(value, year) {
			diags = append(diags, Diagnostic{
				Range:    tag.Range,
				Severity: SeverityError,
				Code:     "INVALID_DATE_TAG",
				Message:  fmt.Sprintf("tag '%s' has invalid date value: %s", tag.Name, tag.Value),
				Data:     map[string]any{"kind": "dateTag", "tag": tag.Name, "value": tag.Value},
			})
		}
	}

	// hledger reads a posting date only from a posting: its comment and the
	// comment lines below it. In the header comment and on the comment lines
	// before the first posting a date: tag is prose, whatever its value.
	for _, lines := range postingCommentLines(postings, comments) {
		for _, line := range lines {
			for _, tag := range line.Tags {
				checkTag(tag)
			}
		}
	}

	// Check posting tags
	for _, posting := range postings {
		for _, tag := range posting.Tags {
			checkTag(tag)
		}
	}

	return diags
}

// checkAssignmentDates reports the balance assignments that carry a posting date.
// hledger refuses them because the amount of an assignment comes from the running
// balance at a date, and a date of its own would make the balancing circular. A
// secondary date (date2: or a bracket with only the part after =) is allowed.
func checkAssignmentDates(tx *ast.Transaction) []Diagnostic {
	var diags []Diagnostic
	var lines [][]ast.Comment
	for i := range tx.Postings {
		p := &tx.Postings[i]
		if p.Amount != nil || p.BalanceAssertion == nil {
			continue
		}
		if lines == nil {
			lines = postingCommentLines(tx.Postings, tx.Comments)
		}
		if _, ok := postingDateOverride(tx, p, lines[i]); !ok {
			continue
		}
		diags = append(diags, Diagnostic{
			Range:    postingRange(p),
			Severity: SeverityError,
			Code:     "ASSIGNMENT_POSTING_DATE",
			Message:  "balance assignments and custom posting dates may not be combined: write the amount explicitly or remove the posting date",
			Data:     map[string]any{"kind": "assignmentPostingDate", "account": p.Account.GetResolvedName()},
		})
	}
	return diags
}

// validateBracketDates reports the bracketed posting dates that hledger refuses
// to load. hledger takes a bracket for a date when everything in it is a digit,
// -, /, . or =, with at least one digit and one of -, / or .; [garbage], [5],
// [ 2024-01-20 ] and [2024-13-05x] stay text. Such a bracket must then be exactly
// [date], [date=date] or [=date] with dates that exist, so [2024-13-05],
// [2024-01-05-] and [2024-01-05=2024-01-06=2024-01-07] make the journal fail.
// Every bracket of the comment of a posting and of the comment lines below it is
// checked.
func validateBracketDates(tx *ast.Transaction) []Diagnostic {
	return postingBracketDateDiagnostics(tx.Postings, tx.Comments, tx.Date.Year)
}

// validateRuleBracketDates reports the bracketed posting dates hledger refuses in
// periodic transactions and auto posting rules. A periodic transaction reads a
// date without a year in the year in force where it is written; an auto posting
// rule has no year.
func validateRuleBracketDates(journal *ast.Journal) []Diagnostic {
	var diags []Diagnostic
	for i := range journal.PeriodicTransactions {
		periodic := &journal.PeriodicTransactions[i]
		diags = append(diags, postingBracketDateDiagnostics(periodic.Postings, periodic.Comments, periodic.Year)...)
	}
	for i := range journal.AutoPostingRules {
		rule := &journal.AutoPostingRules[i]
		diags = append(diags, postingBracketDateDiagnostics(rule.Postings, rule.Comments, unknownYear)...)
	}
	return diags
}

// validateRuleDateTags reports the date: and date2: tags hledger refuses on the
// postings of periodic transactions and auto posting rules, read in the same
// years as their bracketed dates.
func validateRuleDateTags(journal *ast.Journal) []Diagnostic {
	var diags []Diagnostic
	for i := range journal.PeriodicTransactions {
		periodic := &journal.PeriodicTransactions[i]
		diags = append(diags, postingDateTagDiagnostics(periodic.Postings, periodic.Comments, periodic.Year)...)
	}
	for i := range journal.AutoPostingRules {
		rule := &journal.AutoPostingRules[i]
		diags = append(diags, postingDateTagDiagnostics(rule.Postings, rule.Comments, unknownYear)...)
	}
	return diags
}

func postingBracketDateDiagnostics(postings []ast.Posting, comments []ast.Comment, year int) []Diagnostic {
	var diags []Diagnostic
	lines := postingCommentLines(postings, comments)
	for i := range postings {
		posting := &postings[i]
		diags = appendBracketDateDiagnostics(diags, posting.Comment, posting.CommentRange.Start, year)
		for _, line := range lines[i] {
			diags = appendBracketDateDiagnostics(diags, line.Text, line.Range.Start, year)
		}
	}
	return diags
}

func appendBracketDateDiagnostics(diags []Diagnostic, comment string, base ast.Position, transactionYear int) []Diagnostic {
	for start := 0; start < len(comment); start++ {
		if comment[start] != '[' {
			continue
		}
		length := strings.IndexByte(comment[start+1:], ']')
		if length < 0 {
			break
		}
		content := comment[start+1 : start+1+length]
		if !isBracketedDate(content) {
			continue
		}

		// A yearless secondary date takes the year of the primary one.
		primary, secondary, hasSecondary := strings.Cut(content, "=")
		secondaryYear := transactionYear
		if primary != "" {
			if year, ok := bracketDateYear(primary, transactionYear); ok {
				secondaryYear = year
			} else {
				diags = append(diags, bracketDateDiagnostic(comment, base, start+1, primary))
			}
		}
		if hasSecondary {
			if _, ok := bracketDateYear(secondary, secondaryYear); !ok {
				diags = append(diags, bracketDateDiagnostic(comment, base, start+1+len(primary)+1, secondary))
			}
		}
	}
	return diags
}

// bracketDateDiagnostic reports the half of a bracket that starts at byte from of
// the comment, whose text begins just after the comment marker at base.
func bracketDateDiagnostic(comment string, base ast.Position, from int, value string) Diagnostic {
	column := base.Column + 1 + utf8.RuneCountInString(comment[:from])
	message := fmt.Sprintf("posting date has invalid date value: %s", value)
	if value == "" {
		message = "posting date has no date after ="
	}
	return Diagnostic{
		Range: ast.Range{
			Start: ast.Position{Line: base.Line, Column: column, Offset: base.Offset + 1 + from},
			End:   ast.Position{Line: base.Line, Column: column + utf8.RuneCountInString(value), Offset: base.Offset + 1 + from + len(value)},
		},
		Severity: SeverityError,
		Code:     "INVALID_POSTING_DATE",
		Message:  message,
		Data:     map[string]any{"kind": "postingDate", "value": value},
	}
}

// isBracketedDate reports whether hledger reads what is in a bracket as a date:
// only digits, separators and =, with a digit and a separator among them.
func isBracketedDate(content string) bool {
	digit, separator := false, false
	for i := 0; i < len(content); i++ {
		switch c := content[i]; {
		case c >= '0' && c <= '9':
			digit = true
		case c == '-' || c == '/' || c == '.':
			separator = true
		case c != '=':
			return false
		}
	}
	return digit && separator
}

// bracketDateYear reads one half of a bracketed date the way hledger does and
// returns its year. The half is a year of four or more digits, a month and a day
// joined by one separator, or a month and a day in the given year; nothing may
// be left over.
func bracketDateYear(value string, year int) (int, bool) {
	var parts []string
	var separators []byte
	begin := 0
	for i := 0; i < len(value); i++ {
		switch c := value[i]; {
		case c == '-' || c == '/' || c == '.':
			parts = append(parts, value[begin:i])
			separators = append(separators, c)
			begin = i + 1
		case c < '0' || c > '9':
			return 0, false
		}
	}
	parts = append(parts, value[begin:])
	for _, part := range parts {
		if part == "" {
			return 0, false
		}
	}

	switch {
	case len(parts) == 3 && len(parts[0]) >= 4 && separators[0] == separators[1]:
		year = yearNumber(parts[0])
	case len(parts) == 2 && len(parts[0]) < 4 && year != unknownYear:
	default:
		return 0, false
	}
	return year, isValidDateValueIn(value, year)
}

// yearNumber returns a year written in digits, reduced to the same place in the
// 400-year Gregorian cycle so that a year of any length keeps its leap years.
func yearNumber(digits string) int {
	year := 0
	for _, ch := range digits {
		year = (year*10 + int(ch-'0')) % 400
	}
	return year
}

// unknownYear stands for the year of an auto posting rule, which has none: hledger
// refuses a bracketed date without a year there unless it follows a full date.
const unknownYear = -2

// isValidDateValueIn reports whether a date:/date2: tag value is one hledger
// accepts. hledger requires a day: "; date:2024-01" fails to load while
// "; date:1/5" is a valid smart date, so a year-month value is rejected here.
// A value without a year takes the year of the transaction it is read in: 02-29
// is a date in 2024 and not in 2023.
func isValidDateValueIn(value string, transactionYear int) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}

	// Accepted: YYYY-MM-DD, YYYY/MM/DD, YYYY.MM.DD, MM-DD, M-D.
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '-' || r == '/' || r == '.'
	})

	if len(parts) < 2 || len(parts) > 3 {
		return false
	}

	numbers := make([]int, 0, len(parts))
	for _, part := range parts {
		number := 0
		if part == "" {
			return false
		}
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return false
			}
			number = number*10 + int(ch-'0')
		}
		numbers = append(numbers, number)
	}

	year, month, day := transactionYear, numbers[0], numbers[1]
	if len(numbers) == 3 {
		year, month, day = yearNumber(parts[0]), numbers[1], numbers[2]
	}
	if year == 0 {
		// Year 0 is a leap year, and a value without a year in a transaction
		// of no known year is checked against one.
		year = 2000
	}
	return parser.IsValidCalendarDate(year, month, day)
}
