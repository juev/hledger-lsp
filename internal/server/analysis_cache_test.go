package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/analyzer"
)

func TestCachedAnalysis_ReusesResultPerVersion(t *testing.T) {
	srv := NewServer()
	docURI := uri.URI("file:///analysis.journal")

	first := srv.cachedAnalysis(docURI, sampleJournal, analyzer.ExternalDeclarations{})
	second := srv.cachedAnalysis(docURI, sampleJournal, analyzer.ExternalDeclarations{})
	require.NotNil(t, first)
	assert.Same(t, first, second, "the same version must reuse its analysis")

	changed := srv.cachedAnalysis(docURI, sampleJournal+"\n2024-01-16 other\n    expenses:y  $1\n    assets:cash\n", analyzer.ExternalDeclarations{})
	assert.NotSame(t, first, changed, "a new version must be analysed afresh")
}

// The workspace's declarations are part of the verdict: an included file
// changing on disk leaves this document's content alone but can add or remove a
// declared account. A cached analysis must not survive that.
func TestCachedAnalysis_DeclarationsArePartOfTheKey(t *testing.T) {
	srv := NewServer()
	docURI := uri.URI("file:///declarations.journal")

	without := srv.cachedAnalysis(docURI, sampleJournal, analyzer.ExternalDeclarations{})
	with := srv.cachedAnalysis(docURI, sampleJournal, analyzer.ExternalDeclarations{
		Accounts: map[string]bool{"expenses:food": true},
	})
	assert.NotSame(t, without, with,
		"a different declaration set must not be answered from the previous analysis")

	// And the cached entry still serves callers whose declarations match it.
	again := srv.cachedAnalysis(docURI, sampleJournal, analyzer.ExternalDeclarations{
		Accounts: map[string]bool{"expenses:food": true},
	})
	assert.Same(t, with, again)
}

// The analyzer keeps its tolerance and account-check mode in its own settings,
// which the content-keyed cache cannot see. Changing a setting must drop the
// cache rather than serve a verdict the new setting contradicts.
func TestSetSettings_ClearsAnalysisCache(t *testing.T) {
	srv := NewServer()
	docURI := uri.URI("file:///settings.journal")

	before := srv.cachedAnalysis(docURI, sampleJournal, analyzer.ExternalDeclarations{})
	require.NotNil(t, before)

	settings := srv.getSettings()
	settings.Diagnostics.BalanceTolerance += 1
	srv.setSettings(settings)

	after := srv.cachedAnalysis(docURI, sampleJournal, analyzer.ExternalDeclarations{})
	assert.NotSame(t, before, after,
		"a settings change must not be answered from an analysis made under the old ones")
}
