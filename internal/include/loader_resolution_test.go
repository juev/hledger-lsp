package include

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/juev/hledger-lsp/internal/ast"
)

func accountDirectiveName(t *testing.T, directive ast.Directive) string {
	t.Helper()
	accountDirective, ok := directive.(ast.AccountDirective)
	if !ok {
		t.Fatalf("directive is %T, want ast.AccountDirective", directive)
	}
	return accountDirective.Account.Name
}

func writeJournalFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func firstPostingQuantity(t *testing.T, result *ResolvedJournal, txIndex int) string {
	t.Helper()
	transactions := result.AllTransactions()
	if txIndex >= len(transactions) {
		t.Fatalf("transaction index %d out of range (%d)", txIndex, len(transactions))
	}
	amount := transactions[txIndex].Postings[0].Amount
	if amount == nil {
		t.Fatalf("transaction %d first posting has no amount", txIndex)
	}
	return amount.Quantity.String()
}

// A chain of 51 include edges exceeds a depth limit of 50, while a shallow
// root->L1->L2 chain stays within the limit. Depth counts include edges from
// the root, not previously processed files.
func TestLoader_DeepChainExceedsDepthLimitByEdges(t *testing.T) {
	t.Run("chain of 51 edges exceeds limit 50", func(t *testing.T) {
		dir := t.TempDir()
		const files = 52 // file0..file51, file_i includes file_{i+1}
		for i := range files - 1 {
			writeJournalFile(t, filepath.Join(dir, fmt.Sprintf("file%02d.journal", i)),
				fmt.Sprintf("include file%02d.journal\n", i+1))
		}
		writeJournalFile(t, filepath.Join(dir, fmt.Sprintf("file%02d.journal", files-1)),
			"2024-01-01 leaf\n    assets:cash  1 USD\n    equity:opening\n")

		loader := NewLoader()
		loader.SetLimits(Limits{MaxFileSizeBytes: defaultMaxFileSizeBytes, MaxIncludeDepth: 50})
		result, errs := loader.Load(filepath.Join(dir, "file00.journal"))
		if result == nil {
			t.Fatal("result is nil")
		}
		// file0..file50 load (depths 0..50); the edge to file51 (depth 51) errors.
		if got, want := len(result.Occurrences), 51; got != want {
			t.Fatalf("occurrences = %d, want %d", got, want)
		}
		depthErrors := 0
		for _, e := range errs {
			if e.Kind == ErrorDepthExceeded {
				depthErrors++
			}
		}
		if depthErrors != 1 {
			t.Fatalf("ErrorDepthExceeded count = %d, want 1 (errs=%v)", depthErrors, errs)
		}
	})

	t.Run("root to L1 to L2 boundary loads", func(t *testing.T) {
		dir := t.TempDir()
		writeJournalFile(t, filepath.Join(dir, "root.journal"), "include l1.journal\n")
		writeJournalFile(t, filepath.Join(dir, "l1.journal"), "include l2.journal\n")
		writeJournalFile(t, filepath.Join(dir, "l2.journal"),
			"2024-01-01 leaf\n    assets:cash  1 USD\n    equity:opening\n")

		result, errs := NewLoader().Load(filepath.Join(dir, "root.journal"))
		if len(errs) != 0 {
			t.Fatalf("Load() errors = %v", errs)
		}
		if got, want := len(result.Occurrences), 3; got != want {
			t.Fatalf("occurrences = %d, want %d", got, want)
		}
		if got, want := result.Occurrences[2].Via.Depth, 2; got != want {
			t.Errorf("leaf depth = %d, want %d", got, want)
		}
	})
}

