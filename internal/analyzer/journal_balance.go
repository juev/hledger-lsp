package analyzer

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
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
	// CodeOrderDependentBalance marks a transaction whose balance check hledger
	// decides by an order it does not define.
	CodeOrderDependentBalance = "ORDER_DEPENDENT_BALANCE"
)

// multipleInferredMessage keeps hledger's own hint: the usual cause is a single
// space between the account and the amount, which makes the account name absorb
// the amount.
const multipleInferredMessage = "posting has no amount (separate the account and amount with two or more spaces)"

// orderDependentBalanceMessage explains a balance check whose verdict depends on
// the order hledger sums subaccounts in.
const orderDependentBalanceMessage = "whether this transaction balances depends on the order hledger adds up subaccount balances: their costs are written with different decimal places, and hledger takes the tolerance from whichever comes first"

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

// otherCommodities returns what an account holds in commodities other than the
// asserted one, in a stable order, as hledger's `==` form checks it. The
// commodities checked are those the account itself has an entry in, even one that
// has netted to zero; for an inclusive assertion each is totalled with the
// subaccounts, so a subaccount can offset the account's own balance but a
// commodity only a subaccount holds is not looked at. Any non-zero balance
// counts, however small: assertions are exact.
func (b accountBalances) otherCommodities(account, except string, inclusive bool) []string {
	var result []string
	for commodity := range b[account] {
		quantity := b.amount(account, commodity, inclusive)
		if commodity == except || quantity.IsZero() {
			continue
		}
		result = append(result, fmt.Sprintf("%s %s", quantity.String(), displayCommodity(commodity)))
	}
	sort.Strings(result)
	return result
}

// heldCommodities returns the commodities an account (and, for inclusive reads,
// its subaccounts) has a balance entry in.
func (b accountBalances) heldCommodities(account string, inclusive bool) map[string]bool {
	held := make(map[string]bool)
	prefix := account + ":"
	for name, byCommodity := range b {
		if name != account && (!inclusive || !strings.HasPrefix(name, prefix)) {
			continue
		}
		for commodity := range byCommodity {
			held[commodity] = true
		}
	}
	return held
}

// settleAssignment leaves an account with the commodity entries hledger gives it
// after a balance assignment, once the assigned amount has been added. hledger
// replaces the account's own balance rather than adding to it: an inclusive
// assignment computes it from what the subaccounts hold, so the account gets an
// entry, possibly zero, in each of their commodities, and a strict one keeps no
// other entry, the assigned amount having emptied it. A strict assertion checks
// the commodities the account has an entry in, so these entries decide what a
// later one looks at.
func (b accountBalances) settleAssignment(account string, assertion *ast.BalanceAssertion) {
	keep := map[string]bool{assertion.Amount.Commodity.Symbol: true}
	if assertion.IsInclusive {
		prefix := account + ":"
		for name, byCommodity := range b {
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			for commodity := range byCommodity {
				keep[commodity] = true
				b.add(account, commodity, decimal.Zero)
			}
		}
	}
	if !assertion.IsStrict {
		return
	}
	for commodity := range b[account] {
		if !keep[commodity] {
			delete(b[account], commodity)
		}
	}
}

// pricedKey identifies an amount that was written with a cost the way hledger
// tells such amounts apart: by commodity, by the commodity of the cost, and by the
// unit price for @ or as one group for @@. Amounts under one key add up, while
// amounts under different keys stay apart even when their quantities cancel.
type pricedKey struct {
	commodity     string
	costCommodity string
	// unitPrice is the canonical text of the @ price, empty for @@.
	unitPrice string
	total     bool
}

// costKey returns the key an amount with the given cost is held under.
func costKey(amount ast.Amount, cost *ast.Cost) pricedKey {
	key := pricedKey{commodity: amount.Commodity.Symbol, costCommodity: cost.Amount.Commodity.Symbol, total: cost.IsTotal}
	if !cost.IsTotal {
		key.unitPrice = cost.Amount.Quantity.String()
	}
	return key
}

// pricedAmount is what an account holds under one pricedKey: the quantity, and
// what that quantity cost in the cost commodity.
type pricedAmount struct {
	quantity decimal.Decimal
	value    decimal.Decimal
	// places is the precision of the cost literal hledger keeps for the key:
	// that of the first amount added under it (sumSimilarAmountsUsingFirstCost).
	places int32
	// uncertain lists other precisions that literal may have. hledger sums the
	// subaccounts of an inclusive assignment in the order of an internal hash
	// table, so which subaccount's literal comes first cannot be told.
	uncertain []int32
}

// empty reports whether nothing is held under the key any more.
func (a pricedAmount) empty() bool {
	return a.quantity.IsZero() && a.value.IsZero()
}

