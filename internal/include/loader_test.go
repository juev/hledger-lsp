package include

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoader_LoadSingleFile(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.journal")

	content := `2024-01-15 * grocery store
    expenses:food  $50.00
    assets:cash
`
	if err := os.WriteFile(mainFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader()
	result, errs := loader.Load(mainFile)

	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.Primary == nil {
		t.Fatal("primary journal is nil")
	}
	if len(result.Primary.Transactions) != 1 {
		t.Errorf("expected 1 transaction, got %d", len(result.Primary.Transactions))
	}
}

func TestLoader_LoadWithInclude(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.journal")
	accountsFile := filepath.Join(dir, "accounts.journal")

	mainContent := `include accounts.journal

2024-01-15 * grocery store
    expenses:food  $50.00
    assets:cash
`
	accountsContent := `account expenses:food
account assets:cash
`
	if err := os.WriteFile(mainFile, []byte(mainContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(accountsFile, []byte(accountsContent), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader()
	result, errs := loader.Load(mainFile)

	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if result == nil {
		t.Fatal("result is nil")
	}

	allTx := result.AllTransactions()
	if len(allTx) != 1 {
		t.Errorf("expected 1 transaction, got %d", len(allTx))
	}

	allDirs := result.AllDirectives()
	if len(allDirs) != 2 {
		t.Errorf("expected 2 directives, got %d", len(allDirs))
	}

	if len(result.Files) != 1 {
		t.Errorf("expected 1 included file, got %d", len(result.Files))
	}
}

func TestLoader_RecursiveInclude(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.journal")
	level1File := filepath.Join(dir, "level1.journal")
	level2File := filepath.Join(dir, "level2.journal")

	mainContent := `include level1.journal

2024-01-15 * main transaction
    expenses:main  $10.00
    assets:cash
`
	level1Content := `include level2.journal

2024-01-16 * level1 transaction
    expenses:level1  $20.00
    assets:cash
`
	level2Content := `2024-01-17 * level2 transaction
    expenses:level2  $30.00
    assets:cash
`
	if err := os.WriteFile(mainFile, []byte(mainContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(level1File, []byte(level1Content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(level2File, []byte(level2Content), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader()
	result, errs := loader.Load(mainFile)

	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if result == nil {
		t.Fatal("result is nil")
	}

	allTx := result.AllTransactions()
	if len(allTx) != 3 {
		t.Errorf("expected 3 transactions, got %d", len(allTx))
	}

	if len(result.Files) != 2 {
		t.Errorf("expected 2 included files, got %d", len(result.Files))
	}
}

func TestLoader_CycleDetection(t *testing.T) {
	dir := t.TempDir()
	fileA := filepath.Join(dir, "a.journal")
	fileB := filepath.Join(dir, "b.journal")

	contentA := `include b.journal

2024-01-15 * transaction A
    expenses:a  $10.00
    assets:cash
`
	contentB := `include a.journal

2024-01-16 * transaction B
    expenses:b  $20.00
    assets:cash
`
	if err := os.WriteFile(fileA, []byte(contentA), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte(contentB), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader()
	result, errs := loader.Load(fileA)

	if result == nil {
		t.Fatal("result is nil")
	}

	var cycleErrors []LoadError
	for _, e := range errs {
		if e.Kind == ErrorCycleDetected {
			cycleErrors = append(cycleErrors, e)
		}
	}

	if len(cycleErrors) == 0 {
		t.Error("expected cycle detection error, got none")
	}
}

func TestLoader_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.journal")

	content := `include nonexistent.journal

2024-01-15 * transaction
    expenses:food  $50.00
    assets:cash
`
	if err := os.WriteFile(mainFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader()
	result, errs := loader.Load(mainFile)

	if result == nil {
		t.Fatal("result is nil")
	}

	var notFoundErrors []LoadError
	for _, e := range errs {
		if e.Kind == ErrorFileNotFound {
			notFoundErrors = append(notFoundErrors, e)
		}
	}

	if len(notFoundErrors) == 0 {
		t.Error("expected file not found error, got none")
	}
}

func TestLoader_ParseErrorInIncludedFile(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.journal")
	badFile := filepath.Join(dir, "bad.journal")

	mainContent := `include bad.journal

2024-01-15 * transaction
    expenses:food  $50.00
    assets:cash
`
	badContent := `this is not valid hledger syntax
completely broken content 12345 @#$%
`
	if err := os.WriteFile(mainFile, []byte(mainContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badFile, []byte(badContent), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader()
	result, _ := loader.Load(mainFile)

	if result == nil {
		t.Fatal("result is nil")
	}

	if result.Primary == nil {
		t.Fatal("primary journal is nil")
	}
	if len(result.Primary.Transactions) != 1 {
		t.Errorf("expected 1 transaction in main file, got %d", len(result.Primary.Transactions))
	}
}

func TestLoader_LoadFromContent(t *testing.T) {
	content := `2024-01-15 * grocery store
    expenses:food  $50.00
    assets:cash
`
	loader := NewLoader()
	result, errs := loader.LoadFromContent("/virtual/main.journal", content)

	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.Primary == nil {
		t.Fatal("primary journal is nil")
	}
	if len(result.Primary.Transactions) != 1 {
		t.Errorf("expected 1 transaction, got %d", len(result.Primary.Transactions))
	}
}

func TestLoader_SelfInclude(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.journal")

	content := `include main.journal

2024-01-15 * transaction
    expenses:food  $50.00
    assets:cash
`
	if err := os.WriteFile(mainFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader()
	result, errs := loader.Load(mainFile)

	if result == nil {
		t.Fatal("result is nil")
	}

	var cycleErrors []LoadError
	for _, e := range errs {
		if e.Kind == ErrorCycleDetected {
			cycleErrors = append(cycleErrors, e)
		}
	}

	if len(cycleErrors) == 0 {
		t.Error("expected cycle detection error for self-include, got none")
	}
}

func TestLoader_GlobPattern(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.journal")
	fileA := filepath.Join(dir, "a.journal")
	fileB := filepath.Join(dir, "b.journal")

	mainContent := `include *.journal

2024-01-01 * main transaction
    expenses:main  $10
    assets:cash
`
	contentA := `2024-06-15 * transaction A
    expenses:a  $20
    assets:cash
`
	contentB := `2025-06-15 * transaction B
    expenses:b  $30
    assets:cash
`
	if err := os.WriteFile(mainFile, []byte(mainContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileA, []byte(contentA), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte(contentB), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader()
	result, errs := loader.Load(mainFile)

	for _, e := range errs {
		if e.Kind == ErrorCycleDetected {
			continue
		}
		t.Errorf("unexpected error: %v", e)
	}

	if result == nil {
		t.Fatal("result is nil")
	}

	allTx := result.AllTransactions()
	if len(allTx) != 3 {
		t.Errorf("expected 3 transactions, got %d", len(allTx))
	}
}

func TestLoader_GlobPatternNoMatch(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.journal")

	content := `include *.nonexistent

2024-01-15 * transaction
    expenses:food  $50
    assets:cash
`
	if err := os.WriteFile(mainFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader()
	result, errs := loader.Load(mainFile)

	if result == nil {
		t.Fatal("result is nil")
	}

	var notFoundErrors []LoadError
	for _, e := range errs {
		if e.Kind == ErrorFileNotFound {
			notFoundErrors = append(notFoundErrors, e)
		}
	}

	if len(notFoundErrors) == 0 {
		t.Error("expected error for no matching files")
	}
}

func TestLoader_HledgerGlobSyntax(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.journal")
	subdir := filepath.Join(dir, "accounts")
	subFile := filepath.Join(subdir, "accounts.journal")

	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	mainContent := `include <->/*.journal

2024-01-01 * main
    e:main  $10
    a:cash
`
	subContent := `account expenses:food
`
	if err := os.WriteFile(mainFile, []byte(mainContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subFile, []byte(subContent), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader()
	result, errs := loader.Load(mainFile)

	for _, e := range errs {
		t.Logf("error: %v", e)
	}

	if result == nil {
		t.Fatal("result is nil")
	}

	allDirs := result.AllDirectives()
	if len(allDirs) != 1 {
		t.Errorf("expected 1 directive from subdir, got %d", len(allDirs))
	}
}
