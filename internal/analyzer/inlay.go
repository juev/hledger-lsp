package analyzer

import (
	"github.com/shopspring/decimal"

	"github.com/juev/hledger-lsp/internal/ast"
)

// PostingEffect contains the facts derived while applying one posting. All map
// fields are owned by this result and may be safely changed by its caller.
type PostingEffect struct {
	TransactionIndex int
	PostingIndex     int
	InferredAmounts  map[string]decimal.Decimal
	CostContribution map[string]decimal.Decimal
	BalanceAfter     AccountBalances
}

// CalculatePostingEffects derives posting facts in source order. Explicit
// posting amounts update account balances in their native commodity; inferred
// amounts balance only real or balanced-virtual posting groups.
func CalculatePostingEffects(journal *ast.Journal) []PostingEffect {
	effects, _ := calculatePostingEffectsFromTransactions(journal.Transactions)
	return effects
}

func calculatePostingEffectsFromTransactions(transactions []ast.Transaction) ([]PostingEffect, AccountBalances) {
	balances := make(AccountBalances)
	effects := make([]PostingEffect, 0)

	for transactionIndex := range transactions {
		transaction := &transactions[transactionIndex]
		inferred := inferredAmountsByPosting(transaction.Postings)

		for postingIndex := range transaction.Postings {
			posting := &transaction.Postings[postingIndex]
			if posting.Amount != nil {
				addAccountBalance(balances, posting.Account.GetResolvedName(), posting.Amount.Commodity.Symbol, posting.Amount.Quantity)
			}
			if amounts := inferred[postingIndex]; amounts != nil {
				for commodity, quantity := range amounts {
					addAccountBalance(balances, posting.Account.GetResolvedName(), commodity, quantity)
				}
			}

			effects = append(effects, PostingEffect{
				TransactionIndex: transactionIndex,
				PostingIndex:     postingIndex,
				InferredAmounts:  cloneAmounts(inferred[postingIndex]),
				CostContribution: costContribution(posting),
				BalanceAfter:     cloneAccountBalances(balances),
			})
		}
	}

	return effects, balances
}

func inferredAmountsByPosting(postings []ast.Posting) map[int]map[string]decimal.Decimal {
	groups := [2][]int{}
	for index, posting := range postings {
		switch posting.Virtual {
		case ast.VirtualNone:
			groups[0] = append(groups[0], index)
		case ast.VirtualBalanced:
			groups[1] = append(groups[1], index)
		}
	}

	inferred := make(map[int]map[string]decimal.Decimal)
	for _, indexes := range groups {
		group := make([]ast.Posting, 0, len(indexes))
		for _, index := range indexes {
			group = append(group, postings[index])
		}
		_, amounts, ok := inferElidedAmounts(group)
		if !ok {
			continue
		}
		_, inferredIndex := countInferredPostings(group)
		inferred[indexes[inferredIndex]] = amounts
	}

	return inferred
}

func costContribution(posting *ast.Posting) map[string]decimal.Decimal {
	if posting.Amount == nil || posting.Cost == nil {
		return make(map[string]decimal.Decimal)
	}
	return sumByCommodity([]ast.Posting{*posting})
}

func addAccountBalance(balances AccountBalances, account, commodity string, quantity decimal.Decimal) {
	if balances[account] == nil {
		balances[account] = make(map[string]decimal.Decimal)
	}
	balances[account][commodity] = balances[account][commodity].Add(quantity)
}

func cloneAmounts(amounts map[string]decimal.Decimal) map[string]decimal.Decimal {
	cloned := make(map[string]decimal.Decimal, len(amounts))
	for commodity, quantity := range amounts {
		cloned[commodity] = quantity
	}
	return cloned
}

func cloneAccountBalances(balances AccountBalances) AccountBalances {
	cloned := make(AccountBalances, len(balances))
	for account, amounts := range balances {
		cloned[account] = cloneAmounts(amounts)
	}
	return cloned
}
