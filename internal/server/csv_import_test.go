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

	"github.com/juev/hledger-lsp/internal/cli"
	"github.com/juev/hledger-lsp/internal/csvimport"
	"github.com/juev/hledger-lsp/internal/testutil"
)

// previewHledgerPath returns the installed hledger when it can include CSV
// files. Run with the 1.99.5 preview first on PATH and HLEDGER_ORACLE=1.
func previewHledgerPath(t *testing.T) string {
	t.Helper()
	path := testutil.RequireHledger(t)
	client := cli.NewClient(path, 30*time.Second)
	if !client.Available() || !csvimport.SupportsInclude(client.Version()) {
		t.Skipf("hledger %q on PATH cannot include CSV files; put 1.99.5 first on PATH", client.Version())
	}
	return path
}

func writeWorkspaceFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// startWorkspace initializes a server over dir the way a client does, then
// applies the target version and CLI settings.
func startWorkspace(t *testing.T, dir, target, hledger string) *testServer {
	t.Helper()
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")
	ts := newTestServer()
	_, err := ts.Initialize(context.Background(), initParamsWithRootURI(uri.File(dir)))
	require.NoError(t, err)
	require.NoError(t, ts.Initialized(context.Background(), &protocol.InitializedParams{}))
	settings := ts.getSettings()
	settings.TargetVersion = target
	settings.CLI.Path = hledger
	ts.setSettings(settings)
	return ts
}

func diagnosticCodes(diagnostics []protocol.Diagnostic) []string {
	var codes []string
	for _, diagnostic := range diagnostics {
		codes = append(codes, diagnosticCodeString(diagnostic.Code)+": "+tooltipString(diagnostic.Message))
	}
	return codes
}

const importedBankCSV = "date,desc,amount\n2024-01-02,Кофе ☕,-3.505\n2024-01-03,salary,1000.10\n"
const importedBankRules = "skip 1\nfields date, description, amount\ncurrency $\naccount1 assets:bank\naccount2 income:other\n"

// The assertion holds only when the CSV records are part of the journal.
// assertionFailure is what the journal reports when the CSV records are missing.
const assertionFailure = "BALANCE_ASSERTION_FAILED: balance assertion failed in assets:bank: asserted 996.595 $, calculated 0 $ (difference -996.595)"

const importingJournal = "include bank.csv\n\n2024-01-05 check\n    assets:bank  $0 = $996.595\n    equity\n"

func TestPreviewOracle_ServerImportsIncludedCSVUnderTarget2(t *testing.T) {
	hledger := previewHledgerPath(t)
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "bank.csv", importedBankCSV)
	writeWorkspaceFile(t, dir, "bank.csv.rules", importedBankRules)
	mainPath := writeWorkspaceFile(t, dir, "main.journal", importingJournal)
	ts := startWorkspace(t, dir, targetVersion2, hledger)

	diagnostics, err := ts.openAndWait(uri.File(mainPath), importingJournal)

	require.NoError(t, err)
	assert.Empty(t, diagnosticCodes(diagnostics), "the CSV records satisfy the assertion")
}

func TestServerKeepsRefusingCSVIncludesUnderTarget1(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "bank.csv", importedBankCSV)
	writeWorkspaceFile(t, dir, "bank.csv.rules", importedBankRules)
	mainPath := writeWorkspaceFile(t, dir, "main.journal", importingJournal)
	ts := startWorkspace(t, dir, targetVersion1, "/nonexistent/hledger")

	diagnostics, err := ts.openAndWait(uri.File(mainPath), importingJournal)

	require.NoError(t, err)
	codes := diagnosticCodes(diagnostics)
	assert.Contains(t, codes, "INCLUDE_NOT_JOURNAL: included file is not a journal file: bank.csv")
	assert.Contains(t, codes, assertionFailure)
}

