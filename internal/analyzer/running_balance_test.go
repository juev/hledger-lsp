package analyzer

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/include"
)

func runningBalanceAt(t *testing.T, balances map[PostingRef]map[string]decimal.Decimal, transaction, posting int, commodity string) string {
	t.Helper()
	amounts, ok := balances[PostingRef{TransactionIndex: transaction, PostingIndex: posting}]
	require.True(t, ok, "no balance for transaction %d posting %d", transaction, posting)
	return amounts[commodity].String()
}

func primaryRunningBalances(t *testing.T, content string) map[PostingRef]map[string]decimal.Decimal {
	t.Helper()
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: content})
	sourced := resolved.TransactionsWithSource()
	require.NotEmpty(t, sourced)
	return RunningBalances(sourced, sourced[0].OccurrenceID)
}

func TestRunningBalances_AssignmentSetsTheBalance(t *testing.T) {
	balances := primaryRunningBalances(t, `2024-01-01 seed
    assets:cash  100 USD
    equity:opening

2024-01-02 adjust
    assets:cash  = 70 USD
    expenses:misc

2024-01-03 later
    assets:cash  -5 USD
    expenses:misc
`)
	assert.Equal(t, "70", runningBalanceAt(t, balances, 1, 0, "USD"))
	assert.Equal(t, "65", runningBalanceAt(t, balances, 2, 0, "USD"))
}

func TestRunningBalances_ElidedPostingBesideAssignmentBalancesIt(t *testing.T) {
	balances := primaryRunningBalances(t, `2024-01-01 seed
    活动:现金  100 ₽
    капитал

2024-01-02 adjust
    活动:现金  = 70 ₽
    расходы:разное
`)
	assert.Equal(t, "30", runningBalanceAt(t, balances, 1, 1, "₽"))
}

func TestRunningBalances_PostingAppliesAtItsOwnDate(t *testing.T) {
	balances := primaryRunningBalances(t, `2024-01-01 purchase
    card  -10 USD  ; date:2024-01-05
    expenses:food

2024-01-02 other
    card  -1 USD
    expenses:food

2024-01-10 after
    card  -2 USD
    expenses:food
`)
	assert.Equal(t, "-1", runningBalanceAt(t, balances, 1, 0, "USD"), "the dated posting has not landed yet")
	assert.Equal(t, "-11", runningBalanceAt(t, balances, 0, 0, "USD"), "balance at the posting's own date")
	assert.Equal(t, "-13", runningBalanceAt(t, balances, 2, 0, "USD"))
}

func TestRunningBalances_Date2DoesNotMoveAPosting(t *testing.T) {
	balances := primaryRunningBalances(t, `2024-01-01 purchase
    card  -10 USD  ; date2:2024-01-05
    expenses:food

2024-01-02 other
    card  -1 USD
    expenses:food
`)
	assert.Equal(t, "-11", runningBalanceAt(t, balances, 1, 0, "USD"))
}

func TestRunningBalances_CountsOtherFilesButReportsOnlyTheOccurrence(t *testing.T) {
	resolved := resolvedFromSources(t,
		journalSource{path: "/main.journal", content: "2024-02-01 here\n    assets:cash  -10 USD\n    expenses:food\n"},
		journalSource{path: "/other.journal", content: "2024-01-01 there\n    assets:cash  100 USD\n    income:job\n"},
	)
	sourced := resolved.TransactionsWithSource()
	var mainID include.OccurrenceID
	for _, source := range sourced {
		if source.Path == "/main.journal" {
			mainID = source.OccurrenceID
		}
	}
	require.NotZero(t, mainID)

	balances := RunningBalances(sourced, mainID)
	assert.Len(t, balances, 2)
	assert.Equal(t, "90", runningBalanceAt(t, balances, 0, 0, "USD"))
}

