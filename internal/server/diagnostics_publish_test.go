package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// diagnosticsFor returns the last published diagnostic set for one document.
func (m *integrationMockClient) diagnosticsFor(docURI uri.URI) *protocol.PublishDiagnosticsParams {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := len(m.diagnostics) - 1; i >= 0; i-- {
		if m.diagnostics[i].URI == docURI {
			result := m.diagnostics[i]
			return &result
		}
	}
	return nil
}

// waitForDocument waits until the server publishes diagnostics for docURI and
// returns them.
func waitForDocument(t *testing.T, client *integrationMockClient, docURI uri.URI) []protocol.Diagnostic {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		client.waitDiagnostics()
		if params := client.diagnosticsFor(docURI); params != nil {
			return params.Diagnostics
		}
	}
	t.Fatalf("no diagnostics published for %s", docURI)
	return nil
}

func codesIn(diagnostics []protocol.Diagnostic) []string {
	codes := make([]string, 0, len(diagnostics))
	for _, d := range diagnostics {
		codes = append(codes, diagnosticCodeString(d.Code))
	}
	return codes
}

func diagnosticWithCode(diagnostics []protocol.Diagnostic, code string) *protocol.Diagnostic {
	for i := range diagnostics {
		if diagnosticCodeString(diagnostics[i].Code) == code {
			return &diagnostics[i]
		}
	}
	return nil
}