func TestServerReportsAnUnavailableCLIAtTheInclude(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "bank.csv", importedBankCSV)
	writeWorkspaceFile(t, dir, "bank.csv.rules", importedBankRules)
	mainPath := writeWorkspaceFile(t, dir, "main.journal", importingJournal)
	ts := startWorkspace(t, dir, targetVersion2, filepath.Join(dir, "no-hledger"))

	diagnostics, err := ts.openAndWait(uri.File(mainPath), importingJournal)

	require.NoError(t, err)
	codes := diagnosticCodes(diagnostics)
	assert.Contains(t, codes, `INCLUDE_IMPORT_ERROR: cannot import bank.csv: including CSV files needs the hledger CLI, which is not available at "`+filepath.Join(dir, "no-hledger")+`"`)
	assert.Contains(t, codes, assertionFailure, "missing data is never a false balance")
}

// lastDiagnosticsFor returns the codes and messages last published for docURI,
// and whether anything was published for it.
func (ts *testServer) lastDiagnosticsFor(docURI uri.URI) ([]string, bool) {
	ts.client.mu.Lock()
	defer ts.client.mu.Unlock()
	for i := len(ts.client.diagnostics) - 1; i >= 0; i-- {
		if ts.client.diagnostics[i].URI == docURI {
			return diagnosticCodes(ts.client.diagnostics[i].Diagnostics), true
		}
	}
	return nil, false
}

// eventuallyDiagnostics waits until the diagnostics last published for docURI
// satisfy check.
func (ts *testServer) eventuallyDiagnostics(t *testing.T, docURI uri.URI, check func([]string) bool) {
	t.Helper()
	var codes []string
	require.Eventually(t, func() bool {
		var ok bool
		codes, ok = ts.lastDiagnosticsFor(docURI)
		return ok && check(codes)
	}, 15*time.Second, 20*time.Millisecond, "last diagnostics: %v", codes)
}

func contains(codes []string, want string) bool {
	for _, code := range codes {
		if code == want {
			return true
		}
	}
	return false
}