// pricedBalances holds, per account, the part of its balance that keeps its
// cost. hledger drops the cost of postings in transactions without balance
// assignments before the running balance sees them, but keeps it for the
// postings of transactions with assignments. An assignment amount is the asserted
// balance minus the running balance with those costs intact, so the amount it
// infers carries them too.
type pricedBalances map[string]map[pricedKey]pricedAmount

// add adds an amount under a key. The key keeps the cost literal of the first
// amount added under it, even once the amounts cancel out: hledger keeps the
// emptied entry, and its literal, in the running balance. Only an assignment
// replaces it (settleAssignment).
func (b pricedBalances) add(account string, key pricedKey, amount pricedAmount) {
	if account == "" {
		return
	}
	byKey, ok := b[account]
	if !ok {
		byKey = make(map[pricedKey]pricedAmount)
		b[account] = byKey
	}
	held, ok := byKey[key]
	if !ok {
		held.places, held.uncertain = amount.places, amount.uncertain
	}
	held.quantity, held.value = held.quantity.Add(amount.quantity), held.value.Add(amount.value)
	byKey[key] = held
}

// settlePricedAssignment gives an account's priced balance the cost literals it
// has after a balance assignment. hledger does not add the change to the
// balance: it replaces the balance in the asserted commodity (in every
// commodity for ==) with the asserted amount, or for =* with the asserted
// amount less what the subaccounts hold. So a key the replacement leaves empty
// is gone, the asserted cost brings its own literal, and a key the subaccounts
// hold takes theirs.
func (e *journalEvaluator) settlePricedAssignment(account string, assertion *ast.BalanceAssertion) {
	byKey := e.priced[account]
	if len(byKey) == 0 {
		return
	}
	asserted, hasAsserted := assertionPricedPart(assertion)
	commodity := assertion.Amount.Commodity.Symbol
	for key, held := range byKey {
		if key.commodity != commodity && !assertion.IsStrict {
			continue
		}
		switch {
		case held.empty():
			delete(byKey, key)
			continue
		case hasAsserted && key == asserted.pricedKey:
			held.places, held.uncertain = asserted.places, nil
		case assertion.IsInclusive:
			if sub, ok := e.priced.collect(account, key.commodity, scopeSubaccounts)[key]; ok && !sub.empty() {
				held.places, held.uncertain = sub.places, sub.uncertain
			}
		}
		byKey[key] = held
	}
}

// pricedScope selects which accounts an assignment reads priced balances from.
type pricedScope int

const (
	scopeOwn pricedScope = iota
	scopeSubaccounts
	scopeSubtree
)

// collect sums the priced balances of the accounts in scope, per key and for one
// commodity.
//
// The cost literal of a key is the one hledger meets first. An inclusive
// assignment sums the subaccounts first, in an order hledger does not define,
// and takes the account's own balance off after them, so when subaccounts hold
// the key, their literals are the candidates, otherwise the account's own.
func (b pricedBalances) collect(account, commodity string, scope pricedScope) map[pricedKey]pricedAmount {
	collected := make(map[pricedKey]pricedAmount)
	ownPlaces := make(map[pricedKey]pricedAmount)
	subPlaces := make(map[pricedKey]map[int32]bool)
	prefix := account + ":"
	for name, byKey := range b {
		own := name == account
		sub := strings.HasPrefix(name, prefix)
		switch {
		case scope == scopeOwn && !own, scope == scopeSubaccounts && !sub, scope == scopeSubtree && !own && !sub:
			continue
		}
		for key, held := range byKey {
			if key.commodity != commodity {
				continue
			}
			sum := collected[key]
			sum.quantity, sum.value = sum.quantity.Add(held.quantity), sum.value.Add(held.value)
			collected[key] = sum
			// An emptied entry still holds its literal in hledger's running
			// balance, so it is a candidate like any other.
			if own {
				ownPlaces[key] = held
				continue
			}
			if subPlaces[key] == nil {
				subPlaces[key] = make(map[int32]bool)
			}
			subPlaces[key][held.places] = true
			for _, places := range held.uncertain {
				subPlaces[key][places] = true
			}
		}
	}
	for key, sum := range collected {
		if candidates, ok := subPlaces[key]; ok {
			sorted := slices.Sorted(maps.Keys(candidates))
			sum.places, sum.uncertain = sorted[0], sorted[1:]
		} else {
			sum.places, sum.uncertain = ownPlaces[key].places, ownPlaces[key].uncertain
		}
		collected[key] = sum
	}
	return collected
}

// pricedPart is the portion of an inferred amount that carries a cost.
type pricedPart struct {
	pricedKey
	quantity decimal.Decimal
	value    decimal.Decimal
	// places and uncertain are the precision of its cost literal, as in
	// pricedAmount.
	places    int32
	uncertain []int32
}

