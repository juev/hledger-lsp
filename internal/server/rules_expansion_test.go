package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/juev/hledger-lsp/internal/rules"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestRulesExpansion_ServerDiagnosticsRemap verifies that parse errors in
// included files are remapped to the original child URI and range.
func TestRulesExpansion_ServerDiagnosticsRemap(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")
	childFile := filepath.Join(dir, "child.rules")

	writeTestFile(t, mainFile, "skip 1\ninclude child.rules\nfields date,amount\n")
	writeTestFile(t, childFile, "unknown-directive value\n")

	loader := rules.NewLoader()
	result, errs := loader.Load(mainFile)
	if result == nil {
		t.Fatal("result is nil")
	}

	// Should have a parse error mapped to child.rules.
	var foundChildError bool
	for _, e := range errs {
		if e.Kind == rules.ErrorParseError && e.SourcePath == childFile {
			foundChildError = true
			if e.Range.Start.Line != 1 {
				t.Errorf("expected error on line 1 of child, got line %d", e.Range.Start.Line)
			}
		}
	}
	if !foundChildError {
		t.Errorf("expected parse error mapped to child file, got: %v", errs)
	}
}

// TestRulesExpansion_ServerMultiOwnerReload verifies that editing a child
// file triggers reload of all owning roots.
func TestRulesExpansion_ServerMultiOwnerReload(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")
	childFile := filepath.Join(dir, "child.rules")

	writeTestFile(t, mainFile, "include child.rules\nfields date,amount\n")
	writeTestFile(t, childFile, "separator ,\n")

	loader := rules.NewLoader()

	// Initial load.
	result1, errs1 := loader.Load(mainFile)
	if len(errs1) > 0 {
		t.Fatalf("unexpected errors: %v", errs1)
	}
	if len(result1.Occurrences) != 2 {
		t.Fatalf("expected 2 occurrences, got %d", len(result1.Occurrences))
	}

	// Edit child content.
	writeTestFile(t, childFile, "separator ;\ndecimal-mark ,\n")
	loader.InvalidateFile(childFile)

	// Reload — should pick up new child content.
	result2, errs2 := loader.Load(mainFile)
	if len(errs2) > 0 {
		t.Fatalf("unexpected errors after reload: %v", errs2)
	}
	if len(result2.Occurrences) != 2 {
		t.Fatalf("expected 2 occurrences after reload, got %d", len(result2.Occurrences))
	}

	// Verify the expanded content reflects the new child.
	if len(result2.Expanded) <= len(result1.Expanded) {
		t.Errorf("expected expanded content to grow after child edit")
	}
}

// TestRulesExpansion_ServerSuffixFreeInclude verifies that include of a
// non-.rules file works in the server context.
func TestRulesExpansion_ServerSuffixFreeInclude(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")
	incFile := filepath.Join(dir, "common.inc")

	writeTestFile(t, mainFile, "include common.inc\nfields date,amount\n")
	writeTestFile(t, incFile, "separator ,\n")

	loader := rules.NewLoader()
	result, errs := loader.Load(mainFile)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	// Should have 2 occurrences and the separator from .inc.
	if len(result.Occurrences) != 2 {
		t.Fatalf("expected 2 occurrences, got %d", len(result.Occurrences))
	}

	found := false
	for _, d := range result.Primary.Directives {
		if d.Name == "separator" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected separator from .inc file in expanded primary")
	}
}

// TestRulesExpansion_ServerOverlay verifies that LoadOptions overlays provide
// editor content for included files.
func TestRulesExpansion_ServerOverlay(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")
	childFile := filepath.Join(dir, "child.rules")

	writeTestFile(t, mainFile, "include child.rules\nfields date,amount\n")
	writeTestFile(t, childFile, "fields date,amount\n")

	loader := rules.NewLoader()

	// Override child with editor content (4 fields).
	opts := rules.LoadOptions{
		Overlays: map[string]rules.OverlayEntry{
			childFile: {
				SourcePath: childFile,
				Content:    "fields date,payee,amount,description\n",
				Revision:   1,
			},
		},
	}
	result, errs := loader.LoadWithOptions(mainFile, opts)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	// After expansion the primary contains both the child and root fields
	// directives in order. The child's fields directive should come first
	// (from the include site) with 4 names from the overlay.
	if len(result.Primary.FieldsDefs) < 1 {
		t.Fatal("expected at least 1 fields directive")
	}
	first := result.Primary.FieldsDefs[0]
	if len(first.Names) != 4 {
		t.Errorf("expected first fields directive to have 4 names from overlay, got %d: %v", len(first.Names), first.Names)
	}
}
