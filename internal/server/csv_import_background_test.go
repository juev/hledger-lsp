package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// slowFailingHledger is a 1.99.5 that takes a while to refuse every conversion.
func slowFailingHledger(t *testing.T, dir string, pause string) string {
	t.Helper()
	path := filepath.Join(dir, "bin", "hledger")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'hledger 1.99.5, linux-x86_64'; exit 0; fi\n" +
		"sleep " + pause + "\necho 'hledger: Error: bad rules' >&2\nexit 1\n"
	require.NoError(t, os.WriteFile(path, []byte(script), 0o700))
	return path
}

func (ts *testServer) publishedFor(docURI uri.URI) [][]string {
	ts.client.mu.Lock()
	defer ts.client.mu.Unlock()
	var sets [][]string
	for _, params := range ts.client.diagnostics {
		if params.URI == docURI {
			sets = append(sets, diagnosticCodes(params.Diagnostics))
		}
	}
	return sets
}

// A conversion that hledger takes long over must hold neither requests nor the
// workspace: they are answered from what is known, and the journal is not
// judged until its imported data is known, so no assertion is reported as
// failing for want of records that are still being converted.
func TestServerAnswersRequestsWhileAnImportConverts(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "bank.csv", importedBankCSV)
	writeWorkspaceFile(t, dir, "bank.csv.rules", importedBankRules)
	mainPath := writeWorkspaceFile(t, dir, "main.journal", importingJournal)
	ts := startWorkspace(t, dir, targetVersion2, slowFailingHledger(t, dir, "2"))
	settings := ts.getSettings()
	settings.CLI.Timeout = 20 * time.Second
	ts.setSettings(settings)
	mainURI := uri.File(mainPath)

	start := time.Now()
	require.NoError(t, ts.openDocument(mainURI, importingJournal))
	_, err := ts.DocumentSymbol(context.Background(), &protocol.DocumentSymbolParams{TextDocument: protocol.TextDocumentIdentifier{URI: mainURI}})
	require.NoError(t, err)
	_, err = ts.Completion(context.Background(), &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: mainURI}, Position: protocol.Position{Line: 3, Character: 6}},
	})
	require.NoError(t, err)
	assert.Less(t, time.Since(start), time.Second, "requests must not wait for hledger")

	ts.eventuallyDiagnostics(t, mainURI, func(codes []string) bool { return containsPrefix(codes, "INCLUDE_IMPORT_ERROR") })
	assert.GreaterOrEqual(t, time.Since(start), 2*time.Second, "the verdict comes from the finished conversion")
	for _, codes := range ts.publishedFor(mainURI) {
		assert.True(t, containsPrefix(codes, "INCLUDE_IMPORT_ERROR"), "a publish judged the journal without its data: %v", codes)
	}
	codes, _ := ts.lastDiagnosticsFor(mainURI)
	assert.Contains(t, codes, assertionFailure, "missing data is never a false balance")
}
