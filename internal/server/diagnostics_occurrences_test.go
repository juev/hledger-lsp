package server

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// publishedFor opens the buffer in a workspace rooted at dir and returns what the
// server publishes for it.
func publishedFor(t *testing.T, dir, text string, configure func(*testServer)) []protocol.Diagnostic {
	t.Helper()
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")
	ts := initWorkspaceTestServer(t, dir)
	if configure != nil {
		configure(ts)
	}
	docURI := uri.File(filepath.Join(dir, "shared.journal"))
	require.NoError(t, ts.openDocument(docURI, strings.ReplaceAll(text, "\r\n", "\n")))
	return waitForDocument(t, ts.client, docURI)
}

func strict(ts *testServer) { ts.enableOptInDiagnostics("strict") }

func messages(diagnostics []protocol.Diagnostic) []string {
	result := make([]string, 0, len(diagnostics))
	for _, d := range diagnostics {
		result = append(result, tooltipString(d.Message))
	}
	return result
}

func undeclaredAccounts(diagnostics []protocol.Diagnostic) []protocol.Diagnostic {
	var result []protocol.Diagnostic
	for _, d := range diagnostics {
		if fmt.Sprint(d.Code) == "UNDECLARED_ACCOUNT" {
			result = append(result, d)
		}
	}
	return result
}

// An `apply account` in front of the second include prefixes the accounts of the
// second occurrence only. hledger --strict rejects `b:assets:cash`, which is
// declared nowhere; the first occurrence reads clean.
func TestPublishDiagnostics_StrictAccountsAreCheckedAtEveryOccurrence(t *testing.T) {
	samples := map[string]struct{ food, cash string }{
		"ascii":    {"expenses:food", "assets:cash"},
		"cyrillic": {"расходы:еда", "активы:касса"},
		"cjk":      {"費用:食費", "資産:現金"},
	}
	for name, accounts := range samples {
		for variant, newline := range map[string]string{"lf": "\n", "crlf": "\r\n"} {
			t.Run(name+"/"+variant, func(t *testing.T) {
				dir := t.TempDir()
				writeJournals(t, dir, map[string]string{
					"main.journal": "account a:" + accounts.food + "\naccount a:" + accounts.cash + "\naccount b:" + accounts.food + "\n\n" +
						"apply account a\ninclude shared.journal\nend apply account\n\n" +
						"apply account b\ninclude shared.journal\nend apply account\n",
				})
				shared := strings.Join([]string{"2024-01-01 lunch", "    " + accounts.food + "   10 EUR", "    " + accounts.cash, ""}, newline)
				writeJournals(t, dir, map[string]string{"shared.journal": shared})

				undeclared := undeclaredAccounts(publishedFor(t, dir, shared, strict))
				require.Len(t, undeclared, 1, "messages: %v", messages(undeclared))
				message := tooltipString(undeclared[0].Message)
				assert.Contains(t, message, "b:"+accounts.cash)
				assert.Contains(t, message, "(when included from main.journal:10)")
				assert.Equal(t, uint32(2), undeclared[0].Range.Start.Line)
			})
		}
	}
}

// An alias declared between the two includes renames accounts at the second one.
func TestPublishDiagnostics_AliasBetweenIncludesIsCheckedAtEveryOccurrence(t *testing.T) {
	dir := t.TempDir()
	shared := "2024-01-01 x\n    expenses:food   10 EUR\n    assets:cash\n"
	writeJournals(t, dir, map[string]string{
		"main.journal":   "account expenses:food\naccount assets:cash\n\ninclude shared.journal\n\nalias expenses:food = personal:food\ninclude shared.journal\n",
		"shared.journal": shared,
	})

	undeclared := undeclaredAccounts(publishedFor(t, dir, shared, strict))
	require.Len(t, undeclared, 1, "messages: %v", messages(undeclared))
	message := tooltipString(undeclared[0].Message)
	assert.Contains(t, message, "personal:food")
	assert.Contains(t, message, "(when included from main.journal:7)")
}

