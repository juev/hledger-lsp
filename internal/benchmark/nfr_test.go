package benchmark

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/juev/hledger-lsp/internal/server"
	"github.com/juev/hledger-lsp/internal/testutil"
	"github.com/juev/hledger-lsp/internal/workspace"
)

func writeJournalFile(dir, name, content string) (string, error) {
	path := filepath.Join(dir, name)
	return path, os.WriteFile(path, []byte(content), 0644)
}

func setupWorkspaceAt(t *testing.T, dir string) *workspace.Workspace {
	t.Helper()
	loader := include.NewLoader()
	ws := workspace.NewWorkspace(dir, loader)
	if err := ws.Initialize(); err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestNFR_1_1_CompletionLatency(t *testing.T) {
	content := testutil.GenerateJournal(1000)
	srv := server.NewServer()
	uri := protocol.DocumentURI("file:///test.journal")
	srv.StoreDocument(uri, content)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 10, Character: 4},
		},
	}

	const iterations = 100
	start := time.Now()
	for range iterations {
		_, _ = srv.Completion(context.Background(), params)
	}
	totalDuration := time.Since(start)
	avgDuration := totalDuration / iterations

	if avgDuration >= 100*time.Millisecond {
		t.Errorf("NFR-1.1: Completion should be < 100ms, got %v (avg of %d iterations)", avgDuration, iterations)
	} else {
		t.Logf("NFR-1.1 PASS: Completion took %v avg (target: < 100ms, %d iterations)", avgDuration, iterations)
	}
}

func TestNFR_1_2_ParsingLatency(t *testing.T) {
	content := testutil.GenerateJournal(10000)

	start := time.Now()
	_, _ = parser.Parse(content)
	duration := time.Since(start)

	if duration >= 500*time.Millisecond {
		t.Errorf("NFR-1.2: Parsing 10k transactions should be < 500ms, got %v", duration)
	} else {
		t.Logf("NFR-1.2 PASS: Parsing 10k transactions took %v (target: < 500ms)", duration)
	}
}

func TestNFR_1_3_IncrementalUpdateLatency(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	content := testutil.GenerateJournal(1000)
	tmpDir := t.TempDir()

	mainPath, err := writeJournalFile(tmpDir, "main.journal", content)
	if err != nil {
		t.Fatal(err)
	}

	ws := setupWorkspaceAt(t, tmpDir)

	rootPath := ws.RootJournalPath()
	if rootPath == "" {
		t.Fatal("workspace not initialized: root journal path is empty")
	}
	if mainPath != rootPath {
		t.Fatalf("path mismatch: mainPath=%s, rootPath=%s", mainPath, rootPath)
	}

	initialSnapshot := ws.IndexSnapshot()
	if len(initialSnapshot.Accounts.All) == 0 {
		t.Fatal("workspace not initialized: no accounts found")
	}

	const iterations = 100
	modifiedContents := make([]string, iterations)
	for i := range iterations {
		modifiedContents[i] = content + fmt.Sprintf("\n2024-12-31 New Transaction %d\n    expenses:test%d  $%d\n    assets:cash\n", i, i, i+1)
	}

	start := time.Now()
	for i := range iterations {
		ws.UpdateFile(mainPath, modifiedContents[i])
	}
	totalDuration := time.Since(start)
	avgDuration := totalDuration / iterations

	if avgDuration >= 50*time.Millisecond {
		t.Errorf("NFR-1.3: Incremental update should be < 50ms, got %v (avg of %d iterations)", avgDuration, iterations)
	} else {
		t.Logf("NFR-1.3 PASS: Incremental update took %v avg (target: < 50ms, %d iterations)", avgDuration, iterations)
	}
}

func TestNFR_1_4_MemoryUsage(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	content := testutil.GenerateJournal(10000)
	tmpDir := t.TempDir()

	_, err := writeJournalFile(tmpDir, "main.journal", content)
	if err != nil {
		t.Fatal(err)
	}

	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	ws := setupWorkspaceAt(t, tmpDir)
	snapshot := ws.IndexSnapshot()

	if len(snapshot.Accounts.All) == 0 {
		t.Fatal("workspace not initialized: no accounts found")
	}

	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	usedBytes := m2.HeapAlloc - m1.HeapAlloc
	usedMB := usedBytes / (1024 * 1024)

	t.Logf("Heap: before=%dMB, after=%dMB, delta=%dMB (%d bytes)",
		m1.HeapAlloc/(1024*1024), m2.HeapAlloc/(1024*1024), usedMB, usedBytes)
	t.Logf("Accounts: %d, Payees: %d, Transactions: %d",
		len(snapshot.Accounts.All), len(snapshot.Payees), len(snapshot.Transactions))

	if usedMB >= 200 {
		t.Errorf("NFR-1.4: Memory usage should be < 200MB, got %dMB", usedMB)
	} else {
		t.Logf("NFR-1.4 PASS: Memory usage is %dMB (target: < 200MB)", usedMB)
	}
}
