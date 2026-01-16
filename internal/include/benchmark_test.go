package include

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/juev/hledger-lsp/internal/testutil"
)

var (
	smallJournal  = testutil.GenerateJournal(10)
	mediumJournal = testutil.GenerateJournal(100)
	largeJournal  = testutil.GenerateJournal(1000)
)

func setupSingleFile(b *testing.B, content string) string {
	b.Helper()
	tmpDir := b.TempDir()
	path := filepath.Join(tmpDir, "test.journal")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		b.Fatal(err)
	}
	return path
}

func BenchmarkLoader_Load_Small(b *testing.B) {
	path := setupSingleFile(b, smallJournal)
	loader := NewLoader()

	b.ResetTimer()
	for b.Loop() {
		loader.Load(path)
	}
}

func BenchmarkLoader_Load_Medium(b *testing.B) {
	path := setupSingleFile(b, mediumJournal)
	loader := NewLoader()

	b.ResetTimer()
	for b.Loop() {
		loader.Load(path)
	}
}

func BenchmarkLoader_Load_Large(b *testing.B) {
	path := setupSingleFile(b, largeJournal)
	loader := NewLoader()

	b.ResetTimer()
	for b.Loop() {
		loader.Load(path)
	}
}

func BenchmarkLoader_Load_Large_Allocs(b *testing.B) {
	path := setupSingleFile(b, largeJournal)
	loader := NewLoader()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		loader.Load(path)
	}
}

func BenchmarkLoader_LoadFromContent_Small(b *testing.B) {
	loader := NewLoader()

	for b.Loop() {
		loader.LoadFromContent("test.journal", smallJournal)
	}
}

func BenchmarkLoader_LoadFromContent_Large(b *testing.B) {
	loader := NewLoader()

	for b.Loop() {
		loader.LoadFromContent("test.journal", largeJournal)
	}
}

func setupIncludeTree(b *testing.B, numFiles int) string {
	b.Helper()
	tmpDir := b.TempDir()
	mainPath, err := testutil.GenerateIncludeTree(tmpDir, numFiles, 20)
	if err != nil {
		b.Fatal(err)
	}
	return mainPath
}

func BenchmarkLoader_Load_IncludeTree_5Files(b *testing.B) {
	mainPath := setupIncludeTree(b, 5)
	loader := NewLoader()

	b.ResetTimer()
	for b.Loop() {
		loader.Load(mainPath)
	}
}

func BenchmarkLoader_Load_IncludeTree_10Files(b *testing.B) {
	mainPath := setupIncludeTree(b, 10)
	loader := NewLoader()

	b.ResetTimer()
	for b.Loop() {
		loader.Load(mainPath)
	}
}

func BenchmarkLoader_Load_IncludeTree_20Files(b *testing.B) {
	mainPath := setupIncludeTree(b, 20)
	loader := NewLoader()

	b.ResetTimer()
	for b.Loop() {
		loader.Load(mainPath)
	}
}

func BenchmarkLoader_Load_IncludeTree_20Files_Allocs(b *testing.B) {
	mainPath := setupIncludeTree(b, 20)
	loader := NewLoader()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		loader.Load(mainPath)
	}
}
