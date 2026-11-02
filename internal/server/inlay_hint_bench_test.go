package server

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// BenchmarkInlayHint_RunningBalancesRepeatedRequest measures the request an
// editor repeats on every scroll: the document and its include tree are
// unchanged, so after the first request only the hints themselves are rebuilt.
func BenchmarkInlayHint_RunningBalancesRepeatedRequest(b *testing.B) {
	const accounts, transactions = 400, 20000

	var big strings.Builder
	for i := range transactions {
		fmt.Fprintf(&big, "2024-01-%02d tx\n    expenses:a%d  1 USD\n    assets:b%d\n\n", i%28+1, i%accounts, i%accounts)
	}
	open := "2024-02-01 open\n    expenses:food  1 USD\n    assets:cash\n"
	srv, dir := newInlayWorkspace(b, map[string]string{
		"all.journal":  "include big.journal\ninclude open.journal\n",
		"big.journal":  big.String(),
		"open.journal": open,
	})
	enableOnlyRunningBalances(srv)
	docURI := uri.File(filepath.Join(dir, "open.journal"))
	srv.StoreDocument(docURI, open)
	params := &protocol.InlayHintParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
		Range:        protocol.Range{End: protocol.Position{Line: 100}},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := srv.InlayHint(context.Background(), params); err != nil {
			b.Fatal(err)
		}
	}
}
