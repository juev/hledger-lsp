package analyzer

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/parser"
)

// Diagnostic codes produced by the journal-level pass.
const (
	CodeUnbalanced             = "UNBALANCED"
	CodeMultipleInferred       = "MULTIPLE_INFERRED"
	CodeBalanceAssertionFailed = "BALANCE_ASSERTION_FAILED"
)

// multipleInferredMessage keeps hledger's own hint: the usual cause is a single
// space between the account and the amount, which makes the account name absorb
// the amount.
const multipleInferredMessage = "posting has no amount (separate the account and amount with two or more spaces)"

// JournalCheckOptions tunes the journal-level pass.
type JournalCheckOptions struct {
	// PartialContext marks a document that no workspace journal includes, so the
	// pass can only see that file's own transactions. Assertion verdicts are then
	// qualified with a hint instead of pretending to know the whole history.
	PartialContext bool
}

// SourcedDiagnostic is a diagnostic that knows which file it belongs to, so a
// caller can publish it to the right document in a multi-file journal. Path is
// empty when the diagnostic belongs to the document that was analyzed.
type SourcedDiagnostic struct {
	Path     string
	Range    ast.Range
	Severity DiagnosticSeverity
	Code     string
	Message  string
	Data     map[string]any
}

// accountBalances holds running per-account, per-commodity balances while a
// journal is evaluated in date order.
type accountBalances map[string]map[string]decimal.Decimal

func (b accountBalances) add(account, commodity string, quantity decimal.Decimal) {
	if account == "" {
		return
	}
	byCommodity, ok := b[account]
	if !ok {
		byCommodity = make(map[string]decimal.Decimal)
		b[account] = byCommodity
	}
	byCommodity[commodity] = byCommodity[commodity].Add(quantity)
}

func (b accountBalances) amount(account, commodity string, inclusive bool) decimal.Decimal {
	if !inclusive {
		return b[account][commodity]
	}
	total := decimal.Zero
	prefix := account + ":"
	for name, byCommodity := range b {
		if name != account && !strings.HasPrefix(name, prefix) {
			continue
		}
		total = total.Add(byCommodity[commodity])
	}
	return total
}

// otherCommodities returns the balances held by an account (and, for inclusive
// assertions, its subaccounts) in commodities other than the asserted one, in a
// stable order. hledger's `==` form requires these to be empty, and it compares
// each commodity using that commodity's own precision, so a balance like 0.20 EUR
// still fails a dollar assertion.
func (b accountBalances) otherCommodities(account, except string, inclusive bool, userTolerance decimal.Decimal) []string {
	var result []string
	seen := make(map[string]bool)
	prefix := account + ":"
	for name, byCommodity := range b {
		if name != account && (!inclusive || !strings.HasPrefix(name, prefix)) {
			continue
		}
		for commodity, quantity := range byCommodity {
			if commodity == except || quantity.IsZero() || seen[commodity] {
				continue
			}

			tolerance := toleranceForPrecision(decimalPrecision(quantity))
			if userTolerance.IsPositive() && userTolerance.GreaterThan(tolerance) {
				tolerance = userTolerance
			}
			if quantity.Abs().LessThanOrEqual(tolerance) {
				continue
			}

			seen[commodity] = true
			result = append(result, fmt.Sprintf("%s %s", quantity.String(), displayCommodity(commodity)))
		}
	}
	sort.Strings(result)
	return result
}

// journalEvaluator carries the mutable state of one journal evaluation.
type journalEvaluator struct {
	balances        accountBalances
	tolerance       decimal.Decimal
	events          []postingBalanceEvent
	trackAssertions bool
	// partialContext qualifies assertion verdicts that were computed from one
	// file's transactions only.
	partialContext bool
}

// postingEffective is the amount a posting contributes to the journal once
// hledger's inference rules have been applied: the written amount, or the amount
// inferred for an assertion-only posting.
type postingEffective struct {
	native    map[string]decimal.Decimal
	precision int32
}

type postingBalanceEvent struct {
	date         ast.Date
	tx           *ast.Transaction
	path         string
	sourceOrder  int
	postingIndex int
	contribution map[string]decimal.Decimal
}

