package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/uri"
)

func TestPublishDiagnostics_CommodityFormatErrors(t *testing.T) {
	for _, tc := range []struct{ name, newline string }{
		{"LF", "\n"},
		{"CRLF", "\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := newTestServer()

			mismatch := strings.ReplaceAll("commodity 元\n    format 1000.00 圆\n", "\n", tc.newline)
			diagnostics, err := ts.openAndWait(uri.URI("file:///commodity-mismatch.journal"), mismatch)
			require.NoError(t, err)
			failure := diagnosticWithCode(diagnostics, "PARSE_ERROR")
			require.NotNil(t, failure, "got %v", codesIn(diagnostics))
			assert.Equal(t, `commodity directive symbol "元" and format directive symbol "圆" should be the same`, tooltipString(failure.Message))
			assert.Equal(t, uint32(1), failure.Range.Start.Line)
			assert.Equal(t, uint32(11), failure.Range.Start.Character)
			assert.Equal(t, uint32(20), failure.Range.End.Character)

			inline := strings.ReplaceAll("commodity 1000.00 元\n    note 备注\n", "\n", tc.newline)
			diagnostics, err = ts.openAndWait(uri.URI("file:///commodity-inline.journal"), inline)
			require.NoError(t, err)
			failure = diagnosticWithCode(diagnostics, "PARSE_ERROR")
			require.NotNil(t, failure, "got %v", codesIn(diagnostics))
			assert.Contains(t, failure.Message, `subdirective "note" is not allowed`)
			assert.Equal(t, uint32(4), failure.Range.Start.Character)
			assert.Equal(t, uint32(8), failure.Range.End.Character)
		})
	}
}
