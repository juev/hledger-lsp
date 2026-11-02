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

		if !isValidDateValueIn(leadingDateText(tag.Value), tx.Date.Year) {
			diags = append(diags, Diagnostic{
				Range:    tag.Range,
				Severity: SeverityError,
				Code:     "INVALID_DATE_TAG",
				Message:  fmt.Sprintf("tag '%s' has invalid date value: %s", tag.Name, tag.Value),
				Data:     map[string]any{"kind": "dateTag", "tag": tag.Name, "value": tag.Value},
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

// checkAssignmentDates reports the balance assignments that carry a posting date.
// hledger refuses them because the amount of an assignment comes from the running
// balance at a date, and a date of its own would make the balancing circular. A
// secondary date (date2: or a bracket with only the part after =) is allowed.
func checkAssignmentDates(tx *ast.Transaction) []Diagnostic {
	var diags []Diagnostic
	for i := range tx.Postings {
		p := &tx.Postings[i]
		if p.Amount != nil || p.BalanceAssertion == nil {
			continue
		}
		if _, ok := postingDateOverride(tx, p); !ok {
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
// to load. A bracket is a date to hledger only when its whole content is one, so
// [garbage], [ 2024-01-20 ] and [2024-13-05x] are text, while [2024-13-05],
// [02-30] and [=2024-02-30] are dates that do not exist and make the journal fail.
// The whole content must be [date], [date=date] or [=date]; both halves are
// checked, in every bracket of the comment of a posting and of the comment lines
// below it.
func validateBracketDates(tx *ast.Transaction) []Diagnostic {
	var diags []Diagnostic
	for i := range tx.Postings {
		posting := &tx.Postings[i]
		diags = appendBracketDateDiagnostics(diags, posting.Comment, posting.CommentRange.Start, tx.Date.Year)
		for _, line := range commentLinesBelow(tx, i) {
			diags = appendBracketDateDiagnostics(diags, line.Text, line.Range.Start, tx.Date.Year)
		}
	}
	return diags
}

// commentLinesBelow returns the comment lines written under the posting with the
// given index, which belong to that posting although the parser keeps them with
// the transaction.
func commentLinesBelow(tx *ast.Transaction, index int) []ast.Comment {
	first := postingRange(&tx.Postings[index]).Start.Line
	next := int(^uint(0) >> 1)
	if index+1 < len(tx.Postings) {
		next = postingRange(&tx.Postings[index+1]).Start.Line
	}
	var lines []ast.Comment
	for _, line := range tx.Comments {
		if line.Range.Start.Line > first && line.Range.Start.Line < next {
			lines = append(lines, line)
		}
	}
	return lines
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
		primary, secondary, hasSecondary := strings.Cut(content, "=")
		for half, value := range []string{primary, secondary} {
			if half == 1 && !hasSecondary {
				continue
			}
			if !isRefusedBracketDate(primary, secondary, hasSecondary, half, transactionYear) {
				continue
			}
			from := start + 1
			if half == 1 {
				from += len(primary) + 1
			}
			column := base.Column + 1 + utf8.RuneCountInString(comment[:from])
			message := fmt.Sprintf("posting date has invalid date value: %s", value)
			if value == "" {
				message = "posting date has no date after ="
			}
			diags = append(diags, Diagnostic{
				Range: ast.Range{
					Start: ast.Position{Line: base.Line, Column: column, Offset: base.Offset + 1 + from},
					End:   ast.Position{Line: base.Line, Column: column + utf8.RuneCountInString(value), Offset: base.Offset + 1 + from + len(value)},
				},
				Severity: SeverityError,
				Code:     "INVALID_POSTING_DATE",
				Message:  message,
				Data:     map[string]any{"kind": "postingDate", "value": value},
			})
		}
	}
	return diags
}

// isRefusedBracketDate reports whether one half of a bracket makes hledger refuse
// the journal. A bracket is read as a date only when what is in it is a date: a
// half with a letter or a space in it, or a primary of a single number, leaves the
// bracket as text. A date half is refused when it does not exist, when it mixes
// separators, or when it has three parts and a year of fewer than four digits; a
// secondary that stops short of a date (empty, a single number, separators only)
// is refused after a primary that is one, and a date secondary after a primary of a
// single number is refused.
func isRefusedBracketDate(primary, secondary string, hasSecondary bool, half, transactionYear int) bool {
	whole := []string{primary, secondary}
	for i, value := range whole {
		if i == 1 && !hasSecondary {
			break
		}
		if value != "" && !onlyDateCharacters(value) {
			return false
		}
	}
	if hasSecondary && primary != "" && !looksLikeDate(primary) {
		// A single number is no date, but written before = and followed by one
		// it still stops hledger.
		return half == 0 && looksLikeDate(secondary) && !strings.ContainsAny(primary, "-/.")
	}

	value := whole[half]
	if looksLikeDate(value) {
		return !isDateOfOneSeparator(value) || isShortYear(value) || !isValidDateValueIn(value, transactionYear)
	}
	return half == 1 && primary != ""
}

func onlyDateCharacters(value string) bool {
	for _, r := range value {
		if (r < '0' || r > '9') && r != '-' && r != '/' && r != '.' {
			return false
		}
	}
	return true
}

func isDateOfOneSeparator(value string) bool {
	separator := rune(0)
	for _, r := range value {
		if r == '-' || r == '/' || r == '.' {
			if separator != 0 && r != separator {
				return false
			}
			separator = r
		}
	}
	return true
}

// isShortYear reports whether a date of three parts starts with a year of fewer
// than four digits.
func isShortYear(value string) bool {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '-' || r == '/' || r == '.' })
	return len(parts) == 3 && len(parts[0]) < 4
}

// looksLikeDate reports whether a whole string is made of numbers joined by -, /
// or ., the shape hledger reads as a date and refuses when the date does not exist.
func looksLikeDate(value string) bool {
	groups := 0
	digits := false
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			digits = true
		case (r == '-' || r == '/' || r == '.') && digits:
			groups++
			digits = false
		default:
			return false
		}
	}
	return digits && (groups == 1 || groups == 2)
}

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
		year, month, day = numbers[0], numbers[1], numbers[2]
	}
	if year == 0 {
		// Year 0 is a leap year, and a value without a year in a transaction
		// of no known year is checked against one.
		year = 2000
	}
	return parser.IsValidCalendarDate(year, month, day)
}