// The workspace roots are separate trees; a context in one of them is checked
// too.
func TestPublishDiagnostics_OccurrencesOfEveryWorkspaceRootAreChecked(t *testing.T) {
	dir := t.TempDir()
	shared := "2024-01-01 x\n    expenses:food   10 EUR\n    assets:cash\n"
	writeJournals(t, dir, map[string]string{
		"b-root.journal": "account expenses:food\naccount assets:cash\ninclude shared.journal\n",
		"z-root.journal": "account expenses:food\naccount assets:cash\napply account z\ninclude shared.journal\nend apply account\n",
		"shared.journal": shared,
	})

	undeclared := undeclaredAccounts(publishedFor(t, dir, shared, strict))
	require.NotEmpty(t, undeclared)
	for _, d := range undeclared {
		message := tooltipString(d.Message)
		assert.Contains(t, message, "z:")
		assert.Contains(t, message, "(when included from z-root.journal:4)")
	}
}

// A problem every occurrence has is reported once and without a qualifier.
func TestPublishDiagnostics_ProblemOfEveryOccurrenceIsReportedOnce(t *testing.T) {
	dir := t.TempDir()
	shared := "2024-01-01 x\n    expenses:food   10 EUR\n    assets:cash\n"
	writeJournals(t, dir, map[string]string{
		"main.journal":   "include shared.journal\ninclude shared.journal\n",
		"shared.journal": shared,
	})
	// The two includes read the file the same way, so they collapse; a conflicting
	// mark keeps them apart without changing the account names.
	writeJournals(t, dir, map[string]string{
		"main.journal": "decimal-mark .\ninclude shared.journal\ndecimal-mark ,\ninclude shared.journal\n",
	})

	undeclared := undeclaredAccounts(publishedFor(t, dir, shared, strict))
	require.Len(t, undeclared, 2, "one per undeclared account, not per occurrence: %v", messages(undeclared))
	for _, d := range undeclared {
		assert.NotContains(t, tooltipString(d.Message), "when included from")
	}
}

// Nothing is added when every occurrence reads the file as clean.
func TestPublishDiagnostics_CleanOccurrencesStaySilent(t *testing.T) {
	dir := t.TempDir()
	shared := "2024-01-01 x\n    expenses:food   10 EUR\n    assets:cash\n"
	writeJournals(t, dir, map[string]string{
		"main.journal": "account a:expenses:food\naccount a:assets:cash\naccount b:expenses:food\naccount b:assets:cash\n" +
			"apply account a\ninclude shared.journal\nend apply account\napply account b\ninclude shared.journal\nend apply account\n",
		"shared.journal": shared,
	})

	assert.Empty(t, undeclaredAccounts(publishedFor(t, dir, shared, strict)))
}

// A `commodity 1000,00 EUR` is read under the decimal mark of the occurrence. The
// error of the occurrence that rejects it must survive opening the file itself.
func TestPublishDiagnostics_ParseErrorOfLaterOccurrenceSurvivesOpeningTheFile(t *testing.T) {
	dir := t.TempDir()
	shared := "commodity 1000,00 EUR\n\n2024-01-01 x\n    expenses:food   10 EUR\n    assets:cash\n"
	writeJournals(t, dir, map[string]string{
		"main.journal":   "include eu.journal\ninclude us.journal\n",
		"eu.journal":     "decimal-mark ,\ninclude shared.journal\n",
		"us.journal":     "decimal-mark .\ninclude shared.journal\n",
		"shared.journal": shared,
	})

	published := publishedFor(t, dir, shared, nil)
	var errorsFound []protocol.Diagnostic
	for _, d := range published {
		if d.Severity == protocol.DiagnosticSeverityError {
			errorsFound = append(errorsFound, d)
		}
	}
	require.Len(t, errorsFound, 1, "messages: %v", messages(published))
	assert.Contains(t, tooltipString(errorsFound[0].Message), "(when included from us.journal:2)")
	assert.Equal(t, uint32(0), errorsFound[0].Range.Start.Line)
}

// publishedSets returns, in order, the message sets published for docURI so far.
func publishedSets(ts *testServer, docURI uri.URI) [][]string {
	ts.client.mu.Lock()
	defer ts.client.mu.Unlock()
	var sets [][]string
	for _, params := range ts.client.diagnostics {
		if params.URI == docURI {
			sets = append(sets, messages(params.Diagnostics))
		}
	}
	return sets
}

func lastSet(ts *testServer, docURI uri.URI) []string {
	sets := publishedSets(ts, docURI)
	if len(sets) == 0 {
		return nil
	}
	return sets[len(sets)-1]
}