func containsPrefix(codes []string, prefix string) bool {
	for _, code := range codes {
		if len(code) >= len(prefix) && code[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// Data a journal formatter would rewrite: two-space runs inside a quoted field.
const fragileCSV = "date,desc,amount\n2024-01-02,\"a  b   c\",-3.505\n2024-01-03,salary,1000.10\n"

// requireNoJournalFeatures checks that no journal feature answers for an
// import source, and that its bytes stay as they were.
func requireNoJournalFeatures(t *testing.T, ts *testServer, path, content string) {
	t.Helper()
	ctx := context.Background()
	docURI := uri.File(path)
	doc := protocol.TextDocumentIdentifier{URI: docURI}
	position := protocol.Position{Line: 1, Character: 2}
	whole := protocol.Range{Start: protocol.Position{}, End: protocol.Position{Line: 3}}

	formatted, err := ts.Formatting(ctx, &protocol.DocumentFormattingParams{TextDocument: doc})
	require.NoError(t, err)
	assert.Empty(t, formatted, "formatting %s", path)
	ranged, err := ts.RangeFormatting(ctx, &protocol.DocumentRangeFormattingParams{TextDocument: doc, Range: whole})
	require.NoError(t, err)
	assert.Empty(t, ranged, "range formatting %s", path)
	typed, err := ts.OnTypeFormatting(ctx, &protocol.DocumentOnTypeFormattingParams{TextDocument: doc, Position: protocol.Position{Line: 2}, Ch: "\n"})
	require.NoError(t, err)
	assert.Empty(t, typed, "on-type formatting %s", path)
	completion, err := ts.completion(docURI, 1, 2)
	require.NoError(t, err)
	if completion != nil {
		assert.Empty(t, completion.Items, "completion %s", path)
	}
	hints, err := ts.InlayHint(ctx, &protocol.InlayHintParams{TextDocument: doc, Range: whole})
	require.NoError(t, err)
	assert.Empty(t, hints, "inlay hints %s", path)
	actions, err := ts.CodeAction(ctx, &protocol.CodeActionParams{TextDocument: doc, Range: protocol.Range{Start: position, End: position}})
	require.NoError(t, err)
	assert.Empty(t, actions, "code actions %s", path)
	inline, err := ts.InlineCompletion(ctx, &protocol.InlineCompletionParams{TextDocument: doc, Position: position})
	require.NoError(t, err)
	if list, ok := inline.(*protocol.InlineCompletionList); ok && list != nil {
		assert.Empty(t, list.Items, "inline completion %s", path)
	}
	// The document gets no journal analysis of its own: no parse errors.
	if codes, ok := ts.lastDiagnosticsFor(docURI); ok {
		assert.False(t, containsPrefix(codes, "PARSE"), "journal parse diagnostics on %s: %v", path, codes)
		assert.False(t, containsPrefix(codes, "INCLUDE_PARSE"), "journal parse diagnostics on %s: %v", path, codes)
	}
	disk, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, content, string(disk))
}

// importSourceWorkspace writes a workspace whose main journal imports
// bank.csv, and data.journal through a csv: prefix, with a hard link and a
// symlink to bank.csv under journal names.
func importSourceWorkspace(t *testing.T) (dir string, sources []string) {
	t.Helper()
	dir = t.TempDir()
	bank := writeWorkspaceFile(t, dir, "bank.csv", fragileCSV)
	writeWorkspaceFile(t, dir, "bank.csv.rules", importedBankRules)
	data := writeWorkspaceFile(t, dir, "data.journal", fragileCSV)
	writeWorkspaceFile(t, dir, "data.journal.rules", importedBankRules)
	writeWorkspaceFile(t, dir, "main.journal", "include bank.csv\ninclude csv:data.journal\n")
	hard := filepath.Join(dir, "hard.journal")
	require.NoError(t, os.Link(bank, hard))
	soft := filepath.Join(dir, "soft.journal")
	require.NoError(t, os.Symlink(bank, soft))
	return dir, []string{bank, data, hard, soft}
}

func TestPreviewOracle_ServerGivesImportSourcesNoJournalFeatures(t *testing.T) {
	hledger := previewHledgerPath(t)
	dir, sources := importSourceWorkspace(t)
	ts := startWorkspace(t, dir, targetVersion2, hledger)
	mainURI := uri.File(filepath.Join(dir, "main.journal"))
	_, err := ts.openAndWait(mainURI, "include bank.csv\ninclude csv:data.journal\n")
	require.NoError(t, err)

	for _, path := range sources {
		require.NoError(t, ts.openDocument(uri.File(path), fragileCSV))
		requireNoJournalFeatures(t, ts, path, fragileCSV)
	}
}

func TestServerGivesFailedAndRefusedImportSourcesNoJournalFeatures(t *testing.T) {
	for _, target := range []string{targetVersion1, targetVersion2} {
		t.Run("target "+target, func(t *testing.T) {
			dir, sources := importSourceWorkspace(t)
			// Under target 2 the import fails: there is no hledger at this path.
			ts := startWorkspace(t, dir, target, filepath.Join(dir, "no-hledger"))
			mainURI := uri.File(filepath.Join(dir, "main.journal"))
			_, err := ts.openAndWait(mainURI, "include bank.csv\ninclude csv:data.journal\n")
			require.NoError(t, err)

			for _, path := range sources {
				require.NoError(t, ts.openDocument(uri.File(path), fragileCSV))
				requireNoJournalFeatures(t, ts, path, fragileCSV)
			}
		})
	}
}

func TestPreviewOracle_ServerRefusesRenamingAnAccountImportedDataUses(t *testing.T) {
	hledger := previewHledgerPath(t)
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "bank.csv", importedBankCSV)
	writeWorkspaceFile(t, dir, "bank.csv.rules", importedBankRules)
	mainPath := writeWorkspaceFile(t, dir, "main.journal", importingJournal)
	ts := startWorkspace(t, dir, targetVersion2, hledger)
	mainURI := uri.File(mainPath)
	_, err := ts.openAndWait(mainURI, importingJournal)
	require.NoError(t, err)

	edit, err := ts.Rename(context.Background(), &protocol.RenameParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: mainURI},
		Position:     protocol.Position{Line: 3, Character: 6},
		NewName:      "assets:checking",
	})

	require.EqualError(t, err, "account is used by imported CSV data in bank.csv; rename it in the CSV rules")
	assert.Nil(t, edit)
	disk, err := os.ReadFile(filepath.Join(dir, "bank.csv"))
	require.NoError(t, err)
	assert.Equal(t, importedBankCSV, string(disk))
}

