package rules

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExpansion_IncludeBetweenDeclarations verifies that text included between
// two rules declarations is spliced inline before parse, so declarations above
// and below the include site both apply.
func TestExpansion_IncludeBetweenDeclarations(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")
	commonFile := filepath.Join(dir, "common.rules")

	writeRulesFile(t, mainFile, "skip 1\ninclude common.rules\nfields date,amount\n")
	writeRulesFile(t, commonFile, "separator ,\n")

	loader := NewLoader()
	result, errs := loader.Load(mainFile)
	assertNoErrors(t, errs)

	if result == nil || result.Primary == nil {
		t.Fatal("result or primary is nil")
	}
	// After textual expansion, the combined text is:
	//   skip 1
	//   separator ,
	//   fields date,amount
	// So Primary should have skip (SimpleDirective), separator (SimpleDirective)
	// and fields (FieldsDirective).
	if len(result.Primary.FieldsDefs) != 1 {
		t.Errorf("expected 1 fields directive, got %d", len(result.Primary.FieldsDefs))
	}
	// separator from included file should be present.
	found := false
	for _, d := range result.Primary.Directives {
		if d.Name == "separator" && d.Value == "," {
			found = true
		}
	}
	if !found {
		t.Errorf("expected separator from included file in primary directives")
	}
}

// TestExpansion_RepeatedLiteralInclude verifies that the same file included at
// two positions produces two occurrences and its text is spliced in both places.
func TestExpansion_RepeatedLiteralInclude(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")
	commonFile := filepath.Join(dir, "common.rules")

	writeRulesFile(t, mainFile, "include common.rules\nskip 1\ninclude common.rules\n")
	writeRulesFile(t, commonFile, "fields date,amount\n")

	loader := NewLoader()
	result, errs := loader.Load(mainFile)
	assertNoErrors(t, errs)

	if len(result.Occurrences) != 3 {
		t.Fatalf("expected 3 occurrences (root + 2 includes), got %d", len(result.Occurrences))
	}
	// Both occurrences of common should be present.
	var commonCount int
	for _, occ := range result.Occurrences {
		if occ.Path == commonFile {
			commonCount++
		}
	}
	if commonCount != 2 {
		t.Errorf("expected 2 occurrences of common.rules, got %d", commonCount)
	}
}

// TestExpansion_DiamondRepeats verifies that a diamond include (A→B, A→C,
// B→D, C→D) produces two occurrences of D without a cycle error.
func TestExpansion_DiamondRepeats(t *testing.T) {
	dir := t.TempDir()
	aFile := filepath.Join(dir, "a.rules")
	bFile := filepath.Join(dir, "b.rules")
	cFile := filepath.Join(dir, "c.rules")
	dFile := filepath.Join(dir, "d.rules")

	writeRulesFile(t, aFile, "include b.rules\ninclude c.rules\n")
	writeRulesFile(t, bFile, "include d.rules\n")
	writeRulesFile(t, cFile, "include d.rules\n")
	writeRulesFile(t, dFile, "fields date,amount\n")

	loader := NewLoader()
	result, errs := loader.Load(aFile)
	assertNoErrors(t, errs)

	// D should appear twice (via B and via C).
	var dCount int
	for _, occ := range result.Occurrences {
		if occ.Path == dFile {
			dCount++
		}
	}
	if dCount != 2 {
		t.Errorf("expected 2 occurrences of d.rules in diamond, got %d", dCount)
	}
}

// TestExpansion_DepthLimit verifies that edge-based depth counting correctly
// rejects includes that exceed the limit while allowing wide trees.
func TestExpansion_DepthLimit(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")

	// Build a chain of 5 files: main → f1 → f2 → f3 → f4
	// With depth limit 3, the chain main(0)→f1(1)→f2(2)→f3(3) is OK
	// but f3→f4 (depth 4) exceeds the limit.
	writeRulesFile(t, mainFile, "include f1.rules\n")
	writeRulesFile(t, filepath.Join(dir, "f1.rules"), "include f2.rules\n")
	writeRulesFile(t, filepath.Join(dir, "f2.rules"), "include f3.rules\n")
	writeRulesFile(t, filepath.Join(dir, "f3.rules"), "include f4.rules\n")
	writeRulesFile(t, filepath.Join(dir, "f4.rules"), "fields date,amount\n")

	loader := NewLoader()
	loader.SetLimits(Limits{MaxIncludeDepth: 3})
	_, errs := loader.Load(mainFile)

	if !hasErrorKind(errs, ErrorDepthExceeded) {
		t.Errorf("expected ErrorDepthExceeded with depth limit 3 and chain of 4, got: %v", errs)
	}
}

