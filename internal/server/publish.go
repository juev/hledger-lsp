package server

import (
	"context"
	"reflect"
	"sort"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/filetype"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/lsputil"
)

// The diagnostics published for a file are a function of the current state and
// of nothing else: the file's own analysis under each of its include contexts
// (when it is open), and the balance verdicts and load errors of every include
// tree that contains it. Whichever file's edit triggers a publish, every file it
// can affect is recomputed in full from that state, so the set a file shows does
// not depend on who published last.

// journalSource is one include tree a file belongs to, as the journal-level
// checks see it. The tree already holds the open buffers.
type journalSource struct {
	resolved *include.ResolvedJournal
	errors   []include.LoadError
	rootPath string
	// partial marks a document no tree owns: there may be a larger journal behind
	// it that is not open here, and the verdict is qualified accordingly.
	partial bool
}

// publishRound holds what one publish computes once and shares between files.
type publishRound struct {
	server     *Server
	docURI     uri.URI
	docPath    string
	docContent string
	settings   diagnosticsSettings

	// closed marks a round for a file that is not open: there is no buffer to
	// read it from, so it has no standalone view.
	closed         bool
	standalone     []journalSource
	standaloneDone bool
	byTree         map[*include.ResolvedJournal]map[uri.URI][]protocol.Diagnostic
}

// publishAffected recomputes and publishes the diagnostics of the open document
// docURI and of every file its include trees contain, or contained when it was
// last published.
func (s *Server) publishAffected(ctx context.Context, docURI uri.URI, content string, version uint32, revision uint64) {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if ctx.Err() != nil {
		return
	}

	round := &publishRound{
		server:     s,
		docURI:     docURI,
		docPath:    uriToPath(docURI),
		docContent: content,
		settings:   s.getSettings().Diagnostics,
		byTree:     make(map[*include.ResolvedJournal]map[uri.URI][]protocol.Diagnostic),
	}

	// Handlers that edit the current file (the declaration quick fixes) read it
	// as a journal of its own, which the include tree cannot offer for a file
	// that is only included. A root whose tree was built from this very buffer
	// serves them itself.
	if s.workspace != nil && !s.isImportSource(round.docPath) {
		if resolved, _ := s.workspace.ResolvedForRootContent(round.docPath, revision); resolved == nil {
			resolved, _ = s.loader.LoadFromContent(round.docPath, content)
			s.resolved.Store(docURI, resolved)
		}
	}

	s.publishCoverage(ctx, round, &version, false)
}

// publishTreesOf republishes everything the include trees of a file that is not
// open cover: after the file was closed and reverted to what is on disk
// (forget), or after it changed on disk. The file itself is published only when
// its set changed.
func (s *Server) publishTreesOf(ctx context.Context, docURI uri.URI, forget bool) {
	if s.client == nil || filetype.IsRules(string(docURI)) {
		s.forgetPublished(docURI)
		return
	}
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if ctx.Err() != nil {
		return
	}

	round := &publishRound{
		server:   s,
		docURI:   docURI,
		docPath:  uriToPath(docURI),
		settings: s.getSettings().Diagnostics,
		byTree:   make(map[*include.ResolvedJournal]map[uri.URI][]protocol.Diagnostic),
		closed:   true,
	}
	if !s.getSettings().Features.Diagnostics {
		s.publishDiagnosticSet(ctx, docURI, []protocol.Diagnostic{}, nil)
		s.forgetPublished(docURI)
		return
	}
	s.publishCoverage(ctx, round, nil, forget)
}

// publishCoverage publishes the full set of the round's document and of every
// file its trees contain now or contained at its previous publish. The
// coverage is replaced by the current one afterwards, or dropped when the
// document is gone, so it never holds more than the last publish saw.
func (s *Server) publishCoverage(ctx context.Context, round *publishRound, version *uint32, forget bool) {
	// The trees were just read, and an include may now import other files.
	defer s.scheduleImportWatchers()
	docURI := round.docURI
	docSources := round.sourcesFor(docURI)
	if pendingImport(docSources) {
		// importsReady publishes again once the conversion is done; until
		// then the client keeps what it last received.
		return
	}
	current := map[uri.URI]bool{docURI: true}
	for _, source := range docSources {
		for _, err := range source.errors {
			if err.Kind != include.ErrorParseError {
				s.warnOnce(docURI, err.ErrorCode()+":"+err.Path, "hledger-lsp: "+err.Message)
			}
		}
		for index := range source.resolved.Occurrences {
			current[pathToURI(source.resolved.Occurrences[index].Path)] = true
		}
	}

	s.publishedMu.Lock()
	targets := make(map[uri.URI]bool, len(current))
	for target := range current {
		targets[target] = true
	}
	for previous := range s.affected[docURI] {
		targets[previous] = true
	}
	s.publishedMu.Unlock()
	delete(targets, docURI)

	ordered := make([]uri.URI, 0, len(targets))
	for target := range targets {
		ordered = append(ordered, target)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })

	set := round.setFor(docURI)
	if ctx.Err() != nil {
		return
	}
	if version != nil || forget {
		// The analysed document always gets a publish, even when it has no
		// problems: the empty list is what clears previously reported
		// diagnostics, and it carries the version the client is waiting for. A
		// closed document gets one as well, so its panel is cleared.
		s.publishDiagnosticSet(ctx, docURI, set, version)
		s.rememberPublished(docURI, set)
	} else {
		s.publishIfChanged(ctx, docURI, set)
	}

	for _, target := range ordered {
		// A file this tree shares with another one that still waits for a
		// conversion would be judged without the missing records; it keeps what
		// it last showed until that tree is built again.
		if pendingImport(round.sourcesFor(target)) {
			continue
		}
		set := round.setFor(target)
		if ctx.Err() != nil {
			return
		}
		s.publishIfChanged(ctx, target, set)
	}

	s.publishedMu.Lock()
	if forget {
		delete(s.affected, docURI)
	} else {
		if s.affected == nil {
			s.affected = make(map[uri.URI]map[uri.URI]bool)
		}
		s.affected[docURI] = current
	}
	s.publishedMu.Unlock()
}