// amount returns the part as an amount held under its key.
func (p pricedPart) amount() pricedAmount {
	return pricedAmount{quantity: p.quantity, value: p.value, places: p.places, uncertain: p.uncertain}
}

// journalEvaluator carries the mutable state of one journal evaluation.
type journalEvaluator struct {
	balances        accountBalances
	priced          pricedBalances
	tolerance       decimal.Decimal
	events          []postingBalanceEvent
	trackAssertions bool
	// partialContext qualifies assertion verdicts that were computed from one
	// file's transactions only.
	partialContext bool
	// observe, when set, receives each posting's contribution as the replay
	// applies it, and whether it carries a cost.
	observe func(sourceOrder, postingIndex int, contribution map[string]decimal.Decimal, priced bool)
	// dates holds the register dates of each transaction's postings once worked out.
	dates map[*ast.Transaction][]ast.Date
}

// postingEffective is the amount a posting contributes to the journal once
// hledger's inference rules have been applied: the written amount, or the amount
// inferred for an assertion-only posting.
type postingEffective struct {
	native    map[string]decimal.Decimal
	precision int32
	// priced is the part of an assigned amount that carries a cost, already
	// counted in native.
	priced []pricedPart
}

// postingBalanceEvent is one step of the date-ordered replay: a single posting
// whose contribution is already known, or, when assignment is set, a whole
// transaction that needs the running balance to infer its amounts.
type postingBalanceEvent struct {
	date         ast.Date
	tx           *ast.Transaction
	path         string
	sourceOrder  int
	postingIndex int
	contribution map[string]decimal.Decimal
	assignment   bool
}

// CheckJournalBalance evaluates hledger's balance rules and balance assertions
// over a resolved journal.
//
// It follows hledger's two steps. Transactions without balance assignments are
// balanced up front and split into postings. Those postings and the remaining
// transactions are then replayed in date order against one running balance, with
// parse order as the tie breaker: a posting at its primary posting date, and a
// transaction with an assignment as one unit at its transaction date, whatever
// dates its other postings carry. A posting's date2: does not affect this pass.
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

	evaluator := &journalEvaluator{
		balances:        accountBalances{},
		priced:          pricedBalances{},
		tolerance:       userTolerance,
		trackAssertions: trackAssertions,
		partialContext:  opts.PartialContext,
	}
	return evaluator.run(sourced)
}

// run balances the transactions up front and replays them in date order.
func (e *journalEvaluator) run(sourced []include.SourcedTransaction) []SourcedDiagnostic {
	ordered := make([]int, len(sourced))
	for i := range sourced {
		ordered[i] = i
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return compareDates(sourced[ordered[i]].Transaction.Date, sourced[ordered[j]].Transaction.Date) < 0
	})

	var diagnostics []SourcedDiagnostic
	for _, index := range ordered {
		tx, path := &sourced[index].Transaction, sourced[index].Path
		if HasBalanceAssignment(tx) {
			e.deferAssignments(tx, path, index)
			continue
		}
		diagnostics = append(diagnostics, e.evaluate(tx, path, false, func(postingIndex int, contribution map[string]decimal.Decimal, _ bool) {
			e.recordEvent(tx, path, index, postingIndex, contribution)
		})...)
	}
	return append(diagnostics, e.replay()...)
}

// HasBalanceAssignment reports whether a posting's amount must be computed from
// the running balance.
func HasBalanceAssignment(tx *ast.Transaction) bool {
	for i := range tx.Postings {
		if tx.Postings[i].Amount == nil && tx.Postings[i].BalanceAssertion != nil {
			return true
		}
	}
	return false
}

// HasCheckedBalanceAssignment reports whether a balance assignment sits in a
// posting group that must balance (real or balanced-virtual). An assignment on an
// unbalanced-virtual posting is exempt from the balance check, so it does not
// make the transaction's balance depend on the running balance.
func HasCheckedBalanceAssignment(tx *ast.Transaction) bool {
	for i := range tx.Postings {
		p := &tx.Postings[i]
		if p.Amount == nil && p.BalanceAssertion != nil && (p.Virtual == ast.VirtualNone || p.Virtual == ast.VirtualBalanced) {
			return true
		}
	}
	return false
}

