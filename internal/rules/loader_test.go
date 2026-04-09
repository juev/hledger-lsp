package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Test fixture helper: writes a file and fails the test if that fails.
func writeRulesFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestRulesLoader_LoadSingleFile(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")

	writeRulesFile(t, mainFile, `skip 1
fields date,description,amount
`)

	loader := NewLoader()
	result, errs := loader.Load(mainFile)

	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.Primary == nil {
		t.Fatal("primary is nil")
	}
	if len(result.Primary.FieldsDefs) != 1 {
		t.Errorf("expected 1 fields directive, got %d", len(result.Primary.FieldsDefs))
	}
	if len(result.Files) != 0 {
		t.Errorf("expected no included files, got %d", len(result.Files))
	}
}

func TestRulesLoader_LoadWithInclude(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")
	commonFile := filepath.Join(dir, "common.rules")

	writeRulesFile(t, mainFile, `include common.rules
skip 1
`)
	writeRulesFile(t, commonFile, `fields date,payee,amount
date-format %Y-%m-%d
`)

	loader := NewLoader()
	result, errs := loader.Load(mainFile)

	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if result == nil || result.Primary == nil {
		t.Fatal("result or primary is nil")
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 included file, got %d", len(result.Files))
	}
	included, ok := result.Files[commonFile]
	if !ok {
		t.Fatalf("included file not in result.Files; keys: %v", keysOf(result.Files))
	}
	if len(included.FieldsDefs) != 1 {
		t.Errorf("expected 1 fields directive in included file, got %d", len(included.FieldsDefs))
	}
	if len(included.FieldsDefs[0].Names) != 3 {
		t.Errorf("expected 3 field names in included file, got %d", len(included.FieldsDefs[0].Names))
	}
	if len(result.FileOrder) != 1 || result.FileOrder[0] != commonFile {
		t.Errorf("unexpected FileOrder: %v", result.FileOrder)
	}
}

func TestRulesLoader_TransitiveInclude(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")
	midFile := filepath.Join(dir, "mid.rules")
	deepFile := filepath.Join(dir, "deep.rules")

	writeRulesFile(t, mainFile, "include mid.rules\nskip 1\n")
	writeRulesFile(t, midFile, "include deep.rules\n")
	writeRulesFile(t, deepFile, "fields date,payee,amount\n")

	loader := NewLoader()
	result, errs := loader.Load(mainFile)

	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if len(result.Files) != 2 {
		t.Errorf("expected 2 included files, got %d", len(result.Files))
	}
	if _, ok := result.Files[midFile]; !ok {
		t.Errorf("mid.rules missing from result.Files")
	}
	if _, ok := result.Files[deepFile]; !ok {
		t.Errorf("deep.rules missing from result.Files")
	}
}

func TestRulesLoader_SelfInclude(t *testing.T) {
	dir := t.TempDir()
	selfFile := filepath.Join(dir, "self.rules")

	writeRulesFile(t, selfFile, "include self.rules\nfields date,amount\n")

	loader := NewLoader()
	_, errs := loader.Load(selfFile)

	if !hasErrorKind(errs, ErrorCycleDetected) {
		t.Errorf("expected ErrorCycleDetected, got: %v", errs)
	}
}

func TestRulesLoader_CircularInclude(t *testing.T) {
	dir := t.TempDir()
	aFile := filepath.Join(dir, "a.rules")
	bFile := filepath.Join(dir, "b.rules")

	writeRulesFile(t, aFile, "include b.rules\n")
	writeRulesFile(t, bFile, "include a.rules\nfields date,amount\n")

	loader := NewLoader()
	_, errs := loader.Load(aFile)

	if !hasErrorKind(errs, ErrorCycleDetected) {
		t.Errorf("expected ErrorCycleDetected in circular include, got: %v", errs)
	}
}

func TestRulesLoader_MissingInclude(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")

	writeRulesFile(t, mainFile, "include nonexistent.rules\n")

	loader := NewLoader()
	result, errs := loader.Load(mainFile)

	if !hasErrorKind(errs, ErrorFileNotFound) {
		t.Errorf("expected ErrorFileNotFound, got: %v", errs)
	}
	// Primary should still be parsed successfully despite missing include.
	if result == nil || result.Primary == nil {
		t.Fatal("primary must be parsed even when include is missing")
	}
}

func TestRulesLoader_IncludeNotRulesFile(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")
	wrongFile := filepath.Join(dir, "wrong.journal")

	writeRulesFile(t, mainFile, "include wrong.journal\n")
	writeRulesFile(t, wrongFile, "2024-01-01 * test\n")

	loader := NewLoader()
	result, errs := loader.Load(mainFile)

	if !hasErrorKind(errs, ErrorNotRules) {
		t.Errorf("expected ErrorNotRules, got: %v", errs)
	}
	// Non-rules file must not be added to Files map.
	if _, ok := result.Files[wrongFile]; ok {
		t.Errorf("non-rules file should not appear in result.Files")
	}
}

