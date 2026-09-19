package server

import (
	"maps"
	"sync"

	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/parser"
)

// cachedDoc holds the content-derived parse results for one document version.
// Everything derived from the content alone is computed at most once, via the
// sync.Once fields, so the record can be shared across handler goroutines and
// the debounced diagnostics goroutine without coordination. The analysis is the
// exception: it also depends on the workspace's declarations, which can move
// while the content does not, so it is guarded by its own mutex and replaced
// when they change.
type cachedDoc struct {
	content      string
	journal      *ast.Journal
	parseErrs    []parser.ParseError
	balancesOnce sync.Once
	balances     analyzer.AccountBalances
	effectsOnce  sync.Once
	effects      []analyzer.PostingEffect

	// analysis holds the most recent verdict for this version, together with the
	// declarations it was reached under. The declarations come from the
	// workspace and change when an included file does, without this document's
	// content moving, so they belong in the key alongside the content.
	//
	// This is the one field that is not written once: a caller arriving with a
	// different declaration set replaces it. analysisMu guards the pair.
	analysisMu       sync.Mutex
	analysis         *analyzer.AnalysisResult
	analysisExternal analyzer.ExternalDeclarations
}

// cachedAnalysis returns the analysis of the document's journal, reusing the
// previous one when this version has been analysed under the same declarations.
// external must be the declarations the verdict should rest on.
//
// The analyzer reads its tolerance and account-check mode from settings, which
// the content-keyed cache cannot see, so setSettings clears parseCache.
func (s *Server) cachedAnalysis(docURI uri.URI, content string, external analyzer.ExternalDeclarations) *analyzer.AnalysisResult {
	doc := s.cachedParse(docURI, content)

	doc.analysisMu.Lock()
	cached, cachedExternal := doc.analysis, doc.analysisExternal
	doc.analysisMu.Unlock()
	if cached != nil && sameDeclarations(cachedExternal, external) {
		return cached
	}

	// Analyse outside the lock: two callers that arrive together with the same
	// declarations may both compute, which is wasted work but never wrong, and
	// neither blocks a reader for the length of an analysis.
	result := s.analyzeJournal(doc.journal, external)

	doc.analysisMu.Lock()
	doc.analysis, doc.analysisExternal = result, external
	doc.analysisMu.Unlock()
	return result
}

func (s *Server) analyzeJournal(journal *ast.Journal, external analyzer.ExternalDeclarations) *analyzer.AnalysisResult {
	if external.Accounts != nil || external.Commodities != nil {
		return s.analyzer.AnalyzeWithExternalDeclarations(journal, external)
	}
	return s.analyzer.Analyze(journal)
}

func sameDeclarations(a, b analyzer.ExternalDeclarations) bool {
	return maps.Equal(a.Accounts, b.Accounts) && maps.Equal(a.Commodities, b.Commodities)
}

func (s *Server) cachedPostingEffects(docURI uri.URI, content string) []analyzer.PostingEffect {
	doc := s.cachedParse(docURI, content)
	doc.effectsOnce.Do(func() {
		doc.effects = analyzer.CalculatePostingEffects(doc.journal)
	})
	return doc.effects
}

// cachedParse returns the parse result for content, reparsing only when the
// document's cached content differs. The cache is keyed by URI and validated by
// content identity, so a caller always receives a journal parsed from exactly
// the content it passed; invalidation is automatic on content change.
//
// The parse itself comes from the loader's content-keyed cache, which the
// workspace index reads too: a version of a document is parsed once, not once
// per consumer.
func (s *Server) cachedParse(docURI uri.URI, content string) *cachedDoc {
	if v, ok := s.parseCache.Load(docURI); ok {
		if doc, ok := v.(*cachedDoc); ok && doc.content == content {
			return doc
		}
	}
	journal, parseErrs := s.loader.ParseCached(content)
	doc := &cachedDoc{content: content, journal: journal, parseErrs: parseErrs}
	s.parseCache.Store(docURI, doc)
	return doc
}

// cachedJournal returns the cached parsed journal and parse errors for content.
func (s *Server) cachedJournal(docURI uri.URI, content string) (*ast.Journal, []parser.ParseError) {
	doc := s.cachedParse(docURI, content)
	return doc.journal, doc.parseErrs
}

// cachedBalances returns the account balances for content, computing them at
// most once per document version.
func (s *Server) cachedBalances(docURI uri.URI, content string) analyzer.AccountBalances {
	doc := s.cachedParse(docURI, content)
	doc.balancesOnce.Do(func() {
		doc.balances = analyzer.CalculateAccountBalances(doc.journal)
	})
	return doc.balances
}

// invalidateParseCache drops the cached parse for a document (called on DidClose).
func (s *Server) invalidateParseCache(docURI uri.URI) {
	s.parseCache.Delete(docURI)
}

// clearParseCache drops every cached document, together with the balances,
// effects and analysis memoized per version. Called when a setting the analyzer
// reads changes, since content-keyed entries would otherwise survive it.
func (s *Server) clearParseCache() {
	s.parseCache.Range(func(key, _ any) bool {
		s.parseCache.Delete(key)
		return true
	})
}
