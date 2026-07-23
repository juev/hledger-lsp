package server

import (
	"sync"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

const sampleJournal = "2024-01-15 * store\n    expenses:food  $50\n    assets:cash  $-50\n"

func TestCachedParse_ReusesJournalForSameContent(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///a.journal")

	first := srv.cachedParse(uri, sampleJournal)
	second := srv.cachedParse(uri, sampleJournal)

	require.NotNil(t, first.journal)
	assert.Same(t, first.journal, second.journal, "same content must reuse the parsed journal")
	assert.Equal(t, first.parseErrs, second.parseErrs)
}

func TestCachedParse_RebuildsOnContentChange(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///a.journal")

	first := srv.cachedParse(uri, sampleJournal)
	changed := srv.cachedParse(uri, "2024-01-16 * other\n    expenses:y  $2\n    assets:cash  $-2\n")

	assert.NotSame(t, first.journal, changed.journal, "changed content must reparse")
}

func TestCachedParse_RebuildsAfterInvalidate(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///a.journal")

	first := srv.cachedParse(uri, sampleJournal)
	srv.invalidateParseCache(uri)
	second := srv.cachedParse(uri, sampleJournal)

	assert.NotSame(t, first.journal, second.journal, "invalidation must force a reparse")
}

func TestCachedParse_ConcurrentSafe(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///a.journal")

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			doc := srv.cachedParse(uri, sampleJournal)
			assert.NotNil(t, doc.journal)
			_ = srv.cachedBalances(uri, sampleJournal)
		}()
	}
	wg.Wait()
}

func TestCachedBalances_ComputedOnceAndCorrect(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///a.journal")

	balances := srv.cachedBalances(uri, sampleJournal)
	require.NotNil(t, balances)
	assert.True(t, balances["expenses:food"]["$"].Equal(decimal.NewFromInt(50)),
		"expenses:food balance = 50, got %v", balances["expenses:food"]["$"])
	assert.True(t, balances["assets:cash"]["$"].Equal(decimal.NewFromInt(-50)),
		"assets:cash balance = -50, got %v", balances["assets:cash"]["$"])

	// Same content returns the identical cached map (computed once via sync.Once).
	again := srv.cachedBalances(uri, sampleJournal)
	assert.Equal(t, balances, again)
}