func TestPreviewOracle_ServerFollowsSettingsAndDependencyChanges(t *testing.T) {
	hledger := previewHledgerPath(t)
	dir := t.TempDir()
	bank := writeWorkspaceFile(t, dir, "bank.csv", importedBankCSV)
	rules := writeWorkspaceFile(t, dir, "bank.csv.rules", importedBankRules)
	mainPath := writeWorkspaceFile(t, dir, "main.journal", importingJournal)
	ts := startWorkspace(t, dir, targetVersion1, hledger)
	mainURI := uri.File(mainPath)
	before := listingOf(t, dir)

	_, err := ts.openAndWait(mainURI, importingJournal)
	require.NoError(t, err)
	ts.eventuallyDiagnostics(t, mainURI, func(codes []string) bool { return contains(codes, assertionFailure) })

	// Switching the target reads the include again, now through hledger.
	settings := ts.getSettings()
	settings.TargetVersion = targetVersion2
	ts.setSettings(settings)
	ts.eventuallyDiagnostics(t, mainURI, func(codes []string) bool { return len(codes) == 0 })

	// Disabling the CLI is an explicit error, never a balanced journal.
	settings.CLI.Enabled = false
	ts.setSettings(settings)
	ts.eventuallyDiagnostics(t, mainURI, func(codes []string) bool {
		return contains(codes, "INCLUDE_IMPORT_ERROR: cannot import bank.csv: including CSV files needs the hledger CLI, which is disabled (hledger.cli.enabled is false)") && contains(codes, assertionFailure)
	})
	settings.CLI.Enabled = true
	ts.setSettings(settings)
	ts.eventuallyDiagnostics(t, mainURI, func(codes []string) bool { return len(codes) == 0 })

	// A changed data file is noticed through the watcher.
	writeWorkspaceFile(t, dir, "bank.csv", "date,desc,amount\n2024-01-02,Кофе ☕,-3.505\n")
	require.NoError(t, ts.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{{URI: uri.File(bank), Type: protocol.FileChangeTypeChanged}},
	}))
	ts.eventuallyDiagnostics(t, mainURI, func(codes []string) bool {
		return contains(codes, "BALANCE_ASSERTION_FAILED: balance assertion failed in assets:bank: asserted 996.595 $, calculated -3.505 $ (difference -1000.1)")
	})
	writeWorkspaceFile(t, dir, "bank.csv", importedBankCSV)
	require.NoError(t, ts.DidChangeWatchedFiles(context.Background(), &protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{{URI: uri.File(bank), Type: protocol.FileChangeTypeChanged}},
	}))
	ts.eventuallyDiagnostics(t, mainURI, func(codes []string) bool { return len(codes) == 0 })

	// An edited rules buffer is reported until it is saved or closed; the
	// import keeps reading the saved rules.
	rulesURI := uri.File(rules)
	require.NoError(t, ts.openDocument(rulesURI, importedBankRules))
	require.NoError(t, ts.changeDocument(rulesURI, []protocol.TextDocumentContentChangeEvent{
		&protocol.TextDocumentContentChangeWholeDocument{Text: importedBankRules + "account2 income:changed\n"},
	}))
	unsaved := "INCLUDE_IMPORT_UNSAVED: bank.csv.rules has unsaved changes; the import reads the saved file"
	ts.eventuallyDiagnostics(t, mainURI, func(codes []string) bool { return len(codes) == 1 && contains(codes, unsaved) })
	require.NoError(t, ts.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{TextDocument: protocol.TextDocumentIdentifier{URI: rulesURI}}))
	ts.eventuallyDiagnostics(t, mainURI, func(codes []string) bool { return len(codes) == 0 })

	assert.Equal(t, before, listingOf(t, dir), "no file is created beside the inputs")
	for _, path := range []string{rules, mainPath} {
		disk, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, []string{importedBankRules, importingJournal}, string(disk), "%s is unchanged", path)
	}
}

