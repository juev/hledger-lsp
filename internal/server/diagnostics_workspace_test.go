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

func initWorkspaceTestServer(t *testing.T, tmpDir string) *testServer {
	t.Helper()
	ts := newTestServer()
	_, err := ts.Initialize(context.Background(), &protocol.InitializeParams{
		WorkspaceFoldersInitializeParams: protocol.WorkspaceFoldersInitializeParams{
			WorkspaceFolders: protocol.NewNullable([]protocol.WorkspaceFolder{{URI: uri.URI(fmt.Sprintf("file://%s", tmpDir)), Name: "test"}}),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, ts.workspace)
	require.NoError(t, ts.Initialized(context.Background(), &protocol.InitializedParams{}))
	return ts
}

func writeIncludeFixture(t *testing.T) (tmpDir, mainPath, childPath string) {
	t.Helper()
	tmpDir = t.TempDir()
	mainContent := "include child.journal\ninclude missing.journal\n\n2024-01-01 * x\n    expenses:a  $1\n    assets:cash\n"
	childContent := "2024-01-02 * y\n    expenses:b  $2\n    assets:cash\n"
	mainPath = filepath.Join(tmpDir, "main.journal")
	childPath = filepath.Join(tmpDir, "child.journal")
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o644))
	require.NoError(t, os.WriteFile(childPath, []byte(childContent), 0o644))
	return tmpDir, mainPath, childPath
}

// The root file takes the workspace fast path and surfaces its own tree's
// include errors (here: the missing.journal include).
func TestPublishDiagnostics_RootGetsTreeLoadErrors(t *testing.T) {
	tmpDir, mainPath, _ := writeIncludeFixture(t)
	ts := initWorkspaceTestServer(t, tmpDir)

	mainContent, err := os.ReadFile(mainPath)
	require.NoError(t, err)
	mainURI := uri.URI(fmt.Sprintf("file://%s", mainPath))

	diags, err := ts.openAndWait(mainURI, string(mainContent))
	require.NoError(t, err)

	foundMissing := false
	for _, d := range diags {
		if strings.Contains(tooltipString(d.Message), "cannot read included file") || strings.Contains(tooltipString(d.Message), "missing.journal") {
			foundMissing = true
			break
		}
	}
	assert.True(t, foundMissing, "root should surface its tree's missing-include error, got: %+v", diags)
}

func TestPublishDiagnostics_DidOpenUsesBufferForRootLoadErrors(t *testing.T) {
	tmpDir := t.TempDir()
	mainPath := filepath.Join(tmpDir, "main.journal")
	diskContent := "2024-01-01 * x\n    expenses:a  $1\n    assets:cash\n"
	require.NoError(t, os.WriteFile(mainPath, []byte(diskContent), 0o644))

	ts := initWorkspaceTestServer(t, tmpDir)
	mainURI := uri.URI(fmt.Sprintf("file://%s", mainPath))
	bufferContent := "include missing.journal\n\n" + diskContent

	diags, err := ts.openAndWait(mainURI, bufferContent)
	require.NoError(t, err)

	foundMissing := false
	for _, d := range diags {
		if strings.Contains(tooltipString(d.Message), "missing.journal") {
			foundMissing = true
			break
		}
	}
	assert.True(t, foundMissing, "root should use the DidOpen buffer for include errors, got: %+v", diags)
}

func TestPublishDiagnostics_RootFallsBackAfterIncludedFileDidOpen(t *testing.T) {
	tmpDir := t.TempDir()
	mainPath := filepath.Join(tmpDir, "main.journal")
	childPath := filepath.Join(tmpDir, "child.journal")
	diskContent := "include child.journal\n\n2024-01-01 * x\n    expenses:a  $1\n    assets:cash\n"
	childContent := "2024-01-02 * y\n    expenses:b  $2\n    assets:cash\n"
	require.NoError(t, os.WriteFile(mainPath, []byte(diskContent), 0o644))
	require.NoError(t, os.WriteFile(childPath, []byte(childContent), 0o644))

	ts := initWorkspaceTestServer(t, tmpDir)
	ts.diagDebounce = time.Hour
	mainURI := uri.URI(fmt.Sprintf("file://%s", mainPath))
	childURI := uri.URI(fmt.Sprintf("file://%s", childPath))
	bufferContent := "include missing.journal\n" + diskContent

	require.NoError(t, ts.openDocument(mainURI, bufferContent))
	defer ts.cancelDiagnostics(mainURI)
	require.NoError(t, ts.openDocument(childURI, childContent))
	defer ts.cancelDiagnostics(childURI)
	ts.publishDiagnostics(context.Background(), mainURI, bufferContent, 0)

	diagnostics := ts.client.getLastDiagnostics()
	require.NotNil(t, diagnostics)
	assert.Equal(t, mainURI, diagnostics.URI)

	foundMissing := false
	for _, diagnostic := range diagnostics.Diagnostics {
		if strings.Contains(tooltipString(diagnostic.Message), "missing.journal") {
			foundMissing = true
			break
		}
	}
	assert.True(t, foundMissing, "root should use the current buffer after an included file opens, got: %+v", diagnostics.Diagnostics)
}

// child.journal is included by main.journal, so it is NOT a root. It must not
// inherit main's tree errors (the missing.journal include): the root-only guard
// sends it through the LoadFromContent fallback, which scopes errors to child's
// own subtree.
func TestPublishDiagnostics_NonRootDoesNotInheritTreeErrors(t *testing.T) {
	tmpDir, _, childPath := writeIncludeFixture(t)
	ts := initWorkspaceTestServer(t, tmpDir)

	childContent, err := os.ReadFile(childPath)
	require.NoError(t, err)
	childURI := uri.URI(fmt.Sprintf("file://%s", childPath))

	diags, err := ts.openAndWait(childURI, string(childContent))
	require.NoError(t, err)

	for _, d := range diags {
		assert.NotContains(t, d.Message, "missing.journal",
			"non-root file must not inherit sibling include errors, got: %+v", diags)
		assert.NotContains(t, d.Message, "cannot read included file",
			"non-root file must not inherit sibling include errors, got: %+v", diags)
	}
}