// Another workspace root is its own include tree with its own declarations: a
// root that declares nothing makes the accounts of the shared file undeclared
// there, whichever root the server meets first.
func TestPublishDiagnostics_OccurrenceOfAnotherRootUsesItsOwnDeclarations(t *testing.T) {
	shared := "2024-01-01 x\n    expenses:food   10 EUR\n    assets:cash\n"
	declares := "account expenses:food\naccount assets:cash\ninclude shared.journal\n"
	plain := "include shared.journal\n"

	tests := []struct {
		name, aRoot, zRoot, label string
	}{
		{"declaring root first", declares, plain, "(when included from z-root.journal:1)"},
		{"declaring root last", plain, declares, "(when included from a-root.journal:1)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeJournals(t, dir, map[string]string{"a-root.journal": tt.aRoot, "z-root.journal": tt.zRoot, "shared.journal": shared})

			undeclared := undeclaredAccounts(publishedFor(t, dir, shared, strict))
			require.Len(t, undeclared, 2, "messages: %v", messages(undeclared))
			for _, d := range undeclared {
				assert.Contains(t, fmt.Sprint(d.Message), tt.label)
			}
		})
	}
}

// An edit to the including file changes what the included file's diagnostics
// are, so the open included file is published again.
func TestPublishDiagnostics_EditOfIncludingFileRepublishesOpenIncludedFile(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")
	shared := "2024-01-01 lunch\n    expenses:food   10 EUR\n    assets:cash\n"
	declared := "account a:expenses:food\naccount a:assets:cash\naccount b:expenses:food\n\n"
	both := declared + "apply account a\ninclude shared.journal\nend apply account\n\napply account b\ninclude shared.journal\nend apply account\n"
	fixed := "account b:assets:cash\n" + both
	oneInclude := declared + "apply account a\ninclude shared.journal\nend apply account\n"

	for name, edited := range map[string]string{"declaration added": fixed, "second include removed": oneInclude} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeJournals(t, dir, map[string]string{"main.journal": both, "shared.journal": shared})
			ts := initWorkspaceTestServer(t, dir)
			strict(ts)
			mainURI, sharedURI := uri.File(filepath.Join(dir, "main.journal")), uri.File(filepath.Join(dir, "shared.journal"))
			require.NoError(t, ts.openDocument(mainURI, both))
			require.NoError(t, ts.openDocument(sharedURI, shared))
			require.Eventually(t, func() bool { return len(lastSet(ts, sharedURI)) > 0 }, 2*time.Second, 10*time.Millisecond,
				"shared.journal reports the undeclared account of the second include")

			_, err := ts.replaceAndWait(mainURI, edited)
			require.NoError(t, err)
			require.Eventually(t, func() bool { return len(lastSet(ts, sharedURI)) == 0 }, 2*time.Second, 10*time.Millisecond,
				"shared.journal still shows %v", lastSet(ts, sharedURI))
		})
	}
}

// What is published for a file does not depend on which file was opened last.
func TestPublishDiagnostics_IncludedFileSetDoesNotDependOnWhoPublishedLast(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")
	shared := "; пример 費用\ncommodity 1000,00 費用 ; коммент\n\n2024-01-01 обед\n    расходы:еда   10 費用\n    активы:касса\n"
	mainText := "include eu.journal\ninclude us.journal\n"

	dir := t.TempDir()
	writeJournals(t, dir, map[string]string{
		"main.journal": mainText, "eu.journal": "decimal-mark ,\ninclude shared.journal\n",
		"us.journal": "decimal-mark .\ninclude shared.journal\n", "shared.journal": shared,
	})
	ts := initWorkspaceTestServer(t, dir)
	strict(ts)
	mainURI, sharedURI := uri.File(filepath.Join(dir, "main.journal")), uri.File(filepath.Join(dir, "shared.journal"))

	require.NoError(t, ts.openDocument(sharedURI, shared))
	waitForDocument(t, ts.client, sharedURI)
	want := lastSet(ts, sharedURI)
	require.NotEmpty(t, want)
	assert.Contains(t, strings.Join(want, "\n"), "when included from us.journal:2")
	seen := len(publishedSets(ts, sharedURI))

	require.NoError(t, ts.openDocument(mainURI, mainText))
	waitForDocument(t, ts.client, mainURI)
	time.Sleep(150 * time.Millisecond)

	for _, set := range publishedSets(ts, sharedURI)[seen:] {
		assert.ElementsMatch(t, want, set, "opening the including file replaced the included file's set")
	}
}

