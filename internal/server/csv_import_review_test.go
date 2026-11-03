package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/uri"
)

// countingHledger is a 1.99.5 that refuses every conversion and logs each run.
func countingHledger(t *testing.T, dir string) (binary, log string) {
	t.Helper()
	binary = filepath.Join(dir, "bin", "hledger")
	log = filepath.Join(dir, "bin", "runs")
	require.NoError(t, os.MkdirAll(filepath.Dir(binary), 0o755))
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'hledger 1.99.5, linux-x86_64'; exit 0; fi\n" +
		"echo run >> " + log + "\necho 'hledger: Error: bad rules' >&2\nexit 1\n"
	require.NoError(t, os.WriteFile(binary, []byte(script), 0o700))
	return binary, log
}

func runCount(t *testing.T, log string) int {
	t.Helper()
	data, err := os.ReadFile(log)
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err)
	return strings.Count(string(data), "run\n")
}

// A tree with more imports than the conversion cache holds by count must
// settle: every rebuild asks for all of them, and evicting one would start its
// conversion again on every rebuild, so the journal would never be published.
func TestServerSettlesWithMoreImportsThanTheCacheHolds(t *testing.T) {
	dir := t.TempDir()
	const files = 20
	for i := range files {
		name := fmt.Sprintf("st/банк%02d.csv", i)
		writeWorkspaceFile(t, dir, name, "date,amount\n2024-01-02,1\n")
		writeWorkspaceFile(t, dir, name+".rules", "skip 1\nfields date, amount\naccount1 a\naccount2 b\n")
	}
	main := "include st/*.csv\n\n2024-01-05 check\n    a  0 USD = 20 USD\n    b\n"
	mainPath := writeWorkspaceFile(t, dir, "main.journal", main)
	binary, log := countingHledger(t, dir)
	ts := startWorkspace(t, dir, targetVersion2, binary)
	mainURI := uri.File(mainPath)

	require.NoError(t, ts.openDocument(mainURI, main))
	ts.eventuallyDiagnostics(t, mainURI, func(codes []string) bool { return containsPrefix(codes, "INCLUDE_IMPORT_ERROR") })

	settled := runCount(t, log)
	time.Sleep(time.Second)
	assert.Equal(t, settled, runCount(t, log), "conversions keep starting after every import is known")
	assert.LessOrEqual(t, settled, 2*files, "each import is converted once (two hledger runs)")
}

// slowPreview runs the preview binary after a pause, so a conversion is still
// pending while the server publishes.
func slowPreview(t *testing.T, dir, preview string) string {
	t.Helper()
	wrapper := filepath.Join(dir, "bin", "hledger")
	require.NoError(t, os.MkdirAll(filepath.Dir(wrapper), 0o755))
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then exec " + preview + " --version; fi\nsleep 1\nexec " + preview + " \"$@\"\n"
	require.NoError(t, os.WriteFile(wrapper, []byte(script), 0o700))
	return wrapper
}

// A file two trees share must not be judged through the tree that still waits
// for its CSV records, even when the publish comes from the other tree.
func TestPreviewOracle_ServerKeepsAPendingTreeOutOfASharedFile(t *testing.T) {
	preview := previewHledgerPath(t)
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "bank.csv", importedBankCSV)
	writeWorkspaceFile(t, dir, "bank.csv.rules", importedBankRules)
	common := writeWorkspaceFile(t, dir, "common.journal", "2024-01-05 check\n    assets:bank  $0 = $996.595\n    equity\n")
	aContent := "include common.journal\n\n2024-01-01 open\n    assets:bank  $996.595\n    equity\n"
	aPath := writeWorkspaceFile(t, dir, "a.journal", aContent)
	writeWorkspaceFile(t, dir, "b.journal", "include bank.csv\ninclude common.journal\n")
	ts := startWorkspace(t, dir, targetVersion2, slowPreview(t, dir, preview))

	require.NoError(t, ts.openDocument(uri.File(aPath), aContent))
	time.Sleep(4 * time.Second)

	for _, codes := range ts.publishedFor(uri.File(common)) {
		assert.False(t, containsPrefix(codes, "BALANCE_ASSERTION_FAILED"), "a pending tree judged the shared file: %v", codes)
	}
}

// An open journal outside every workspace tree is loaded on its own; when its
// conversion finishes it must be published like the trees the workspace knows.
func TestPreviewOracle_ServerPublishesAStandaloneJournalOnceItsImportIsDone(t *testing.T) {
	preview := previewHledgerPath(t)
	root := t.TempDir()
	writeWorkspaceFile(t, root, "other.journal", "2024-01-01 x\n    a  1 USD\n    b\n")
	outside := t.TempDir()
	writeWorkspaceFile(t, outside, "bank.csv", importedBankCSV)
	writeWorkspaceFile(t, outside, "bank.csv.rules", importedBankRules)
	mainPath := writeWorkspaceFile(t, outside, "main.journal", importingJournal)
	ts := startWorkspace(t, root, targetVersion2, slowPreview(t, root, preview))
	mainURI := uri.File(mainPath)

	require.NoError(t, ts.openDocument(mainURI, importingJournal))

	ts.eventuallyDiagnostics(t, mainURI, func([]string) bool { return true })
	codes, _ := ts.lastDiagnosticsFor(mainURI)
	assert.Empty(t, codes, "the CSV records satisfy the assertion")
}
