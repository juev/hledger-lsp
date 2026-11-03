package analyzer

import (
	"github.com/shopspring/decimal"

	"github.com/juev/hledger-lsp/internal/ast"
)

func CheckBalance(tx *ast.Transaction, userTolerance decimal.Decimal) *BalanceResult {
	result := NewBalanceResult()

	realPostings, balancedVirtual := groupPostings(tx.Postings)

	written := tx.Postings
	for _, group := range [][]ast.Posting{realPostings, balancedVirtual} {
		if count, _ := countInferredPostings(group); count == 1 {
			written = append(written[:len(written):len(written)], elidedAmountPostings(group)...)
		}
	}
	realGroup := checkPostingGroup(realPostings, maxPrecisionByCommodity(written, realPostings), userTolerance)
	virtualGroup := checkPostingGroup(balancedVirtual, maxPrecisionByCommodity(written, balancedVirtual), userTolerance)

	if realGroup.multipleInferred || virtualGroup.multipleInferred {
		result.Balanced = false
		return result
	}

	result.InferredIdx = realGroup.inferredIdx
	if result.InferredIdx < 0 {
		result.InferredIdx = virtualGroup.inferredIdx
	}

	for commodity, diff := range realGroup.differences {
		result.Differences[commodity] = diff
	}
	for commodity, diff := range virtualGroup.differences {
		result.Differences[commodity] = diff
	}
	for commodity, diff := range realGroup.signedDifferences {
		result.SignedDifferences[commodity] = diff
	}
	for commodity, diff := range virtualGroup.signedDifferences {
		result.SignedDifferences[commodity] = diff
	}
	result.Balanced = len(result.Differences) == 0

	return result
}

// groupPostings splits a transaction's postings into the two groups that must
// each balance to zero independently: real postings and balanced (bracketed)
// virtual postings. Unbalanced (parenthesized) virtual postings are exempt from
// the balance rule and are omitted.
func groupPostings(postings []ast.Posting) (real, balancedVirtual []ast.Posting) {
	for _, p := range postings {
		switch p.Virtual {
		case ast.VirtualNone:
			real = append(real, p)
		case ast.VirtualBalanced:
			balancedVirtual = append(balancedVirtual, p)
		}
	}
	return real, balancedVirtual
}

// postingGroupResult captures the balance outcome of a single posting group.
type postingGroupResult struct {
	multipleInferred  bool
	inferredIdx       int
	differences       map[string]decimal.Decimal
	signedDifferences map[string]decimal.Decimal
}

// checkPostingGroup balances one posting group (real or balanced-virtual) per
// the hledger rules: a single elided posting is inferred and always balances;
// more than one cannot be inferred; otherwise the group must sum to zero within
// each commodity's tolerance, except that hledger infers a currency conversion
// price when a cost-free group is left with exactly two residual commodities of
// opposite sign (e.g. "10 AAPL" and "$-110" in one transaction).
func checkPostingGroup(postings []ast.Posting, precisions map[string]int32, userTolerance decimal.Decimal) postingGroupResult {
	return checkPostingGroupMixed(postings, nil, precisions, userTolerance)
}

// checkPostingGroupMixed is checkPostingGroup for a group in which some
// postings were split from one multi-commodity amount. multi is index-aligned
// with postings and marks those pieces; nil means there are none.
func checkPostingGroupMixed(postings []ast.Posting, multi []bool, precisions map[string]int32, userTolerance decimal.Decimal) postingGroupResult {
	inferredCount, inferredIdx := countInferredPostings(postings)
	if inferredCount > 1 {
		return postingGroupResult{multipleInferred: true, inferredIdx: -1}
	}
	if inferredCount == 1 {
		return postingGroupResult{inferredIdx: inferredIdx}
	}

	balances := sumByCommodity(postings)

	differences := make(map[string]decimal.Decimal)
	signed := make(map[string]decimal.Decimal)
	for commodity, sum := range balances {
		tolerance := toleranceForPrecision(precisions[commodity])
		if userTolerance.IsPositive() && userTolerance.GreaterThan(tolerance) {
			tolerance = userTolerance
		}
		// hledger treats a residual equal to the tolerance as balanced: with a
		// tolerance of 0.5 a residual of exactly 0.5 is accepted (verified).
		if sum.Abs().GreaterThan(tolerance) {
			differences[commodity] = sum.Abs()
			signed[commodity] = sum
		}
	}

	if isTwoCommodityConversion(postings, multi, signed) {
		return postingGroupResult{inferredIdx: -1}
	}

	return postingGroupResult{inferredIdx: -1, differences: differences, signedDifferences: signed}
}

