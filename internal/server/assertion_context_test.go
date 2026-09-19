package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/uri"
)

// A year-closing entry as produced by the clopen workflow: every asset posting
// carries an assertion that the account is zero afterwards.
const clopenYearFile = `2019-12-31 closing balances  ; clopen:
    Активы:Наличные                      -1.755,00 RUB      = 0,00 RUB
    Активы:Тинькофф:Вклад-1910           -922.982,38 RUB    = 0,00 RUB
    equity:opening/closing balances
`

// The journal's root establishes the balances the closing entry asserts against.
const clopenRootWithOpening = `include 2019.journal

2019-01-01 opening balances
    Активы:Наличные                       1.755,00 RUB
    Активы:Тинькофф:Вклад-1910          922.982,38 RUB
    equity:opening
`

func TestPublishDiagnostics_IncludedFileAssertionsUseWholeJournal(t *testing.T) {
	// The assertion depends on transactions from the root file. Evaluating the
	// included file as a journal of its own reported a failure for an entry
	// hledger accepts (`hledger -f main.journal` exits 0).
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()
	mainPath := filepath.Join(tmpDir, "main.journal")
	yearPath := filepath.Join(tmpDir, "2019.journal")
	require.NoError(t, os.WriteFile(mainPath, []byte(clopenRootWithOpening), 0o644))
	require.NoError(t, os.WriteFile(yearPath, []byte(clopenYearFile), 0o644))

	ts := initWorkspaceTestServer(t, tmpDir)
	yearURI := uri.File(yearPath)

	require.NoError(t, ts.openDocument(yearURI, clopenYearFile))

	diagnostics := waitForDocument(t, ts.client, yearURI)
	assert.Nil(t, diagnosticWithCode(diagnostics, "BALANCE_ASSERTION_FAILED"),
		"the closing entry must be checked against every file of its journal, got %v", codesIn(diagnostics))
}

func TestPublishDiagnostics_IncludedFileAssertionsStillFailWithoutHistory(t *testing.T) {
	// Control: with the opening transaction missing there is nothing to close, so
	// the assertion must still be reported.
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()
	mainPath := filepath.Join(tmpDir, "main.journal")
	yearPath := filepath.Join(tmpDir, "2019.journal")
	require.NoError(t, os.WriteFile(mainPath, []byte("include 2019.journal\n"), 0o644))
	require.NoError(t, os.WriteFile(yearPath, []byte(clopenYearFile), 0o644))

	ts := initWorkspaceTestServer(t, tmpDir)
	yearURI := uri.File(yearPath)

	require.NoError(t, ts.openDocument(yearURI, clopenYearFile))

	diagnostics := waitForDocument(t, ts.client, yearURI)
	assertion := diagnosticWithCode(diagnostics, "BALANCE_ASSERTION_FAILED")
	require.NotNil(t, assertion, "a closing entry without the opening history is broken, got %v", codesIn(diagnostics))
	assert.Contains(t, tooltipString(assertion.Message), "calculated -1755 RUB")
}

func TestPublishDiagnostics_AssertionHintWhenJournalIsNotInWorkspace(t *testing.T) {
	// The file is not part of any journal the workspace discovered, so the verdict
	// can only cover its own transactions. The message says so instead of leaving
	// the user wondering why hledger accepts their journal.
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	workspaceDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, "other.journal"),
		[]byte("2024-01-01 unrelated\n    a:aa  $1\n    b:bb\n"), 0o644))

	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "2019.journal")
	require.NoError(t, os.WriteFile(outsidePath, []byte(clopenYearFile), 0o644))

	ts := initWorkspaceTestServer(t, workspaceDir)
	outsideURI := uri.File(outsidePath)

	require.NoError(t, ts.openDocument(outsideURI, clopenYearFile))

	diagnostics := waitForDocument(t, ts.client, outsideURI)
	assertion := diagnosticWithCode(diagnostics, "BALANCE_ASSERTION_FAILED")
	require.NotNil(t, assertion, "got %v", codesIn(diagnostics))
	assert.Contains(t, tooltipString(assertion.Message), "not included by a journal in the workspace",
		"the message must explain that only this file's transactions were compared")
	assert.Contains(t, strings.ToLower(string(assertion.Data)), "partialcontext")
}
