package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestCompletion_NonzeroAccountBalances(t *testing.T) {
	const history = `account assets:unused

2024-01-01 Opening
    assets:closed  10 USD
    assets:positive  20 USD
    assets:negative  -5 USD
    assets:tiny  0.000000001 USD
    assets:zero  0 USD
    assets:mixed  7 USD
    assets:mixed  -7 EUR
    assets:partly-zero  3 USD
    assets:partly-zero  4 EUR
    equity:opening

2024-01-02 Closing
    assets:closed  -10 USD
    assets:partly-zero  -3 USD
    equity:opening

2024-01-03 Editing
    assets:positive  -20 USD
    assets:closed  10 USD
    `
	for _, query := range []string{"", "assets:"} {
		t.Run(query, func(t *testing.T) {
			srv := NewServer()
			docURI := uri.URI("file:///test.journal")
			content := history + query
			srv.StoreDocument(docURI, content)
			result, err := srv.completion(context.Background(), &protocol.CompletionParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
					Position: protocol.Position{
						Line:      uint32(strings.Count(content, "\n")),
						Character: uint32(4 + len(query)),
					},
				},
			})
			require.NoError(t, err)
			labels := extractLabels(result.Items)
			for _, account := range []string{"assets:positive", "assets:negative", "assets:tiny", "assets:mixed", "assets:partly-zero"} {
				assert.Contains(t, labels, account)
			}
			for _, account := range []string{"assets:closed", "assets:zero", "assets:unused"} {
				assert.NotContains(t, labels, account)
			}
			if query == "" {
				assert.Contains(t, labels, "equity:opening", "infer the omitted posting amount")
			}
		})
	}
}

func TestCompletion_NonzeroAccountsBeforeMaxResults(t *testing.T) {
	srv := NewServer()
	srv.settings.Completion.MaxResults = 1
	content := `2024-01-01 Shop
    assets:closed  10 USD
    equity:opening

2024-01-02 Shop
    assets:closed  -10 USD
    equity:opening

2024-01-03 Other
    assets:active  5 USD
    equity:opening

2024-01-04 Shop
    assets:`
	docURI := uri.URI("file:///test.journal")
	srv.StoreDocument(docURI, content)
	result, err := srv.completion(context.Background(), &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 13, Character: 11},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"assets:active"}, extractLabels(result.Items))
}

func TestCompletion_NonzeroAccountBalancesAcrossIncludes(t *testing.T) {
	for _, lineEnding := range []string{"\n", "\r\n"} {
		t.Run(map[string]string{"\n": "LF", "\r\n": "CRLF"}[lineEnding], func(t *testing.T) {
			dir := t.TempDir()
			childContent := `2024-01-01 Opening
    активы:закрыт  10 USD
    資産:現金  5 USD
    equity:opening
`
			mainContent := `include child.journal
include child.journal

2024-01-02 Closing
    активы:закрыт  -20 USD
    equity:opening

2024-01-03 Editing
    資産:現金  -10 USD
    `
			mainContent = strings.ReplaceAll(mainContent, "\n", lineEnding)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "child.journal"), []byte(strings.ReplaceAll(childContent, "\n", lineEnding)), 0o644))
			mainPath := filepath.Join(dir, "main.journal")
			require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o644))
			ts := newTestServer()
			docURI := pathToURI(mainPath)
			_, err := ts.openAndWait(docURI, mainContent)
			require.NoError(t, err)
			result, err := ts.completion(docURI, 9, 4)
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{"資産:現金", "equity:opening"}, extractLabels(result.Items))
		})
	}
}
