package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juev/hledger-lsp/internal/parser"
)

func TestGenerateJournal_ProducesNonEmptyContent(t *testing.T) {
	content := GenerateJournal(100)
	if len(content) == 0 {
		t.Error("Generated journal is empty")
	}
	if !strings.Contains(content, "2020-") {
		t.Error("Generated journal does not contain expected date format")
	}
}

func TestGenerateJournal_CreatesExpectedTransactions(t *testing.T) {
	content := GenerateJournal(50)
	journal, _ := parser.Parse(content)

	if len(journal.Transactions) != 50 {
		t.Errorf("Expected 50 transactions, got %d", len(journal.Transactions))
	}
}

func TestGenerateJournal_ContainsExpectedAccounts(t *testing.T) {
	content := GenerateJournal(10)

	expectedAccounts := []string{
		"expenses:food:groceries",
		"assets:bank:checking",
		"assets:cash",
	}

	for _, acc := range expectedAccounts {
		if !strings.Contains(content, acc) {
			t.Errorf("Expected account %q not found in generated journal", acc)
		}
	}
}

func TestGenerateIncludeTree_CreatesFiles(t *testing.T) {
	tmpDir := t.TempDir()
	mainPath, err := GenerateIncludeTree(tmpDir, 3, 10)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(mainPath); os.IsNotExist(err) {
		t.Error("Main journal file was not created")
	}

	for i := range 3 {
		filePath := filepath.Join(tmpDir, fmt.Sprintf("file%d.journal", i))
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Errorf("Include file %s was not created", filePath)
		}
	}
}

func TestGenerateIncludeTree_MainContainsIncludes(t *testing.T) {
	tmpDir := t.TempDir()
	mainPath, err := GenerateIncludeTree(tmpDir, 3, 10)
	if err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}

	for i := range 3 {
		expected := fmt.Sprintf("include file%d.journal", i)
		if !strings.Contains(string(content), expected) {
			t.Errorf("Expected include directive %q not found", expected)
		}
	}
}
