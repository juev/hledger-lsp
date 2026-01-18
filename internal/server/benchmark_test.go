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
	docURI := protocol.DocumentURI("file:///bench.journal")
	srv.documents.Store(docURI, smallContent)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 1, Character: 4},
		},
	}

	ctx := context.Background()
	for b.Loop() {
		_, _ = srv.Completion(ctx, params)
	}
}

func BenchmarkCompletion_Account_Medium(b *testing.B) {
	srv := NewServer()
	docURI := protocol.DocumentURI("file:///bench.journal")
	srv.documents.Store(docURI, mediumContent)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 1, Character: 4},
		},
	}

	ctx := context.Background()
	for b.Loop() {
		_, _ = srv.Completion(ctx, params)
	}
}

func BenchmarkCompletion_Account_Large(b *testing.B) {
	srv := NewServer()
	docURI := protocol.DocumentURI("file:///bench.journal")
	srv.documents.Store(docURI, largeContent)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 1, Character: 4},
		},
	}

	ctx := context.Background()
	for b.Loop() {
		_, _ = srv.Completion(ctx, params)
	}
}

func BenchmarkCompletion_Payee(b *testing.B) {
	srv := NewServer()
	docURI := protocol.DocumentURI("file:///bench.journal")
	srv.documents.Store(docURI, largeContent)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 0, Character: 11},
		},
	}

	ctx := context.Background()
	for b.Loop() {
		_, _ = srv.Completion(ctx, params)
	}
}

func BenchmarkCompletion_Commodity(b *testing.B) {
	srv := NewServer()
	docURI := protocol.DocumentURI("file:///bench.journal")
	srv.documents.Store(docURI, largeContent)

	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 1, Character: 35},
		},
		Context: &protocol.CompletionContext{
			TriggerKind:      protocol.CompletionTriggerKindTriggerCharacter,
			TriggerCharacter: "@",
		},
	}

	ctx := context.Background()
	for b.Loop() {
		_, _ = srv.Completion(ctx, params)
	}
}

func BenchmarkDetermineContext_Posting(b *testing.B) {
	for b.Loop() {
		determineCompletionContext(largeContent, protocol.Position{Line: 1, Character: 4}, nil)
	}
}

func BenchmarkDetermineContext_Transaction(b *testing.B) {
	for b.Loop() {
		determineCompletionContext(largeContent, protocol.Position{Line: 0, Character: 11}, nil)
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

type noopClient struct{}

func (noopClient) Progress(context.Context, *protocol.ProgressParams) error { return nil }
func (noopClient) WorkDoneProgressCreate(context.Context, *protocol.WorkDoneProgressCreateParams) error {
	return nil
}
func (noopClient) LogMessage(context.Context, *protocol.LogMessageParams) error { return nil }
func (noopClient) PublishDiagnostics(context.Context, *protocol.PublishDiagnosticsParams) error {
	return nil
}
func (noopClient) ShowMessage(context.Context, *protocol.ShowMessageParams) error { return nil }
func (noopClient) ShowMessageRequest(context.Context, *protocol.ShowMessageRequestParams) (*protocol.MessageActionItem, error) {
	return nil, nil
}
func (noopClient) Telemetry(context.Context, interface{}) error                           { return nil }
func (noopClient) RegisterCapability(context.Context, *protocol.RegistrationParams) error { return nil }
func (noopClient) UnregisterCapability(context.Context, *protocol.UnregistrationParams) error {
	return nil
}
func (noopClient) ApplyEdit(context.Context, *protocol.ApplyWorkspaceEditParams) (bool, error) {
	return false, nil
}
func (noopClient) Configuration(context.Context, *protocol.ConfigurationParams) ([]interface{}, error) {
	return nil, nil
}
func (noopClient) WorkspaceFolders(context.Context) ([]protocol.WorkspaceFolder, error) {
	return nil, nil
}

func setupBenchServer(b *testing.B, content string, withClient bool) (*Server, protocol.DocumentURI) {
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

	docURI := protocol.DocumentURI("file://" + mainPath)
	srv.documents.Store(docURI, content)

	return srv, docURI
}

func BenchmarkDidChange_Incremental_Small(b *testing.B) {
	srv, docURI := setupBenchServer(b, smallContent, false)
	ctx := context.Background()

	change := protocol.TextDocumentContentChangeEvent{
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

	change := protocol.TextDocumentContentChangeEvent{
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

	change := protocol.TextDocumentContentChangeEvent{
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

func BenchmarkPublishDiagnostics_Small(b *testing.B) {
	srv, docURI := setupBenchServer(b, smallContent, true)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		srv.publishDiagnostics(ctx, docURI, smallContent)
	}
}

func BenchmarkPublishDiagnostics_Medium(b *testing.B) {
	srv, docURI := setupBenchServer(b, mediumContent, true)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		srv.publishDiagnostics(ctx, docURI, mediumContent)
	}
}

func BenchmarkPublishDiagnostics_Large(b *testing.B) {
	srv, docURI := setupBenchServer(b, largeContent, true)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		srv.publishDiagnostics(ctx, docURI, largeContent)
	}
}