// evaluate infers a transaction's amounts and checks that it balances, passing
// each posting's contribution to emit. Written and assigned amounts are emitted
// in date order, parse order within a date, and elided ones last, because
// assigned amounts must see the postings before them: a posting dated before its
// transaction reaches an assignment written above it, and one dated after does
// not reach an assignment written below it. Assignments read e.balances, so a
// transaction that has them may only be evaluated from replay. Replay passes
// carryCosts for those transactions, whose postings keep their cost in the
// running balance.
func (e *journalEvaluator) evaluate(tx *ast.Transaction, path string, carryCosts bool, emit func(postingIndex int, contribution map[string]decimal.Decimal, priced bool)) []SourcedDiagnostic {
	effective := make([]postingEffective, len(tx.Postings))
	var diagnostics []SourcedDiagnostic

	groups := postingGroups(tx.Postings)

	// hledger infers at most one amount per balancing group, and parenthesised
	// virtual postings are never inferred, so the candidates are counted per
	// group rather than per transaction.
	elidedByGroup := make(map[int]int) // group index -> posting index
	inferredPerGroup := make(map[int]int)

	for _, i := range e.postingsInDateOrder(tx) {
		p := &tx.Postings[i]
		switch {
		case p.Amount != nil:
			effective[i] = postingEffective{
				native:    map[string]decimal.Decimal{p.Amount.Commodity.Symbol: p.Amount.Quantity},
				precision: decimalPrecision(p.Amount.Quantity),
			}
			if carryCosts && p.Cost != nil {
				if _, value, ok := balanceSumContribution(*p); ok {
					e.priced.add(p.Account.GetResolvedName(), costKey(*p.Amount, p.Cost), pricedAmount{
						quantity: p.Amount.Quantity,
						value:    value,
						places:   decimalPrecision(p.Cost.Amount.Quantity),
					})
				}
			}
		case p.BalanceAssertion != nil:
			native, priced := e.assignedContribution(p)
			effective[i] = postingEffective{
				native:    native,
				priced:    priced,
				precision: decimalPrecision(p.BalanceAssertion.Amount.Quantity),
			}
			for _, part := range priced {
				e.priced.add(p.Account.GetResolvedName(), part.pricedKey, part.amount())
			}
			e.settlePricedAssignment(p.Account.GetResolvedName(), p.BalanceAssertion)
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

		emit(i, effective[i].native, len(effective[i].priced) > 0)
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
		emit(index, e.elidedContribution(tx, effective, index, groups[group]), false)
	}

	diagnostics = append(diagnostics, e.balanceDiagnostics(tx, path, effective, elidedByGroup, groups)...)

	return diagnostics
}

// assignedContribution returns the amount hledger infers for an assertion-only
// posting: what moves the account to the asserted balance. A strict assignment
// also replaces the account's other commodities, leaving nothing in them, and an
// inclusive one offsets what subaccounts hold in them, so the amount can span
// several commodities. The second result is the part of that amount that carries
// a cost, because the balance it replaces held amounts that kept theirs.
func (e *journalEvaluator) assignedContribution(p *ast.Posting) (map[string]decimal.Decimal, []pricedPart) {
	assertion := p.BalanceAssertion
	account := p.Account.GetResolvedName()
	commodity := assertion.Amount.Commodity.Symbol

	prior := e.balances.amount(account, commodity, assertion.IsInclusive)
	contribution := map[string]decimal.Decimal{commodity: assertion.Amount.Quantity.Sub(prior)}

	scope := scopeOwn
	if assertion.IsInclusive {
		scope = scopeSubtree
	}
	// hledger subtracts the balance from the asserted amount, so a cost written
	// on the assertion is the first literal of its key.
	var priced []pricedPart
	if assertedPart, ok := assertionPricedPart(assertion); ok {
		priced = append(priced, assertedPart)
	}
	priced = append(priced, e.pricedParts(account, commodity, scope)...)

	for other := range e.balances.heldCommodities(account, assertion.IsInclusive) {
		if other == commodity {
			continue
		}
		own := e.balances.amount(account, other, false)
		subaccounts := e.balances.amount(account, other, assertion.IsInclusive).Sub(own)

		target := own.Sub(subaccounts)
		otherScope := scopeSubaccounts
		switch {
		case assertion.IsStrict:
			target = subaccounts.Neg()
			otherScope = scopeOwn
			if assertion.IsInclusive {
				otherScope = scopeSubtree
			}
		case !assertion.IsInclusive:
			// A plain assertion leaves the account's other commodities alone.
			continue
		}
		// The quantity can net to zero while a priced part remains (a posting
		// at one price offset by another at a different price), and that part
		// still moves the transaction's sum.
		parts := e.pricedParts(account, other, otherScope)
		if change := target.Sub(own); !change.IsZero() || len(parts) > 0 {
			contribution[other] = change
			priced = append(priced, parts...)
		}
	}
	return contribution, mergePricedParts(priced)
}

// assertionPricedPart returns the asserted amount as a priced part when the
// assertion carries a cost. hledger gives the inferred posting that cost, and it
// covers the whole asserted amount rather than only the change, while the balance
// it replaces is taken out at the costs it was held at.
func assertionPricedPart(assertion *ast.BalanceAssertion) (pricedPart, bool) {
	if assertion.Cost == nil {
		return pricedPart{}, false
	}
	_, value, ok := balanceSumContribution(ast.Posting{Amount: &assertion.Amount, Cost: assertion.Cost})
	if !ok {
		return pricedPart{}, false
	}
	return pricedPart{
		pricedKey: costKey(assertion.Amount, assertion.Cost),
		quantity:  assertion.Amount.Quantity,
		value:     value,
		places:    decimalPrecision(assertion.Cost.Amount.Quantity),
	}, true
}

// mergePricedParts sums parts that share a key and drops those that cancel out,
// so an assignment that restates the balance an account already holds at the same
// cost has no priced part left and moves nothing. Parts at different prices stay
// apart, as they do in hledger.
func mergePricedParts(parts []pricedPart) []pricedPart {
	if len(parts) == 0 {
		return parts
	}
	// The first part of a key gives the merged part its cost literal.
	sums := make(map[pricedKey]pricedAmount, len(parts))
	for _, part := range parts {
		sum, ok := sums[part.pricedKey]
		if !ok {
			sum.places, sum.uncertain = part.places, part.uncertain
		}
		sum.quantity, sum.value = sum.quantity.Add(part.quantity), sum.value.Add(part.value)
		sums[part.pricedKey] = sum
	}
	merged := make([]pricedPart, 0, len(sums))
	for key, sum := range sums {
		if sum.empty() {
			continue
		}
		merged = append(merged, pricedPart{pricedKey: key, quantity: sum.quantity, value: sum.value, places: sum.places, uncertain: sum.uncertain})
	}
	sort.Slice(merged, func(i, j int) bool {
		return lessPricedKey(merged[i].pricedKey, merged[j].pricedKey)
	})
	return merged
}

func lessPricedKey(a, b pricedKey) bool {
	switch {
	case a.commodity != b.commodity:
		return a.commodity < b.commodity
	case a.costCommodity != b.costCommodity:
		return a.costCommodity < b.costCommodity
	case a.total != b.total:
		return !a.total
	}
	return a.unitPrice < b.unitPrice
}

// pricedParts returns the negation of the priced balances an assignment replaces
// in one commodity, in a stable order.
func (e *journalEvaluator) pricedParts(account, commodity string, scope pricedScope) []pricedPart {
	held := e.priced.collect(account, commodity, scope)
	parts := make([]pricedPart, 0, len(held))
	for key, amount := range held {
		if amount.quantity.IsZero() && amount.value.IsZero() {
			continue
		}
		parts = append(parts, pricedPart{pricedKey: key, quantity: amount.quantity.Neg(), value: amount.value.Neg(), places: amount.places, uncertain: amount.uncertain})
	}
	sort.Slice(parts, func(i, j int) bool { return lessPricedKey(parts[i].pricedKey, parts[j].pricedKey) })
	return parts
}

// deferAssignments schedules a transaction with balance assignments for replay
// at its transaction date.
func (e *journalEvaluator) deferAssignments(tx *ast.Transaction, path string, sourceOrder int) {
	e.events = append(e.events, postingBalanceEvent{
		date:        tx.Date,
		tx:          tx,
		path:        path,
		sourceOrder: sourceOrder,
		assignment:  true,
	})
}

func (e *journalEvaluator) recordEvent(tx *ast.Transaction, path string, sourceOrder, postingIndex int, contribution map[string]decimal.Decimal) {
	if !e.trackAssertions {
		return
	}
	e.events = append(e.events, postingBalanceEvent{
		// A transaction without balance assignments lists every posting at
		// its own date, so the register date is the primary posting date.
		date:         e.registerDates(tx)[postingIndex],
		tx:           tx,
		path:         path,
		sourceOrder:  sourceOrder,
		postingIndex: postingIndex,
		contribution: contribution,
	})
}

// replay walks the events in date order with one running balance, evaluating
// transactions with assignments on the way and checking every assertion against
// the balance as it stands after its posting.
func (e *journalEvaluator) replay() []SourcedDiagnostic {
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

	var balanceDiagnostics, assertionDiagnostics []SourcedDiagnostic
	for _, event := range e.events {
		step := func(postingIndex int, contribution map[string]decimal.Decimal, priced bool) {
			posting := &event.tx.Postings[postingIndex]
			e.apply(posting.Account.GetResolvedName(), contribution)
			if posting.Amount == nil && posting.BalanceAssertion != nil {
				e.balances.settleAssignment(posting.Account.GetResolvedName(), posting.BalanceAssertion)
			}
			if e.observe != nil {
				e.observe(event.sourceOrder, postingIndex, contribution, priced)
			}
			if posting.BalanceAssertion != nil {
				assertionDiagnostics = append(assertionDiagnostics, e.assertionDiagnostics(event.tx, postingIndex, event.path)...)
			}
		}
		if event.assignment {
			balanceDiagnostics = append(balanceDiagnostics, e.evaluate(event.tx, event.path, true, step)...)
			continue
		}
		step(event.postingIndex, event.contribution, false)
	}
	return append(balanceDiagnostics, assertionDiagnostics...)
}

// postingsInDateOrder returns the indices of a transaction's postings ordered by
// date, keeping parse order within a date.
func (e *journalEvaluator) postingsInDateOrder(tx *ast.Transaction) []int {
	order := make([]int, len(tx.Postings))
	dates := e.registerDates(tx)
	for i := range tx.Postings {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return compareDates(dates[order[i]], dates[order[j]]) < 0
	})
	return order
}

