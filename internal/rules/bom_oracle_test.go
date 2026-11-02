package rules

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/testutil"
	"github.com/juev/hledger-lsp/internal/textutil"
)

// TestOracle_RulesByteOrderMarkMatchesHledger compares where a byte order mark
// is accepted in a rules file: one at the start of the root file, nowhere else.
func TestOracle_RulesByteOrderMarkMatchesHledger(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	bom := textutil.BOM
	body := "skip 1\nfields date,description,amount\naccount1 assets:cash\n"
	layouts := map[string]map[string]string{
		"root":                   {"main.rules": bom + body},
		"root CRLF":              {"main.rules": bom + "skip 1\r\nfields date,description,amount\r\naccount1 assets:cash\r\n"},
		"double in root":         {"main.rules": bom + bom + body},
		"root include first":     {"main.rules": bom + "include inc.rules\n", "inc.rules": body},
		"included with a mark":   {"main.rules": body + "include inc.rules\n", "inc.rules": bom + "if x\n  account2 y\n"},
		"root and included mark": {"main.rules": bom + body + "include inc.rules\n", "inc.rules": bom + "if x\n  account2 y\n"},
		"mark after a line":      {"main.rules": "skip 1\n" + bom + "fields date,description,amount\naccount1 assets:cash\n"},
	}
	for name, files := range layouts {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			for file, content := range files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644))
			}
			require.NoError(t, os.WriteFile(filepath.Join(dir, "data.csv"), []byte("date,description,amount\n2024-01-02,shop,5\n"), 0o644))
			root := filepath.Join(dir, "main.rules")

			cmd := exec.Command(hledgerPath, "-f", filepath.Join(dir, "data.csv"), "--rules", root, "print")
			accepted := cmd.Run() == nil

			result, errs := NewLoader().Load(root)
			ours := len(errs) == 0 && result != nil
			assert.Equal(t, accepted, ours, "hledger accepted=%v, loader errors=%v", accepted, errs)
		})
	}
}