func TestRulesLoader_LoadFromContent(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")
	commonFile := filepath.Join(dir, "common.rules")

	// Write the real common file to disk.
	writeRulesFile(t, commonFile, "fields date,payee,amount\n")

	// main file does not exist on disk — we pass its content directly.
	editorContent := "include common.rules\nskip 1\n"

	loader := NewLoader()
	result, errs := loader.LoadFromContent(mainFile, editorContent)

	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if _, ok := result.Files[commonFile]; !ok {
		t.Errorf("common.rules should be loaded from disk when LoadFromContent is used")
	}
}

func TestRulesLoader_CacheInvalidation(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")
	commonFile := filepath.Join(dir, "common.rules")

	writeRulesFile(t, mainFile, "include common.rules\n")
	writeRulesFile(t, commonFile, "fields date,amount\n")

	loader := NewLoader()
	first, _ := loader.Load(mainFile)
	if n := len(first.Files[commonFile].FieldsDefs[0].Names); n != 2 {
		t.Fatalf("expected 2 field names in first load, got %d", n)
	}

	// Update the included file on disk.
	writeRulesFile(t, commonFile, "fields date,payee,amount,description\n")

	// Without invalidation, the cached version is returned.
	loader.InvalidateFile(commonFile)

	second, _ := loader.Load(mainFile)
	if n := len(second.Files[commonFile].FieldsDefs[0].Names); n != 4 {
		t.Errorf("expected 4 field names after invalidation, got %d", n)
	}
}

func TestRulesLoader_CRLFLineEndings(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")
	commonFile := filepath.Join(dir, "common.rules")

	writeRulesFile(t, mainFile, "include common.rules\r\nskip 1\r\n")
	writeRulesFile(t, commonFile, "fields date,payee,amount\r\n")

	loader := NewLoader()
	result, errs := loader.Load(mainFile)

	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	included, ok := result.Files[commonFile]
	if !ok {
		t.Fatal("common.rules not loaded")
	}
	if len(included.FieldsDefs) != 1 {
		t.Fatalf("expected 1 fields directive, got %d", len(included.FieldsDefs))
	}
	names := included.FieldsDefs[0].Names
	if len(names) != 3 || names[0] != "date" || names[2] != "amount" {
		t.Errorf("CRLF normalization broken: got %v", names)
	}
}

func TestRulesLoader_ContentGetterPreferred(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")
	commonFile := filepath.Join(dir, "common.rules")

	writeRulesFile(t, mainFile, "include common.rules\n")
	// On-disk version has 2 fields.
	writeRulesFile(t, commonFile, "fields date,amount\n")

	loader := NewLoader()
	// Editor version has 4 fields — the getter overrides the disk copy.
	loader.SetContentGetter(func(path string) (string, bool, error) {
		if path == commonFile {
			return "fields date,payee,amount,description\n", true, nil
		}
		return "", false, nil
	})

	result, _ := loader.Load(mainFile)
	included, ok := result.Files[commonFile]
	if !ok {
		t.Fatal("common.rules not loaded")
	}
	if n := len(included.FieldsDefs[0].Names); n != 4 {
		t.Errorf("expected editor copy (4 fields), got %d — content getter was ignored", n)
	}
}

func TestRulesLoader_RelativePathResolution(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	mainFile := filepath.Join(subdir, "main.rules")
	commonFile := filepath.Join(dir, "common.rules")

	writeRulesFile(t, mainFile, "include ../common.rules\n")
	writeRulesFile(t, commonFile, "fields date,payee,amount\n")

	loader := NewLoader()
	result, errs := loader.Load(mainFile)

	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if _, ok := result.Files[commonFile]; !ok {
		t.Errorf("expected common.rules at %s in result.Files; keys: %v",
			commonFile, keysOf(result.Files))
	}
}

func TestRulesLoader_FileTooLarge(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.rules")

	writeRulesFile(t, mainFile, strings.Repeat("x", 200))

	loader := NewLoader()
	loader.SetLimits(Limits{MaxFileSizeBytes: 100, MaxIncludeDepth: 10})
	_, errs := loader.Load(mainFile)

	if !hasErrorKind(errs, ErrorFileTooLarge) {
		t.Errorf("expected ErrorFileTooLarge, got: %v", errs)
	}
}

func TestRulesLoader_InvalidateClearsEntry(t *testing.T) {
	dir := t.TempDir()
	commonFile := filepath.Join(dir, "common.rules")
	writeRulesFile(t, commonFile, "fields a,b\n")

	loader := NewLoader()
	loader.Load(commonFile) // populate cache (through Load → loadWithContent)

	// Snapshot cache presence via ClearCache vs InvalidateFile semantics:
	// after InvalidateFile the entry should be re-read from disk next time.
	writeRulesFile(t, commonFile, "fields a,b,c\n")
	loader.InvalidateFile(commonFile)

	result, _ := loader.Load(commonFile)
	if len(result.Primary.FieldsDefs[0].Names) != 3 {
		t.Errorf("after InvalidateFile, expected fresh parse with 3 fields")
	}
}

// hasErrorKind reports whether errs contains at least one LoadError with the given Kind.
func hasErrorKind(errs []LoadError, kind ErrorKind) bool {
	for _, e := range errs {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

func keysOf(m map[string]*RulesFile) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