// CheckJournalBalance evaluates hledger's balance rules and balance assertions
// over a resolved journal.
//
// Transaction balancing and amount inference happen first. Assertions are then
// checked against postings in primary posting-date order, with parse order as
// the tie breaker, as hledger does. A posting's date2: does not affect this pass.
func CheckJournalBalance(resolved *include.ResolvedJournal, userTolerance decimal.Decimal, options ...JournalCheckOptions) []SourcedDiagnostic {
	if resolved == nil {
		return nil
	}

	opts := JournalCheckOptions{}
	if len(options) > 0 {
		opts = options[0]
	}
	sourced := resolved.TransactionsWithSource()
	if len(sourced) == 0 {
		return nil
	}
	trackAssertions := false
	for _, source := range sourced {
		for _, posting := range source.Transaction.Postings {
			if posting.BalanceAssertion != nil {
				trackAssertions = true
				break
			}
		}
		if trackAssertions {
			break
		}
	}

	ordered := make([]int, len(sourced))
	for i := range sourced {
		ordered[i] = i
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return compareDates(sourced[ordered[i]].Transaction.Date, sourced[ordered[j]].Transaction.Date) < 0
	})

	evaluator := &journalEvaluator{
		balances:        accountBalances{},
		tolerance:       userTolerance,
		trackAssertions: trackAssertions,
	}

	evaluator.partialContext = opts.PartialContext

	var diagnostics []SourcedDiagnostic
	for _, index := range ordered {
		diagnostics = append(diagnostics, evaluator.evaluate(&sourced[index].Transaction, sourced[index].Path, index)...)
	}
	diagnostics = append(diagnostics, evaluator.checkAssertions()...)
	return diagnostics
}

func (e *journalEvaluator) evaluate(tx *ast.Transaction, path string, sourceOrder int) []SourcedDiagnostic {
	effective := make([]postingEffective, len(tx.Postings))
	var diagnostics []SourcedDiagnostic

	// The transaction pass infers assertion-only amounts from the running
	// transaction-date balance. hledger rejects a custom date on the assignment
	// itself; effects of other backdated postings on assignments are still outside
	// this pass.
	groups := postingGroups(tx.Postings)

	// hledger infers at most one amount per balancing group, and parenthesised
	// virtual postings are never inferred, so the candidates are counted per
	// group rather than per transaction.
	elidedByGroup := make(map[int]int) // group index -> posting index
	inferredPerGroup := make(map[int]int)

	for i := range tx.Postings {
		p := &tx.Postings[i]
		switch {
		case p.Amount != nil:
			effective[i] = postingEffective{
				native:    map[string]decimal.Decimal{p.Amount.Commodity.Symbol: p.Amount.Quantity},
				precision: decimalPrecision(p.Amount.Quantity),
			}
		case p.BalanceAssertion != nil:
			// hledger infers the amount that makes the assertion hold: the
			// asserted balance minus what the account already holds.
			commodity := p.BalanceAssertion.Amount.Commodity.Symbol
			prior := e.balances.amount(p.Account.GetResolvedName(), commodity, p.BalanceAssertion.IsInclusive)
			inferred := p.BalanceAssertion.Amount.Quantity.Sub(prior)
			effective[i] = postingEffective{
				native:    map[string]decimal.Decimal{commodity: inferred},
				precision: decimalPrecision(p.BalanceAssertion.Amount.Quantity),
			}
		default:
			if group, ok := groupOf(groups, i); ok {
				inferredPerGroup[group]++
			}

			if group, ok := groupOf(groups, i); ok && inferredPerGroup[group] == 1 {
				elidedByGroup[group] = i
			} else if ok && inferredPerGroup[group] > 1 {
				delete(elidedByGroup, group)
			}
			continue
		}

		e.apply(p.Account.GetResolvedName(), effective[i].native)
		e.recordEvent(tx, path, sourceOrder, i, effective[i].native)
	}

	// More than one amount-less posting in a group cannot be inferred, so the
	// transaction has no balance hledger is willing to compute.
	for group, count := range inferredPerGroup {
		if count <= 1 {
			continue
		}
		for _, idx := range groups[group] {
			p := &tx.Postings[idx]
			if p.Amount != nil || p.BalanceAssertion != nil {
				continue
			}
			diagnostics = append(diagnostics, SourcedDiagnostic{
				Path:     path,
				Range:    postingRange(p),
				Severity: SeverityError,
				Code:     CodeMultipleInferred,
				Message:  multipleInferredMessage,
				Data: map[string]any{
					"kind":    "multipleInferred",
					"account": p.Account.GetResolvedName(),
				},
			})
		}
		return diagnostics
	}

	for group, index := range elidedByGroup {
		elided := tx.Postings[index]
		contribution := e.elidedContribution(tx, effective, index, groups[group])
		e.apply(elided.Account.GetResolvedName(), contribution)
		e.recordEvent(tx, path, sourceOrder, index, contribution)
	}

	diagnostics = append(diagnostics, e.balanceDiagnostics(tx, path, effective, elidedByGroup, groups)...)

	return diagnostics
}