func TestRunningBalances_HoldsOnlyThePostingsAccountAndIsNotShared(t *testing.T) {
	balances := primaryRunningBalances(t, `2024-01-15 first
    активы:наличные  10 ₽
    資産:現金  -10 ₽

2024-01-16 second
    активы:наличные  5 ₽
    資産:現金  -5 ₽
`)
	first := balances[PostingRef{TransactionIndex: 0, PostingIndex: 0}]
	require.Len(t, first, 1)
	first["₽"] = decimal.Zero
	assert.Equal(t, "15", runningBalanceAt(t, balances, 1, 0, "₽"))
}

func TestRunningBalances_UnbalancedTransactionStillRuns(t *testing.T) {
	balances := primaryRunningBalances(t, "2024-01-01 broken\n    a  1 USD\n    b  1 USD\n\n2024-01-02 next\n    a  1 USD\n    b  -1 USD\n")
	assert.Equal(t, "2", runningBalanceAt(t, balances, 1, 0, "USD"))
}

// The expected values were checked against hledger 1.52 `reg`.
func TestRunningBalances_DatedPostingOfAssignmentTransactionAppliesAtItsOwnDate(t *testing.T) {
	balances := primaryRunningBalances(t, `2024-01-01 seed
    card  100 USD
    e

2024-01-05 assign
    x  =5 USD
    card  -10 USD  ; date:2024-01-10
    e

2024-01-07 between
    card  -1 USD
    e

2024-01-20 after
    card  -2 USD
    e
`)
	assert.Equal(t, "99", runningBalanceAt(t, balances, 2, 0, "USD"), "the dated posting has not landed yet")
	assert.Equal(t, "89", runningBalanceAt(t, balances, 1, 1, "USD"), "balance at the posting's own date")
	assert.Equal(t, "87", runningBalanceAt(t, balances, 3, 0, "USD"))
	assert.Equal(t, "-95", runningBalanceAt(t, balances, 1, 2, "USD"), "the elided amount still balances the transaction")
}

func TestRunningBalances_DatedPostingOfAssignmentTransactionNonASCII(t *testing.T) {
	balances := primaryRunningBalances(t, `2024-01-02 назначение
    активы:現金  = 5 ₽
    долг:カード  -10 ₽  ; date:2024-01-10
    расходы

2024-01-07 между
    долг:カード  -1 ₽
    расходы
`)
	assert.Equal(t, "-1", runningBalanceAt(t, balances, 1, 0, "₽"))
	assert.Equal(t, "-11", runningBalanceAt(t, balances, 0, 1, "₽"))
}

func TestRunningBalances_ReaderSeesAssignedAmountFromTheCheckersOrder(t *testing.T) {
	// The assignment is resolved with the dated posting of the same transaction
	// still pending, but the balance reads as of the register: both matter.
	balances := primaryRunningBalances(t, `2024-01-01 seed
    a:x  40 EUR
    e

2024-01-02 assign
    a  == 20 EUR
    a:x  -30 EUR  ; date:2024-01-01
    b

2024-01-03 after
    a:x  1 EUR
    e
`)
	assert.Equal(t, "20", runningBalanceAt(t, balances, 1, 0, "EUR"))
	assert.Equal(t, "10", runningBalanceAt(t, balances, 1, 1, "EUR"))
	assert.Equal(t, "11", runningBalanceAt(t, balances, 2, 0, "EUR"))
}

// hledger refuses date: on an assignment posting outright, so there is no register
// to match; it stays at the transaction date, as in the balance check.
func TestRunningBalances_DatedAssignmentPostingStaysAtTransactionDate(t *testing.T) {
	balances := primaryRunningBalances(t, `2024-01-10 assign
    a  =5 USD  ; date:2024-01-20
    e

2024-01-15 check
    a  1 USD
    e
`)
	assert.Equal(t, "6", runningBalanceAt(t, balances, 1, 0, "USD"))
}

func hasRunningBalance(balances map[PostingRef]map[string]decimal.Decimal, transaction, posting int) bool {
	_, ok := balances[PostingRef{TransactionIndex: transaction, PostingIndex: posting}]
	return ok
}

