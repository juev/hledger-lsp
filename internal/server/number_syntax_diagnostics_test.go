package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/uri"
)

func TestPublishDiagnostics_NumberSyntaxMatchesHledger(t *testing.T) {
	accepted := "2024-01-01 x\n    a  -EUR 1,500\n    b  .5 EUR\n    c  -руб 5\n    d  .5 元\n    e\n"
	rejected := "decimal-mark ,\n2024-01-01 x\n    a  1.5E3 X\n    b\n"

	for _, tc := range []struct{ name, newline string }{
		{"LF", "\n"},
		{"CRLF", "\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := newTestServer()

			okDiagnostics, err := ts.openAndWait(uri.URI("file:///number-syntax-ok.journal"), strings.ReplaceAll(accepted, "\n", tc.newline))
			require.NoError(t, err)
			assert.Nil(t, diagnosticWithCode(okDiagnostics, "PARSE_ERROR"), "got %v", codesIn(okDiagnostics))

			badDiagnostics, err := ts.openAndWait(uri.URI("file:///number-syntax-bad.journal"), strings.ReplaceAll(rejected, "\n", tc.newline))
			require.NoError(t, err)
			failure := diagnosticWithCode(badDiagnostics, "PARSE_ERROR")
			require.NotNil(t, failure, "got %v", codesIn(badDiagnostics))
			assert.Contains(t, failure.Message, "digit separators and exponents may not be used together")
			assert.Equal(t, uint32(2), failure.Range.Start.Line)
			assert.Equal(t, uint32(7), failure.Range.Start.Character)
		})
	}
}
