//go:build perf

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

// Ratios are asserted relative to the 1k baseline rather than against absolute
// wall-clock limits: shared CI runners vary enough that absolute thresholds
// flake, while a linear regression in the document-size term moves the ratio
// regardless of how fast the machine is.
const (
	// didChangeRatioLimit bounds 10k/1k for DidChange alone. The handler must
	// not do document-size work at all, so 2x leaves room for measurement noise.
	didChangeRatioLimit = 2.0
	// perKeystrokeCycleRatioLimit bounds 10k/1k for DidChange + Completion. The
	// cycle still re-parses once per keystroke, so it cannot be flat; 4x against
	// a linear 11x is the achievable bar without incremental AST parsing.
	perKeystrokeCycleRatioLimit = 4.0
	// keystrokeBurstRatioLimit bounds the per-keystroke cost of a burst with one
	// trailing request, relative to a full cycle per keystroke. Coalescing must
	// make a burst keystroke at least 2x cheaper than a cold cycle.
	keystrokeBurstRatioLimit = 0.5
)

// keystrokeServer builds an initialized workspace-mode server over a generated
// journal and returns it with the document URI. Workspace mode matters: without
// it DidChange takes the no-workspace fallback and skips the hot path.
func keystrokeServer(t *testing.T, transactions int) (*server.Server, uri.URI, string) {
	t.Helper()
	content := testutil.GenerateJournal(transactions)
	tmpDir := t.TempDir()
	mainPath, err := writeJournalFile(tmpDir, "main.journal", content)
	if err != nil {
		t.Fatal(err)
	}
	srv := newBenchmarkServerWithWorkspace(t, tmpDir)
	docURI := uri.URI("file://" + mainPath)
	srv.StoreDocument(docURI, content)
	return srv, docURI, mainPath
}

// typeKeystroke applies one incremental edit. The insertion point is line 1
// rather than {0,0}-{0,0}, which the server treats as a full-document replace.
func typeKeystroke(t *testing.T, srv *server.Server, docURI uri.URI) {
	t.Helper()
	change := &protocol.TextDocumentContentChangePartial{
		Range: protocol.Range{
			Start: protocol.Position{Line: 1, Character: 0},
			End:   protocol.Position{Line: 1, Character: 0},
		},
		Text: "; k\n",
	}
	if err := srv.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: docURI},
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{change},
	}); err != nil {
		t.Fatal(err)
	}
}

func completionParamsFor(docURI uri.URI) *protocol.CompletionParams {
	return &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 1, Character: 4},
		},
	}
}

// TestNFR_PerKeystrokeCycle measures one DidChange (incremental edit) followed by
// a Completion request in workspace mode, and asserts the 10k/1k ratio. The
// handler re-parses the document on every keystroke, so the cycle stays
// proportional to journal size; what this test guards is that the constant does
// not carry document-size work beyond the parse itself.
func TestNFR_PerKeystrokeCycle(t *testing.T) {
	measure := func(transactions int) time.Duration {
		srv, docURI, _ := keystrokeServer(t, transactions)
		completionParams := completionParamsFor(docURI)
		typeKeystroke(t, srv, docURI)

		const iterations = 50
		start := time.Now()
		for range iterations {
			typeKeystroke(t, srv, docURI)
			_, _ = srv.Completion(context.Background(), completionParams)
		}
		return time.Since(start) / iterations
	}

	small := measure(1000)
	large := measure(10000)
	ratio := float64(large) / float64(small)
	t.Logf("NFR per-keystroke cycle: 1k=%v, 10k=%v (ratio %.2fx)", small, large, ratio)

	if ratio > perKeystrokeCycleRatioLimit {
		t.Errorf("NFR per-keystroke cycle: 10k (%v) is %.2fx the cost of 1k (%v); expected <= %.1fx",
			large, ratio, small, perKeystrokeCycleRatioLimit)
	}
}

// TestNFR_DidChangeSubLinear verifies criterion 2 literally: the DidChange
// handler, with no request between keystrokes, must not scale with document
// size. Parsing is O(n) and is allowed to stay; what must not scale is the
// include-tree re-resolution and the workspace index rebuild.
func TestNFR_DidChangeSubLinear(t *testing.T) {
	measure := func(transactions int) time.Duration {
		srv, docURI, _ := keystrokeServer(t, transactions)
		typeKeystroke(t, srv, docURI) // warm the parse cache

		const iterations = 20
		start := time.Now()
		for range iterations {
			typeKeystroke(t, srv, docURI)
		}
		return time.Since(start) / iterations
	}

	small := measure(1000)
	large := measure(10000)
	ratio := float64(large) / float64(small)
	t.Logf("NFR DidChange sub-linear: 1k=%v, 10k=%v (ratio %.2fx)", small, large, ratio)

	if ratio > didChangeRatioLimit {
		t.Errorf("NFR DidChange: 10k (%v) is %.2fx the cost of 1k (%v); expected <= %.1fx",
			large, ratio, small, didChangeRatioLimit)
	}
}

// TestNFR_KeystrokeBurst verifies that a burst of keystrokes followed by a single
// request costs far less per keystroke than a full cycle per keystroke: the O(n)
// work runs once for the burst, not once per edit. The comparison is made at a
// fixed journal size, so it isolates coalescing from size scaling.
func TestNFR_KeystrokeBurst(t *testing.T) {
	const burst = 20

	measure := func(requestPerKeystroke bool) time.Duration {
		srv, docURI, _ := keystrokeServer(t, 10000)
		completionParams := completionParamsFor(docURI)
		typeKeystroke(t, srv, docURI)
		_, _ = srv.Completion(context.Background(), completionParams)

		start := time.Now()
		for range burst {
			typeKeystroke(t, srv, docURI)
			if requestPerKeystroke {
				_, _ = srv.Completion(context.Background(), completionParams)
			}
		}
		if !requestPerKeystroke {
			_, _ = srv.Completion(context.Background(), completionParams)
		}
		return time.Since(start) / burst
	}

	fullCycle := measure(true)
	burstCycle := measure(false)
	ratio := float64(burstCycle) / float64(fullCycle)
	t.Logf("NFR keystroke burst (%d keystrokes, 10k tx): full cycle=%v/keystroke, burst=%v/keystroke (ratio %.2fx)",
		burst, fullCycle, burstCycle, ratio)

	if ratio > keystrokeBurstRatioLimit {
		t.Errorf("NFR keystroke burst: burst costs %.2fx of a full cycle per keystroke (%v vs %v); expected <= %.2fx",
			ratio, burstCycle, fullCycle, keystrokeBurstRatioLimit)
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