// TestExpansion_DepthBoundary verifies that a chain at exactly the limit works.
func TestExpansion_DepthBoundary(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")

	writeRulesFile(t, mainFile, "include l1.rules\n")
	writeRulesFile(t, filepath.Join(dir, "l1.rules"), "include l2.rules\n")
	writeRulesFile(t, filepath.Join(dir, "l2.rules"), "fields date,amount\n")

	loader := NewLoader()
	loader.SetLimits(Limits{MaxIncludeDepth: 3})
	result, errs := loader.Load(mainFile)
	assertNoErrors(t, errs)

	if result == nil || result.Primary == nil {
		t.Fatal("result or primary is nil")
	}
}

// TestExpansion_SourceMappedParseErrors verifies that parse errors in expanded
// text are remapped to original file URI and line.
func TestExpansion_SourceMappedParseErrors(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")
	commonFile := filepath.Join(dir, "common.rules")

	writeRulesFile(t, mainFile, "skip 1\ninclude common.rules\nfields date,amount\n")
	// common.rules has a bad directive on line 1.
	writeRulesFile(t, commonFile, "bad-directive value\n")

	loader := NewLoader()
	_, errs := loader.Load(mainFile)

	// Should have a parse error mapped to common.rules.
	found := false
	for _, e := range errs {
		if e.Kind == ErrorParseError && e.SourcePath == commonFile {
			found = true
			if e.Range.Start.Line != 1 {
				t.Errorf("expected error on line 1 of common.rules, got line %d", e.Range.Start.Line)
			}
		}
	}
	if !found {
		t.Errorf("expected parse error mapped to common.rules, got: %v", errs)
	}
}

// TestExpansion_SuffixFreeInclude verifies that `include common.inc` works
// without requiring .rules suffix.
func TestExpansion_SuffixFreeInclude(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")
	incFile := filepath.Join(dir, "common.inc")

	writeRulesFile(t, mainFile, "include common.inc\nfields date,amount\n")
	writeRulesFile(t, incFile, "separator ,\n")

	loader := NewLoader()
	result, errs := loader.Load(mainFile)
	assertNoErrors(t, errs)

	if len(result.Occurrences) != 2 {
		t.Fatalf("expected 2 occurrences, got %d", len(result.Occurrences))
	}
	// separator from .inc should be present in primary.
	found := false
	for _, d := range result.Primary.Directives {
		if d.Name == "separator" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected separator from .inc file in primary")
	}
}

// TestExpansion_SelfIncludeIgnored verifies that immediate self-include is
// silently ignored (not a cycle error).
func TestExpansion_SelfIncludeIgnored(t *testing.T) {
	dir := t.TempDir()
	selfFile := filepath.Join(dir, "self.rules")

	writeRulesFile(t, selfFile, "include self.rules\nfields date,amount\n")

	loader := NewLoader()
	result, errs := loader.Load(selfFile)

	// Self-include should be ignored, not produce a cycle error.
	for _, e := range errs {
		if e.Kind == ErrorCycleDetected {
			t.Errorf("self-include should be silently ignored, got ErrorCycleDetected")
		}
	}

	if result == nil || result.Primary == nil {
		t.Fatal("result or primary is nil")
	}
	if len(result.Primary.FieldsDefs) != 1 {
		t.Errorf("expected 1 fields directive despite self-include, got %d", len(result.Primary.FieldsDefs))
	}
}

// TestExpansion_AncestorCycleDetected verifies that an active-ancestor back
// edge produces a cycle error.
func TestExpansion_AncestorCycleDetected(t *testing.T) {
	dir := t.TempDir()
	aFile := filepath.Join(dir, "a.rules")
	bFile := filepath.Join(dir, "b.rules")

	writeRulesFile(t, aFile, "include b.rules\n")
	writeRulesFile(t, bFile, "include a.rules\n")

	loader := NewLoader()
	_, errs := loader.Load(aFile)

	if !hasErrorKind(errs, ErrorCycleDetected) {
		t.Errorf("expected ErrorCycleDetected for ancestor back-edge, got: %v", errs)
	}
}