func writeJournal(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestPublishDiagnostics_IncludedParseErrorGoesToItsURI(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()
	mainPath := filepath.Join(tmpDir, "main.journal")
	childPath := filepath.Join(tmpDir, "child.journal")

	// '%' is not a comment form in hledger, so the included file is broken while
	// the root journal itself is fine.
	writeJournal(t, childPath, "2024-01-02 y\n    expenses:b  $2\n    assets:cash\n\n% not a comment\n")
	mainContent := "include child.journal\n\n2024-01-01 x\n    expenses:a  $1\n    assets:cash\n"
	writeJournal(t, mainPath, mainContent)

	ts := initWorkspaceTestServer(t, tmpDir)
	mainURI := uri.File(mainPath)
	childURI := uri.File(childPath)

	require.NoError(t, ts.openDocument(mainURI, mainContent))

	childDiagnostics := waitForDocument(t, ts.client, childURI)
	parseError := diagnosticWithCode(childDiagnostics, "PARSE_UNEXPECTED")
	require.NotNil(t, parseError, "the included file's syntax error must be published there, got %v", codesIn(childDiagnostics))
	assert.Contains(t, tooltipString(parseError.Message), "unexpected content")

	mainParams := ts.client.diagnosticsFor(mainURI)
	require.NotNil(t, mainParams)
	assert.Nil(t, diagnosticWithCode(mainParams.Diagnostics, "PARSE_UNEXPECTED"),
		"the root file must not inherit the included file's syntax error")
}

func TestPublishDiagnostics_NestedIncludeErrorLandsInOwningFile(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()
	mainPath := filepath.Join(tmpDir, "main.journal")
	midPath := filepath.Join(tmpDir, "mid.journal")

	midContent := "include missing.journal\n\n2024-01-02 mid\n    expenses:b  $2\n    assets:cash\n"
	mainContent := "include mid.journal\n\n2024-01-01 root\n    expenses:a  $1\n    assets:cash\n"
	writeJournal(t, midPath, midContent)
	writeJournal(t, mainPath, mainContent)

	ts := initWorkspaceTestServer(t, tmpDir)
	mainURI := uri.File(mainPath)
	midURI := uri.File(midPath)

	require.NoError(t, ts.openDocument(mainURI, mainContent))

	midDiagnostics := waitForDocument(t, ts.client, midURI)
	missing := diagnosticWithCode(midDiagnostics, "INCLUDE_NOT_FOUND")
	require.NotNil(t, missing, "the include failure belongs to the file containing it, got %v", codesIn(midDiagnostics))
	assert.Contains(t, tooltipString(missing.Message), "missing.journal")
	assert.Equal(t, uint32(0), missing.Range.Start.Line, "the range points at the include line inside mid.journal")

	mainParams := ts.client.diagnosticsFor(mainURI)
	require.NotNil(t, mainParams)
	assert.Nil(t, diagnosticWithCode(mainParams.Diagnostics, "INCLUDE_NOT_FOUND"),
		"the root must not carry the nested include's error")
}

func TestPublishDiagnostics_AssertionFailureLandsInIncludedFile(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()
	mainPath := filepath.Join(tmpDir, "main.journal")
	childPath := filepath.Join(tmpDir, "child.journal")

	// The assertion in the child file is evaluated against the balance built from
	// both files in date order, and reported where it is written.
	writeJournal(t, childPath, "2024-01-02 child\n    a:aa  $5 = $99\n    c:cc\n")
	mainContent := "include child.journal\n\n2024-01-01 root\n    a:aa  $10\n    b:bb\n"
	writeJournal(t, mainPath, mainContent)

	ts := initWorkspaceTestServer(t, tmpDir)
	mainURI := uri.File(mainPath)
	childURI := uri.File(childPath)

	require.NoError(t, ts.openDocument(mainURI, mainContent))

	childDiagnostics := waitForDocument(t, ts.client, childURI)
	assertion := diagnosticWithCode(childDiagnostics, "BALANCE_ASSERTION_FAILED")
	require.NotNil(t, assertion, "the failing assertion must be published in its own file, got %v", codesIn(childDiagnostics))
	assert.Contains(t, tooltipString(assertion.Message), "calculated 15 $")
	assert.NotNil(t, assertion.Data, "assertion diagnostics carry structured data for clients")
	assert.Contains(t, string(assertion.Data), "balanceAssertion")

	mainParams := ts.client.diagnosticsFor(mainURI)
	require.NotNil(t, mainParams)
	assert.Nil(t, diagnosticWithCode(mainParams.Diagnostics, "BALANCE_ASSERTION_FAILED"))
}

func TestPublishDiagnostics_AssertionFailureReportedInRoot(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///assertion.journal")

	content := `2024-01-01 x
    assets:cash  $-100 = $50
    expenses:food  $100
`
	diagnostics, err := ts.openAndWait(docURI, content)
	require.NoError(t, err)

	assertion := diagnosticWithCode(diagnostics, "BALANCE_ASSERTION_FAILED")
	require.NotNil(t, assertion, "a wrong balance assertion must be reported, got %v", codesIn(diagnostics))
	assert.Equal(t, protocol.DiagnosticSeverityError, assertion.Severity)
	assert.Equal(t, uint32(1), assertion.Range.Start.Line, "the range covers the assertion itself")
}

func TestPublishDiagnostics_PassingAssertionsStaySilent(t *testing.T) {
	ts := newTestServer()
	ts.enableOptInDiagnostics(accountCheckOff)
	docURI := uri.URI("file:///assertions.journal")

	content := `2024-01-01 x
    assets:cash  $100 = $100
    equity

2024-01-02 y
    assets:cash  $5 == $105
    equity
`
	diagnostics, err := ts.openAndWait(docURI, content)
	require.NoError(t, err)
	assert.Nil(t, diagnosticWithCode(diagnostics, "BALANCE_ASSERTION_FAILED"), "got %v", codesIn(diagnostics))
	assert.Nil(t, diagnosticWithCode(diagnostics, "UNBALANCED"), "got %v", codesIn(diagnostics))
}

func TestPublishDiagnostics_HledgerConversionIsNotFlagged(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///conversion.journal")

	// hledger 1.52.4 accepts this transaction: two residual commodities with
	// opposite signs and no explicit cost imply a conversion price.
	content := `2024-01-15 convert
    assets:bank:eur  100 EUR
    assets:bank:usd  $-110
`
	diagnostics, err := ts.openAndWait(docURI, content)
	require.NoError(t, err)
	assert.Nil(t, diagnosticWithCode(diagnostics, "UNBALANCED"), "got %v", codesIn(diagnostics))
}

func TestPublishDiagnostics_CostPrecisionIsNotFlagged(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///cost.journal")

	content := `2024-01-01 buy
    a:aa  1.005 AAPL @ $2
    b:bb  $-2.0
`
	diagnostics, err := ts.openAndWait(docURI, content)
	require.NoError(t, err)
	assert.Nil(t, diagnosticWithCode(diagnostics, "UNBALANCED"), "got %v", codesIn(diagnostics))
}

func TestPublishDiagnostics_InvalidDateCarriesCode(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///dates.journal")

	content := `2024-02-31 broken
    expenses:food  $50
    assets:cash
`
	diagnostics, err := ts.openAndWait(docURI, content)
	require.NoError(t, err)

	invalid := diagnosticWithCode(diagnostics, "INVALID_DATE")
	require.NotNil(t, invalid, "an impossible date must be reported, got %v", codesIn(diagnostics))
	assert.Contains(t, tooltipString(invalid.Message), "invalid date: 2024-02-31")
	assert.Len(t, diagnostics, 1, "one diagnostic for the broken header, not one per posting line")
}

func TestPublishDiagnostics_MultipleInferredPointsAtEachPosting(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///inferred.journal")

	// The classic single-space mistake: the account name absorbs the amount.
	content := `2024-01-01 x
    expenses:food $100
    assets:cash
`
	diagnostics, err := ts.openAndWait(docURI, content)
	require.NoError(t, err)

	var inferred []protocol.Diagnostic
	for _, d := range diagnostics {
		if diagnosticCodeString(d.Code) == "MULTIPLE_INFERRED" {
			inferred = append(inferred, d)
		}
	}
	require.NotEmpty(t, inferred, "got %v", codesIn(diagnostics))
	assert.Contains(t, tooltipString(inferred[0].Message), "two or more spaces",
		"the message must carry hledger's hint about the separator")
	assert.Equal(t, uint32(1), inferred[0].Range.Start.Line,
		"the diagnostic points at the posting line rather than the whole transaction")
}

func TestPublishDiagnostics_RulesDiagnosticsDeduplicated(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///test.rules")

	content := "skip 1\nfields date, description, amount\nunknownthing\n"

	diagnostics, err := ts.openAndWait(docURI, content)
	require.NoError(t, err)
	require.NotEmpty(t, diagnostics, "an unknown rules directive must be reported")

	seen := make(map[string]int)
	for _, d := range diagnostics {
		key := fmt.Sprintf("%d:%d-%d:%d:%s", d.Range.Start.Line, d.Range.Start.Character,
			d.Range.End.Line, d.Range.End.Character, tooltipString(d.Message))
		seen[key]++
	}
	for key, count := range seen {
		assert.Equal(t, 1, count, "duplicate diagnostics for %s", key)
	}
}

func TestPublishDiagnostics_WatchedRepublishUsesDocumentVersion(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()
	mainPath := filepath.Join(tmpDir, "main.journal")
	childPath := filepath.Join(tmpDir, "child.journal")

	writeJournal(t, childPath, "2024-01-02 child\n    expenses:b  $2\n    assets:cash\n")
	mainContent := "include child.journal\n\n2024-01-01 root\n    expenses:a  $1\n    assets:cash\n"
	writeJournal(t, mainPath, mainContent)

	ts := initWorkspaceTestServer(t, tmpDir)
	mainURI := uri.File(mainPath)

	require.NoError(t, ts.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: mainURI, Version: 7, Text: mainContent},
	}))
	waitForDocument(t, ts.client, mainURI)

	require.NoError(t, ts.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{{URI: uri.File(childPath), Type: protocol.FileChangeTypeChanged}},
	}))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ts.client.waitDiagnostics()
		params := ts.client.diagnosticsFor(mainURI)
		if params == nil {
			continue
		}
		if version, ok := params.Version.Get(); ok {
			assert.Equal(t, int32(7), version, "a republished set must carry the document's version")
			return
		}
	}
	t.Fatal("no versioned republish happened for the watched file change")
}

