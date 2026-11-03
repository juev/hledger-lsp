package csvimport

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/cli"
	"github.com/juev/hledger-lsp/internal/testutil"
)

// previewHledger returns the installed hledger when it can include CSV files,
// skipping otherwise. Run with the preview first on PATH:
//
//	PATH=/path/to/hledger-1.99.5:$PATH HLEDGER_ORACLE=1 go test ./internal/csvimport
func previewHledger(t *testing.T) string {
	t.Helper()
	path := testutil.RequireHledger(t)
	client := cli.NewClient(path, 30*time.Second)
	if !client.Available() || !atLeast(client.Version(), minimumVersion) {
		t.Skipf("hledger %q on PATH cannot include CSV files; put 1.99.5 or newer first on PATH", client.Version())
	}
	return path
}

// stableHledger returns the installed hledger when it is a 1.x release that
// cannot include CSV files, skipping otherwise.
func stableHledger(t *testing.T) string {
	t.Helper()
	path := testutil.RequireHledger(t)
	client := cli.NewClient(path, 30*time.Second)
	if !client.Available() || client.Version() == "" || atLeast(client.Version(), minimumVersion) {
		t.Skipf("hledger %q on PATH is not a 1.x release", client.Version())
	}
	return path
}

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// listing records every entry under dir with its size and modification time, so
// a test can show hledger wrote nothing beside the inputs.
func listing(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries := make(map[string]string)
	require.NoError(t, filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		require.NoError(t, err)
		entries[path] = fmt.Sprintf("%s %d %d", info.Mode(), info.Size(), info.ModTime().UnixNano())
		return nil
	}))
	return entries
}