// TestExpansion_CompletionDeclarationOrder verifies that completion sees fields
// declared in included files in textual-expansion order.
func TestExpansion_CompletionDeclarationOrder(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")
	commonFile := filepath.Join(dir, "common.rules")

	writeRulesFile(t, mainFile, "fields date\ninclude common.rules\nfields amount\n")
	writeRulesFile(t, commonFile, "fields payee\n")

	loader := NewLoader()
	result, errs := loader.Load(mainFile)
	assertNoErrors(t, errs)

	fields := collectDeclaredFieldsExpanded(result, result.Primary)
	// After textual expansion the combined text is:
	//   fields date
	//   fields payee   (from common)
	//   fields amount
	// So declared fields should be: date, payee, amount
	if len(fields) < 3 {
		t.Fatalf("expected at least 3 declared fields, got %d: %v", len(fields), fields)
	}
	if fields[0] != "date" || fields[1] != "payee" || fields[2] != "amount" {
		t.Errorf("expected [date payee amount], got %v", fields)
	}
}

// TestExpansion_ColdWarmEquality verifies that loading with empty cache and
// with pre-warmed cache produce identical results.
func TestExpansion_ColdWarmEquality(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")
	commonFile := filepath.Join(dir, "common.rules")

	writeRulesFile(t, mainFile, "include common.rules\nfields date,amount\n")
	writeRulesFile(t, commonFile, "separator ,\n")

	// Cold load.
	coldLoader := NewLoader()
	coldResult, coldErrs := coldLoader.Load(mainFile)
	assertNoErrors(t, coldErrs)

	// Warm load (populate cache first).
	warmLoader := NewLoader()
	warmLoader.Load(mainFile) // warm up
	warmResult, warmErrs := warmLoader.Load(mainFile)
	assertNoErrors(t, warmErrs)

	// Compare primary directive counts.
	if len(coldResult.Primary.Directives) != len(warmResult.Primary.Directives) {
		t.Errorf("cold/warm directive count mismatch: %d vs %d",
			len(coldResult.Primary.Directives), len(warmResult.Primary.Directives))
	}
	if len(coldResult.Primary.FieldsDefs) != len(warmResult.Primary.FieldsDefs) {
		t.Errorf("cold/warm fields count mismatch: %d vs %d",
			len(coldResult.Primary.FieldsDefs), len(warmResult.Primary.FieldsDefs))
	}
	if len(coldResult.Occurrences) != len(warmResult.Occurrences) {
		t.Errorf("cold/warm occurrence count mismatch: %d vs %d",
			len(coldResult.Occurrences), len(warmResult.Occurrences))
	}
}

// TestExpansion_LoadOptions verifies that immutable LoadOptions replaces
// ContentGetter for providing editor content.
func TestExpansion_LoadOptions(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")
	commonFile := filepath.Join(dir, "common.rules")

	writeRulesFile(t, mainFile, "include common.rules\n")
	// On-disk version has 2 fields.
	writeRulesFile(t, commonFile, "fields date,amount\n")

	loader := NewLoader()
	// Editor override via LoadOptions.
	opts := LoadOptions{
		Overlays: map[string]OverlayEntry{
			commonFile: {
				SourcePath: commonFile,
				Content:    "fields date,payee,amount,description\n",
				Revision:   1,
			},
		},
	}
	result, errs := loader.LoadWithOptions(mainFile, opts)
	assertNoErrors(t, errs)

	// After expansion the primary contains both the child and root fields
	// directives. The first fields directive comes from the overlay (4 names).
	if len(result.Primary.FieldsDefs) < 1 {
		t.Fatal("expected at least 1 fields directive")
	}
	first := result.Primary.FieldsDefs[0]
	if len(first.Names) != 4 {
		t.Errorf("expected first fields directive to have 4 names from overlay, got %d: %v", len(first.Names), first.Names)
	}
}

// TestExpansion_ByCanonicalLookup verifies that occurrences can be found by
// canonical (real) path even when included through a symlink.
func TestExpansion_ByCanonicalLookup(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")
	realFile := filepath.Join(dir, "real.rules")
	linkFile := filepath.Join(dir, "link.rules")

	writeRulesFile(t, mainFile, "include link.rules\n")
	writeRulesFile(t, realFile, "fields date,amount\n")
	if err := os.Symlink(realFile, linkFile); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	loader := NewLoader()
	result, errs := loader.Load(mainFile)
	assertNoErrors(t, errs)

	// OccurrencesForCanonical should find the occurrence using the real path,
	// even though it was included through a symlink.
	occs := result.OccurrencesForCanonical(realFile)
	if len(occs) == 0 {
		t.Errorf("expected OccurrencesForCanonical(%s) to find occurrence, got empty", realFile)
	}
}

func assertNoErrors(t *testing.T, errs []LoadError) {
	t.Helper()
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}
