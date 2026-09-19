package server

import (
	"context"

	"github.com/shopspring/decimal"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/document"
	"github.com/juev/hledger-lsp/internal/filetype"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/lsputil"
)

// journalDiagEntry memoises the journal-level verdicts for one resolved tree.
type journalDiagEntry struct {
	resolved  *include.ResolvedJournal
	tolerance string
	partial   bool
	diags     []analyzer.SourcedDiagnostic
}

// journalDiagnostics returns hledger's balance and balance-assertion verdicts for
// the resolved include tree. The result is memoised per tree revision and
// tolerance, because the pass runs on every published diagnostic set.
func (s *Server) journalDiagnostics(resolved *include.ResolvedJournal, partialContext bool) []analyzer.SourcedDiagnostic {
	if resolved == nil {
		return nil
	}

	tolerance := decimal.NewFromFloat(s.getSettings().Diagnostics.BalanceTolerance)

	s.journalDiagMu.Lock()
	defer s.journalDiagMu.Unlock()

	if entry := s.journalDiag; entry != nil && entry.resolved == resolved &&
		entry.tolerance == tolerance.String() && entry.partial == partialContext {
		return entry.diags
	}

	diags := analyzer.CheckJournalBalance(resolved, tolerance, analyzer.JournalCheckOptions{PartialContext: partialContext})
	s.journalDiag = &journalDiagEntry{resolved: resolved, tolerance: tolerance.String(), partial: partialContext, diags: diags}
	return diags
}

// invalidateJournalDiagnostics drops the memoised journal-level verdicts, which
// a settings change (tolerance, account checks) makes stale.
func (s *Server) invalidateJournalDiagnostics() {
	s.journalDiagMu.Lock()
	s.journalDiag = nil
	s.journalDiagMu.Unlock()
}

// journalBalanceCodes are the codes produced by the journal-level pass. The
// per-buffer analysis must not duplicate them.
func isJournalBalanceCode(code string) bool {
	switch code {
	case analyzer.CodeUnbalanced, analyzer.CodeMultipleInferred, analyzer.CodeBalanceAssertionFailed:
		return true
	default:
		return false
	}
}

// sourcedDiagnosticsByURI converts journal-level diagnostics into protocol
// diagnostics grouped by the file they belong to. A diagnostic whose source file
// is not the analysed document is published to that file's own URI, so an
// included journal shows its errors where they are.
func (s *Server) sourcedDiagnosticsByURI(docURI uri.URI, path string, diags []analyzer.SourcedDiagnostic, settings diagnosticsSettings) map[uri.URI][]protocol.Diagnostic {
	byURI := make(map[uri.URI][]protocol.Diagnostic)

	// Building a position mapper costs a split of the whole file, so build at most
	// one per target file per publish instead of one per diagnostic.
	mappers := make(map[uri.URI]*lsputil.PositionMapper)
	mapperFor := func(target uri.URI) *lsputil.PositionMapper {
		if mapper, ok := mappers[target]; ok {
			return mapper
		}
		mapper := s.mapperFor(target)
		mappers[target] = mapper
		return mapper
	}
	for _, diag := range diags {
		if !s.shouldIncludeDiagnostic(diag.Code, settings) {
			continue
		}

		target := docURI
		if diag.Path != "" && diag.Path != path {
			target = pathToURI(diag.Path)
		}

		converted := protocol.Diagnostic{
			Severity: toProtocolSeverity(diag.Severity),
			Source:   protocol.NewOptional("hledger-lsp"),
			Message:  protocol.String(diag.Message),
			Code:     protocol.String(diag.Code),
		}
		// Ranges are expressed in the coordinates of the owning file, so use that
		// file's content for the byte-offset conversion when it is available.
		if mapper := mapperFor(target); mapper != nil {
			converted.Range = astRangeToLSP(mapper, diag.Range)
		} else {
			converted.Range = *astRangeToProtocol(diag.Range)
		}
		if len(diag.Data) > 0 {
			if raw, err := protocol.Marshal(diag.Data); err == nil {
				converted.Data = protocol.LSPAny(raw)
			}
		}

		byURI[target] = append(byURI[target], converted)
	}
	return byURI
}

// loadErrorDiagnostic converts an include/load failure into a diagnostic. The
// range is expressed in the file that contains the failing directive, which is
// how hledger reports it.
func loadErrorDiagnostic(err include.LoadError) protocol.Diagnostic {
	severity := protocol.DiagnosticSeverityError
	if err.Kind == include.ErrorNotJournal || err.Kind == include.ErrorFileTooLarge {
		severity = protocol.DiagnosticSeverityWarning
	}

	return protocol.Diagnostic{
		Range:    *astRangeToProtocol(err.Range),
		Severity: severity,
		Source:   protocol.NewOptional("hledger-lsp"),
		Message:  protocol.String(err.Message),
		Code:     protocol.String(err.ErrorCode()),
	}
}

// publishDiagnosticSet publishes one document's diagnostics. The version is set
// only when the caller tracks it (the analysed document itself), because
// fan-out to other files carries no version of its own.
func (s *Server) publishDiagnosticSet(ctx context.Context, docURI uri.URI, diagnostics []protocol.Diagnostic, version *uint32) {
	if s.client == nil {
		return
	}

	params := &protocol.PublishDiagnosticsParams{
		URI:         docURI,
		Diagnostics: diagnostics,
	}
	if version != nil {
		params.Version = protocol.NewOptional(int32(*version))
	}
	_ = s.client.PublishDiagnostics(ctx, params)
}

// republishDiagnostics recomputes diagnostics for every open journal. It runs
// after a settings change so the effect is visible without restarting the server.
func (s *Server) republishDiagnostics() {
	var docs []docRef
	s.documents.Range(func(docURI uri.URI, text *document.Text) bool {
		if !filetype.IsRules(string(docURI)) {
			docs = append(docs, docRef{uri: docURI, text: text})
		}
		return true
	})

	for _, doc := range docs {
		// The revision is read here; the rope is materialized later, inside
		// publishDiagnostics. An edit landing in between therefore leaves this
		// revision older than the content that gets analysed, and the tree built
		// from the newer revision will not match it — the pair can only
		// mismatch, which makes publishDiagnostics load the content directly,
		// never wrongly match.
		var revision uint64
		if s.workspace != nil {
			revision = s.workspace.ContentRevision(uriToPath(doc.uri))
		}
		version := s.documentVersion(doc.uri)
		go s.publishDiagnostics(context.Background(), doc.uri, doc.text, version, revision)
	}
}

// docRef pairs a document with its rope for the republish loop, which must not
// hold anything while the diagnostics run on their own goroutines.
type docRef struct {
	uri  uri.URI
	text *document.Text
}

// setDocumentVersion records the client's version for a document.
func (s *Server) setDocumentVersion(docURI uri.URI, version uint32) {
	s.docVersions.Store(docURI, version)
}

// documentVersion returns the last version the client reported, or 0 when the
// document is unknown.
func (s *Server) documentVersion(docURI uri.URI) uint32 {
	if value, ok := s.docVersions.Load(docURI); ok {
		if version, ok := value.(uint32); ok {
			return version
		}
	}
	return 0
}
