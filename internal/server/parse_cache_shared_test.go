package server

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/testutil"
)

// A version of a document is parsed once, not once per consumer. The server
// reaches the loader's content-keyed parse cache through its own loader, and
// the workspace reaches the same cache through the same instance, so the
// request-time parse and the workspace index share one journal.
//
// Identity is the observable: a second parse of the same content would return a
// different pointer.
func TestServer_ParseCacheIsShared(t *testing.T) {
	srv := NewServer()
	docURI := uri.URI("file:///shared.journal")
	content := testutil.GenerateJournal(50)
	srv.StoreDocument(docURI, content)

	shared, sharedErrs := srv.loader.ParseCached(content)
	require.NotNil(t, shared)
	require.Empty(t, sharedErrs)

	viaServer, viaServerErrs := srv.cachedJournal(docURI, content)
	require.Empty(t, viaServerErrs)
	require.Same(t, shared, viaServer,
		"the request-time parse must come from the loader's shared cache")

	viaWorkspaceShape, _ := srv.loader.ParseCached(content)
	require.Same(t, shared, viaWorkspaceShape,
		"the workspace index parses through the same loader instance and so the same cache")
}

// TestServer_ParseCacheFollowsEdits verifies the cache is keyed by content, not
// by document: a new version is a new entry, so a reader never receives a
// journal parsed from the previous text.
func TestServer_ParseCacheFollowsEdits(t *testing.T) {
	srv := NewServer()
	docURI := uri.URI("file:///edited.journal")
	before := testutil.GenerateJournal(50)
	after := before + "2024-12-31 added\n    expenses:new  $1\n    assets:cash\n"

	srv.StoreDocument(docURI, before)
	journalBefore, _ := srv.cachedJournal(docURI, before)

	journalAfter, _ := srv.cachedJournal(docURI, after)
	require.NotSame(t, journalBefore, journalAfter,
		"different content must not be answered from the previous version's parse")

	again, _ := srv.cachedJournal(docURI, after)
	require.Same(t, journalAfter, again, "the same version must stay cached")
}