// A glob can match the same canonical source through two paths (a real file
// and a symlink). hledger does not deduplicate literal/glob matches, so both
// produce occurrences; ByCanonical resolves the shared filesystem identity.
func TestLoader_GlobMatchesSameCanonicalTwice(t *testing.T) {
	dir := t.TempDir()
	realPath := filepath.Join(dir, "real.journal")
	linkPath := filepath.Join(dir, "link.journal")
	writeJournalFile(t, realPath, "2024-01-01 shared\n    assets:cash  1 USD\n    equity:opening\n")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	writeJournalFile(t, filepath.Join(dir, "main.journal"), "include *.journal\n")

	result, errs := NewLoader().Load(filepath.Join(dir, "main.journal"))
	if len(errs) != 0 {
		t.Fatalf("Load() errors = %v", errs)
	}
	// root + link + real (main excluded from its own glob).
	if got, want := len(result.Occurrences), 3; got != want {
		t.Fatalf("occurrences = %d, want %d", got, want)
	}
	canonical := canonicalPath(realPath)
	if got, want := len(result.ByCanonical[canonical]), 2; got != want {
		t.Fatalf("ByCanonical[real] = %d, want %d", got, want)
	}
	if got, want := len(result.AllTransactions()), 2; got != want {
		t.Fatalf("AllTransactions() = %d, want %d", got, want)
	}
}

// A diamond (A includes B and C; both include D) yields two occurrences of D
// and no cycle error: D is not an active ancestor when re-included.
func TestLoader_DiamondProducesTwoOccurrencesNoCycle(t *testing.T) {
	dir := t.TempDir()
	writeJournalFile(t, filepath.Join(dir, "a.journal"), "include b.journal\ninclude c.journal\n")
	writeJournalFile(t, filepath.Join(dir, "b.journal"), "include d.journal\n")
	writeJournalFile(t, filepath.Join(dir, "c.journal"), "include d.journal\n")
	writeJournalFile(t, filepath.Join(dir, "d.journal"), "2024-01-01 d\n    assets:cash  1 USD\n    equity:opening\n")

	result, errs := NewLoader().Load(filepath.Join(dir, "a.journal"))
	if len(errs) != 0 {
		t.Fatalf("Load() errors = %v, want none", errs)
	}
	// Preorder: a(1), b(2), d(3 via b), c(4), d(5 via c).
	if got, want := len(result.Occurrences), 5; got != want {
		t.Fatalf("occurrences = %d, want %d", got, want)
	}
	dPath := filepath.Join(dir, "d.journal")
	dOccurrences := result.OccurrencesForPath(dPath)
	if got, want := len(dOccurrences), 2; got != want {
		t.Fatalf("OccurrencesForPath(d) = %d, want %d", got, want)
	}
	if got, want := dOccurrences[0].ID, OccurrenceID(3); got != want {
		t.Errorf("first d ID = %d, want %d", got, want)
	}
	if got, want := dOccurrences[1].ID, OccurrenceID(5); got != want {
		t.Errorf("second d ID = %d, want %d", got, want)
	}
	if got, want := dOccurrences[0].Via.ParentID, OccurrenceID(2); got != want {
		t.Errorf("first d parent = %d, want 2 (b)", got)
	}
	if got, want := dOccurrences[1].Via.ParentID, OccurrenceID(4); got != want {
		t.Errorf("second d parent = %d, want 4 (c)", got)
	}
}

