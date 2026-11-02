package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// hledger ends an account name at two whitespace characters in a row: a lone
// tab keeps the cursor inside the account, a tab pair moves it to the amount.
func TestPostingContext_TabSeparator(t *testing.T) {
	cases := []struct {
		name string
		line string
		want CompletionContextType
	}{
		{"lone tab, cursor after it", "    expenses:food\t", ContextAccount},
		{"lone tab, cursor in the text after it", "    expenses:food\t50", ContextAccount},
		{"two tabs", "    expenses:food\t\t", ContextCommodity},
		{"tab and space", "    expenses:food\t ", ContextCommodity},
		{"space and tab", "    expenses:food \t", ContextCommodity},
		{"Cyrillic two tabs", "    расходы:еда\t\t", ContextCommodity},
		{"Cyrillic lone tab", "    расходы:еда\t", ContextAccount},
		{"CJK two tabs", "    支出:食品\t\t", ContextCommodity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			character := uint32(len([]rune(tc.line)))
			got := determinePostingContext(tc.line, protocol.Position{Line: 1, Character: character})
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDiagnostics_LoneTabBetweenAccountAndAmountIsReported(t *testing.T) {
	for name, input := range map[string]string{
		"LF":       "2024-01-01 a\n    e:x\t5 USD\n    a:y\n",
		"CRLF":     "2024-01-01 a\r\n    e:x\t5 USD\r\n    a:y\r\n",
		"Cyrillic": "2024-01-01 кафе\n    расходы:еда\t5 ₽\n    активы:наличные\n",
	} {
		t.Run(name, func(t *testing.T) {
			ts := newTestServer()
			diags, err := ts.openAndWait(uri.URI("file:///tab.journal"), input)
			require.NoError(t, err)
			assert.NotNil(t, diagnosticWithCode(diags, "MULTIPLE_INFERRED"), "got %v", codesIn(diags))
		})
	}

	ts := newTestServer()
	diags, err := ts.openAndWait(uri.URI("file:///tabs.journal"), "2024-01-01 a\n    e:x\t\t5 USD\n    a:y\n")
	require.NoError(t, err)
	assert.Empty(t, codesIn(diags), "two tabs separate the amount")
}
