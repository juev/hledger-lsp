package analyzer

import (
	"github.com/shopspring/decimal"

	"github.com/juev/hledger-lsp/internal/ast"
)

// AccountBalances maps account name -> commodity -> balance
type AccountBalances map[string]map[string]decimal.Decimal

// CalculateAccountBalances computes the balance for each account across all transactions.
// Returns a map of account name to commodity to balance.
// Postings with inferred amounts (nil Amount) are skipped.
func CalculateAccountBalances(journal *ast.Journal) AccountBalances {
	balances := make(AccountBalances)

	for i := range journal.Transactions {
		tx := &journal.Transactions[i]
		for j := range tx.Postings {
			p := &tx.Postings[j]
			if p.Amount == nil {
				continue
			}

			accountName := p.Account.Name
			commodity := p.Amount.Commodity.Symbol

			if balances[accountName] == nil {
				balances[accountName] = make(map[string]decimal.Decimal)
			}

			balances[accountName][commodity] = balances[accountName][commodity].Add(p.Amount.Quantity)
		}
	}

	return balances
}
