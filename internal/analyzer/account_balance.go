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
//
// Balances are accumulated directly rather than taken from
// calculatePostingEffectsFromTransactions: that walk snapshots the whole balance
// map after every posting for inlay hints, so reusing it here copies every
// account once per posting. Completion calls this once per keystroke.
func CalculateAccountBalancesFromTransactions(transactions []ast.Transaction) AccountBalances {
	balances := make(AccountBalances)
	for transactionIndex := range transactions {
		transaction := &transactions[transactionIndex]
		inferred := inferredAmountsByPosting(transaction.Postings)
		for postingIndex := range transaction.Postings {
			addPostingBalance(balances, &transaction.Postings[postingIndex], inferred[postingIndex])
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
