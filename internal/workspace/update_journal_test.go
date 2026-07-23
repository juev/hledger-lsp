package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/parser"
)

func TestWorkspace_UpdateFileWithJournal_MatchesUpdateFile(t *testing.T) {
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")

	tmpDir := t.TempDir()
	content := "2024-01-01 store\n    expenses:food  $20\n    assets:cash\n"
	mainPath := filepath.Join(tmpDir, "main.journal")
	require.NoError(t, os.WriteFile(mainPath, []byte(content), 0o644))

	edited := "2024-01-01 store\n    expenses:food  $20\n    assets:cash\n\n2024-01-02 extra\n    expenses:rent  $5\n    assets:bank\n"

	// Reference path: UpdateFile parses internally.
	wsRef := NewWorkspace(tmpDir, include.NewLoader())
	require.NoError(t, wsRef.Initialize())
	wsRef.UpdateFile(mainPath, edited)
	refAccounts := wsRef.IndexSnapshot().Accounts.All

	// UpdateFileWithJournal receives a pre-parsed journal and must build the
	// identical index.
	ws := NewWorkspace(tmpDir, include.NewLoader())
	require.NoError(t, ws.Initialize())
	journal, _ := parser.Parse(edited)
	ws.UpdateFileWithJournal(mainPath, edited, journal)

	snap := ws.IndexSnapshot()
	assert.Equal(t, refAccounts, snap.Accounts.All)
	assert.Contains(t, snap.Accounts.All, "expenses:rent")

	// The owning root's resolved journal must reflect the edited transaction.
	resolved := ws.GetResolvedForFile(mainPath)
	require.NotNil(t, resolved)
	found := false
	for _, tx := range resolved.AllTransactions() {
		if tx.Description == "extra" {
			found = true
			break
		}
	}
	assert.True(t, found, "resolved journal must include the edited transaction")
}
