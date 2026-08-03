package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// A journal whose account name carries an emoji: "Расходы:🍜" is 9 runes but
// 10 UTF-16 code units, so a range reported in lexer columns ends one unit
// short of where the editor puts the account.
const emojiAccountJournal = "account Расходы:🍜\n\n2024-01-01 обед\n    Расходы:🍜  $10\n    Активы:Наличные\n"

const (
	emojiAccountStartChar = 4                       // four spaces of indent
	emojiAccountEndChar   = 4 + 8 + 2               // indent + "Расходы:" + the emoji as a surrogate pair
	emojiDeclEndChar      = len("account ") + 8 + 2 //nolint:gocritic // same account on the directive line
)

func TestReferences_ReportsUTF16RangesForEmojiAccount(t *testing.T) {
	srv := NewServer()
	docURI := uri.URI("file:///emoji.journal")
	srv.StoreDocument(docURI, emojiAccountJournal)

	result, err := srv.References(context.Background(), &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 3, Character: 6},
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: true},
	})
	require.NoError(t, err)
	require.NotEmpty(t, result)

	for _, location := range result {
		switch location.Range.Start.Line {
		case 0:
			assert.Equal(t, uint32(len("account ")), location.Range.Start.Character)
			assert.Equal(t, uint32(emojiDeclEndChar), location.Range.End.Character)
		case 3:
			assert.Equal(t, uint32(emojiAccountStartChar), location.Range.Start.Character)
			assert.Equal(t, uint32(emojiAccountEndChar), location.Range.End.Character)
		default:
			t.Fatalf("unexpected reference on line %d", location.Range.Start.Line)
		}
	}
}

func TestDiagnostics_ReportUTF16RangesForEmojiAccount(t *testing.T) {
	ts := newTestServer()
	ts.cliClient = nil
	docURI := uri.URI("file:///emoji.journal")
	// The undeclared account sits behind an emoji, so its column in runes and
	// its column in UTF-16 code units differ.
	content := "account Активы:Наличные\n\n2024-01-01 обед\n    Расходы:🍜  $10\n    Активы:Наличные\n"

	diagnostics, err := ts.openAndWait(docURI, content)
	require.NoError(t, err)

	diag := requireDiagnosticByCode(t, diagnostics, "UNDECLARED_ACCOUNT")
	assert.Equal(t, uint32(3), diag.Range.Start.Line)
	assert.Equal(t, uint32(emojiAccountStartChar), diag.Range.Start.Character)
	assert.Equal(t, uint32(emojiAccountEndChar), diag.Range.End.Character)
}
