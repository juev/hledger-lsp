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
	multipleInferred bool
	inferredIdx      int
	differences      map[string]decimal.Decimal
}

// checkPostingGroup balances one posting group (real or balanced-virtual) per
// the hledger rules: a single elided posting is inferred and always balances;
// more than one cannot be inferred; otherwise the group must sum to zero within
// each commodity's tolerance.
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
	for commodity, sum := range balances {
		tolerance := toleranceForPrecision(precisions[commodity])
		if userTolerance.IsPositive() && userTolerance.GreaterThan(tolerance) {
			tolerance = userTolerance
		}
		if sum.Abs().GreaterThanOrEqual(tolerance) {
			differences[commodity] = sum.Abs()
		}
	}

	return postingGroupResult{inferredIdx: -1, differences: differences}
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
		// Map posting amount precision to the balance commodity.
		// For cost postings, that's the cost commodity (not the posting's native commodity).
		// Cost price precision is intentionally excluded per hledger spec.
		commodity := p.Amount.Commodity.Symbol
		if p.Cost != nil {
			commodity = p.Cost.Amount.Commodity.Symbol
		}
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