func TestLoader_DeterministicIDsAndProvenance(t *testing.T) {
	dir := t.TempDir()
	writeJournalFile(t, filepath.Join(dir, "root.journal"), "include child.journal\ninclude child.journal\n")
	writeJournalFile(t, filepath.Join(dir, "child.journal"), "2024-01-01 c\n    assets:cash  1 USD\n    equity:opening\n")

	result, errs := NewLoader().Load(filepath.Join(dir, "root.journal"))
	if len(errs) != 0 {
		t.Fatalf("Load() errors = %v", errs)
	}
	if got, want := len(result.Occurrences), 3; got != want {
		t.Fatalf("occurrences = %d, want %d", got, want)
	}
	for index, occurrence := range result.Occurrences {
		if got, want := occurrence.ID, OccurrenceID(index+1); got != want {
			t.Errorf("occurrence %d ID = %d, want %d", index, got, want)
		}
	}
	root := result.Occurrences[0]
	if root.Via.ParentID != 0 {
		t.Errorf("root ParentID = %d, want 0", root.Via.ParentID)
	}
	if root.Via.Depth != 0 {
		t.Errorf("root Depth = %d, want 0", root.Via.Depth)
	}
	// Each literal include is its own single-match include site, so both child
	// occurrences carry MatchIndex 0; they are distinguished by ID and include
	// range, not by MatchIndex (which indexes matches within one glob).
	for childIndex, occurrence := range result.Occurrences[1:] {
		if got, want := occurrence.Via.ParentID, OccurrenceID(1); got != want {
			t.Errorf("child %d ParentID = %d, want 1", childIndex, got)
		}
		if got, want := occurrence.Via.MatchIndex, 0; got != want {
			t.Errorf("child %d MatchIndex = %d, want 0", childIndex, got)
		}
		if got, want := occurrence.Via.Depth, 1; got != want {
			t.Errorf("child %d Depth = %d, want 1", childIndex, got)
		}
		if got, want := occurrence.Via.RawPattern, "child.journal"; got != want {
			t.Errorf("child %d RawPattern = %q, want %q", childIndex, got, want)
		}
	}
	if result.Occurrences[1].Via.IncludeRange.Start.Offset == result.Occurrences[2].Via.IncludeRange.Start.Offset {
		t.Errorf("both includes report the same IncludeRange offset %d", result.Occurrences[1].Via.IncludeRange.Start.Offset)
	}
}

