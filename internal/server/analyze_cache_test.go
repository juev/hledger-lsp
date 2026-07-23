package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func TestAnalyze_ReusesCachedJournalAndPreservesDiagnostics(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///a.journal")
	path := uriToPath(uri)

	// Content with an unterminated comment block: analyze must surface the
	// parse-error diagnostic (now sourced from the shared cache parse).
	content := "comment\nunterminated block\n"

	warm := srv.cachedParse(uri, content)
	require.NotNil(t, warm.journal)

	diags := srv.analyze(uri, path, content)

	require.NotEmpty(t, diags, "analyze must surface the parse-error diagnostic")
	found := false
	for _, d := range diags {
		if d.Source == "hledger-lsp" && strings.Contains(d.Message, "unterminated") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected an 'unterminated' parse diagnostic, got: %+v", diags)

	// The cache entry is unchanged: analyze reused it rather than parsing anew.
	after := srv.cachedParse(uri, content)
	assert.Same(t, warm.journal, after.journal, "analyze must reuse the cached journal")
}