// registerDates returns the date each posting of a transaction is listed at,
// worked out once per transaction and kept for the rest of the run.
func (e *journalEvaluator) registerDates(tx *ast.Transaction) []ast.Date {
	if dates, ok := e.dates[tx]; ok {
		return dates
	}
	dates := registerDates(tx)
	if e.dates == nil {
		e.dates = make(map[*ast.Transaction][]ast.Date)
	}
	e.dates[tx] = dates
	return dates
}

// registerDates returns the date each posting of a transaction is listed at: its
// date: or bracketed-date override, else the transaction date. A yearless value
// inherits the transaction year; an invalid one has its own diagnostic and falls
// back to the transaction date. hledger refuses date: on an assignment posting,
// so that one has no date of its own to honour.
func registerDates(tx *ast.Transaction) []ast.Date {
	dates := make([]ast.Date, len(tx.Postings))
	lines := postingCommentLines(tx.Postings, tx.Comments)
	for i := range tx.Postings {
		posting := &tx.Postings[i]
		dates[i] = tx.Date
		if posting.Amount == nil && posting.BalanceAssertion != nil {
			continue
		}
		if date, ok := postingDateOverride(tx, posting, lines[i]); ok {
			dates[i] = date
		}
	}
	return dates
}

// postingDateOverride returns the date a posting carries of its own. hledger
// reads the comment of the posting line and then the comment lines below it, each
// on its own, and takes the first date it meets: a date: tag, or the primary date
// of a bracketed date. A date: value is read up to its first character that
// cannot belong to a date, while a bracketed date must be the whole bracket, so
// [ 2024-01-20 ] is not one. Names are case sensitive, so Date: is an ordinary
// tag.
func postingDateOverride(tx *ast.Transaction, posting *ast.Posting, lines []ast.Comment) (ast.Date, bool) {
	if date, ok := commentDate(posting.Comment, tx.Date); ok {
		return date, true
	}
	for _, line := range lines {
		if date, ok := commentDate(line.Text, tx.Date); ok {
			return date, true
		}
	}
	return ast.Date{}, false
}