// Including itself through a symlink resolves to the same canonical path and is
// ignored as an immediate self include (no occurrence, no cycle error).
func TestLoader_SelfIncludeViaSymlinkIgnored(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.journal")
	linkPath := filepath.Join(dir, "self-link.journal")
	writeJournalFile(t, mainPath, "include self-link.journal\n2024-01-01 t\n    assets:cash  1 USD\n    equity:opening\n")
	if err := os.Symlink(mainPath, linkPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	result, errs := NewLoader().Load(mainPath)
	if len(errs) != 0 {
		t.Fatalf("Load() errors = %v, want none (self include must be ignored)", errs)
	}
	if got, want := len(result.Occurrences), 1; got != want {
		t.Fatalf("occurrences = %d, want %d", got, want)
	}
}

func TestLoader_ByCanonicalFindsSymlinkOccurrence(t *testing.T) {
	dir := t.TempDir()
	realPath := filepath.Join(dir, "real.journal")
	linkPath := filepath.Join(dir, "link.journal")
	writeJournalFile(t, realPath, "2024-01-01 shared\n    assets:cash  1 USD\n    equity:opening\n")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	writeJournalFile(t, filepath.Join(dir, "main.journal"), "include link.journal\n")

	result, errs := NewLoader().Load(filepath.Join(dir, "main.journal"))
	if len(errs) != 0 {
		t.Fatalf("Load() errors = %v", errs)
	}
	byCanonical := result.OccurrencesForCanonical(realPath)
	if got, want := len(byCanonical), 1; got != want {
		t.Fatalf("OccurrencesForCanonical(real) = %d, want %d", got, want)
	}
	if got, want := byCanonical[0].Path, linkPath; got != want {
		t.Errorf("canonical occurrence Path = %q, want symlink path %q", got, want)
	}
	if got, want := byCanonical[0].CanonicalPath, canonicalPath(realPath); got != want {
		t.Errorf("canonical occurrence CanonicalPath = %q, want %q", got, want)
	}
	if got, want := len(result.OccurrencesForPath(linkPath)), 1; got != want {
		t.Errorf("OccurrencesForPath(link) = %d, want %d", got, want)
	}
}

func TestLoader_ColdWarmCacheEquivalent(t *testing.T) {
	dir := t.TempDir()
	writeJournalFile(t, filepath.Join(dir, "main.journal"), "include child.journal\n2024-01-02 after\n    assets:cash  1 USD\n    equity:opening\n")
	writeJournalFile(t, filepath.Join(dir, "child.journal"), "2024-01-01 child\n    assets:cash  1 USD\n    equity:opening\n")

	loader := NewLoader()
	mainPath := filepath.Join(dir, "main.journal")

	cold, coldErrs := loader.Load(mainPath)
	if len(coldErrs) != 0 {
		t.Fatalf("cold Load() errors = %v", coldErrs)
	}
	warm, warmErrs := loader.Load(mainPath)
	if len(warmErrs) != 0 {
		t.Fatalf("warm Load() errors = %v", warmErrs)
	}

	if len(cold.Occurrences) != len(warm.Occurrences) {
		t.Fatalf("occurrences cold=%d warm=%d", len(cold.Occurrences), len(warm.Occurrences))
	}
	coldTx, warmTx := cold.AllTransactions(), warm.AllTransactions()
	if len(coldTx) != len(warmTx) {
		t.Fatalf("transactions cold=%d warm=%d", len(coldTx), len(warmTx))
	}
	for index := range coldTx {
		if coldTx[index].Description != warmTx[index].Description {
			t.Errorf("transaction %d cold=%q warm=%q", index, coldTx[index].Description, warmTx[index].Description)
		}
	}
}

// Items interleave parent directives around the include point: a directive
// before the include precedes the child, a directive after follows it.
func TestLoader_InlineOrderIncludesDirectives(t *testing.T) {
	dir := t.TempDir()
	writeJournalFile(t, filepath.Join(dir, "main.journal"),
		"account assets:before\ninclude child.journal\naccount assets:after\n")
	writeJournalFile(t, filepath.Join(dir, "child.journal"), "account assets:child\n")

	result, errs := NewLoader().Load(filepath.Join(dir, "main.journal"))
	if len(errs) != 0 {
		t.Fatalf("Load() errors = %v", errs)
	}

	directives := result.AllDirectives()
	if got, want := len(directives), 3; got != want {
		t.Fatalf("AllDirectives() = %d, want %d", got, want)
	}
	wantOrder := []string{"assets:before", "assets:child", "assets:after"}
	for index, want := range wantOrder {
		if got := accountDirectiveName(t, directives[index]); got != want {
			t.Errorf("directive %d = %q, want %q", index, got, want)
		}
	}
}

// The same child included at two points receives the parser context active at
// each include site: an account prefix in effect at the first include resolves
// the child's accounts differently from the second include.
func TestLoader_ChildIncludedTwiceGetsEachIncludeSiteContext(t *testing.T) {
	dir := t.TempDir()
	writeJournalFile(t, filepath.Join(dir, "main.journal"),
		"apply account first\ninclude child.journal\nend apply account\napply account second\ninclude child.journal\nend apply account\n")
	writeJournalFile(t, filepath.Join(dir, "child.journal"), "2024-01-01 child\n    cash\n    assets:y\n")

	result, errs := NewLoader().Load(filepath.Join(dir, "main.journal"))
	if len(errs) != 0 {
		t.Fatalf("Load() errors = %v", errs)
	}
	occurrences := result.OccurrencesForPath(filepath.Join(dir, "child.journal"))
	if got, want := len(occurrences), 2; got != want {
		t.Fatalf("child occurrences = %d, want %d", got, want)
	}
	first := occurrences[0].Journal.Transactions[0].Postings[0].Account.ResolvedName
	second := occurrences[1].Journal.Transactions[0].Postings[0].Account.ResolvedName
	if first != "first:cash" {
		t.Errorf("first occurrence account = %q, want %q", first, "first:cash")
	}
	if second != "second:cash" {
		t.Errorf("second occurrence account = %q, want %q", second, "second:cash")
	}
}

// A child's declared commodity style merges forward into the parent after the
// include: `1.000 EUR` parses as 1000 once the child declares a comma decimal
// mark for EUR, whereas without the declaration it parses as 1.
func TestLoader_ChildCommodityMergesForwardToParentAfter(t *testing.T) {
	dir := t.TempDir()
	writeJournalFile(t, filepath.Join(dir, "main.journal"),
		"include child.journal\n2024-01-02 after\n    expenses:x  1.000 EUR\n    assets:y\n")
	writeJournalFile(t, filepath.Join(dir, "child.journal"), "commodity 1.000,00 EUR\n")

	result, errs := NewLoader().Load(filepath.Join(dir, "main.journal"))
	if len(errs) != 0 {
		t.Fatalf("Load() errors = %v", errs)
	}
	if got, want := firstPostingQuantity(t, result, 0), "1000"; got != want {
		t.Errorf("parent-after quantity = %q, want %q (child commodity must merge forward)", got, want)
	}
}

// Within a glob, each match is processed in order and sees the declared
// commodity styles of earlier matches: a.journal declares EUR's comma mark, so
// b.journal's `1.000 EUR` parses as 1000.
func TestLoader_ChildCommodityMergesForwardToNextGlobSibling(t *testing.T) {
	dir := t.TempDir()
	writeJournalFile(t, filepath.Join(dir, "main.journal"), "include *.journal\n")
	writeJournalFile(t, filepath.Join(dir, "a.journal"), "commodity 1.000,00 EUR\n")
	writeJournalFile(t, filepath.Join(dir, "b.journal"), "2024-01-01 b\n    expenses:x  1.000 EUR\n    assets:y\n")

	result, errs := NewLoader().Load(filepath.Join(dir, "main.journal"))
	if len(errs) != 0 {
		t.Fatalf("Load() errors = %v", errs)
	}
	if got, want := firstPostingQuantity(t, result, 0), "1000"; got != want {
		t.Errorf("glob sibling quantity = %q, want %q (later glob match must see earlier commodity)", got, want)
	}
}

// Child parse state other than declared commodity styles stays local to the
// child occurrence: D, decimal-mark, account prefix and alias do not affect the
// parent after the include.
func TestLoader_ChildLocalStateDoesNotLeakToParent(t *testing.T) {
	cases := []struct {
		name  string
		child string
		// parentAfter is the first posting of the parent transaction after include.
		wantQuantity string
		wantAccount  string
	}{
		{
			name:         "D default commodity stays local",
			child:        "D 1.000,00 EUR\n2024-01-01 c\n    expenses:x  1.000 EUR\n    assets:y\n",
			wantQuantity: "1",
			wantAccount:  "cash",
		},
		{
			name:         "decimal-mark stays local",
			child:        "decimal-mark ,\n2024-01-01 c\n    expenses:x  1.000 EUR\n    assets:y\n",
			wantQuantity: "1",
			wantAccount:  "cash",
		},
		{
			name:         "account prefix stays local",
			child:        "apply account childpfx\n2024-01-01 c\n    cash\n    assets:y\n",
			wantQuantity: "",
			wantAccount:  "cash",
		},
		{
			name:         "alias stays local",
			child:        "alias cash = Assets:ChildCash\n2024-01-01 c\n    cash\n    assets:y\n",
			wantQuantity: "",
			wantAccount:  "cash",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeJournalFile(t, filepath.Join(dir, "main.journal"),
				"include child.journal\n2024-01-02 after\n    cash  1.000 EUR\n    assets:y\n")
			writeJournalFile(t, filepath.Join(dir, "child.journal"), tc.child)

			result, errs := NewLoader().Load(filepath.Join(dir, "main.journal"))
			if len(errs) != 0 {
				t.Fatalf("Load() errors = %v", errs)
			}
			transactions := result.AllTransactions()
			// transactions[0] is the child's, transactions[1] is parent-after.
			if got, want := len(transactions), 2; got != want {
				t.Fatalf("transactions = %d, want %d", got, want)
			}
			parentAfter := transactions[1].Postings[0]
			if got, want := parentAfter.Account.ResolvedName, tc.wantAccount; got != want {
				t.Errorf("parent-after account = %q, want %q (child prefix/alias must not leak)", got, want)
			}
			if tc.wantQuantity != "" {
				if parentAfter.Amount == nil {
					t.Fatal("parent-after posting has no amount")
				}
				if got, want := parentAfter.Amount.Quantity.String(), tc.wantQuantity; got != want {
					t.Errorf("parent-after quantity = %q, want %q (child D/decimal-mark must not leak)", got, want)
				}
			}
		})
	}
}
