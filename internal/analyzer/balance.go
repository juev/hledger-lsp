package analyzer

import (
	"github.com/shopspring/decimal"

	"github.com/juev/hledger-lsp/internal/ast"
)

func CheckBalance(tx *ast.Transaction, userTolerance decimal.Decimal) *BalanceResult {
	result := NewBalanceResult()

	realPostings, balancedVirtual := groupPostings(tx.Postings)

	realGroup := checkPostingGroup(realPostings, userTolerance)
	virtualGroup := checkPostingGroup(balancedVirtual, userTolerance)

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
func checkPostingGroup(postings []ast.Posting, userTolerance decimal.Decimal) postingGroupResult {
	inferredCount, inferredIdx := countInferredPostings(postings)
	if inferredCount > 1 {
		return postingGroupResult{multipleInferred: true, inferredIdx: -1}
	}
	if inferredCount == 1 {
		return postingGroupResult{inferredIdx: inferredIdx}
	}

	balances := sumByCommodity(postings)
	precisions := maxPrecisionByCommodity(postings)

	differences := make(map[string]decimal.Decimal)
	signed := make(map[string]decimal.Decimal)
	for commodity, sum := range balances {
		tolerance := toleranceForPrecision(precisions[commodity])
		if userTolerance.IsPositive() && userTolerance.GreaterThan(tolerance) {
			tolerance = userTolerance
		}
		if sum.Abs().GreaterThanOrEqual(tolerance) {
			differences[commodity] = sum.Abs()
			signed[commodity] = sum
		}
	}

	if isTwoCommodityConversion(postings, signed) {
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
func isTwoCommodityConversion(postings []ast.Posting, signed map[string]decimal.Decimal) bool {
	if len(signed) != 2 || hasExplicitCost(postings) {
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

func maxPrecisionByCommodity(postings []ast.Posting) map[string]int32 {
	precisions := make(map[string]int32)
	for _, p := range postings {
		if p.Amount == nil {
			continue
		}
		// hledger counts precision per commodity from the amounts denominated
		// in that commodity. A posting whose amount is converted at a cost
		// contributes to the cost commodity, but neither the native amount's
		// precision nor the cost amount's precision tightens the cost
		// commodity's tolerance (docs/hledger.md:178-181, verified against
		// hledger 1.52.4: "1.005 AAPL @ $2" + "$-2.0" balances).
		commodity := p.Amount.Commodity.Symbol
		prec := decimalPrecision(p.Amount.Quantity)
		if prec > precisions[commodity] {
			precisions[commodity] = prec
		}
	}
	return precisions
}

func toleranceForPrecision(precision int32) decimal.Decimal {
	return decimal.New(5, -precision-1)
}

func sumByCommodity(postings []ast.Posting) map[string]decimal.Decimal {
	balances := make(map[string]decimal.Decimal)

	for _, p := range postings {
		if p.Amount == nil {
			continue
		}

		if p.Cost != nil {
			commodity := p.Cost.Amount.Commodity.Symbol
			var quantity decimal.Decimal
			if p.Cost.IsTotal {
				quantity = p.Cost.Amount.Quantity
			} else {
				quantity = p.Cost.Amount.Quantity.Mul(p.Amount.Quantity.Abs())
			}
			if p.Amount.Quantity.IsNegative() {
				quantity = quantity.Neg()
			}
			balances[commodity] = balances[commodity].Add(quantity)
		} else {
			commodity := p.Amount.Commodity.Symbol
			balances[commodity] = balances[commodity].Add(p.Amount.Quantity)
		}
	}

	return balances
}