// commentDate returns the first date written in one comment line: the earlier of
// its date: tag and its first bracketed date. A date without a year takes the
// year of the transaction date.
func commentDate(comment string, transaction ast.Date) (ast.Date, bool) {
	best, bestAt, found := ast.Date{}, 0, false
	consider := func(date ast.Date, at int) {
		if !found || at < bestAt {
			best, bestAt, found = date, at, true
		}
	}

	for _, tag := range parser.ScanPostingCommentTags(comment) {
		if tag.Name != "date" {
			continue
		}
		if date, ok := parsePostingDateValue(leadingDateText(tag.Value), transaction); ok {
			consider(date, tag.NameStart)
		}
	}

	for start := 0; start < len(comment); start++ {
		if comment[start] != '[' {
			continue
		}
		end := strings.IndexByte(comment[start+1:], ']')
		if end < 0 {
			break
		}
		// hledger reads a bracket as a date only by the grammar validateBracketDates
		// checks; any other bracket, such as [2024-01-05=garbage], is text.
		content := comment[start+1 : start+1+end]
		if !isBracketedDate(content) {
			continue
		}
		primary, _, _ := strings.Cut(content, "=")
		if _, ok := bracketDateYear(primary, transaction.CalendarYear()); !ok {
			continue
		}
		if date, ok := parsePostingDateValue(primary, transaction); ok {
			consider(date, start)
			break
		}
	}
	return best, found
}

// postingCommentLines returns, for each posting, the ; lines among comments that
// are written under it, before the next posting. The parser keeps these lines
// with the transaction; lines above the first posting belong to none. Postings
// and comments are in source order, so one pass over both assigns every line.
func postingCommentLines(postings []ast.Posting, comments []ast.Comment) [][]ast.Comment {
	owned := make([][]ast.Comment, len(postings))
	if len(comments) == 0 || len(postings) == 0 {
		return owned
	}
	if !slices.IsSortedFunc(comments, func(a, b ast.Comment) int {
		return cmp.Compare(a.Range.Start.Line, b.Range.Start.Line)
	}) {
		comments = slices.Clone(comments)
		slices.SortStableFunc(comments, func(a, b ast.Comment) int {
			return cmp.Compare(a.Range.Start.Line, b.Range.Start.Line)
		})
	}
	owner := -1
	for _, line := range comments {
		at := line.Range.Start.Line
		for owner+1 < len(postings) && postingRange(&postings[owner+1]).Start.Line < at {
			owner++
		}
		if line.Marker != ';' || owner < 0 {
			continue
		}
		if owner+1 < len(postings) && postingRange(&postings[owner+1]).Start.Line == at {
			continue
		}
		owned[owner] = append(owned[owner], line)
	}
	return owned
}

