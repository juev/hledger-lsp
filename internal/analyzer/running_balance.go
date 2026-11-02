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
//
// A posting the register leaves out has no entry, though its amount still counts
// towards the balances: an assignment that moves nothing, with no cost, has an
// empty amount, and so does an elided posting whose group holds no other amount.
// A written or inferred zero is a real amount the register lists.
func RunningBalances(sourced []include.SourcedTransaction, id include.OccurrenceID) map[PostingRef]map[string]decimal.Decimal {
	var entries []registerEntry
	evaluator := &journalEvaluator{balances: accountBalances{}, priced: pricedBalances{}, trackAssertions: true}
	evaluator.observe = func(sourceOrder, postingIndex int, contribution map[string]decimal.Decimal, priced bool) {
		tx := &sourced[sourceOrder].Transaction
		entries = append(entries, registerEntry{registerDate(tx, &tx.Postings[postingIndex]), sourceOrder, postingIndex, contribution, priced})
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

	hidden := registerHidden(sourced, entries)
	balances := accountBalances{}
	result := make(map[PostingRef]map[string]decimal.Decimal)
	for _, step := range entries {
		source := &sourced[step.sourceOrder]
		account := source.Transaction.Postings[step.postingIndex].Account.GetResolvedName()
		for commodity, quantity := range step.contribution {
			balances.add(account, commodity, quantity)
		}
		if source.OccurrenceID == id && !hidden[postingKey{step.sourceOrder, step.postingIndex}] {
			result[PostingRef{TransactionIndex: source.Index, PostingIndex: step.postingIndex}] = cloneAmounts(balances[account])
		}
	}
	return result
}

type registerEntry struct {
	date         ast.Date
	sourceOrder  int
	postingIndex int
	contribution map[string]decimal.Decimal
	priced       bool
}

type postingKey struct {
	sourceOrder  int
	postingIndex int
}

// registerHidden returns the postings hledger's register leaves out because
// their amount is empty. An assignment that moves nothing is empty unless it has
// a cost, and so is an elided posting whose balancing group has no other amount
// to offset, even when it is alone; an explicit zero, or a zero that cancels
// written amounts, is still listed.
func registerHidden(sourced []include.SourcedTransaction, entries []registerEntry) map[postingKey]bool {
	hidden := make(map[postingKey]bool)
	allZero := func(contribution map[string]decimal.Decimal) bool {
		for _, quantity := range contribution {
			if !quantity.IsZero() {
				return false
			}
		}
		return true
	}
	for _, e := range entries {
		p := &sourced[e.sourceOrder].Transaction.Postings[e.postingIndex]
		if p.Amount == nil && p.BalanceAssertion != nil && p.BalanceAssertion.Cost == nil && !e.priced && allZero(e.contribution) {
			hidden[postingKey{e.sourceOrder, e.postingIndex}] = true
		}
	}
	groupsOf := make(map[int][][]int)
	for _, e := range entries {
		tx := &sourced[e.sourceOrder].Transaction
		p := &tx.Postings[e.postingIndex]
		if p.Amount != nil || p.BalanceAssertion != nil || !allZero(e.contribution) {
			continue
		}
		groups, ok := groupsOf[e.sourceOrder]
		if !ok {
			groups = postingGroups(tx.Postings)
			groupsOf[e.sourceOrder] = groups
		}
		group, ok := groupOf(groups, e.postingIndex)
		if !ok {
			continue
		}
		alone := true
		for _, other := range groups[group] {
			if other != e.postingIndex && !hidden[postingKey{e.sourceOrder, other}] {
				alone = false
				break
			}
		}
		if alone {
			hidden[postingKey{e.sourceOrder, e.postingIndex}] = true
		}
	}
	return hidden
}