func (e *journalEvaluator) recordEvent(tx *ast.Transaction, path string, sourceOrder, postingIndex int, contribution map[string]decimal.Decimal) {
	if !e.trackAssertions {
		return
	}
	e.events = append(e.events, postingBalanceEvent{
		date:         primaryPostingDate(tx, &tx.Postings[postingIndex]),
		tx:           tx,
		path:         path,
		sourceOrder:  sourceOrder,
		postingIndex: postingIndex,
		contribution: contribution,
	})
}

func (e *journalEvaluator) checkAssertions() []SourcedDiagnostic {
	sort.SliceStable(e.events, func(i, j int) bool {
		a, b := e.events[i], e.events[j]
		if byDate := compareDates(a.date, b.date); byDate != 0 {
			return byDate < 0
		}
		if a.sourceOrder != b.sourceOrder {
			return a.sourceOrder < b.sourceOrder
		}
		return a.postingIndex < b.postingIndex
	})

	e.balances = accountBalances{}
	var diagnostics []SourcedDiagnostic
	for _, event := range e.events {
		posting := &event.tx.Postings[event.postingIndex]
		e.apply(posting.Account.GetResolvedName(), event.contribution)
		if posting.BalanceAssertion != nil {
			diagnostics = append(diagnostics, e.assertionDiagnostics(event.tx, event.postingIndex, event.path)...)
		}
	}
	return diagnostics
}

// primaryPostingDate returns a posting's date: or bracketed-date override. A
// yearless value inherits the transaction year. Invalid date: values have their
// own diagnostic and fall back to the transaction date here.
func primaryPostingDate(tx *ast.Transaction, posting *ast.Posting) ast.Date {
	for _, tag := range posting.Tags {
		if tag.Name == "date" {
			if date, ok := parsePostingDateValue(tag.Value, tx.Date.Year); ok {
				return date
			}
		}
	}

	comment := posting.Comment
	for {
		start := strings.IndexByte(comment, '[')
		if start < 0 {
			break
		}
		comment = comment[start+1:]
		end := strings.IndexByte(comment, ']')
		if end < 0 {
			break
		}
		primary, _, _ := strings.Cut(comment[:end], "=")
		if date, ok := parsePostingDateValue(primary, tx.Date.Year); ok {
			return date
		}
		comment = comment[end+1:]
	}
	return tx.Date
}

func parsePostingDateValue(value string, transactionYear int) (ast.Date, bool) {
	value = strings.TrimSpace(value)
	separator := strings.IndexAny(value, "-/.")
	if separator < 0 {
		return ast.Date{}, false
	}
	parts := strings.Split(value, string(value[separator]))
	if len(parts) != 2 && len(parts) != 3 {
		return ast.Date{}, false
	}
	year := transactionYear
	if len(parts) == 3 {
		parsed, err := strconv.Atoi(parts[0])
		if err != nil {
			return ast.Date{}, false
		}
		year = parsed
		parts = parts[1:]
	}
	month, monthErr := strconv.Atoi(parts[0])
	day, dayErr := strconv.Atoi(parts[1])
	if monthErr != nil || dayErr != nil || !parser.IsValidCalendarDate(year, month, day) {
		return ast.Date{}, false
	}
	return ast.Date{Year: year, Month: month, Day: day}, true
}

// groupOf returns the index of the balancing group that contains a posting.
func groupOf(groups [][]int, posting int) (int, bool) {
	for group, indices := range groups {
		if groupContains(indices, posting) {
			return group, true
		}
	}
	return 0, false
}

func (e *journalEvaluator) apply(account string, contribution map[string]decimal.Decimal) {
	for commodity, quantity := range contribution {
		e.balances.add(account, commodity, quantity)
	}
}