// leadingDateText returns the part of a tag value that reads as a date: digits
// joined by -, / or ., after any leading spaces.
func leadingDateText(value string) string {
	value = strings.TrimLeft(value, " \t")
	end := 0
	for end < len(value) && (value[end] >= '0' && value[end] <= '9' || value[end] == '-' || value[end] == '/' || value[end] == '.') {
		end++
	}
	return strings.TrimRight(value[:end], "-/.")
}

// parsePostingDateValue reads a posting date. A value without a year takes the
// year of the transaction date.
func parsePostingDateValue(value string, transaction ast.Date) (ast.Date, bool) {
	value = strings.TrimSpace(value)
	separator := strings.IndexAny(value, "-/.")
	if separator < 0 {
		return ast.Date{}, false
	}
	parts := strings.Split(value, string(value[separator]))
	if len(parts) != 2 && len(parts) != 3 {
		return ast.Date{}, false
	}
	date := ast.Date{Year: transaction.Year, YearDigits: transaction.YearDigits}
	if len(parts) == 3 {
		// hledger reads a year of any length.
		year, digits, ok := ast.ParseYear(parts[0])
		if !ok {
			return ast.Date{}, false
		}
		date.Year, date.YearDigits = year, digits
		parts = parts[1:]
	}
	month, monthErr := strconv.Atoi(parts[0])
	day, dayErr := strconv.Atoi(parts[1])
	if monthErr != nil || dayErr != nil || !parser.IsValidCalendarDate(date.CalendarYear(), month, day) {
		return ast.Date{}, false
	}
	date.Month, date.Day = month, day
	return date, true
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
	lenient := e.balanceDiagnosticsWith(tx, path, effective, elidedByGroup, groups, placesLenient)
	if !hasUncertainPlaces(effective) {
		return lenient
	}
	// Some cost literal comes from whichever subaccount hledger's hash table
	// lists first. When the finest and the coarsest of the literals it can be
	// give different verdicts, the verdict depends on that order, which hledger
	// does not define, so it is reported as such rather than as a definite
	// error or a silent pass.
	strict := e.balanceDiagnosticsWith(tx, path, effective, elidedByGroup, groups, placesStrict)
	if len(strict) == len(lenient) {
		return lenient
	}
	return append(lenient, SourcedDiagnostic{
		Path:     path,
		Range:    tx.Range,
		Severity: SeverityWarning,
		Code:     CodeOrderDependentBalance,
		Message:  orderDependentBalanceMessage,
		Data:     map[string]any{"kind": "orderDependentBalance"},
	})
}

// hasUncertainPlaces reports whether a posting of a transaction carries a cost
// whose literal hledger may take from more than one subaccount.
func hasUncertainPlaces(effective []postingEffective) bool {
	for _, posting := range effective {
		for _, part := range posting.priced {
			if len(part.uncertain) > 0 {
				return true
			}
		}
	}
	return false
}

// placesChoice picks, for a cost literal that can have one of several
// precisions, the coarsest (lenient: the widest tolerance) or the finest.
type placesChoice int

const (
	placesLenient placesChoice = iota
	placesStrict
)

func (c placesChoice) of(places int32, uncertain []int32) int32 {
	chosen := places
	for _, other := range uncertain {
		if (c == placesLenient && other < chosen) || (c == placesStrict && other > chosen) {
			chosen = other
		}
	}
	return chosen
}