// hledger 1.52.4 `reg` omits an assignment that moves nothing, and the elided
// posting that balances it; it lists written zeros and zeros that cancel.
func TestRunningBalances_RegisterHidesEmptyAmounts(t *testing.T) {
	balances := primaryRunningBalances(t, `2024-01-01 seed
    assets:cash  100 USD
    equity:opening

2024-01-02 no-op assignment
    assets:cash  = 100 USD
    equity:opening

2024-01-03 written zero
    assets:cash  0 USD
    equity:opening

2024-01-04 zero that cancels
    assets:cash  5 USD
    assets:cash  -5 USD
    equity:opening

2024-01-05 no-op assignment in a second commodity
    assets:cash  = 100 USD
    assets:cash  = 0 EUR
    equity:opening

2024-01-06 moving assignment
    assets:cash  = 150 USD
    equity:opening
`)
	assert.True(t, hasRunningBalance(balances, 0, 0))

	assert.False(t, hasRunningBalance(balances, 1, 0), "assignment that moves nothing")
	assert.False(t, hasRunningBalance(balances, 1, 1), "elided posting balancing nothing")

	assert.Equal(t, "100", runningBalanceAt(t, balances, 2, 0, "USD"), "written zero")
	assert.True(t, hasRunningBalance(balances, 2, 1))
	assert.True(t, hasRunningBalance(balances, 3, 0))
	assert.True(t, hasRunningBalance(balances, 3, 1))
	assert.True(t, hasRunningBalance(balances, 3, 2), "elided zero beside written amounts")

	assert.False(t, hasRunningBalance(balances, 4, 0))
	assert.False(t, hasRunningBalance(balances, 4, 1))
	assert.False(t, hasRunningBalance(balances, 4, 2))

	assert.Equal(t, "150", runningBalanceAt(t, balances, 5, 0, "USD"))
	assert.Equal(t, "-150", runningBalanceAt(t, balances, 5, 1, "USD"))
}

func TestRunningBalances_HiddenAssignmentStillCountsTowardsLaterBalances(t *testing.T) {
	balances := primaryRunningBalances(t, `2024-01-01 сид
    активы:касса  100 ₽
    капитал

2024-01-02 проверка
    активы:касса  = 100 ₽
    капитал

2024-01-03 расход
    активы:касса  -30 ₽
    расходы
`)
	assert.False(t, hasRunningBalance(balances, 1, 0))
	assert.Equal(t, "70", runningBalanceAt(t, balances, 2, 0, "₽"))
}

func TestRunningBalances_AssignmentWithCostIsListedByTheRegister(t *testing.T) {
	balances := primaryRunningBalances(t, `2024-01-01 seed
    assets:cash  100 USD
    equity:opening

2024-01-02 cost
    assets:cash  = 100 USD @ 2 EUR
    equity:opening

2024-01-03 total cost
    assets:cash  = 100 USD @@ 5 EUR
    equity:opening
`)
	for _, tx := range []int{1, 2} {
		assert.True(t, hasRunningBalance(balances, tx, 0), "transaction %d assignment", tx)
		assert.True(t, hasRunningBalance(balances, tx, 1), "transaction %d elided posting", tx)
	}
}

func TestRunningBalances_ElidedPostingAloneInItsGroupIsHidden(t *testing.T) {
	balances := primaryRunningBalances(t, `2024-01-01 seed
    assets:cash  100 USD
    equity:opening

2024-01-02 bracketed elided
    assets:cash  1 USD
    equity:opening
    [virtual:b]

2024-01-03 только elided
    единственная
`)
	assert.True(t, hasRunningBalance(balances, 1, 0))
	assert.True(t, hasRunningBalance(balances, 1, 1))
	assert.False(t, hasRunningBalance(balances, 1, 2))
	assert.False(t, hasRunningBalance(balances, 2, 0))
}

