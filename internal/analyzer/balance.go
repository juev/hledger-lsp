package analyzer

import (
	"github.com/shopspring/decimal"

	"github.com/juev/hledger-lsp/internal/ast"
)

func CheckBalance(tx *ast.Transaction) *BalanceResult {
	result := NewBalanceResult()

	realPostings := filterRealPostings(tx.Postings)
	inferredCount, inferredIdx := countInferredPostings(realPostings)

	if inferredCount > 1 {
		result.Balanced = false
		return result
	}

	result.InferredIdx = inferredIdx

	balances := sumByCommodity(realPostings)

	if inferredCount == 1 {
		if len(balances) <= 1 {
			result.Balanced = true
			return result
		}
		result.Balanced = false
		return result
	}

	for commodity, sum := range balances {
		if !sum.IsZero() {
			result.Balanced = false
			result.Differences[commodity] = sum.Abs()
		}
	}

	return result
}

func filterRealPostings(postings []ast.Posting) []ast.Posting {
	var real []ast.Posting
	for _, p := range postings {
		if p.Virtual == ast.VirtualNone || p.Virtual == ast.VirtualBalanced {
			real = append(real, p)
		}
	}
	return real
}

func countInferredPostings(postings []ast.Posting) (count int, lastIdx int) {
	lastIdx = -1
	for i, p := range postings {
		if p.Amount == nil {
			count++
			lastIdx = i
		}
	}
	return
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