// isTwoCommodityConversion reports whether hledger would accept this group by
// inferring a conversion price between two commodities: the group must have no
// explicit @/@@ cost and exactly two residual commodities, one positive and one
// negative. Verified against hledger 1.52.4:
//
//	"10 AAPL" + "$-110"                 → accepted
//	"10 AAPL" + "5 EUR"                 → rejected (same sign)
//	"10 AAPL @ $100" + "$-900" + "-5 EUR" + "4 EUR" → rejected (explicit cost)
//
// hledger prices only the postings whose amount is a single commodity, and only
// in the commodity of the first posting amount. A posting that holds several
// commodities (a strict or inclusive balance assignment clearing the other
// commodities of an account) stays unpriced, so when these postings hold a
// non-zero total of that first commodity the conversion no longer closes the sum
// and the transaction is rejected. Verified against hledger 1.52.4.
func isTwoCommodityConversion(postings []ast.Posting, multi []bool, signed map[string]decimal.Decimal) bool {
	if len(signed) != 2 || hasExplicitCost(postings) {
		return false
	}
	if holdsPricedCommodityInMixedPosting(postings, multi, signed) {
		return false
	}
	positive, negative := 0, 0
	for _, sum := range signed {
		switch {
		case sum.IsNegative():
			negative++
		default:
			positive++
		}
	}
	return positive == 1 && negative == 1
}

// holdsPricedCommodityInMixedPosting reports whether the multi-commodity
// postings leave a non-zero total of the commodity hledger would price: the
// commodity of the first posting amount. Single-commodity postings are converted
// at the inferred price and close the sum, so only what the unpriced postings
// hold in that commodity remains; pieces of opposite sign cancel.
func holdsPricedCommodityInMixedPosting(postings []ast.Posting, multi []bool, signed map[string]decimal.Decimal) bool {
	priced := ""
	for i := range postings {
		if postings[i].Amount == nil {
			continue
		}
		if _, residual := signed[postings[i].Amount.Commodity.Symbol]; residual {
			priced = postings[i].Amount.Commodity.Symbol
			break
		}
	}

	total := decimal.Zero
	var precision int32
	for i := range postings {
		if i >= len(multi) || !multi[i] || postings[i].Amount == nil || postings[i].Amount.Commodity.Symbol != priced {
			continue
		}
		total = total.Add(postings[i].Amount.Quantity)
		if p := decimalPrecision(postings[i].Amount.Quantity); p > precision {
			precision = p
		}
	}
	return total.Abs().GreaterThan(toleranceForPrecision(precision))
}

// hasExplicitCost reports whether any posting in the group carries an explicit
// @/@@ cost. hledger only infers a conversion price for cost-free transactions.
func hasExplicitCost(postings []ast.Posting) bool {
	for _, p := range postings {
		if p.Cost != nil {
			return true
		}
	}
	return false
}

func countInferredPostings(postings []ast.Posting) (count int, lastIdx int) {
	lastIdx = -1
	for i, p := range postings {
		if p.Amount == nil && p.BalanceAssertion == nil {
			count++
			lastIdx = i
		}
	}
	return
}

func decimalPrecision(d decimal.Decimal) int32 {
	exp := -d.Exponent()
	if exp < 0 {
		return 0
	}
	return exp
}

// costPlaces returns the number of decimals a cost literal is written with.
func costPlaces(cost *ast.Cost) int32 {
	if cost.Places != nil {
		return *cost.Places
	}
	return decimalPrecision(cost.Amount.Quantity)
}

// postingNativeAmount returns the amount a posting contributes to its account
// balance: the written amount, or — for a posting that carries only a balance
// assertion — the amount hledger infers for it. CheckJournalBalance infers that
// amount from the running account balance; this transaction-local view falls back
// to the asserted amount, which is the same value when the account starts at zero.
func postingNativeAmount(p ast.Posting) *ast.Amount {
	if p.Amount != nil {
		return p.Amount
	}
	if p.BalanceAssertion != nil {
		return &p.BalanceAssertion.Amount
	}
	return nil
}

// balanceSumContribution returns the commodity and signed quantity a posting
// contributes to the balancing sum of its posting group, converting through its
// @/@@ cost when one is present. It reports false for a posting without an amount.
func balanceSumContribution(p ast.Posting) (string, decimal.Decimal, bool) {
	if p.Amount == nil {
		return "", decimal.Zero, false
	}
	if p.Cost == nil {
		return p.Amount.Commodity.Symbol, p.Amount.Quantity, true
	}

	var quantity decimal.Decimal
	if p.Cost.IsTotal {
		quantity = p.Cost.Amount.Quantity
	} else {
		quantity = p.Cost.Amount.Quantity.Mul(p.Amount.Quantity.Abs())
	}
	if p.Amount.Quantity.IsNegative() {
		quantity = quantity.Neg()
	}
	return p.Cost.Amount.Commodity.Symbol, quantity, true
}

