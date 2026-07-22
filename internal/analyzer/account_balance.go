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

		// Infer elided amounts within each balancing group independently, using
		// the same grouping as the balance check so what is verified and what is
		// inferred stay consistent.
		realPostings, balancedVirtual := groupPostings(tx.Postings)
		for _, group := range [][]ast.Posting{realPostings, balancedVirtual} {
			account, inferred, ok := inferElidedAmounts(group)
			if !ok {
				continue
			}
			for commodity, quantity := range inferred {
				addBalance(account, commodity, quantity)
			}
		}
	}

	return balances
}

// inferElidedAmounts returns the account and per-commodity amounts inferred for
// the single posting in a balancing group that omits its amount. The elided
// amount balances its own group (real or balanced-virtual), not the transaction
// as a whole. ok is false unless the group has exactly one elided posting.
func inferElidedAmounts(group []ast.Posting) (account string, amounts map[string]decimal.Decimal, ok bool) {
	inferredCount, inferredIdx := countInferredPostings(group)
	if inferredCount != 1 {
		return "", nil, false
	}

	amounts = make(map[string]decimal.Decimal)
	for commodity, sum := range sumByCommodity(group) {
		if sum.IsZero() {
			continue
		}
		amounts[commodity] = sum.Neg()
	}

	return group[inferredIdx].Account.GetResolvedName(), amounts, true
}
