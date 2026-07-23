package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/include"
)

func TestPublishDiagnostics_FileTooLargeSkipsParse(t *testing.T) {
	ts := newTestServer()
	ts.loader.SetLimits(include.Limits{
		MaxFileSizeBytes: 50,
		MaxIncludeDepth:  50,
	})

	// Over the 50-byte limit AND contains an unterminated comment block, which
	// would yield a parse-error diagnostic if the content were parsed.
	content := "comment\nThis comment block is never terminated and exceeds the limit.\n"
	uri := uri.URI("file:///too-large.journal")

	diags, err := ts.openAndWait(uri, content)
	require.NoError(t, err)

	require.Len(t, diags, 1, "expected only the file-too-large diagnostic, got: %+v", diags)
	d := diags[0]
	assert.Contains(t, tooltipString(d.Message), "file too large")
	assert.NotContains(t, tooltipString(d.Message), "unterminated")
	assert.Equal(t, protocol.DiagnosticSeverityError, d.Severity)
	assert.Equal(t, "hledger-lsp", optionalString(d.Source))
}