func TestRunningBalances_AssignmentAfterAssignmentTransactionKeepsItsCost(t *testing.T) {
	// hledger keeps the cost of postings in a transaction with an assignment, so
	// the amount assigned to b later carries 29 EUR @ 2 USD and the elided
	// posting is -13 EUR, -58 USD. Checked against hledger 1.52 `reg x`.
	for name, accounts := range map[string][3]string{
		"ascii":    {"b", "c", "x"},
		"cyrillic": {"банк", "касса", "расходы"},
		"cjk":      {"銀行", "現金", "費用"},
	} {
		t.Run(name, func(t *testing.T) {
			balances := primaryRunningBalances(t, "2024-01-03 t3\n    "+accounts[0]+"  -29 EUR @ 2 USD\n    "+accounts[1]+"  = -7 EUR\n    d\n\n"+
				"2024-01-16 t1\n    "+accounts[0]+"  = 13 EUR\n    "+accounts[2]+"\n")
			assert.Equal(t, "-13", runningBalanceAt(t, balances, 1, 1, "EUR"))
			assert.Equal(t, "-58", runningBalanceAt(t, balances, 1, 1, "USD"))
			assert.Equal(t, "13", runningBalanceAt(t, balances, 1, 0, "EUR"))
		})
	}
}

func TestRunningBalances_OrdinaryTransactionCostIsNotKept(t *testing.T) {
	balances := primaryRunningBalances(t, `2024-01-03 t3
    b  -29 EUR @ 2 USD
    c

2024-01-16 t1
    b  = 13 EUR
    x
`)
	assert.Equal(t, "-42", runningBalanceAt(t, balances, 1, 1, "EUR"))
	assert.Equal(t, "0", runningBalanceAt(t, balances, 1, 1, "USD"))
}

func TestRunningBalances_ReportedJournalMatchesRegister(t *testing.T) {
	balances := primaryRunningBalances(t, `alias q = a:x

2024-01-01 t0
    a:x  ==* 24 EUR
    b  =* -5 EUR
    q

2024-01-16 t1
    b  = 13 EUR
    банк  3 EUR
    a:x  18 EUR @ 3 USD  ; date:2024-01-04
    q

2024-01-05 t2
    a  29 EUR
    a

2024-01-03 t3
    a:x  -20 USD  ; date:2024-01-10
    b  -29 EUR @ 2 USD
    現金:食  ==* -7 EUR
    банк

2024-01-08 t4
    a:x  ==* 1 EUR
    現金:食
`)
	assert.Equal(t, "-2", runningBalanceAt(t, balances, 1, 3, "EUR"))
	assert.Equal(t, "-112", runningBalanceAt(t, balances, 1, 3, "USD"))
}

func TestRunningBalances_AssignmentKeepsCostOfCommodityThatNetsToZero(t *testing.T) {
	// The EUR quantity cancels, but 29 EUR @ 2 USD stays priced, so the strict
	// assignment moves -63 USD to the elided posting. hledger 1.52.4 `reg -I x`.
	balances := primaryRunningBalances(t, `2024-01-01 t1
    b  -29 EUR @ 2 USD
    c  = -7 EUR
    d

2024-01-02 t2
    b  29 EUR
    e

2024-01-03 t3
    b  == 5 USD
    x
`)
	assert.Equal(t, "-63", runningBalanceAt(t, balances, 2, 1, "USD"))
	assert.Equal(t, "29", runningBalanceAt(t, balances, 2, 1, "EUR"))
}

func TestRunningBalances_AssignmentThatCarriesACostIsListed(t *testing.T) {
	// The assigned amount is -3 EUR plus 3 EUR @ 4 USD, which sums to zero in the
	// register but is not an empty amount, so hledger 1.52.4 lists the posting.
	balances := primaryRunningBalances(t, `2024-01-09 t2
    d  -3 EUR @ 4 USD
    d  =* -3 EUR
    a:x
`)
	assert.True(t, hasRunningBalance(balances, 0, 1))
	assert.Equal(t, "-3", runningBalanceAt(t, balances, 0, 1, "EUR"))
}

