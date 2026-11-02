package server

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

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
