//go:build !race

package benchmark

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/document"
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
	uri := uri.URI("file:///test.journal")
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

// newBenchmarkServerWithWorkspace initializes a server in workspace-folder mode
// so DidChange exercises the real hot path (UpdateFileWithJournal + include
// resolution) rather than the no-workspace fallback.
func newBenchmarkServerWithWorkspace(t *testing.T, dir string) *server.Server {
	t.Helper()
	srv := server.NewServer()
	if _, err := srv.Initialize(context.Background(), &protocol.InitializeParams{
		WorkspaceFoldersInitializeParams: protocol.WorkspaceFoldersInitializeParams{
			WorkspaceFolders: protocol.NewNullable([]protocol.WorkspaceFolder{{
				URI:  uri.URI("file://" + dir),
				Name: "bench",
			}}),
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := srv.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	return srv
}

// TestNFR_PerKeystrokeCycle measures one DidChange (incremental edit) followed by
// a Completion request in workspace mode. DidChange parses the edited content
// once (UpdateFileWithJournal reuses the cached journal); Completion reuses that
// cached journal instead of reparsing.
func TestNFR_PerKeystrokeCycle(t *testing.T) {
	for _, n := range []int{1000, 10000} {
		t.Run(fmt.Sprintf("%dtx", n), func(t *testing.T) {
			content := testutil.GenerateJournal(n)
			tmpDir := t.TempDir()
			mainPath, err := writeJournalFile(tmpDir, "main.journal", content)
			if err != nil {
				t.Fatal(err)
			}
			srv := newBenchmarkServerWithWorkspace(t, tmpDir)
			docURI := uri.URI("file://" + mainPath)
			srv.StoreDocument(docURI, content)

			completionParams := &protocol.CompletionParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
					Position:     protocol.Position{Line: 1, Character: 4},
				},
			}

			const iterations = 50
			start := time.Now()
			for range iterations {
				// Incremental insert at line 1 (NOT {0,0}-{0,0}, which the server
				// treats as a full-document replace). Keeps the document large so
				// each keystroke does a real parse of the whole journal.
				change := &protocol.TextDocumentContentChangePartial{
					Range: protocol.Range{
						Start: protocol.Position{Line: 1, Character: 0},
						End:   protocol.Position{Line: 1, Character: 0},
					},
					Text: "; k\n",
				}
				_ = srv.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
					TextDocument: protocol.VersionedTextDocumentIdentifier{
						TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: docURI},
					},
					ContentChanges: []protocol.TextDocumentContentChangeEvent{change},
				})
				_, _ = srv.Completion(context.Background(), completionParams)
			}
			avg := time.Since(start) / iterations

			limit := 50 * time.Millisecond
			if n >= 10000 {
				limit = 200 * time.Millisecond
			}
			if avg >= limit {
				t.Errorf("NFR per-keystroke cycle (%d tx) should be < %v, got %v", n, limit, avg)
			} else {
				t.Logf("NFR per-keystroke cycle PASS (%d tx): %v avg (DidChange + Completion, target < %v)", n, avg, limit)
			}
		})
	}
}

// TestNFR_RepeatedRequestReusesCache verifies that repeated requests for an
// unchanged document reuse the cached parse: the first (cold) request parses the
// 10k-transaction document, subsequent (warm) requests skip the parse.
func TestNFR_RepeatedRequestReusesCache(t *testing.T) {
	content := testutil.GenerateJournal(10000)
	srv := server.NewServer()
	docURI := uri.URI("file:///test.journal")
	srv.StoreDocument(docURI, content)

	hoverParams := &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 1, Character: 6},
		},
	}

	coldStart := time.Now()
	_, _ = srv.Hover(context.Background(), hoverParams)
	cold := time.Since(coldStart)

	const iterations = 100
	warmStart := time.Now()
	for range iterations {
		_, _ = srv.Hover(context.Background(), hoverParams)
	}
	warmAvg := time.Since(warmStart) / iterations

	t.Logf("NFR cache reuse (10k tx): cold first hover %v, warm avg %v over %d iterations", cold, warmAvg, iterations)
	if warmAvg >= cold {
		t.Errorf("NFR cache reuse: warm avg (%v) should be faster than cold first request (%v)", warmAvg, cold)
	} else {
		t.Logf("NFR cache reuse PASS: warm avg %v < cold %v", warmAvg, cold)
	}
}

// TestNFR_ApplyChangeSubLinear verifies criterion 4: applying an edit to the
// document rope does not scale with the total line count. A single-character
// insert in the middle of a 10k-transaction journal must not be ~10x slower than
// the same edit on a 1k-transaction journal (the rope edit is O(log n) in lines).
func TestNFR_ApplyChangeSubLinear(t *testing.T) {
	measure := func(transactions int) time.Duration {
		content := testutil.GenerateJournal(transactions)
		lineCount := strings.Count(content, "\n") + 1
		mid := uint32(lineCount / 2)
		r := protocol.Range{
			Start: protocol.Position{Line: mid, Character: 0},
			End:   protocol.Position{Line: mid, Character: 0},
		}
		nt := document.NewText(content)

		const iterations = 500
		start := time.Now()
		for range iterations {
			nt.ApplyChange(r, "x")
		}
		return time.Since(start) / iterations
	}

	small := measure(1000)
	large := measure(10000)
	t.Logf("NFR ApplyChange sub-linear: 1k=%v, 10k=%v (ratio %.2fx)", small, large, float64(large)/float64(small))

	// 10x more lines must not mean 10x slower edits; allow generous headroom for
	// measurement noise while still catching a linear (O(n)) regression.
	if large > small*6 {
		t.Errorf("NFR ApplyChange: 10k (%v) more than 6x slower than 1k (%v); expected sub-linear", large, small)
	} else {
		t.Logf("NFR ApplyChange sub-linear PASS: 10k/1k ratio %.2fx (< 6x)", float64(large)/float64(small))
	}
}
