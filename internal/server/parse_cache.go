package server

import (
	"maps"
	"sync"

	"github.com/shopspring/decimal"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/include"
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
	content string
	// resolved is the include tree the content was read under. The tree decides
	// what an ambiguous number means, so it is part of the key: editing an
	// included file changes the parse of a document whose text did not move.
	resolved     *include.ResolvedJournal
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

	// running holds the running balances of this version under the include tree
	// they were replayed from. The workspace replaces the tree's pointer when any
	// file in it changes, so the pointer stands for the whole tree's content;
	// runningMu serialises the replay so concurrent requests share one.
	runningMu sync.Mutex
	running   *runningBalanceEntry

	// others holds the parse and analysis of the document under the include
	// contexts other than the primary one, keyed by tree and occurrence. The
	// keys carry the tree's pointer, so a changed tree never reuses an entry;
	// occurrenceReadings drops the entries no longer asked for.
	othersMu sync.Mutex
	others   map[readingKey]*otherReading
}

// readingKey identifies an occurrence within one resolved tree.
type readingKey struct {
	tree *include.ResolvedJournal
	id   include.OccurrenceID
}

// otherReading is the document read under a context other than the primary one.
type otherReading struct {
	journal   *ast.Journal
	parseErrs []parser.ParseError

	analysisMu       sync.Mutex
	analysis         *analyzer.AnalysisResult
	analysisExternal analyzer.ExternalDeclarations
}

// runningBalanceEntry is one replay together with the inputs that produced it.
// tree is nil for a document outside any include tree.
type runningBalanceEntry struct {
	tree     *include.ResolvedJournal
	id       include.OccurrenceID
	balances map[analyzer.PostingRef]map[string]decimal.Decimal
}

// cachedRunningBalances returns the running balances of the document's postings,
// replaying the journal only when this version has not been replayed under the
// same include tree and occurrence. A lone document (tree == nil) is replayed
// from journal. The result is shared between callers and must not be modified.
func (s *Server) cachedRunningBalances(docURI uri.URI, content string, tree *include.ResolvedJournal, id include.OccurrenceID) map[analyzer.PostingRef]map[string]decimal.Decimal {
	doc := s.cachedParse(docURI, content)

	doc.runningMu.Lock()
	defer doc.runningMu.Unlock()
	if r := doc.running; r != nil && r.tree == tree && r.id == id {
		return r.balances
	}
	var balances map[analyzer.PostingRef]map[string]decimal.Decimal
	if tree != nil {
		balances = analyzer.RunningBalances(tree.TransactionsWithSource(), id)
	} else {
		balances = analyzer.RunningBalances(journalTransactions(doc.journal), 0)
	}
	doc.running = &runningBalanceEntry{tree: tree, id: id, balances: balances}
	return balances
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
// document's cached content or include tree differs. The cache is keyed by URI
// and validated by identity, so a caller always receives a journal parsed from
// exactly the content it passed; invalidation is automatic on either change.
func (s *Server) cachedParse(docURI uri.URI, content string) *cachedDoc {
	resolved := s.getWorkspaceResolved(docURI)
	if v, ok := s.parseCache.Load(docURI); ok {
		if doc, ok := v.(*cachedDoc); ok && doc.content == content && doc.resolved == resolved {
			return doc
		}
	}
	journal, parseErrs := s.parseDocument(docURI, content, resolved)
	doc := &cachedDoc{content: content, resolved: resolved, journal: journal, parseErrs: parseErrs}
	s.parseCache.Store(docURI, doc)
	return doc
}

// parseDocument reads content the way hledger reads the file: with the include
// context of its first occurrence, the same occurrence
// commodityFormatsForDocument takes its formats from. Read without that context
// `15,000` is 15 even when an include declares `.` as the decimal mark, and every
// balance, hint and rewritten number built on it is wrong.
//
// A document outside any include tree has no context to apply. Its parse comes
// from the loader's content-keyed cache, which the workspace index reads too.
func (s *Server) parseDocument(docURI uri.URI, content string, resolved *include.ResolvedJournal) (*ast.Journal, []parser.ParseError) {
	if resolved != nil {
		occurrenceID := firstOccurrenceIDForPath(resolved, uriToPath(docURI))
		if journal, parseErrs, ok := resolved.ParseOccurrence(occurrenceID, content); ok {
			return journal, parseErrs
		}
	}
	return s.loader.ParseCached(content)
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
