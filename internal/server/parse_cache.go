package server

import (
	"sync"

	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/parser"
)

// cachedDoc holds the content-derived parse results for one document version.
// It is immutable once published into Server.parseCache: fields are never
// mutated after Store, and balances is computed at most once via balancesOnce.
// This makes it safe to share across handler goroutines and the debounced
// diagnostics goroutine.
type cachedDoc struct {
	content      string
	journal      *ast.Journal
	parseErrs    []parser.ParseError
	balancesOnce sync.Once
	balances     analyzer.AccountBalances
	effectsOnce  sync.Once
	effects      []analyzer.PostingEffect
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
func (s *Server) cachedParse(docURI uri.URI, content string) *cachedDoc {
	if v, ok := s.parseCache.Load(docURI); ok {
		if doc, ok := v.(*cachedDoc); ok && doc.content == content {
			return doc
		}
	}
	journal, parseErrs := parser.Parse(content)
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