func TestPublishDiagnostics_EmptySetClearsProblems(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///cleared.journal")

	broken := "2024-01-01 x\n    expenses:food  $50\n    assets:cash  $20\n"
	diagnostics, err := ts.openAndWait(docURI, broken)
	require.NoError(t, err)
	require.NotNil(t, diagnosticWithCode(diagnostics, "UNBALANCED"))

	fixed, err := ts.replaceAndWait(docURI, "2024-01-01 x\n    expenses:food  $50\n    assets:cash  $-50\n")
	require.NoError(t, err)
	assert.Empty(t, fixed, "a fixed document must be republished with no diagnostics, got %v", codesIn(fixed))
}

func TestPublishDiagnostics_DeclaredAccountsSilenceLintMode(t *testing.T) {
	ts := newTestServer()
	ts.enableOptInDiagnostics(accountCheckLint)
	docURI := uri.URI("file:///declared.journal")

	content := `account food:snacks
account assets:cash

2024-01-01 x
    food:snacks  $50
    assets:cash
`
	diagnostics, err := ts.openAndWait(docURI, content)
	require.NoError(t, err)
	assert.Nil(t, diagnosticWithCode(diagnostics, "UNDECLARED_ACCOUNT"), "got %v", codesIn(diagnostics))
}

func TestPublishDiagnostics_AssistantSummary(t *testing.T) {
	// Guard against a publish that loses the document version on the analysed
	// document itself.
	ts := newTestServer()
	docURI := uri.URI("file:///version.journal")

	require.NoError(t, ts.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     docURI,
			Version: 3,
			Text:    "2024-01-01 x\n    expenses:food  $50\n    assets:cash  $-50\n",
		},
	}))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ts.client.waitDiagnostics()
		params := ts.client.diagnosticsFor(docURI)
		if params == nil {
			continue
		}
		version, ok := params.Version.Get()
		require.True(t, ok, "the analysed document's set must carry a version")
		assert.Equal(t, int32(3), version)
		return
	}
	t.Fatal("no diagnostics published")
}

