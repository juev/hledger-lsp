package include

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/testutil"
	"github.com/juev/hledger-lsp/internal/textutil"
)

// TestOracle_ByteOrderMarkMatchesHledger compares where a UTF-8 byte order
// mark is accepted: hledger allows one at the start of a file, in the root and
// in an included journal, and rejects it anywhere else.
func TestOracle_ByteOrderMarkMatchesHledger(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	bom := textutil.BOM
	tx := "2024-01-01 x\n    a  5 RUB\n    b\n"
	layouts := map[string]map[string]string{
		"root":              {"main.journal": bom + tx},
		"root crlf":         {"main.journal": bom + "2024-01-01 x\r\n    a  5 RUB\r\n    b\r\n"},
		"root comment":      {"main.journal": bom + "; c\n" + tx},
		"included":          {"main.journal": "include t.journal\n", "t.journal": bom + tx},
		"root and included": {"main.journal": bom + "include t.journal\n", "t.journal": bom + tx},
		"after a line":      {"main.journal": "; c\n" + bom + tx},
		"mid file":          {"main.journal": tx + bom + "2024-01-02 y\n    a  1 RUB\n    b\n"},
		"included mid file": {"main.journal": "include t.journal\n", "t.journal": tx + bom + "2024-01-02 y\n    a  1 RUB\n    b\n"},
	}
	for name, files := range layouts {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			for file, content := range files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644))
			}
			root := filepath.Join(dir, "main.journal")

			want := testutil.HledgerReadFile(t, hledgerPath, root)
			assert.Equal(t, want, ourReadFile(root))
		})
	}
}
