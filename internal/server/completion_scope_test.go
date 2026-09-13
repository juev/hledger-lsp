package server

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/lsputil"
)

func requestScopedCompletion(t *testing.T, srv *Server, params *protocol.CompletionParams, scope string) *ScopedCompletionResult {
	t.Helper()
	raw, err := protocol.Marshal(ScopedCompletionParams{
		TextDocument: params.TextDocument, Position: params.Position, Context: params.Context, AccountScope: scope,
	})
	require.NoError(t, err)
	result, err := srv.Request(context.Background(), "hledger/completion", protocol.LSPAny(raw))
	require.NoError(t, err)
	response, ok := result.(*ScopedCompletionResult)
	require.True(t, ok)
	return response
}

func TestScopedCompletion_ExpandsAccountsWithoutChangingStandardRequests(t *testing.T) {
	const content = `account assets:unused

2024-01-01 Opening
    assets:closed  10 USD
    assets:positive  20 USD
    assets:negative  -5 USD
    assets:tiny  0.000000001 USD
    assets:mixed  7 USD
    assets:mixed  -7 EUR
    equity:opening

2024-01-02 Closing
    assets:closed  -10 USD
    equity:opening

2024-01-03 Editing
    assets:positive  -20 USD
    assets:closed  10 USD
    assets:`
	srv := NewServer()
	srv.settings.Completion.MaxResults = 1
	docURI := uri.URI("file:///scope.journal")
	srv.StoreDocument(docURI, content)
	params := &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
		Position:     protocol.Position{Line: uint32(strings.Count(content, "\n")), Character: 11},
	}}
	before, err := srv.completion(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, before.Items, 1)
	ordinary := requestScopedCompletion(t, srv, params, "nonzero")
	assert.Equal(t, before, ordinary.CompletionList)
	require.NotNil(t, ordinary.AccountRange)

	full := requestScopedCompletion(t, srv, params, "all")
	labels := extractLabels(full.CompletionList.Items)
	assert.ElementsMatch(t, []string{"assets:positive", "assets:negative", "assets:tiny", "assets:mixed", "assets:closed", "assets:unused"}, labels)
	require.Len(t, labels, 6)
	assert.ElementsMatch(t, []string{"assets:positive", "assets:negative", "assets:tiny", "assets:mixed"}, labels[:4])
	assert.Equal(t, before.Items[0].Label, labels[0], "expansion keeps the first ordinary suggestion")
	assert.Equal(t, ordinary.AccountRange, full.AccountRange)
	assert.True(t, full.CompletionList.IsIncomplete)
	for _, item := range full.CompletionList.Items {
		assert.Equal(t, &protocol.TextEdit{Range: *full.AccountRange, NewText: item.Label}, item.TextEdit)
		var data completionResolveData
		require.NoError(t, protocol.Unmarshal(item.Data, &data))
		assert.Equal(t, "account", data.Kind)
		assert.Equal(t, docURI, data.DocURI)
		resolved, resolveErr := srv.CompletionResolve(context.Background(), &item)
		require.NoError(t, resolveErr)
		assert.NotNil(t, resolved.Documentation)
	}
	after, err := srv.completion(context.Background(), params)
	require.NoError(t, err)
	assert.Equal(t, before, after, "a full request does not change subsequent standard requests")
}

func TestScopedCompletion_ReportsEmptyAccountContextAndUnicodeRanges(t *testing.T) {
	for _, lineEnding := range []string{"\n", "\r\n"} {
		t.Run(map[string]string{"\n": "LF", "\r\n": "CRLF"}[lineEnding], func(t *testing.T) {
			content := strings.ReplaceAll("account 資産:現金\naccount 資産:預金\n\n2026-09-13 Example\n    資産:現", "\n", lineEnding)
			srv := NewServer()
			docURI := uri.URI("file:///unicode-scope.journal")
			srv.StoreDocument(docURI, content)
			params := &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
				Position:     protocol.Position{Line: 4, Character: uint32(lsputil.UTF16Len("    資産:現"))},
			}}
			ordinary := requestScopedCompletion(t, srv, params, "nonzero")
			assert.Empty(t, ordinary.CompletionList.Items)
			require.NotNil(t, ordinary.AccountRange)
			assert.Equal(t, uint32(4), ordinary.AccountRange.Start.Character)
			assert.Equal(t, params.Position, ordinary.AccountRange.End)
			full := requestScopedCompletion(t, srv, params, "all")
			assert.Equal(t, []string{"資産:現金"}, extractLabels(full.CompletionList.Items))
		})
	}
}

func TestScopedCompletion_OtherContextsKeepTheirLimit(t *testing.T) {
	srv := NewServer()
	srv.settings.Completion.MaxResults = 1
	docURI := uri.URI("file:///payees.journal")
	content := "2024-01-01 One\n    expenses:food  1 USD\n    assets:cash\n\n2024-01-02 Two\n    expenses:food  1 USD\n    assets:cash\n\n2024-01-03 "
	srv.StoreDocument(docURI, content)
	params := &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
		Position:     protocol.Position{Line: 8, Character: 11},
	}}
	ordinary, err := srv.completion(context.Background(), params)
	require.NoError(t, err)
	full := requestScopedCompletion(t, srv, params, "all")
	assert.Nil(t, full.AccountRange)
	assert.Equal(t, ordinary, full.CompletionList)
	assert.Len(t, full.CompletionList.Items, 1)
}

func TestScopedCompletion_InvalidParams(t *testing.T) {
	for _, raw := range []any{
		"not protocol JSON", protocol.LSPAny(`null`), protocol.LSPAny(`{invalid`),
		protocol.LSPAny(`{"accountScope":"all"}`),
		protocol.LSPAny(`{"textDocument":{"uri":"file:///test.journal"},"accountScope":"invalid"}`),
	} {
		srv := NewServer()
		result, err := srv.Request(context.Background(), "hledger/completion", raw)
		require.Nil(t, result)
		var rpcErr *jsonrpc2.Error
		require.ErrorAs(t, err, &rpcErr)
		assert.Equal(t, jsonrpc2.InvalidParams, rpcErr.Code)
	}
}

func TestScopedCompletion_LargeDeclaredAccountList(t *testing.T) {
	srv := NewServer()
	const count = 5000
	var content strings.Builder
	for i := range count {
		fmt.Fprintf(&content, "account assets:account%04d\n", i)
	}
	content.WriteString("\n2026-09-13 Example\n    assets:")
	docURI := uri.URI("file:///large.journal")
	srv.StoreDocument(docURI, content.String())
	params := &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
		Position:     protocol.Position{Line: count + 2, Character: 11},
	}}
	assert.Empty(t, requestScopedCompletion(t, srv, params, "nonzero").CompletionList.Items)
	assert.Len(t, requestScopedCompletion(t, srv, params, "all").CompletionList.Items, count)
}

func TestScopedCompletion_CapabilityFollowsCompletionFeature(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		srv := NewServer()
		srv.settings.Features.Completion = enabled
		result, err := srv.Initialize(context.Background(), &protocol.InitializeParams{})
		require.NoError(t, err)
		if enabled {
			var experimental map[string]any
			require.NoError(t, protocol.Unmarshal(result.Capabilities.Experimental, &experimental))
			assert.Equal(t, map[string]any{"accountScope": true}, experimental["hledgerCompletion"])
		} else {
			assert.Empty(t, result.Capabilities.Experimental)
		}
	}
}