// An assertion that fails only in the journal of one root is published for the
// open included file, naming that root; the other root reads it clean.
func TestPublishDiagnostics_AssertionOfAnotherRootReachesOpenIncludedFile(t *testing.T) {
	shared := "2024-01-02 x\n    expenses:food   10 EUR\n    assets:cash   -10 EUR = -10 EUR\n"
	failing := "2024-01-01 open\n    assets:cash   100 EUR\n    equity\n\ninclude shared.journal\n"
	clean := "include shared.journal\n"

	tests := []struct{ name, aRoot, zRoot, label string }{
		{"failing root last", clean, failing, "(when included from z-root.journal:5)"},
		{"failing root first", failing, clean, "(when included from a-root.journal:5)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeJournals(t, dir, map[string]string{"a-root.journal": tt.aRoot, "z-root.journal": tt.zRoot, "shared.journal": shared})

			published := publishedFor(t, dir, shared, nil)
			var failed []protocol.Diagnostic
			for _, d := range published {
				if fmt.Sprint(d.Code) == "BALANCE_ASSERTION_FAILED" {
					failed = append(failed, d)
				}
			}
			require.Len(t, failed, 1, "messages: %v", messages(published))
			assert.Contains(t, fmt.Sprint(failed[0].Message), tt.label)
		})
	}
}

// A file that had problems from this root and no longer has any is cleared.
func TestPublishDiagnostics_IncludedFileThatBecameCleanIsCleared(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")
	shared := "2024-01-02 x\n    expenses:food   10 EUR\n    assets:cash   -10 EUR = -10 EUR\n"
	failing := "2024-01-01 open\n    assets:cash   100 EUR\n    equity\n\ninclude shared.journal\n"
	fixed := "2024-01-01 open\n    assets:cash   0 EUR\n    equity\n\ninclude shared.journal\n"
	removed := "2024-01-01 open\n    assets:cash   100 EUR\n    equity\n"

	for name, edited := range map[string]string{"assertion fixed": fixed, "include removed": removed} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeJournals(t, dir, map[string]string{"main.journal": failing, "shared.journal": shared})
			ts := initWorkspaceTestServer(t, dir)
			mainURI, sharedURI := uri.File(filepath.Join(dir, "main.journal")), uri.File(filepath.Join(dir, "shared.journal"))

			require.NoError(t, ts.openDocument(mainURI, failing))
			require.Eventually(t, func() bool { return len(lastSet(ts, sharedURI)) == 1 }, 2*time.Second, 10*time.Millisecond)

			_, err := ts.replaceAndWait(mainURI, edited)
			require.NoError(t, err)
			require.Eventually(t, func() bool {
				sets := publishedSets(ts, sharedURI)
				return len(sets) > 0 && len(sets[len(sets)-1]) == 0
			}, 2*time.Second, 10*time.Millisecond, "shared.journal still shows %v", lastSet(ts, sharedURI))
		})
	}
}

func TestIncludeLabel_NamesTheDirectiveAndNothingForTheRoot(t *testing.T) {
	dir := t.TempDir()
	writeJournals(t, dir, map[string]string{"main.journal": "\ninclude child.journal\n", "child.journal": "; child\n"})
	ts, _ := formatServer(t, dir, filepath.Join(dir, "child.journal"), "; child\n")

	trees := ts.workspace.GetIncludeTreesForFile(filepath.Join(dir, "child.journal"))
	require.Len(t, trees, 1)
	root := trees[0].Resolved.Occurrence(1)
	require.NotNil(t, root)
	assert.Empty(t, includeLabel(*root), "the root of a tree has no include directive")

	occurrences := trees[0].Resolved.OccurrencesForPath(filepath.Join(dir, "child.journal"))
	require.Len(t, occurrences, 1)
	assert.Equal(t, "main.journal:2", includeLabel(occurrences[0]))
}

