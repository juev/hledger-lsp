package analyzer

import (
	"github.com/shopspring/decimal"

	"github.com/juev/hledger-lsp/internal/ast"
)

// AccountBalances maps account name -> commodity -> balance
type AccountBalances map[string]map[string]decimal.Decimal

// CalculateAccountBalances computes the balance for each account across all transactions.
// Returns a map of account name to commodity to balance.
func CalculateAccountBalances(journal *ast.Journal) AccountBalances {
	return CalculateAccountBalancesFromTransactions(journal.Transactions)
}

// CalculateAccountBalancesFromTransactions computes the balance for each account.
// A single posting per transaction may omit its amount; that elided amount is
// inferred to balance the transaction (per the hledger journal format) so the
// account still reflects the implicit posting.
func CalculateAccountBalancesFromTransactions(transactions []ast.Transaction) AccountBalances {
	balances := make(AccountBalances)

	addBalance := func(account, commodity string, quantity decimal.Decimal) {
		if balances[account] == nil {
			balances[account] = make(map[string]decimal.Decimal)
		}
		balances[account][commodity] = balances[account][commodity].Add(quantity)
	}

	for i := range transactions {
		tx := &transactions[i]
		for j := range tx.Postings {
			p := &tx.Postings[j]
			if p.Amount == nil {
				continue
			}
			addBalance(p.Account.GetResolvedName(), p.Amount.Commodity.Symbol, p.Amount.Quantity)
		}

		if account, inferred, ok := inferElidedAmounts(tx); ok {
			for commodity, quantity := range inferred {
				addBalance(account, commodity, quantity)
			}
		}
	}

	return balances
}

// inferElidedAmounts returns the account and per-commodity amounts inferred for
// the single real posting that omits its amount, balancing the transaction.
// It reuses the same posting-classification rules as the balance check, so what
// is verified and what is inferred stay consistent. ok is false unless exactly
// one real posting has an elided amount.
func inferElidedAmounts(tx *ast.Transaction) (account string, amounts map[string]decimal.Decimal, ok bool) {
	realPostings := filterRealPostings(tx.Postings)
	inferredCount, inferredIdx := countInferredPostings(realPostings)
	if inferredCount != 1 {
		return "", nil, false
	}

	amounts = make(map[string]decimal.Decimal)
	for commodity, sum := range sumByCommodity(realPostings) {
		if sum.IsZero() {
			continue
		}
		amounts[commodity] = sum.Neg()
	}

	return realPostings[inferredIdx].Account.GetResolvedName(), amounts, true
}