func (s *Server) publishIfChanged(ctx context.Context, target uri.URI, set []protocol.Diagnostic) {
	s.publishedMu.Lock()
	previous := s.published[target]
	s.publishedMu.Unlock()
	if len(set) == 0 && len(previous) == 0 {
		return
	}
	if reflect.DeepEqual(set, previous) {
		return
	}

	var version *uint32
	if _, open := s.documents.rope(target); open {
		current := s.documentVersion(target)
		version = &current
	}
	s.publishDiagnosticSet(ctx, target, set, version)
	s.rememberPublished(target, set)
}

func (s *Server) rememberPublished(target uri.URI, set []protocol.Diagnostic) {
	s.publishedMu.Lock()
	defer s.publishedMu.Unlock()
	if len(set) == 0 {
		delete(s.published, target)
		return
	}
	if s.published == nil {
		s.published = make(map[uri.URI][]protocol.Diagnostic)
	}
	s.published[target] = set
}

func (s *Server) forgetPublished(target uri.URI) {
	s.publishedMu.Lock()
	delete(s.published, target)
	delete(s.affected, target)
	s.publishedMu.Unlock()
}

// sourcesFor lists the include trees that contain the file. Balance rules and
// assertions depend on every file of the journal, so a file is evaluated in the
// context of each root that includes it instead of as a journal of its own;
// otherwise an assertion in a year file would not see the balances the
// journal's other files establish. The workspace keeps those trees fresh with
// the open buffers. A document no tree owns is read from its own content, and
// its verdict is qualified because a larger journal may lie behind it.
func (r *publishRound) sourcesFor(target uri.URI) []journalSource {
	s := r.server
	path := uriToPath(target)
	if s.workspace != nil {
		var sources []journalSource
		for _, tree := range s.workspace.GetIncludeTreesForFile(path) {
			if tree.Resolved != nil {
				sources = append(sources, journalSource{resolved: tree.Resolved, errors: tree.LoadErrors, rootPath: tree.RootPath})
			}
		}
		if len(sources) > 0 {
			return sources
		}
	}
	// A document no tree owns is read as a journal of its own, which an import
	// source is not.
	if target != r.docURI || r.closed || s.isImportSource(path) {
		return nil
	}
	if !r.standaloneDone {
		resolved, loadErrors := s.loader.LoadFromContent(path, r.docContent)
		s.resolved.Store(target, resolved)
		r.standalone = []journalSource{{resolved: resolved, errors: loadErrors, rootPath: path, partial: s.workspace != nil}}
		r.standaloneDone = true
	}
	return r.standalone
}

// treeDiagnostics returns the balance verdicts of one tree, grouped by file.
func (r *publishRound) treeDiagnostics(source journalSource) map[uri.URI][]protocol.Diagnostic {
	if cached, ok := r.byTree[source.resolved]; ok {
		return cached
	}
	s := r.server
	byURI := s.sourcedDiagnosticsByURI(pathToURI(source.rootPath), source.rootPath, s.journalDiagnostics(source.resolved, source.partial), r.settings)
	r.byTree[source.resolved] = byURI
	return byURI
}

// setFor computes the full set a file shows.
func (r *publishRound) setFor(target uri.URI) []protocol.Diagnostic {
	s := r.server
	path := uriToPath(target)
	sources := r.sourcesFor(target)

	lists := make([][]protocol.Diagnostic, 0, len(sources))
	labels := make([][]string, 0, len(sources))
	for _, source := range sources {
		lists = append(lists, r.treeDiagnostics(source)[target])
		var treeLabels []string
		for _, occurrence := range source.resolved.OccurrencesForCanonical(path) {
			treeLabels = append(treeLabels, includeLabel(occurrence))
		}
		labels = append(labels, treeLabels)
	}
	var set []protocol.Diagnostic
	switch len(lists) {
	case 0:
	case 1:
		set = append(set, lists[0]...)
	default:
		set = append(set, mergeQualified(lists, labels)...)
	}

	// Per-buffer diagnostics: parse errors, account declarations and date tags,
	// under every include context of the open buffer. Balance and assertion codes
	// come from the journal-level pass above, which evaluates the whole include
	// tree in hledger's date order.
	var mapper *lsputil.PositionMapper
	if text, open := s.documents.rope(target); open && !s.isImportSource(path) {
		content := r.docContent
		if target != r.docURI {
			content = text.Materialize()
		}
		set = append(set, s.analyzeWithJournalBalance(target, path, content, false)...)
		mapper = lsputil.NewPositionMapper(content)
	}

	// A load failure belongs to the file that contains the failing directive.
	for _, source := range sources {
		for _, err := range source.errors {
			errPath := err.SourcePath
			if errPath == "" {
				errPath = source.rootPath
			}
			if errPath != path {
				continue
			}
			diagnostic := loadErrorDiagnostic(err)
			if mapper == nil {
				mapper = s.mapperFor(target)
			}
			if mapper != nil {
				diagnostic.Range = astRangeToLSP(mapper, err.Range)
			}
			// The per-buffer analysis reads an open document under every include
			// context, so it normally reports the document's own syntax errors
			// already. One it does not report still belongs here: dropping it
			// would hide the error of an occurrence.
			if err.Kind == include.ErrorParseError && reportsParseError(set, diagnostic) {
				continue
			}
			set = append(set, diagnostic)
		}
	}
	return exactDedupDiagnostics(set)
}