// An edit of an open included file that nobody saved is part of the journal the
// root is checked against, as it is for hledger reading the file.
func TestPublishDiagnostics_RootSeesUnsavedEditOfOpenIncludedFile(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")
	mainText := "include shared.journal\n\n2024-12-31 check\n    assets:cash   0 EUR = 100 EUR\n    equity\n"
	shared := "2024-01-01 seed\n    assets:cash   100 EUR\n    equity\n"
	edited := "2024-01-01 seed\n    assets:cash   50 EUR\n    equity\n"

	dir := t.TempDir()
	writeJournals(t, dir, map[string]string{"main.journal": mainText, "shared.journal": shared})
	ts := initWorkspaceTestServer(t, dir)
	mainURI, sharedURI := uri.File(filepath.Join(dir, "main.journal")), uri.File(filepath.Join(dir, "shared.journal"))
	require.NoError(t, ts.openDocument(mainURI, mainText))
	require.NoError(t, ts.openDocument(sharedURI, shared))
	require.Eventually(t, func() bool { return len(lastSet(ts, mainURI)) == 0 && len(publishedSets(ts, mainURI)) > 0 }, 2*time.Second, 10*time.Millisecond)

	_, err := ts.replaceAndWait(sharedURI, edited)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(lastSet(ts, mainURI)) == 1 }, 2*time.Second, 10*time.Millisecond,
		"main.journal shows %v", lastSet(ts, mainURI))
	assert.Contains(t, lastSet(ts, mainURI)[0], "asserted 100 EUR, calculated 50 EUR")
}

// A closed file keeps the problem another root still has when the first root is
// fixed: its set is computed from all the trees that contain it.
func TestPublishDiagnostics_ClosedFileKeepsTheProblemOfTheOtherRoot(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")
	shared := "2024-01-02 x\n    expenses:food   10 EUR\n    assets:cash   -10 EUR = -10 EUR\n"
	root := func(opening string) string {
		return "2024-01-01 open\n    assets:cash   " + opening + " EUR\n    equity\n\ninclude shared.journal\n"
	}
	dir := t.TempDir()
	writeJournals(t, dir, map[string]string{"a-root.journal": root("100"), "b-root.journal": root("50"), "shared.journal": shared})
	ts := initWorkspaceTestServer(t, dir)
	aURI, sharedURI := uri.File(filepath.Join(dir, "a-root.journal")), uri.File(filepath.Join(dir, "shared.journal"))

	require.NoError(t, ts.openDocument(aURI, root("100")))
	require.Eventually(t, func() bool { return len(lastSet(ts, sharedURI)) == 2 }, 2*time.Second, 10*time.Millisecond,
		"shared.journal shows %v", lastSet(ts, sharedURI))

	_, err := ts.replaceAndWait(aURI, root("0"))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		set := lastSet(ts, sharedURI)
		return len(set) == 1 && strings.Contains(set[0], "b-root.journal")
	}, 2*time.Second, 10*time.Millisecond, "shared.journal shows %v", lastSet(ts, sharedURI))
}

// Without a workspace every publish loads the document on its own; the verdicts
// of the loads before it must not pile up.
func TestPublishDiagnostics_VerdictsDoNotAccumulateWithoutAWorkspace(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///lone.journal")
	text := func(n int) string {
		return fmt.Sprintf("2024-01-01 x\n    expenses:food   %d EUR\n    assets:cash\n", n)
	}
	require.NoError(t, ts.openDocument(docURI, text(0)))
	for n := 1; n <= 30; n++ {
		_, err := ts.replaceAndWait(docURI, text(n))
		require.NoError(t, err)
	}
	require.Eventually(t, func() bool { return len(publishedSets(ts, docURI)) >= 30 }, 2*time.Second, 10*time.Millisecond)

	ts.journalDiagMu.Lock()
	defer ts.journalDiagMu.Unlock()
	assert.LessOrEqual(t, len(ts.journalDiag), 1)
}

// Closing a file drops what the server remembers about its publishes.
func TestDidClose_ForgetsWhatWasPublishedForTheDocument(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///lone.journal")
	text := "2024-01-01 x\n    expenses:food   10 EUR\n    assets:cash   -5 EUR\n"
	_, err := ts.openAndWait(docURI, text)
	require.NoError(t, err)
	require.NotEmpty(t, lastSet(ts, docURI), "the unbalanced transaction is reported")

	require.NoError(t, ts.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{TextDocument: protocol.TextDocumentIdentifier{URI: docURI}}))

	assert.Empty(t, lastSet(ts, docURI))
	ts.publishedMu.Lock()
	defer ts.publishedMu.Unlock()
	assert.NotContains(t, ts.published, docURI)
	assert.NotContains(t, ts.affected, docURI)
}