func TestPublishDiagnostics_BackdatedAssertionMatchesHledger(t *testing.T) {
	content := `2026-02-01 opening
    负债:信用卡  -699.41 CNY
    equity

2026-02-07 later
    负债:信用卡  -6130.28 CNY
    equity

2026-02-08 reconcile
    负债:信用卡  0 CNY = -699.41 CNY  ; date:2026-02-06
    equity  0 CNY
`
	for _, tc := range []struct {
		name    string
		newline string
	}{
		{name: "LF", newline: "\n"},
		{name: "CRLF", newline: "\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := newTestServer()
			journal := strings.ReplaceAll(content, "\n", tc.newline)
			valid, err := ts.openAndWait(uri.URI("file:///posting-date-valid.journal"), journal)
			require.NoError(t, err)
			assert.Nil(t, diagnosticWithCode(valid, "BALANCE_ASSERTION_FAILED"), "got %v", codesIn(valid))

			invalidJournal := strings.Replace(journal, "= -699.41 CNY", "= -700.41 CNY", 1)
			invalid, err := ts.openAndWait(uri.URI("file:///posting-date-invalid.journal"), invalidJournal)
			require.NoError(t, err)
			failure := diagnosticWithCode(invalid, "BALANCE_ASSERTION_FAILED")
			require.NotNil(t, failure, "a genuinely wrong assertion must still be reported, got %v", codesIn(invalid))
			assert.Contains(t, tooltipString(failure.Message), "calculated -699.41 CNY")
		})
	}
}

func TestPublishDiagnostics_IncludedAssertionRangeUsesItsOwnFile(t *testing.T) {
	// A multi-byte account in the included file must still map to the right
	// UTF-16 column, which requires the range conversion to use that file's
	// content rather than the root document's.
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()
	mainPath := filepath.Join(tmpDir, "main.journal")
	childPath := filepath.Join(tmpDir, "child.journal")

	childContent := "2024-01-02 child\n    расходы:🍜  $5 = $99\n    b:bb\n"
	mainContent := "include child.journal\n\n2024-01-01 root\n    расходы:🍜  $10\n    a:aa\n"
	writeJournal(t, childPath, childContent)
	writeJournal(t, mainPath, mainContent)

	ts := initWorkspaceTestServer(t, tmpDir)
	mainURI := uri.File(mainPath)
	childURI := uri.File(childPath)

	require.NoError(t, ts.openDocument(mainURI, mainContent))

	childDiagnostics := waitForDocument(t, ts.client, childURI)
	assertion := diagnosticWithCode(childDiagnostics, "BALANCE_ASSERTION_FAILED")
	require.NotNil(t, assertion, "got %v", codesIn(childDiagnostics))

	// "    расходы:🍜  $5 = $99": the assertion starts after 4 spaces, 7 Cyrillic
	// letters, a colon, an emoji (2 UTF-16 units), two spaces, "$5" and a space.
	assert.Equal(t, uint32(19), assertion.Range.Start.Character,
		"the range must be expressed in the owning file's UTF-16 columns")
	assert.True(t, strings.Contains(tooltipString(assertion.Message), "balance assertion failed"),
		"message: %s", tooltipString(assertion.Message))
}

