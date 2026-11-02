//go:build !race

package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/workspace"
)

// setupOccurrenceBench opens shared.journal in a workspace where the journal
// includes it once per entry of marks, each under that decimal mark. An empty
// mark includes it with no directive in front.
func setupOccurrenceBench(b *testing.B, content string, marks []string) (*Server, uri.URI) {
	b.Helper()
	dir := b.TempDir()
	var main strings.Builder
	for index, mark := range marks {
		name := "include" + string(rune('a'+index)) + ".journal"
		main.WriteString("include " + name + "\n")
		body := "include shared.journal\n"
		if mark != "" {
			body = "decimal-mark " + mark + "\n" + body
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	for name, text := range map[string]string{"main.journal": main.String(), "shared.journal": content} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			b.Fatal(err)
		}
	}

	srv := NewServer()
	srv.loader = include.NewLoader()
	srv.workspace = workspace.NewWorkspace(dir, srv.loader)
	if err := srv.workspace.Initialize(); err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(dir, "shared.journal")
	docURI := uri.File(path)
	srv.documents.Store(docURI, content)
	srv.workspace.MarkFileDirty(path, workspace.StaticContent(content))
	return srv, docURI
}

// benchmarkFormatAfterEdit measures the first Format request after each edit,
// not repeated requests for a cached document version.
func benchmarkFormatAfterEdit(b *testing.B, marks []string) {
	srv, docURI := setupOccurrenceBench(b, largeContent, marks)
	path := uriToPath(docURI)
	params := &protocol.DocumentFormattingParams{TextDocument: protocol.TextDocumentIdentifier{URI: docURI}}
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		content := largeContent + strings.Repeat(" ", i%2)
		srv.StoreDocument(docURI, content)
		srv.workspace.MarkFileDirty(path, workspace.StaticContent(content))
		if _, err := srv.Format(ctx, params); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFormat_Large_IncludedOnce(b *testing.B) {
	benchmarkFormatAfterEdit(b, []string{""})
}

func BenchmarkFormat_Large_IncludedTwiceSameContext(b *testing.B) {
	benchmarkFormatAfterEdit(b, []string{"", ""})
}

func BenchmarkFormat_Large_IncludedTwiceConflictingMarks(b *testing.B) {
	benchmarkFormatAfterEdit(b, []string{",", "."})
}

// benchmarkAnalyzeAfterEdit measures the per-buffer diagnostics of the first
// request after each edit.
func benchmarkAnalyzeAfterEdit(b *testing.B, marks []string) {
	srv, docURI := setupOccurrenceBench(b, largeContent, marks)
	path := uriToPath(docURI)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		content := largeContent + strings.Repeat(" ", i%2)
		srv.StoreDocument(docURI, content)
		srv.workspace.MarkFileDirty(path, workspace.StaticContent(content))
		_ = srv.analyze(docURI, path, content)
	}
}

func BenchmarkAnalyze_Large_IncludedOnce(b *testing.B) {
	benchmarkAnalyzeAfterEdit(b, []string{""})
}

func BenchmarkAnalyze_Large_IncludedTwiceSameContext(b *testing.B) {
	benchmarkAnalyzeAfterEdit(b, []string{"", ""})
}

func BenchmarkAnalyze_Large_IncludedTwiceConflictingMarks(b *testing.B) {
	benchmarkAnalyzeAfterEdit(b, []string{",", "."})
}

// benchmarkInlayHintAfterEdit measures the first inlay hint request after each
// edit, with every kind of hint on.
func benchmarkInlayHintAfterEdit(b *testing.B, marks []string) {
	srv, docURI := setupOccurrenceBench(b, largeContent, marks)
	settings := srv.getSettings()
	settings.InlayHints.InferredAmounts = true
	settings.InlayHints.CostExpansion = true
	settings.InlayHints.RunningBalances = true
	srv.setSettings(settings)
	path := uriToPath(docURI)
	params := &protocol.InlayHintParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
		Range:        protocol.Range{End: protocol.Position{Line: 100}},
	}
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		content := largeContent + strings.Repeat(" ", i%2)
		srv.StoreDocument(docURI, content)
		srv.workspace.MarkFileDirty(path, workspace.StaticContent(content))
		if _, err := srv.InlayHint(ctx, params); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInlayHint_Large_IncludedOnce(b *testing.B) {
	benchmarkInlayHintAfterEdit(b, []string{""})
}

func BenchmarkInlayHint_Large_IncludedTwiceSameContext(b *testing.B) {
	benchmarkInlayHintAfterEdit(b, []string{"", ""})
}

func BenchmarkInlayHint_Large_IncludedTwiceConflictingMarks(b *testing.B) {
	benchmarkInlayHintAfterEdit(b, []string{",", "."})
}
