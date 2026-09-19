package include

import (
	"os"
	"path/filepath"
	"testing"
)

// The canonical path doubles as the key of the text cache, so InvalidateFile
// must evict the entry that was stored under the resolved path even when the
// caller names the file through a symlink. The memoized resolution is what
// makes that cheap; these tests pin that it did not change the answer.
func TestLoader_InvalidateFileThroughSymlinkEvictsRealPath(t *testing.T) {
	dir := t.TempDir()
	realPath := filepath.Join(dir, "real.journal")
	linkPath := filepath.Join(dir, "link.journal")
	writeJournalFile(t, realPath, "2024-01-01 first\n    assets:cash  1 USD\n    equity:opening\n")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	loader := NewLoader()
	if _, errs := loader.Load(linkPath); len(errs) != 0 {
		t.Fatalf("first Load() errors = %v", errs)
	}

	writeJournalFile(t, realPath, "2024-01-02 second\n    assets:bank  2 USD\n    equity:opening\n")
	loader.InvalidateFile(linkPath)

	resolved, errs := loader.Load(linkPath)
	if len(errs) != 0 {
		t.Fatalf("second Load() errors = %v", errs)
	}
	descriptions := make(map[string]bool)
	for _, tx := range resolved.AllTransactions() {
		descriptions[tx.Description] = true
	}
	if !descriptions["second"] {
		t.Errorf("Load() after InvalidateFile returned stale content: %v", descriptions)
	}
}

func TestLoader_CanonicalPathAgreesWithUncachedResolution(t *testing.T) {
	dir := t.TempDir()
	realPath := filepath.Join(dir, "real.journal")
	linkPath := filepath.Join(dir, "link.journal")
	writeJournalFile(t, realPath, "2024-01-01 x\n    assets:cash  1 USD\n    equity:opening\n")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	loader := NewLoader()
	first := loader.canonicalPath(linkPath)
	second := loader.canonicalPath(linkPath)
	if first != second {
		t.Fatalf("canonicalPath is not stable: %q then %q", first, second)
	}
	if want := resolveCanonicalPath(absoluteClean(linkPath)); first != want {
		t.Errorf("memoized canonical path = %q, uncached = %q", first, want)
	}
	if first == absoluteClean(linkPath) {
		t.Errorf("canonicalPath(%q) returned the symlink itself, so the cache key does not follow the link", linkPath)
	}
}
