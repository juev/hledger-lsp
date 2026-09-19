package include

import (
	"fmt"
	"strings"
	"testing"
)

func journalOf(transactions int) string {
	var sb strings.Builder
	for i := range transactions {
		fmt.Fprintf(&sb, "2024-01-%02d payee %d\n    expenses:food  $%d\n    assets:cash\n\n", i%28+1, i, i+1)
	}
	return sb.String()
}

// Identity is the observable: a caller that gets the same pointer back did not
// parse anything.
func TestLoader_ParseCached_ReusesResult(t *testing.T) {
	loader := NewLoader()
	content := journalOf(10)

	first, errs := loader.ParseCached(content)
	if len(errs) != 0 {
		t.Fatalf("ParseCached errors = %v", errs)
	}
	second, _ := loader.ParseCached(content)
	if first != second {
		t.Error("the same content must return the same journal, not a re-parse")
	}

	other, _ := loader.ParseCached(journalOf(11))
	if other == first {
		t.Error("different content must not return the cached journal")
	}
}

// paddedSource builds a comment of exactly size bytes, so the test's arithmetic
// matches the cache's own accounting, which counts len(content).
func paddedSource(marker byte, size int) string {
	prefix := fmt.Sprintf("; %c ", marker)
	return prefix + strings.Repeat("x", size-len(prefix)-1) + "\n"
}

func TestLoader_ParseCached_EvictsLeastRecentlyUsed(t *testing.T) {
	loader := NewLoader()

	// Each entry is under the per-entry cap but a third of the budget, so a
	// fourth source cannot fit and the least recently used has to go.
	chunk := parseCacheBudget/3 + 1
	if chunk > parseCacheMaxEntry {
		t.Fatalf("test chunk %d exceeds the per-entry cap %d", chunk, parseCacheMaxEntry)
	}

	first, _ := loader.ParseCached(paddedSource('a', chunk))
	if first == nil {
		t.Fatal("parse must succeed")
	}
	for _, marker := range []byte{'b', 'c', 'd'} {
		if _, errs := loader.ParseCached(paddedSource(marker, chunk)); len(errs) != 0 {
			t.Fatalf("parse of %q failed: %v", marker, errs)
		}
	}

	if bytes, _ := loader.parseCacheStats(); bytes > parseCacheBudget {
		t.Errorf("cache holds %d bytes, over the %d budget", bytes, parseCacheBudget)
	}

	if again, _ := loader.ParseCached(paddedSource('a', chunk)); again == first {
		t.Error("the least recently used entry must have been evicted")
	}
}

func TestLoader_ParseCached_SkipsOversizedContent(t *testing.T) {
	loader := NewLoader()
	huge := paddedSource('h', parseCacheMaxEntry+1)

	first, _ := loader.ParseCached(huge)
	if _, entries := loader.parseCacheStats(); entries != 0 {
		t.Errorf("content over the per-entry cap must not be cached, got %d entries", entries)
	}
	if bytes, _ := loader.parseCacheStats(); bytes != 0 {
		t.Errorf("oversized content must not be counted against the budget, got %d bytes", bytes)
	}

	second, _ := loader.ParseCached(huge)
	if first == second {
		t.Error("an oversized source must be parsed again rather than cached")
	}
}

func TestLoader_ParseCached_StaysWithinBudget(t *testing.T) {
	loader := NewLoader()
	// Each entry is small enough to be cached but there are far more of them
	// than the budget can hold.
	for i := range 200 {
		loader.ParseCached(journalOf(20) + fmt.Sprintf("; %d\n", i))
	}

	bytes, entries := loader.parseCacheStats()
	if bytes > parseCacheBudget {
		t.Errorf("cache holds %d bytes, over the %d budget", bytes, parseCacheBudget)
	}
	if entries == 0 {
		t.Error("cache evicted everything, so it is not caching at all")
	}
	if entries > 200 {
		t.Errorf("cache holds %d entries for 200 distinct sources", entries)
	}
}
