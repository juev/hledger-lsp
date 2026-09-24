//go:build !race

package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/workspace"
)

func generateJournal(numTransactions int) string {
	var sb strings.Builder

	accounts := []string{
		"expenses:food:groceries",
		"expenses:food:restaurants",
		"expenses:transport:fuel",
		"expenses:utilities:electricity",
		"expenses:utilities:water",
		"assets:bank:checking",
		"assets:bank:savings",
		"assets:cash",
		"liabilities:credit:visa",
		"income:salary",
	}

	commodities := []string{"$", "EUR", "RUB"}

	for i := range numTransactions {
		year := 2020 + (i / 365)
		month := (i/30)%12 + 1
		day := i%28 + 1

		fromAcc := accounts[i%len(accounts)]
		toAcc := accounts[(i+1)%len(accounts)]
		commodity := commodities[i%len(commodities)]
		amount := (i%1000 + 1) * 10

		fmt.Fprintf(&sb, "%04d-%02d-%02d * Payee %d | Transaction note\n", year, month, day, i)
		fmt.Fprintf(&sb, "    %s  %s%d.%02d\n", fromAcc, commodity, amount/100, amount%100)

		if i%5 == 0 {
			fmt.Fprintf(&sb, "    %s  %s%d.%02d @ $1.10\n", toAcc, commodity, amount/100, amount%100)
			sb.WriteString("    assets:cash\n")
		} else {
			fmt.Fprintf(&sb, "    %s\n", toAcc)
		}

		if i%10 == 0 {
			fmt.Fprintf(&sb, "    ; tag:value%d\n", i)
		}

		sb.WriteString("\n")
	}

	return sb.String()
}

var (
	smallContent  = generateJournal(10)
	mediumContent = generateJournal(100)
	largeContent  = generateJournal(1000)
)

func BenchmarkCompletion_Account_Small(b *testing.B) {
	srv := NewServer()
	docURI := uri.URI("file:///bench.journal")
	srv.documents.Store(docURI, smallContent)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 1, Character: 4},
		},
	}

	ctx := context.Background()
	for b.Loop() {
		_, _ = srv.completion(ctx, params)
	}
}

func BenchmarkCompletion_Account_Medium(b *testing.B) {
	srv := NewServer()
	docURI := uri.URI("file:///bench.journal")
	srv.documents.Store(docURI, mediumContent)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 1, Character: 4},
		},
	}

	ctx := context.Background()
	for b.Loop() {
		_, _ = srv.completion(ctx, params)
	}
}

func BenchmarkCompletion_Account_Large(b *testing.B) {
	srv := NewServer()
	docURI := uri.URI("file:///bench.journal")
	srv.documents.Store(docURI, largeContent)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 1, Character: 4},
		},
	}

	ctx := context.Background()
	for b.Loop() {
		_, _ = srv.completion(ctx, params)
	}
}

func BenchmarkCompletion_Payee(b *testing.B) {
	srv := NewServer()
	docURI := uri.URI("file:///bench.journal")
	srv.documents.Store(docURI, largeContent)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 0, Character: 11},
		},
	}

	ctx := context.Background()
	for b.Loop() {
		_, _ = srv.completion(ctx, params)
	}
}

// Measure the first ghost-text request after each edit, rather than repeated
// requests for the same cached document version.
func BenchmarkInlineCompletion_AfterEdit_Large(b *testing.B) {
	base := largeContent + "2025-01-01 * Payee 999\n"
	srv, docURI := setupBenchServer(b, base, false)
	path := uriToPath(docURI)
	line := uint32(strings.Count(base, "\n"))
	params := inlineCompletionParams(docURI, line, 0)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		content := base + strings.Repeat(" ", i%2)
		srv.StoreDocument(docURI, content)
		srv.workspace.MarkFileDirty(path, workspace.StaticContent(content))
		result, err := srv.InlineCompletion(ctx, params)
		list, ok := result.(*protocol.InlineCompletionList)
		if err != nil || !ok || len(list.Items) != 1 {
			b.Fatalf("inline completion: %v", err)
		}
	}
}

func BenchmarkCompletion_Commodity(b *testing.B) {
	srv := NewServer()
	docURI := uri.URI("file:///bench.journal")
	srv.documents.Store(docURI, largeContent)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 1, Character: 35},
		},
		Context: protocol.CompletionContext{
			TriggerKind:      protocol.CompletionTriggerKindTriggerCharacter,
			TriggerCharacter: stringPtr("@"),
		},
	}

	ctx := context.Background()
	for b.Loop() {
		_, _ = srv.completion(ctx, params)
	}
}

func BenchmarkDetermineContext_Posting(b *testing.B) {
	for b.Loop() {
		determineCompletionContext(largeContent, protocol.Position{Line: 1, Character: 4})
	}
}

func BenchmarkDetermineContext_Transaction(b *testing.B) {
	for b.Loop() {
		determineCompletionContext(largeContent, protocol.Position{Line: 0, Character: 11})
	}
}