// balanceDiagnostics checks the two posting groups of a transaction against
// hledger's balance rules and returns transaction-level diagnostics.
func (e *journalEvaluator) balanceDiagnostics(tx *ast.Transaction, path string, effective []postingEffective, elidedByGroup map[int]int, groups [][]int) []SourcedDiagnostic {
	var diagnostics []SourcedDiagnostic

	for group, indices := range groups {
		elidedIdx, hasElided := elidedByGroup[group]
		postings := make([]ast.Posting, 0, len(indices))
		for _, idx := range indices {
			if hasElided && idx == elidedIdx {
				continue
			}
			p := tx.Postings[idx]
			if p.Amount == nil {
				// Assertion-only postings are balanced by construction; their
				// inferred amount is what keeps the group in balance.
				p.Amount = synthesizedAmount(effective[idx])
			}
			postings = append(postings, p)
		}

		if hasElided {
			// Exactly one posting is elided in this group, so it absorbs the
			// residual and the group is balanced by construction.
			continue
		}

		result := checkPostingGroup(postings, e.tolerance)
		if result.multipleInferred {
			// An elided posting in the *other* group cannot balance this one.
			diagnostics = append(diagnostics, SourcedDiagnostic{
				Path:     path,
				Range:    tx.Range,
				Severity: SeverityError,
				Code:     CodeMultipleInferred,
				Message:  multipleInferredMessage,
				Data:     map[string]any{"kind": "multipleInferred"},
			})
			continue
		}
		if len(result.signedDifferences) == 0 {
			continue
		}

		diagnostics = append(diagnostics, SourcedDiagnostic{
			Path:     path,
			Range:    tx.Range,
			Severity: SeverityError,
			Code:     CodeUnbalanced,
			Message:  "transaction does not balance: " + formatDifferences(result.signedDifferences),
			Data: map[string]any{
				"kind":        "unbalanced",
				"differences": decimalStrings(result.signedDifferences),
			},
		})
	}

	return diagnostics
}

// elidedContribution returns the mixed amount hledger infers for the single
// amount-less posting of a transaction: the negation of the cost-aware residual
// of its own posting group.
func (e *journalEvaluator) elidedContribution(tx *ast.Transaction, effective []postingEffective, elidedIdx int, group []int) map[string]decimal.Decimal {
	contribution := make(map[string]decimal.Decimal)

	postings := make([]ast.Posting, 0, len(group))
	for _, idx := range group {
		if idx == elidedIdx {
			continue
		}
		p := tx.Postings[idx]
		if p.Amount == nil {
			p.Amount = synthesizedAmount(effective[idx])
		}
		postings = append(postings, p)
	}
	for commodity, sum := range sumByCommodity(postings) {
		contribution[commodity] = sum.Neg()
	}
	return contribution
}

// assertionDiagnostics checks one balance assertion against the balance as it
// stands after the asserting posting, which is how hledger evaluates it: an
// assertion sees earlier postings of the same transaction, and later postings of
// that transaction do not change its verdict.
func (e *journalEvaluator) assertionDiagnostics(tx *ast.Transaction, postingIndex int, path string) []SourcedDiagnostic {
	var diagnostics []SourcedDiagnostic

	p := &tx.Postings[postingIndex]
	assertion := p.BalanceAssertion
	account := p.Account.GetResolvedName()
	commodity := assertion.Amount.Commodity.Symbol
	actual := e.balances.amount(account, commodity, assertion.IsInclusive)
	tolerance := toleranceForPrecision(decimalPrecision(assertion.Amount.Quantity))
	if e.tolerance.IsPositive() && e.tolerance.GreaterThan(tolerance) {
		tolerance = e.tolerance
	}

	difference := actual.Sub(assertion.Amount.Quantity)
	if difference.Abs().GreaterThan(tolerance) {
		message := fmt.Sprintf("balance assertion failed in %s: asserted %s %s, calculated %s %s (difference %s)",
			account,
			assertion.Amount.Quantity.String(), displayCommodity(commodity),
			actual.String(), displayCommodity(commodity),
			difference.String())
		if e.partialContext {
			message += "; this file is not included by a journal in the workspace, so only its own transactions were compared"
		}

		diagnostics = append(diagnostics, SourcedDiagnostic{
			Path:     path,
			Range:    assertion.Range,
			Severity: SeverityError,
			Code:     CodeBalanceAssertionFailed,
			Message:  message,
			Data: map[string]any{
				"kind":           "balanceAssertion",
				"account":        account,
				"commodity":      commodity,
				"expected":       assertion.Amount.Quantity.String(),
				"actual":         actual.String(),
				"strict":         assertion.IsStrict,
				"inclusive":      assertion.IsInclusive,
				"partialContext": e.partialContext,
			},
		})
		return diagnostics
	}

	if !assertion.IsStrict {
		return diagnostics
	}

	// `==` requires the account to hold nothing else. hledger compares every
	// other commodity with its own precision, so a small EUR balance still fails
	// a dollar assertion.
	if others := e.balances.otherCommodities(account, commodity, assertion.IsInclusive, e.tolerance); len(others) > 0 {
		diagnostics = append(diagnostics, SourcedDiagnostic{
			Path:     path,
			Range:    assertion.Range,
			Severity: SeverityError,
			Code:     CodeBalanceAssertionFailed,
			Message: fmt.Sprintf("balance assertion failed in %s: %s also holds %s",
				account, displayCommodity(commodity), strings.Join(others, ", ")),
			Data: map[string]any{
				"kind":      "balanceAssertion",
				"account":   account,
				"commodity": commodity,
				"expected":  assertion.Amount.Quantity.String(),
				"actual":    actual.String(),
				"strict":    true,
				"inclusive": assertion.IsInclusive,
			},
		})
	}

	return diagnostics
}

