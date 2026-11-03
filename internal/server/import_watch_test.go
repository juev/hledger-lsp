package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// watchRecordingClient records the watcher registrations of the import sources.
type watchRecordingClient struct {
	*integrationMockClient
	mu       sync.Mutex
	watched  [][]string // the patterns of every registration of importWatcherID
	removals int
}

func (c *watchRecordingClient) RegisterCapability(_ context.Context, params *protocol.RegistrationParams) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, registration := range params.Registrations {
		if registration.ID != importWatcherID {
			continue
		}
		data, err := json.Marshal(registration.RegisterOptions)
		if err != nil {
			return err
		}
		var options struct {
			Watchers []struct {
				GlobPattern json.RawMessage `json:"globPattern"`
			} `json:"watchers"`
		}
		if err := json.Unmarshal(data, &options); err != nil {
			return err
		}
		var patterns []string
		for _, watcher := range options.Watchers {
			patterns = append(patterns, string(watcher.GlobPattern))
		}
		c.watched = append(c.watched, patterns)
	}
	return nil
}

func (c *watchRecordingClient) UnregisterCapability(_ context.Context, params *protocol.UnregistrationParams) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, removal := range params.Unregisterations {
		if removal.ID == importWatcherID {
			c.removals++
		}
	}
	return nil
}

func (c *watchRecordingClient) state() ([][]string, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]string(nil), c.watched...), c.removals
}

func startWatchingServer(t *testing.T, dir string, relative bool) (*testServer, *watchRecordingClient) {
	t.Helper()
	t.Setenv("LEDGER_FILE", "")
	t.Setenv("HLEDGER_JOURNAL", "")
	ts := newTestServer()
	client := &watchRecordingClient{integrationMockClient: ts.client}
	ts.SetClient(client)
	params := initParamsWithRootURI(uri.File(dir))
	params.Capabilities.Workspace = &protocol.WorkspaceClientCapabilities{
		DidChangeWatchedFiles: &protocol.DidChangeWatchedFilesClientCapabilities{
			DynamicRegistration:    boolPtr(true),
			RelativePatternSupport: boolPtr(relative),
		},
	}
	_, err := ts.Initialize(context.Background(), params)
	require.NoError(t, err)
	settings := ts.getSettings()
	settings.TargetVersion = targetVersion2
	settings.CLI.Path = filepath.Join(dir, "no-hledger")
	ts.setSettings(settings)
	require.NoError(t, ts.Initialized(context.Background(), &protocol.InitializedParams{}))
	return ts, client
}

func eventually(t *testing.T, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			require.FailNow(t, message)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A reader prefix can name a data file of any extension. Its import reads the
// file from disk, so the server must ask the client to watch it, even though no
// static pattern covers it, and stop once no include reads it any more.
func TestImportWatchers_FollowSourcesWithAnyExtension(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "данные.txt", "date,amount\n2024-01-02,1\n")
	writeWorkspaceFile(t, dir, "данные.txt.rules", "skip 1\nfields date, amount\naccount1 a\n")
	main := "include csv:данные.txt\r\n"
	mainPath := writeWorkspaceFile(t, dir, "main.journal", main)
	ts, client := startWatchingServer(t, dir, true)

	_, err := ts.openAndWait(uri.File(mainPath), strings.ReplaceAll(main, "\r\n", "\n"))
	require.NoError(t, err)

	eventually(t, func() bool { watched, _ := client.state(); return len(watched) > 0 }, "no watcher for the data file")
	watched, _ := client.state()
	require.Len(t, watched[len(watched)-1], 1, "the rules file is already watched by **/*.rules")
	pattern := watched[len(watched)-1][0]
	assert.Contains(t, pattern, `"pattern":"данные.txt"`)
	assert.Contains(t, pattern, string(uri.File(dir)))

	_, err = ts.replaceAndWait(uri.File(mainPath), "2024-01-01 x\n    a  1 USD\n    b\n")
	require.NoError(t, err)
	eventually(t, func() bool { _, removals := client.state(); return removals == 1 }, "the watcher outlived the include")
	watched, _ = client.state()
	assert.Len(t, watched, 1, "nothing is left to watch")
}

func TestImportWatchers_WithoutRelativePatternsWatchTheName(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "bank.dat", "date,amount\n2024-01-02,1\n")
	writeWorkspaceFile(t, dir, "bank.dat.rules", "skip 1\nfields date, amount\naccount1 a\n")
	main := "include ssv:bank.dat\n"
	mainPath := writeWorkspaceFile(t, dir, "main.journal", main)
	ts, client := startWatchingServer(t, dir, false)

	_, err := ts.openAndWait(uri.File(mainPath), main)
	require.NoError(t, err)

	eventually(t, func() bool { watched, _ := client.state(); return len(watched) > 0 }, "no watcher for the data file")
	watched, _ := client.state()
	assert.Equal(t, []string{`"**/bank.dat"`}, watched[len(watched)-1])
}

func TestEscapeGlob(t *testing.T) {
	assert.Equal(t, "a[*]b[?][[]c[]][{]d[}]", escapeGlob("a*b?[c]{d}"))
	assert.Equal(t, "выписка.txt", escapeGlob("выписка.txt"))
}

func TestCoveredByStaticWatchers(t *testing.T) {
	assert.True(t, coveredByStaticWatchers("/a/bank.csv"))
	assert.True(t, coveredByStaticWatchers("/a/bank.csv.rules"))
	assert.False(t, coveredByStaticWatchers("/a/bank.txt"))
	assert.False(t, coveredByStaticWatchers("/a/bank.CSV"))
}
