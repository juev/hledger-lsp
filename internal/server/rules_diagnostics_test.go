package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func diagCode(d protocol.Diagnostic) string {
	code, _ := d.Code.(protocol.String)
	return string(code)
}

// Rules parse diagnostics keep their own severity and code (issue #89).
func TestServer_RulesParseDiagnostics_SeverityAndCode(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		code     string
		severity protocol.DiagnosticSeverity
	}{
		{
			name:     "unknown directive is a warning",
			content:  "skip 1\nfields date, description, amount\nbogus-directive here\n",
			code:     "UNKNOWN_DIRECTIVE",
			severity: protocol.DiagnosticSeverityWarning,
		},
		{
			name:     "cyrillic unknown directive",
			content:  "skip 1\nfields date, description, amount\nдиректива значение\n",
			code:     "UNKNOWN_DIRECTIVE",
			severity: protocol.DiagnosticSeverityWarning,
		},
		{
			name:     "crlf",
			content:  "skip 1\r\nfields date, description, amount\r\nbogus-directive here\r\n",
			code:     "UNKNOWN_DIRECTIVE",
			severity: protocol.DiagnosticSeverityWarning,
		},
		{
			name:     "parse error stays an error",
			content:  "fields date, description, amount\n\nif,account2\n",
			code:     "IF_TABLE_NO_ROWS",
			severity: protocol.DiagnosticSeverityError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestServer()
			diags, err := ts.openAndWait(uri.URI("file:///bank.csv.rules"), tt.content)
			require.NoError(t, err)
			require.Len(t, diags, 1, "%v", diags)
			assert.Equal(t, tt.code, diagCode(diags[0]))
			assert.Equal(t, tt.severity, diags[0].Severity)
		})
	}
}

func TestServer_RulesParseDiagnostics_IncludedFileKeepsSeverityAndCode(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.rules")
	childPath := filepath.Join(dir, "child.rules")
	mainContent := "include child.rules\nfields date, description, amount\n"
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o644))
	require.NoError(t, os.WriteFile(childPath, []byte("bogus-directive here\n"), 0o644))

	srv := NewServer()
	byURI := srv.analyzeRulesResolved(mainPath, mainContent)

	assert.Empty(t, byURI[pathToURI(mainPath)])
	child := byURI[pathToURI(childPath)]
	require.Len(t, child, 1, "%v", byURI)
	assert.Equal(t, "UNKNOWN_DIRECTIVE", diagCode(child[0]))
	assert.Equal(t, protocol.DiagnosticSeverityWarning, child[0].Severity)
	assert.Equal(t, uint32(0), child[0].Range.Start.Line)
}
