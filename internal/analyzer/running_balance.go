package analyzer

import (
	"sort"

	"github.com/shopspring/decimal"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/include"
)

// PostingRef identifies a posting by its position in its source journal.
type PostingRef struct {
	TransactionIndex int
	PostingIndex     int
}

// RunningBalances returns each posting's account balance as of that posting, per
// commodity, for the postings of occurrence id. Every transaction counts towards
// the balances, but only the postings of that occurrence are reported: a snapshot
// per posting is what makes this walk expensive, and a caller needs one file.
//
// A balance is the one hledger's register shows, which takes two passes. The
// balance checker's replay decides what each posting contributes, resolving
// assignments in the order hledger does, where a transaction with an assignment
// is one unit at its transaction date. The register then applies every posting at
// its own date, so its balances are summed separately in that order. A posting's
// balance reads as of its date, so it can include postings written later in the
// file and leave out ones written earlier.
func RunningBalances(sourced []include.SourcedTransaction, id include.OccurrenceID) map[PostingRef]map[string]decimal.Decimal {
	type entry struct {
		date         ast.Date
		sourceOrder  int
		postingIndex int
		contribution map[string]decimal.Decimal
	}
	var entries []entry
	evaluator := &journalEvaluator{balances: accountBalances{}, trackAssertions: true}
	evaluator.observe = func(sourceOrder, postingIndex int, contribution map[string]decimal.Decimal) {
		tx := &sourced[sourceOrder].Transaction
		entries = append(entries, entry{registerDate(tx, &tx.Postings[postingIndex]), sourceOrder, postingIndex, contribution})
	}
	evaluator.run(sourced)

	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if byDate := compareDates(a.date, b.date); byDate != 0 {
			return byDate < 0
		}
		if a.sourceOrder != b.sourceOrder {
			return a.sourceOrder < b.sourceOrder
		}
		return a.postingIndex < b.postingIndex
	})

	balances := accountBalances{}
	result := make(map[PostingRef]map[string]decimal.Decimal)
	for _, step := range entries {
		source := &sourced[step.sourceOrder]
		account := source.Transaction.Postings[step.postingIndex].Account.GetResolvedName()
		for commodity, quantity := range step.contribution {
			balances.add(account, commodity, quantity)
		}
		if source.OccurrenceID == id {
			result[PostingRef{TransactionIndex: source.Index, PostingIndex: step.postingIndex}] = cloneAmounts(balances[account])
		}
	}
	return result
}