func BenchmarkExtractAccountPrefix(b *testing.B) {
	for b.Loop() {
		extractAccountPrefix(largeContent, protocol.Position{Line: 1, Character: 20})
	}
}

func BenchmarkApplyChange_Small(b *testing.B) {
	r := protocol.Range{
		Start: protocol.Position{Line: 1, Character: 4},
		End:   protocol.Position{Line: 1, Character: 10},
	}

	for b.Loop() {
		applyChange(smallContent, r, "assets:")
	}
}

func BenchmarkApplyChange_Large(b *testing.B) {
	r := protocol.Range{
		Start: protocol.Position{Line: 100, Character: 4},
		End:   protocol.Position{Line: 100, Character: 10},
	}

	for b.Loop() {
		applyChange(largeContent, r, "assets:")
	}
}

type noopClient struct{ protocol.UnimplementedClient }

func setupBenchServer(b *testing.B, content string, withClient bool) (*Server, uri.URI) {
	b.Helper()
	tmpDir := b.TempDir()
	mainPath := filepath.Join(tmpDir, "bench.journal")
	if err := os.WriteFile(mainPath, []byte(content), 0644); err != nil {
		b.Fatal(err)
	}

	srv := NewServer()
	if withClient {
		srv.SetClient(noopClient{})
	}
	srv.loader = include.NewLoader()
	srv.workspace = workspace.NewWorkspace(tmpDir, srv.loader)
	if err := srv.workspace.Initialize(); err != nil {
		b.Fatal(err)
	}

	docURI := uri.File(mainPath)
	srv.documents.Store(docURI, content)
	// Register the buffer with the workspace the way DidOpen does, so the
	// deferred recompute runs exactly once and the benchmarks below measure
	// steady state rather than a permanently dirty workspace.
	srv.workspace.MarkFileDirty(mainPath, workspace.StaticContent(content))

	return srv, docURI
}

func BenchmarkDidChange_Incremental_Small(b *testing.B) {
	srv, docURI := setupBenchServer(b, smallContent, false)
	ctx := context.Background()

	change := &protocol.TextDocumentContentChangePartial{
		Range: protocol.Range{
			Start: protocol.Position{Line: 1, Character: 4},
			End:   protocol.Position{Line: 1, Character: 10},
		},
		Text: "assets:new",
	}

	params := &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: docURI},
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{change},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_ = srv.DidChange(ctx, params)
	}
}

func BenchmarkDidChange_Incremental_Medium(b *testing.B) {
	srv, docURI := setupBenchServer(b, mediumContent, false)
	ctx := context.Background()

	change := &protocol.TextDocumentContentChangePartial{
		Range: protocol.Range{
			Start: protocol.Position{Line: 50, Character: 4},
			End:   protocol.Position{Line: 50, Character: 10},
		},
		Text: "assets:new",
	}

	params := &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: docURI},
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{change},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_ = srv.DidChange(ctx, params)
	}
}

func BenchmarkDidChange_Incremental_Large(b *testing.B) {
	srv, docURI := setupBenchServer(b, largeContent, false)
	ctx := context.Background()

	change := &protocol.TextDocumentContentChangePartial{
		Range: protocol.Range{
			Start: protocol.Position{Line: 500, Character: 4},
			End:   protocol.Position{Line: 500, Character: 10},
		},
		Text: "assets:new",
	}

	params := &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: docURI},
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{change},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_ = srv.DidChange(ctx, params)
	}
}

// benchmarkPublishDiagnostics measures the publish that follows an edit, which
// is the only kind there is: a publish is scheduled by a document change, so it
// always sees a version nothing has analysed yet.
//
// It edits between rounds for that reason. Publishing the same version in a
// loop would measure the caches — the journal, the workspace tree and the
// analysis are all kept per version — and report a fraction of the real cost.
func benchmarkPublishDiagnostics(b *testing.B, content string) {
	b.Helper()
	srv, docURI := setupBenchServer(b, content, true)
	ctx := context.Background()
	path := uriToPath(docURI)

	text, ok := srv.documents.rope(docURI)
	if !ok {
		b.Fatal("no rope for the benchmarked document")
	}
	rng := protocol.Range{
		Start: protocol.Position{Line: 1, Character: 0},
		End:   protocol.Position{Line: 1, Character: 0},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		text.ApplyChange(rng, fmt.Sprintf("; %d\n", i))
		revision := srv.workspace.MarkFileDirty(path, text)
		srv.publishDiagnostics(ctx, docURI, text, 0, revision)
	}
}

func BenchmarkPublishDiagnostics_Small(b *testing.B) {
	benchmarkPublishDiagnostics(b, smallContent)
}

func BenchmarkPublishDiagnostics_Medium(b *testing.B) {
	benchmarkPublishDiagnostics(b, mediumContent)
}

func BenchmarkPublishDiagnostics_Large(b *testing.B) {
	benchmarkPublishDiagnostics(b, largeContent)
}