// elidedAmountPostings returns one posting per commodity of the amount hledger
// infers for the single elided posting of a group, carrying the precision that
// amount is written with, so that it counts among the amounts of the transaction.
//
// hledger sums the group before converting it (Balancing.hs,
// transactionInferBalancingAmount, 1.52.4): amounts under one cost key — the
// commodity, the cost commodity and, for @, the unit price — are added into one,
// which keeps the cost literal of the first of them (sumSimilarAmountsUsingFirstCost),
// and only then is each converted. So a converted amount has the precision of the
// first cost literal of its key, an amount without a cost the finest precision of
// its quantities, and the inferred amount the finest of those per commodity. A
// commodity the group sums to zero in still has one, an inferred zero.
func elidedAmountPostings(group []ast.Posting) []ast.Posting {
	precisions := make(map[string]int32)
	raise := func(commodity string, precision int32) {
		if held, ok := precisions[commodity]; !ok || precision > held {
			precisions[commodity] = precision
		}
	}
	firstCost := make(map[pricedKey]bool)
	for i := range group {
		p := &group[i]
		if p.Amount == nil {
			continue
		}
		if p.Cost == nil {
			raise(p.Amount.Commodity.Symbol, decimalPrecision(p.Amount.Quantity))
			continue
		}
		if key := costKey(*p.Amount, p.Cost); !firstCost[key] {
			firstCost[key] = true
			raise(p.Cost.Amount.Commodity.Symbol, costPlaces(p.Cost))
		}
	}

	var inferred []ast.Posting
	for commodity, precision := range precisions {
		inferred = append(inferred, ast.Posting{Amount: &ast.Amount{Quantity: decimal.New(1, -precision), Commodity: ast.Commodity{Symbol: commodity}}})
	}
	return inferred
}

// maxPrecisionByCommodity returns, per commodity, the number of decimals the
// balancing tolerance of one posting group is derived from. It is the finest
// precision of the amounts written in that commodity anywhere in the transaction
// (written), and those only: the precision of a posting's native amount does not
// tighten the commodity it is converted into (docs/hledger.md:178-181, verified
// against hledger 1.52.4: "1.005 AAPL @ $2" + "$-2.0" balances). A commodity that
// is written nowhere but appears as the cost of the group's own amounts takes the
// finest precision of those costs, per unit for @ and as a total for @@: hledger
// converts each posting of the group on its own before adding them up, and a cost
// of zero keeps the precision it is written with.
func maxPrecisionByCommodity(written, group []ast.Posting) map[string]int32 {
	precisions := make(map[string]int32)
	raise := func(commodity string, precision int32) {
		if held, ok := precisions[commodity]; !ok || precision > held {
			precisions[commodity] = precision
		}
	}
	for i := range written {
		if amount := postingNativeAmount(written[i]); amount != nil {
			raise(amount.Commodity.Symbol, decimalPrecision(amount.Quantity))
		}
	}

	costs := make(map[string]int32)
	for i := range group {
		if cost := group[i].Cost; cost != nil && group[i].Amount != nil {
			commodity := cost.Amount.Commodity.Symbol
			if held, ok := costs[commodity]; !ok || costPlaces(cost) > held {
				costs[commodity] = costPlaces(cost)
			}
		}
	}
	for commodity, precision := range costs {
		if _, ok := precisions[commodity]; !ok {
			precisions[commodity] = precision
		}
	}
	return precisions
}

func toleranceForPrecision(precision int32) decimal.Decimal {
	return decimal.New(5, -precision-1)
}

func sumByCommodity(postings []ast.Posting) map[string]decimal.Decimal {
	balances := make(map[string]decimal.Decimal)

	for i := range postings {
		p := &postings[i]

		if p.Amount == nil {
			// A posting that only asserts a balance still contributes: hledger
			// infers the amount that makes the assertion hold, which for an
			// account starting at zero is the asserted amount itself.
			if p.BalanceAssertion != nil {
				commodity := p.BalanceAssertion.Amount.Commodity.Symbol
				balances[commodity] = balances[commodity].Add(p.BalanceAssertion.Amount.Quantity)
			}
			continue
		}

		commodity, quantity, ok := balanceSumContribution(*p)
		if !ok {
			continue
		}
		balances[commodity] = balances[commodity].Add(quantity)
	}

	return balances
}
