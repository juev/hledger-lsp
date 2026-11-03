package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// hledger reads a date: tag in the transaction header and on the comment lines
// before the first posting as prose (issue #172); only the comment of a posting
// and the ; lines below it hold a posting date.
func TestPublishDiagnostics_HeaderDateTagsAreProse(t *testing.T) {
	for _, le := range lineEndings {
		t.Run(le.name, func(t *testing.T) {
			lines := []string{
				"2023-01-01 касса  ; 日本 date:13-05",
				"    ; заметка date:2024-13-05 date2:",
				"    現金:口座  5 USD",
				"    ; оплата 🍕 date:2024-13-05",
				"    капитал",
			}
			ts := newTestServer()
			diagnostics, err := ts.openAndWait(uri.URI("file:///header-date.journal"), strings.Join(lines, le.newline))
			require.NoError(t, err)

			type position struct {
				code string
				at   protocol.Position
			}
			var got []position
			for _, d := range diagnostics {
				if code := diagnosticCodeString(d.Code); code == "INVALID_DATE_TAG" || code == "EMPTY_DATE_TAG" {
					got = append(got, position{code, d.Range.Start})
				}
			}
			assert.Equal(t, []position{
				{"INVALID_DATE_TAG", protocol.Position{Line: 3, Character: utf16Col(t, lines[3], "date:")}},
			}, got)
		})
	}
}