func TestStableOracle_ServerAutoTargetReportsHledgerThatCannotIncludeCSV(t *testing.T) {
	path := testutil.RequireHledger(t)
	client := cli.NewClient(path, 30*time.Second)
	if !client.Available() || client.Version() == "" || csvimport.SupportsInclude(client.Version()) {
		t.Skipf("hledger %q on PATH is not a 1.x release", client.Version())
	}
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "bank.csv", importedBankCSV)
	writeWorkspaceFile(t, dir, "bank.csv.rules", importedBankRules)
	mainPath := writeWorkspaceFile(t, dir, "main.journal", importingJournal)
	ts := startWorkspace(t, dir, targetVersionAuto, path)

	diagnostics, err := ts.openAndWait(uri.File(mainPath), importingJournal)

	require.NoError(t, err)
	codes := diagnosticCodes(diagnostics)
	assert.Contains(t, codes, "INCLUDE_IMPORT_ERROR: cannot import bank.csv: hledger "+client.Version()+" cannot include CSV files; hledger 1.99.5 or newer is needed")
	assert.Contains(t, codes, assertionFailure)
}

func TestPreviewOracle_ServerAutoTargetImportsWithAPreview(t *testing.T) {
	hledger := previewHledgerPath(t)
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "bank.csv", importedBankCSV)
	writeWorkspaceFile(t, dir, "bank.csv.rules", importedBankRules)
	mainPath := writeWorkspaceFile(t, dir, "main.journal", importingJournal)
	ts := startWorkspace(t, dir, targetVersionAuto, hledger)

	diagnostics, err := ts.openAndWait(uri.File(mainPath), importingJournal)

	require.NoError(t, err)
	assert.Empty(t, diagnosticCodes(diagnostics))
}

func listingOf(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries := make(map[string]string)
	require.NoError(t, filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		require.NoError(t, err)
		entries[path] = info.Mode().String()
		return nil
	}))
	return entries
}

func TestSettings_TargetVersion(t *testing.T) {
	assert.Equal(t, targetVersion1, defaultServerSettings().TargetVersion, "hledger 1 stays the default")

	for raw, want := range map[interface{}]string{"2": targetVersion2, float64(2): targetVersion2, 1: targetVersion1, " AUTO ": targetVersionAuto} {
		settings, issues := parseSettingsFromRawWithIssues(defaultServerSettings(), map[string]interface{}{"targetVersion": raw})
		assert.Equal(t, want, settings.TargetVersion, "%#v", raw)
		assert.Empty(t, issues, "%#v", raw)
	}
	for _, raw := range []interface{}{"3", 2.5, "latest"} {
		settings, issues := parseSettingsFromRawWithIssues(defaultServerSettings(), map[string]interface{}{"targetVersion": raw})
		assert.Equal(t, targetVersion1, settings.TargetVersion, "%#v", raw)
		require.Len(t, issues, 1, "%#v", raw)
		assert.Contains(t, issues[0], `is not one of "1", "2", "auto"; using "1"`)
	}
}

func TestImportSettingsChanged(t *testing.T) {
	base := defaultServerSettings()
	two := base
	two.TargetVersion = targetVersion2
	assert.True(t, importSettingsChanged(base, two))

	otherPath := base
	otherPath.CLI.Path = "/other/hledger"
	assert.False(t, importSettingsChanged(base, otherPath), "under target 1 the CLI does not read includes")

	twoOtherTimeout := two
	twoOtherTimeout.CLI.Timeout = time.Second
	assert.True(t, importSettingsChanged(two, twoOtherTimeout))
	twoDisabled := two
	twoDisabled.CLI.Enabled = false
	assert.True(t, importSettingsChanged(two, twoDisabled))
}
