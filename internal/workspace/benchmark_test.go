package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/testutil"
)

var (
	smallJournal  = testutil.GenerateJournal(10)
	mediumJournal = testutil.GenerateJournal(100)
	largeJournal  = testutil.GenerateJournal(1000)
	xlargeJournal = testutil.GenerateJournal(10000)
)

func BenchmarkBuildFileIndex_Small(b *testing.B) {
	for b.Loop() {
		BuildFileIndexFromContent("test.journal", smallJournal)
	}
}

func BenchmarkBuildFileIndex_Medium(b *testing.B) {
	for b.Loop() {
		BuildFileIndexFromContent("test.journal", mediumJournal)
	}
}

func BenchmarkBuildFileIndex_Large(b *testing.B) {
	for b.Loop() {
		BuildFileIndexFromContent("test.journal", largeJournal)
	}
}

func BenchmarkBuildFileIndex_XLarge(b *testing.B) {
	for b.Loop() {
		BuildFileIndexFromContent("test.journal", xlargeJournal)
	}
}

func BenchmarkBuildFileIndex_Large_Allocs(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		BuildFileIndexFromContent("test.journal", largeJournal)
	}
}

func setupWorkspace(b *testing.B, content string) (*Workspace, string) {
	b.Helper()
	tmpDir := b.TempDir()
	mainPath := filepath.Join(tmpDir, "main.journal")
	if err := os.WriteFile(mainPath, []byte(content), 0644); err != nil {
		b.Fatal(err)
	}

	loader := include.NewLoader()
	ws := NewWorkspace(tmpDir, loader)
	if err := ws.Initialize(); err != nil {
		b.Fatal(err)
	}
	return ws, mainPath
}

func BenchmarkWorkspace_UpdateFile_Small(b *testing.B) {
	ws, mainPath := setupWorkspace(b, smallJournal)
	modified := smallJournal + "\n2024-12-31 New Transaction\n    expenses:test  $1\n    assets:cash\n"

	b.ResetTimer()
	for b.Loop() {
		ws.UpdateFile(mainPath, modified)
	}
}

func BenchmarkWorkspace_UpdateFile_Medium(b *testing.B) {
	ws, mainPath := setupWorkspace(b, mediumJournal)
	modified := mediumJournal + "\n2024-12-31 New Transaction\n    expenses:test  $1\n    assets:cash\n"

	b.ResetTimer()
	for b.Loop() {
		ws.UpdateFile(mainPath, modified)
	}
}

func BenchmarkWorkspace_UpdateFile_Large(b *testing.B) {
	ws, mainPath := setupWorkspace(b, largeJournal)
	modified := largeJournal + "\n2024-12-31 New Transaction\n    expenses:test  $1\n    assets:cash\n"

	b.ResetTimer()
	for b.Loop() {
		ws.UpdateFile(mainPath, modified)
	}
}

func BenchmarkWorkspace_UpdateFile_Large_Allocs(b *testing.B) {
	ws, mainPath := setupWorkspace(b, largeJournal)
	modified := largeJournal + "\n2024-12-31 New Transaction\n    expenses:test  $1\n    assets:cash\n"

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		ws.UpdateFile(mainPath, modified)
	}
}

func BenchmarkWorkspace_IndexSnapshot_Small(b *testing.B) {
	ws, _ := setupWorkspace(b, smallJournal)

	b.ResetTimer()
	for b.Loop() {
		_ = ws.IndexSnapshot()
	}
}

func BenchmarkWorkspace_IndexSnapshot_Medium(b *testing.B) {
	ws, _ := setupWorkspace(b, mediumJournal)

	b.ResetTimer()
	for b.Loop() {
		_ = ws.IndexSnapshot()
	}
}

func BenchmarkWorkspace_IndexSnapshot_Large(b *testing.B) {
	ws, _ := setupWorkspace(b, largeJournal)

	b.ResetTimer()
	for b.Loop() {
		_ = ws.IndexSnapshot()
	}
}