func TestPublishDiagnostics_BalanceAssertionsCanBeDisabled(t *testing.T) {
	// hledger.diagnostics.balanceAssertions turns the whole assertion pass off,
	// which matters on journals whose history is incomplete.
	ts := newTestServer()
	settings := ts.getSettings()
	settings.Diagnostics.BalanceAssertions = false
	ts.setSettings(settings)

	docURI := uri.URI("file:///assertions-off.journal")
	content := "2024-01-01 x\n    assets:cash  $-100 = $50\n    expenses:food  $100\n"

	diagnostics, err := ts.openAndWait(docURI, content)
	require.NoError(t, err)
	assert.Nil(t, diagnosticWithCode(diagnostics, "BALANCE_ASSERTION_FAILED"),
		"the failing assertion must stay silent when the check is disabled, got %v", codesIn(diagnostics))

	// The setting is about assertions only: the balance check still runs.
	unbalanced, err := ts.openAndWait(uri.URI("file:///unbalanced.journal"), "2024-01-01 x\n    assets:cash  $100\n    expenses:food  $50\n")
	require.NoError(t, err)
	assert.NotNil(t, diagnosticWithCode(unbalanced, "UNBALANCED"))
}

func TestPublishDiagnostics_OpenIncludedFileKeepsItsOwnDiagnostics(t *testing.T) {
	// An open buffer owns its diagnostics: publishing the resolved tree to an
	// open included file would replace the buffer's problems with a set computed
	// from disk content and buffer offsets.
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()
	mainPath := filepath.Join(tmpDir, "main.journal")
	childPath := filepath.Join(tmpDir, "child.journal")

	writeJournal(t, childPath, "2024-01-02 child\n    a:aa  $5\n    c:cc\n")
	mainContent := "include child.journal\n\n2024-01-01 root\n    a:aa  $10\n    b:bb\n"
	writeJournal(t, mainPath, mainContent)

	ts := initWorkspaceTestServer(t, tmpDir)
	mainURI := uri.File(mainPath)
	childURI := uri.File(childPath)

	// The open child buffer carries a per-buffer problem the disk copy does not.
	childBuffer := "2024-01-02 child\n    a:aa  $5  ; date:2024-01\n    c:cc\n"
	require.NoError(t, ts.openDocument(childURI, childBuffer))
	childBefore := waitForDocument(t, ts.client, childURI)
	require.NotNil(t, diagnosticWithCode(childBefore, "INVALID_DATE_TAG"),
		"the buffer's own problem must be reported, got %v", codesIn(childBefore))

	require.NoError(t, ts.openDocument(mainURI, mainContent))
	time.Sleep(150 * time.Millisecond)

	childAfter := ts.client.diagnosticsFor(childURI)
	require.NotNil(t, childAfter)
	assert.NotNil(t, diagnosticWithCode(childAfter.Diagnostics, "INVALID_DATE_TAG"),
		"publishing for the root must not drop the open buffer's own diagnostics")
}

func TestPublishDiagnostics_StrictModeStillOffersDeclareAccount(t *testing.T) {
	ts := newTestServer()
	settings := ts.getSettings()
	settings.Diagnostics.AccountCheck = accountCheckStrict
	ts.setSettings(settings)

	docURI := uri.URI("file:///strict.journal")
	content := "2024-01-01 x\n    food:snacks  $50\n    assets:cash\n"

	diagnostics, err := ts.openAndWait(docURI, content)
	require.NoError(t, err)
	undeclared := diagnosticWithCode(diagnostics, "UNDECLARED_ACCOUNT")
	require.NotNil(t, undeclared, "strict mode reports the undeclared account, got %v", codesIn(diagnostics))

	// A client that supports code action literals is what receives the fix.
	ts.clientCapabilities.supportsCodeActionLiterals = true

	actions, err := ts.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
		Range:        undeclared.Range,
		Context: protocol.CodeActionContext{
			Diagnostics: []protocol.Diagnostic{*undeclared},
		},
	})
	require.NoError(t, err)

	var titles []string
	for _, action := range actions {
		if ca, ok := action.(*protocol.CodeAction); ok {
			titles = append(titles, ca.Title)
		}
	}
	assert.Contains(t, titles, "Declare account food:snacks",
		"the declaration fix must also work in strict mode")
}
