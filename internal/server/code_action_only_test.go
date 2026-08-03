package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// onlyKinds asks for code actions on the amountless posting of a transaction
// that also has an unbalanced sibling, so every action source can contribute.
func onlyKinds(t *testing.T, only []protocol.CodeActionKind) []protocol.CodeActionKind {
	t.Helper()

	ts := newTestServer()
	ts.clientCapabilities.supportsCodeActionLiterals = true
	// source.hledger actions are gated on the CLI being present; a fake keeps
	// the filtering under test independent of what is installed.
	ts.cliClient = &fakeCLIClient{available: true}
	docURI := uri.URI("file:///test.journal")
	content := "2026-08-03 Good\n    Expenses:Test  -20.50 USD\n    Expenses:Food\n"
	require.NoError(t, ts.openDocument(docURI, content))

	entries, err := ts.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
		Range:        protocol.Range{Start: protocol.Position{Line: 2}, End: protocol.Position{Line: 2}},
		Context:      protocol.CodeActionContext{Only: only},
	})
	require.NoError(t, err)

	kinds := make([]protocol.CodeActionKind, 0)
	for _, action := range codeActionEntries(entries) {
		require.NotNil(t, action.Kind)
		kinds = append(kinds, *action.Kind)
	}
	return kinds
}

func TestCodeAction_Only_ExactKind(t *testing.T) {
	kinds := onlyKinds(t, []protocol.CodeActionKind{insertInferredAmountKind})

	assert.Equal(t, []protocol.CodeActionKind{insertInferredAmountKind}, kinds)
}

func TestCodeAction_Only_ParentKindMatchesChildren(t *testing.T) {
	kinds := onlyKinds(t, []protocol.CodeActionKind{protocol.CodeActionKindQuickFix})

	assert.Contains(t, kinds, insertInferredAmountKind, "a dotted child of quickfix passes the quickfix filter")
	assert.NotContains(t, kinds, protocol.CodeActionKind("source.hledger"))
}

func TestCodeAction_Only_SourceKind(t *testing.T) {
	kinds := onlyKinds(t, []protocol.CodeActionKind{"source"})

	require.NotEmpty(t, kinds)
	for _, kind := range kinds {
		assert.Equal(t, protocol.CodeActionKind("source.hledger"), kind)
	}
}

func TestCodeAction_Only_UnrelatedKindReturnsNothing(t *testing.T) {
	assert.Empty(t, onlyKinds(t, []protocol.CodeActionKind{"refactor.extract"}))
}

func TestCodeAction_Only_EmptyKeepsEveryKind(t *testing.T) {
	kinds := onlyKinds(t, nil)

	assert.Contains(t, kinds, insertInferredAmountKind)
	assert.Contains(t, kinds, protocol.CodeActionKind("source.hledger"))
}

func TestCodeActionKindAllowed(t *testing.T) {
	quickFix := protocol.CodeActionKindQuickFix

	assert.True(t, codeActionKindAllowed(&quickFix, nil), "no filter accepts everything")
	assert.True(t, codeActionKindAllowed(&quickFix, []protocol.CodeActionKind{"quickfix"}))
	assert.True(t, codeActionKindAllowed(&insertInferredAmountKindValue, []protocol.CodeActionKind{"quickfix"}))
	assert.False(t, codeActionKindAllowed(&quickFix, []protocol.CodeActionKind{"quickfix.hledger"}), "a child filter does not accept the parent")
	assert.False(t, codeActionKindAllowed(&quickFix, []protocol.CodeActionKind{"quickfixes"}), "matching is on dotted segments, not raw prefixes")
	assert.False(t, codeActionKindAllowed(nil, []protocol.CodeActionKind{"quickfix"}), "an action without a kind matches no filter")
}

var insertInferredAmountKindValue = insertInferredAmountKind
