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
// towards the balances: an assignment that moves nothing, with no cost left, has
// an empty amount, and so does an elided posting whose group holds no other amount.
// A written or inferred zero is a real amount the register lists.
func RunningBalances(sourced []include.SourcedTransaction, id include.OccurrenceID) map[PostingRef]map[string]decimal.Decimal {
	return RunningBalancesFor(sourced, []include.OccurrenceID{id})[id]
}

// RunningBalancesFor replays the journal once and returns the running balances of
// each of the occurrences ids, keyed by occurrence. A file included many times
// costs one replay instead of one per inclusion.
func RunningBalancesFor(sourced []include.SourcedTransaction, ids []include.OccurrenceID) map[include.OccurrenceID]map[PostingRef]map[string]decimal.Decimal {
	replays := ReplayFor(sourced, ids)
	result := make(map[include.OccurrenceID]map[PostingRef]map[string]decimal.Decimal, len(replays))
	for id, replay := range replays {
		result[id] = replay.Balances
	}
	return result
}

// Replay is what one replay of the journal says about the postings of an
// occurrence.
type Replay struct {
	// Balances is each posting's running balance, as RunningBalances returns it.
	Balances map[PostingRef]map[string]decimal.Decimal
	// Assigned is the amount hledger gives each amount-less posting of a
	// transaction with a balance assignment: the change that makes the
	// assignment hold, and the elided amount that balances it. Both depend on the
	// balance the account had before, so the transaction alone cannot tell them.
	Assigned map[PostingRef]map[string]decimal.Decimal
	// Moved is the amount each posting with a balance adds to its account in
	// this replay. The commodities of a posting that elides its amount or
	// assigns a balance depend on where the journal reads it.
	Moved map[PostingRef]map[string]decimal.Decimal
}

// ReplayFor is RunningBalancesFor with the assigned amounts of each occurrence.
func ReplayFor(sourced []include.SourcedTransaction, ids []include.OccurrenceID) map[include.OccurrenceID]Replay {
	var entries []registerEntry
	evaluator := &journalEvaluator{balances: accountBalances{}, priced: pricedBalances{}, trackAssertions: true}
	evaluator.observe = func(sourceOrder, postingIndex int, contribution map[string]decimal.Decimal, priced bool) {
		tx := &sourced[sourceOrder].Transaction
		entries = append(entries, registerEntry{evaluator.registerDates(tx)[postingIndex], sourceOrder, postingIndex, contribution, priced})
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
	result := make(map[include.OccurrenceID]Replay, len(ids))
	for _, id := range ids {
		result[id] = Replay{
			Balances: make(map[PostingRef]map[string]decimal.Decimal),
			Assigned: make(map[PostingRef]map[string]decimal.Decimal),
			Moved:    make(map[PostingRef]map[string]decimal.Decimal),
		}
	}
	for _, step := range entries {
		source := &sourced[step.sourceOrder]
		posting := &source.Transaction.Postings[step.postingIndex]
		account := posting.Account.GetResolvedName()
		for commodity, quantity := range step.contribution {
			balances.add(account, commodity, quantity)
		}
		wanted, ok := result[source.OccurrenceID]
		if !ok {
			continue
		}
		ref := PostingRef{TransactionIndex: source.Index, PostingIndex: step.postingIndex}
		if !hidden[postingKey{step.sourceOrder, step.postingIndex}] {
			wanted.Balances[ref] = cloneAmounts(balances[account])
			wanted.Moved[ref] = cloneAmounts(step.contribution)
		}
		if posting.Amount == nil && HasBalanceAssignment(&source.Transaction) {
			wanted.Assigned[ref] = cloneAmounts(step.contribution)
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
// their amount is empty. An assignment that moves nothing is empty unless a cost
// is left in it, whether it replaces a priced balance or asserts one, and so is
// an elided posting whose balancing group has no other amount to offset, even
// when it is alone; an explicit zero, or a zero that cancels written amounts, is
// still listed.
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
		if p.Amount == nil && p.BalanceAssertion != nil && !e.priced && allZero(e.contribution) {
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