func (e *journalEvaluator) balanceDiagnosticsWith(tx *ast.Transaction, path string, effective []postingEffective, elidedByGroup map[int]int, groups [][]int, choice placesChoice) []SourcedDiagnostic {
	var diagnostics []SourcedDiagnostic

	type groupPostings struct {
		postings []ast.Posting
		multi    []bool
	}
	built := make([]groupPostings, len(groups))
	for group, indices := range groups {
		elidedIdx, hasElided := elidedByGroup[group]
		postings := make([]ast.Posting, 0, len(indices))
		var multi []bool
		for _, idx := range indices {
			if hasElided && idx == elidedIdx {
				continue
			}
			// Assertion-only postings are balanced by construction; their
			// inferred amount is what keeps the group in balance.
			before := len(postings)
			postings = appendEffectivePosting(postings, tx.Postings[idx], effective[idx], choice)
			for len(multi) < before {
				multi = append(multi, false)
			}
			mixed := tx.Postings[idx].Amount == nil && nonZeroCommodities(effective[idx].native) > 1
			for len(multi) < len(postings) {
				multi = append(multi, mixed)
			}
		}
		built[group] = groupPostings{postings, multi}
	}

	// The tolerance of a commodity comes from every amount written in it in the
	// transaction, not only from those of the group being checked, and the amount
	// hledger infers for an elided posting counts as written.
	var written []ast.Posting
	elided := make(map[int]bool, len(elidedByGroup))
	for _, idx := range elidedByGroup {
		elided[idx] = true
	}
	for idx := range tx.Postings {
		if !elided[idx] {
			written = appendEffectivePosting(written, tx.Postings[idx], effective[idx], choice)
		}
	}
	for group := range elidedByGroup {
		written = append(written, elidedAmountPostings(built[group].postings)...)
	}

	for group := range groups {
		_, hasElided := elidedByGroup[group]
		postings, multi := built[group].postings, built[group].multi

		if hasElided {
			// Exactly one posting is elided in this group, so it absorbs the
			// residual and the group is balanced by construction.
			continue
		}

		result := checkPostingGroupMixed(postings, multi, maxPrecisionByCommodity(written, postings), e.tolerance)
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

// nonZeroCommodities counts the commodities an inferred amount actually moves.
func nonZeroCommodities(amount map[string]decimal.Decimal) int {
	count := 0
	for _, quantity := range amount {
		if !quantity.IsZero() {
			count++
		}
	}
	return count
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
		postings = appendEffectivePosting(postings, tx.Postings[idx], effective[idx], placesLenient)
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

	// hledger compares an assertion with the calculated balance exactly: neither
	// the precision of the asserted amount, nor the commodity style, nor the
	// balance tolerance of transactions widens it.
	difference := actual.Sub(assertion.Amount.Quantity)
	if !difference.IsZero() {
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

	// `==` requires the account to hold nothing else, so even a small EUR balance
	// fails a dollar assertion.
	if others := e.balances.otherCommodities(account, commodity, assertion.IsInclusive); len(others) > 0 {
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

// appendEffectivePosting appends a posting for the transaction-level balance
// check. An inferred amount can be mixed, so it becomes one posting per commodity,
// and the part of it that carries a cost becomes a posting with that cost.
func appendEffectivePosting(postings []ast.Posting, posting ast.Posting, effective postingEffective, choice placesChoice) []ast.Posting {
	if posting.Amount != nil {
		return append(postings, posting)
	}

	commodities := make([]string, 0, len(effective.native))
	for commodity := range effective.native {
		commodities = append(commodities, commodity)
	}
	sort.Strings(commodities)

	for _, commodity := range commodities {
		plain := effective.native[commodity]
		var parts []pricedPart
		for _, part := range effective.priced {
			if part.commodity == commodity {
				plain = plain.Sub(part.quantity)
				parts = append(parts, part)
			}
		}

		if !plain.IsZero() || len(parts) == 0 {
			posting.Amount = &ast.Amount{Quantity: plain, Commodity: ast.Commodity{Symbol: commodity}}
			posting.Cost = nil
			postings = append(postings, posting)
		}
		for _, part := range parts {
			postings = append(postings, pricedPosting(posting, part, choice))
		}
	}
	return postings
}

// pricedPosting renders a priced part as a posting that keeps its cost, so the
// balance check sees that the transaction names a conversion rate. An @ part is
// written at its unit price. An @@ part has no price of its own: its quantity and
// its total cost can differ in sign or the quantity can be zero once parts are
// added, so the total is stored with the sign that makes the posting convert to
// exactly the part's value.
func pricedPosting(posting ast.Posting, part pricedPart, choice placesChoice) ast.Posting {
	posting.Amount = &ast.Amount{Quantity: part.quantity, Commodity: ast.Commodity{Symbol: part.commodity}}
	places := choice.of(part.places, part.uncertain)
	cost := &ast.Cost{Amount: ast.Amount{Commodity: ast.Commodity{Symbol: part.costCommodity}}, Places: &places}
	if part.total {
		cost.IsTotal = true
		cost.Amount.Quantity = part.value
		if part.quantity.IsNegative() {
			cost.Amount.Quantity = part.value.Neg()
		}
	} else {
		cost.Amount.Quantity, _ = decimal.NewFromString(part.unitPrice)
	}
	posting.Cost = cost
	return posting
}

func postingRange(p *ast.Posting) ast.Range {
	if p.Range.Start.Line > 0 {
		return p.Range
	}
	return p.Account.Range
}

// compareDates orders two journal dates, treating an unset date as the earliest.
func compareDates(a, b ast.Date) int {
	return a.Compare(b)
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