func TestJournalEvaluator_ObserveReportsPricedPerPosting(t *testing.T) {
	// The assignment is the last posting of its transaction and carries
	// 29 EUR @ 2 USD. The plain transaction after it must not inherit that.
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2024-01-01 t1
    b  -29 EUR @ 2 USD
    c  = -7 EUR
    d  7 EUR
    d  58 USD

2024-01-02 t2
    x  -13 EUR
    y  -58 USD
    b  = 13 EUR

2024-01-03 t3
    p  1 EUR
    q  -1 EUR
`})
	type key struct{ order, posting int }
	priced := map[key]bool{}
	evaluator := &journalEvaluator{balances: accountBalances{}, priced: pricedBalances{}, trackAssertions: true}
	evaluator.observe = func(order, posting int, _ map[string]decimal.Decimal, isPriced bool) {
		priced[key{order, posting}] = isPriced
	}
	evaluator.run(resolved.TransactionsWithSource())

	assert.True(t, priced[key{1, 2}], "the assignment posting carries a cost")
	assert.False(t, priced[key{1, 0}])
	assert.False(t, priced[key{2, 0}], "a plain posting after it carries none")
	assert.False(t, priced[key{2, 1}])
}

// One replay answers several occurrences exactly as one replay per occurrence.
func TestRunningBalancesFor_MatchesOneReplayPerOccurrence(t *testing.T) {
	resolved := resolvedFromSources(t,
		journalSource{path: "/a.journal", content: "2024-01-01 seed\n    assets:cash  100 USD\n    equity\n"},
		journalSource{path: "/b.journal", content: "2024-01-02 счёт 費用\n    assets:cash  -30 USD\n    expenses\n"},
		journalSource{path: "/c.journal", content: "2024-01-03 later\n    assets:cash  -5 USD\n    expenses\n"},
	)
	sourced := resolved.TransactionsWithSource()
	ids := []include.OccurrenceID{3, 1, 2}

	together := RunningBalancesFor(sourced, ids)
	require.Len(t, together, 3)
	for _, id := range ids {
		assert.Equal(t, RunningBalances(sourced, id), together[id], "occurrence %d", id)
		assert.NotEmpty(t, together[id])
	}
}

func TestRunningBalances_AssertionCostIsInferredInTheCostCommodity(t *testing.T) {
	balances := primaryRunningBalances(t, `2024-01-01 касса
    касса:наличные  = 2 EUR @ 2 USD
    капитал

2024-01-02 касса
    касса:наличные  = 2 EUR @ 2 USD
    капитал
`)

	assert.True(t, balances[PostingRef{TransactionIndex: 0, PostingIndex: 0}]["EUR"].Equal(decimal.NewFromInt(2)))
	assert.True(t, balances[PostingRef{TransactionIndex: 0, PostingIndex: 1}]["USD"].Equal(decimal.NewFromInt(-4)))
	assert.NotContains(t, balances[PostingRef{TransactionIndex: 0, PostingIndex: 1}], "EUR")

	// The second assignment moves nothing, and hledger's register lists no row for it.
	assert.NotContains(t, balances, PostingRef{TransactionIndex: 1, PostingIndex: 0})
	assert.NotContains(t, balances, PostingRef{TransactionIndex: 1, PostingIndex: 1})
}

// An assignment moves its account by the difference from the balance before it,
// and the elided posting balances that difference: `hledger print -x` writes
// 4,700 and -4,700 for the second transaction, not 522,000.
func TestReplayFor_AssignedAmountsAreTheChange(t *testing.T) {
	resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: `2026-07-31 Zestimate
    активы:дом  =517300 USD
    equity:gains

2026-08-03 Zestimate
    активы:дом  =522000 USD
    equity:gains

2026-08-04 plain
    активы:дом  1 USD
    equity:gains
`})
	sourced := resolved.TransactionsWithSource()
	replay := ReplayFor(sourced, []include.OccurrenceID{sourced[0].OccurrenceID})[sourced[0].OccurrenceID]

	assert.Equal(t, "517300", replay.Assigned[PostingRef{0, 0}]["USD"].String())
	assert.Equal(t, "-517300", replay.Assigned[PostingRef{0, 1}]["USD"].String())
	assert.Equal(t, "4700", replay.Assigned[PostingRef{1, 0}]["USD"].String())
	assert.Equal(t, "-4700", replay.Assigned[PostingRef{1, 1}]["USD"].String())
	assert.Len(t, replay.Assigned, 4, "a transaction without an assignment has no assigned amounts")
	assert.Equal(t, "522001", runningBalanceAt(t, replay.Balances, 2, 0, "USD"))
}