// postingGroups returns the posting indices of each group that must balance
// independently: real postings and balanced (bracketed) virtual postings.
// Unbalanced (parenthesised) virtual postings are exempt.
func postingGroups(postings []ast.Posting) [][]int {
	real := make([]int, 0, len(postings))
	balancedVirtual := make([]int, 0, len(postings))
	for i := range postings {
		switch postings[i].Virtual {
		case ast.VirtualNone:
			real = append(real, i)
		case ast.VirtualBalanced:
			balancedVirtual = append(balancedVirtual, i)
		}
	}
	groups := make([][]int, 0, 2)
	if len(real) > 0 {
		groups = append(groups, real)
	}
	if len(balancedVirtual) > 0 {
		groups = append(groups, balancedVirtual)
	}
	return groups
}

func groupContains(group []int, idx int) bool {
	for _, candidate := range group {
		if candidate == idx {
			return true
		}
	}
	return false
}

// synthesizedAmount turns an inferred mixed contribution into an amount for the
// transaction-level balance check.
func synthesizedAmount(effective postingEffective) *ast.Amount {
	commodities := make([]string, 0, len(effective.native))
	for commodity := range effective.native {
		commodities = append(commodities, commodity)
	}
	sort.Strings(commodities)

	commodity := ""
	if len(commodities) > 0 {
		commodity = commodities[0]
	}
	quantity := decimal.Zero
	for _, name := range commodities {
		quantity = quantity.Add(effective.native[name])
	}
	return &ast.Amount{Quantity: quantity, Commodity: ast.Commodity{Symbol: commodity}}
}

func postingRange(p *ast.Posting) ast.Range {
	if p.Range.Start.Line > 0 {
		return p.Range
	}
	return p.Account.Range
}

// compareDates orders two journal dates, treating an unset date as the earliest.
func compareDates(a, b ast.Date) int {
	switch {
	case a.Year != b.Year:
		return a.Year - b.Year
	case a.Month != b.Month:
		return a.Month - b.Month
	default:
		return a.Day - b.Day
	}
}

// formatDifferences renders residuals for the UNBALANCED message: sorted by
// commodity, signed, with a placeholder for commodity-less amounts.
func formatDifferences(differences map[string]decimal.Decimal) string {
	commodities := make([]string, 0, len(differences))
	for commodity := range differences {
		commodities = append(commodities, commodity)
	}
	sort.Strings(commodities)

	parts := make([]string, 0, len(commodities))
	for _, commodity := range commodities {
		parts = append(parts, fmt.Sprintf("%s off by %s", displayCommodity(commodity), differences[commodity].String()))
	}
	return strings.Join(parts, "; ")
}

func decimalStrings(differences map[string]decimal.Decimal) map[string]any {
	result := make(map[string]any, len(differences))
	for commodity, difference := range differences {
		result[displayCommodity(commodity)] = difference.String()
	}
	return result
}

func displayCommodity(commodity string) string {
	if commodity == "" {
		return "(no commodity)"
	}
	return commodity
}
