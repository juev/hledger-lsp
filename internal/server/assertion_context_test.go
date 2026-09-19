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

// The same clopen entry as clopenYearFile, except the year file writes its
// activity bare, the way the D directive allows: hledger reads every bare
// number as RUB, so the RUB assertions still hold.
const clopenYearFileBareActivity = `2019-06-01 salary
    Активы:Наличные                        1.755,00
    income:salary

2019-12-31 closing balances  ; clopen:
    Активы:Наличные                      -1.755,00 RUB      = 0,00 RUB
    equity:opening/closing balances
`

func TestPublishDiagnostics_BareActivityBalancesRubAssertionsUnderDefaultCommodity(t *testing.T) {
	// hledger -f main.journal exits 0 for this tree. Before the D directive was
	// applied, the bare activity landed in a commodity-less balance while the
	// assertions summed RUB, and every clopen assertion was reported as failed.
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()
	mainPath := filepath.Join(tmpDir, "main.journal")
	yearPath := filepath.Join(tmpDir, "2019.journal")
	mainContent := "D 1.000,00 RUB\ncommodity RUB\n\ninclude 2019.journal\n"
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o644))
	require.NoError(t, os.WriteFile(yearPath, []byte(clopenYearFileBareActivity), 0o644))

	ts := initWorkspaceTestServer(t, tmpDir)
	yearURI := uri.File(yearPath)

	require.NoError(t, ts.openDocument(yearURI, clopenYearFileBareActivity))

	diagnostics := waitForDocument(t, ts.client, yearURI)
	assert.Nil(t, diagnosticWithCode(diagnostics, "BALANCE_ASSERTION_FAILED"),
		"bare amounts inherit the default commodity, got %v", codesIn(diagnostics))
}

func TestPublishDiagnostics_BareActivityStillFailsForAnotherCommodity(t *testing.T) {
	// Control: the default commodity makes bare amounts RUB, not a wildcard. An
	// assertion of a non-zero amount in another commodity still fails, as it does
	// in hledger (`hledger -f main.journal print` exits 1 for this tree).
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()
	mainPath := filepath.Join(tmpDir, "main.journal")
	yearPath := filepath.Join(tmpDir, "2019.journal")
	mainContent := "D 1.000,00 RUB\ncommodity RUB\n\ninclude 2019.journal\n"
	yearContent := `2019-06-01 salary
    Активы:Наличные                        1.755,00
    income:salary

2019-12-31 closing balances  ; clopen:
    Активы:Наличные                      -1.755,00 RUB      = 1.755,00 USD
    equity:opening/closing balances
`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o644))
	require.NoError(t, os.WriteFile(yearPath, []byte(yearContent), 0o644))

	ts := initWorkspaceTestServer(t, tmpDir)
	yearURI := uri.File(yearPath)

	require.NoError(t, ts.openDocument(yearURI, yearContent))

	diagnostics := waitForDocument(t, ts.client, yearURI)
	assertion := diagnosticWithCode(diagnostics, "BALANCE_ASSERTION_FAILED")
	require.NotNil(t, assertion, "a USD assertion cannot be satisfied by RUB amounts, got %v", codesIn(diagnostics))
	assert.Contains(t, tooltipString(assertion.Message), "USD")
}
