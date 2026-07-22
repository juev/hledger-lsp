package include

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoader_OccurrencesInlineOrderAndRepeatedInclude(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.journal")
	childPath := filepath.Join(dir, "child.journal")

	main := `2024-01-01 parent before
    assets:cash  1 USD
    equity:opening
include child.journal
2024-01-03 parent after
    assets:cash  1 USD
    equity:opening
include child.journal
`
	child := `2024-01-02 child
    assets:cash  1 USD
    equity:opening
`
	if err := os.WriteFile(mainPath, []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(childPath, []byte(child), 0o644); err != nil {
		t.Fatal(err)
	}

	result, errs := NewLoader().Load(mainPath)
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
	if got, want := result.Occurrences[1].Via.ParentID, OccurrenceID(1); got != want {
		t.Errorf("first child ParentID = %d, want %d", got, want)
	}
	if got, want := result.Occurrences[2].Via.MatchIndex, 0; got != want {
		t.Errorf("second literal MatchIndex = %d, want %d", got, want)
	}
	if got, want := len(result.OccurrencesForPath(childPath)), 2; got != want {
		t.Errorf("OccurrencesForPath() = %d, want %d", got, want)
	}

	transactions := result.AllTransactions()
	if got, want := len(transactions), 4; got != want {
		t.Fatalf("AllTransactions() count = %d, want %d", got, want)
	}
	for index, want := range []string{"parent before", "child", "parent after", "child"} {
		if got := transactions[index].Description; got != want {
			t.Errorf("transaction %d = %q, want %q", index, got, want)
		}
	}
}

func TestLoader_DepthCountsEdgesAndDoesNotCountSiblings(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.journal")
	childPath := filepath.Join(dir, "child.journal")

	main := ""
	for range 51 {
		main += "include child.journal\n"
	}
	if err := os.WriteFile(mainPath, []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(childPath, []byte(`2024-01-01 child
    assets:cash  1 USD
    equity:opening
`), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader()
	loader.SetLimits(Limits{MaxFileSizeBytes: defaultMaxFileSizeBytes, MaxIncludeDepth: 1})
	result, errs := loader.Load(mainPath)
	if len(errs) != 0 {
		t.Fatalf("Load() errors = %v", errs)
	}
	if got, want := len(result.Occurrences), 52; got != want {
		t.Fatalf("occurrences = %d, want %d", got, want)
	}
	if got, want := len(result.AllTransactions()), 51; got != want {
		t.Fatalf("AllTransactions() = %d, want %d", got, want)
	}
}

func TestLoader_ImmediateSelfIncludeIsIgnoredButAncestorBackEdgeErrors(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.journal")
	childPath := filepath.Join(dir, "child.journal")
	if err := os.WriteFile(mainPath, []byte(`include main.journal
include child.journal
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(childPath, []byte(`include main.journal
`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, errs := NewLoader().Load(mainPath)
	if result == nil {
		t.Fatal("Load() result is nil")
	}
	if got, want := len(result.Occurrences), 2; got != want {
		t.Fatalf("occurrences = %d, want %d", got, want)
	}
	if got, want := len(errs), 1; got != want {
		t.Fatalf("errors = %v, want one ancestor cycle error", errs)
	}
	if got, want := errs[0].Kind, ErrorCycleDetected; got != want {
		t.Errorf("error kind = %v, want %v", got, want)
	}
}
