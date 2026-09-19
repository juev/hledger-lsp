package analyzer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/include"
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

// otherCommodities returns the non-zero balances held by an account (and, for
// inclusive assertions, its subaccounts) except one commodity, in a stable
// order. hledger's `==` form requires these to be empty.
func (b accountBalances) otherCommodities(account, except string, inclusive bool, tolerance decimal.Decimal) []string {
	var result []string
	seen := make(map[string]bool)
	prefix := account + ":"
	for name, byCommodity := range b {
		if name != account && (!inclusive || !strings.HasPrefix(name, prefix)) {
			continue
		}
		for commodity, quantity := range byCommodity {
			if commodity == except || quantity.Abs().LessThanOrEqual(tolerance) || seen[commodity] {
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
	balances  accountBalances
	tolerance decimal.Decimal
}

// postingEffective is the amount a posting contributes to the journal once
// hledger's inference rules have been applied: the written amount, or the amount
// inferred for an assertion-only posting.
type postingEffective struct {
	native    map[string]decimal.Decimal
	precision int32
}

// CheckJournalBalance evaluates hledger's balance rules and balance assertions
// over a resolved journal.
//
// hledger processes a journal in date order, so an assertion sees every posting
// dated before it regardless of which file contains it. The evaluation therefore
// walks transactions sorted by (date, tree order) and keeps running per-account
// balances, which also lets an amount-less posting that carries a balance
// assertion take the amount hledger infers for it (asserted minus the account's
// balance at that point).
//
// Deliberate simplification: postings are ordered by their transaction date, and
// posting-level `date:`/`date2:` tags do not move a posting between transactions.
// hledger reorders individual postings; journals that rely on that remain rare,
// and treating the transaction as the unit keeps the pass linear.
func CheckJournalBalance(resolved *include.ResolvedJournal, userTolerance decimal.Decimal) []SourcedDiagnostic {
	if resolved == nil {
		return nil
	}
	sourced := resolved.TransactionsWithSource()
	if len(sourced) == 0 {
		return nil
	}

	ordered := make([]include.SourcedTransaction, len(sourced))
	copy(ordered, sourced)
	sort.SliceStable(ordered, func(i, j int) bool {
		return compareDates(ordered[i].Transaction.Date, ordered[j].Transaction.Date) < 0
	})

	evaluator := &journalEvaluator{balances: accountBalances{}, tolerance: userTolerance}

	var diagnostics []SourcedDiagnostic
	for i := range ordered {
		diagnostics = append(diagnostics, evaluator.evaluate(&ordered[i].Transaction, ordered[i].Path)...)
	}
	return diagnostics
}

func (e *journalEvaluator) evaluate(tx *ast.Transaction, path string) []SourcedDiagnostic {
	effective := make([]postingEffective, len(tx.Postings))
	elidedIdx := -1
	elidedCount := 0

	// Written order matters: an assertion-only posting takes the amount that
	// makes its assertion hold against the balance including the postings
	// written before it, exactly like hledger.
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
			elidedIdx = i
			elidedCount++
			continue
		}
		e.apply(p.Account.GetResolvedName(), effective[i].native)
	}

	var diagnostics []SourcedDiagnostic
	if elidedCount > 1 {
		// hledger cannot infer more than one amount, so the transaction has no
		// balance to check and its assertions are not evaluated either.
		for i := range tx.Postings {
			p := &tx.Postings[i]
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

	if elidedIdx >= 0 {
		elided := tx.Postings[elidedIdx]
		e.apply(elided.Account.GetResolvedName(), e.elidedContribution(tx, effective, elidedIdx))
	}
	diagnostics = append(diagnostics, e.balanceDiagnostics(tx, path, effective, elidedIdx)...)

	return append(diagnostics, e.assertionDiagnostics(tx, path)...)
}

func (e *journalEvaluator) apply(account string, contribution map[string]decimal.Decimal) {
	for commodity, quantity := range contribution {
		e.balances.add(account, commodity, quantity)
	}
}

// balanceDiagnostics checks the two posting groups of a transaction against
// hledger's balance rules and returns transaction-level diagnostics.
func (e *journalEvaluator) balanceDiagnostics(tx *ast.Transaction, path string, effective []postingEffective, elidedIdx int) []SourcedDiagnostic {
	var diagnostics []SourcedDiagnostic

	for _, group := range postingGroups(tx.Postings) {
		postings := make([]ast.Posting, 0, len(group))
		for _, idx := range group {
			if idx == elidedIdx {
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

		if elidedIdx >= 0 && groupContains(group, elidedIdx) {
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
func (e *journalEvaluator) elidedContribution(tx *ast.Transaction, effective []postingEffective, elidedIdx int) map[string]decimal.Decimal {
	contribution := make(map[string]decimal.Decimal)
	if elidedIdx < 0 {
		return contribution
	}

	for _, group := range postingGroups(tx.Postings) {
		if !groupContains(group, elidedIdx) {
			continue
		}
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
	}
	return contribution
}

// assertionDiagnostics checks every balance assertion against the balances that
// include the transaction it belongs to, matching hledger (the asserted posting
// counts towards its own assertion).
func (e *journalEvaluator) assertionDiagnostics(tx *ast.Transaction, path string) []SourcedDiagnostic {
	var diagnostics []SourcedDiagnostic

	for i := range tx.Postings {
		p := &tx.Postings[i]
		if p.BalanceAssertion == nil {
			continue
		}

		assertion := p.BalanceAssertion
		account := p.Account.GetResolvedName()
		commodity := assertion.Amount.Commodity.Symbol
		actual := e.balances.amount(account, commodity, assertion.IsInclusive)
		tolerance := toleranceForPrecision(decimalPrecision(assertion.Amount.Quantity))
		if e.tolerance.IsPositive() && e.tolerance.GreaterThan(tolerance) {
			tolerance = e.tolerance
		}

		difference := actual.Sub(assertion.Amount.Quantity)
		if !difference.Abs().LessThanOrEqual(tolerance) {
			diagnostics = append(diagnostics, SourcedDiagnostic{
				Path:     path,
				Range:    assertion.Range,
				Severity: SeverityError,
				Code:     CodeBalanceAssertionFailed,
				Message: fmt.Sprintf("balance assertion failed in %s: asserted %s %s, calculated %s %s (difference %s)",
					account,
					assertion.Amount.Quantity.String(), displayCommodity(commodity),
					actual.String(), displayCommodity(commodity),
					difference.String()),
				Data: map[string]any{
					"kind":      "balanceAssertion",
					"account":   account,
					"commodity": commodity,
					"expected":  assertion.Amount.Quantity.String(),
					"actual":    actual.String(),
					"strict":    assertion.IsStrict,
					"inclusive": assertion.IsInclusive,
				},
			})
			continue
		}

		if !assertion.IsStrict {
			continue
		}

		if others := e.balances.otherCommodities(account, commodity, assertion.IsInclusive, tolerance); len(others) > 0 {
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
