package server

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/formatter"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/parser"
)

// occurrenceReading is a document read the way one include context reads it.
// hledger reads a file once per inclusion, and the inclusions can differ in
// decimal mark, account prefix, alias and the commodity styles declared so far,
// so one buffer can mean different things at different occurrences.
type occurrenceReading struct {
	tree      *include.ResolvedJournal
	id        include.OccurrenceID
	journal   *ast.Journal
	parseErrs []parser.ParseError
	// formatsAt returns the commodity formats in force at a byte offset of the
	// document, wholeDocument for those at its end.
	formatsAt func(offset int) map[string]formatter.CommodityFormat
	// context is the parse context the occurrence inherited; hasContext is false
	// for a reading that has no occurrence (a document outside every tree).
	context    parser.Context
	hasContext bool
	// other is nil for the primary reading, which the document cache serves.
	other *otherReading
	// includedFrom lists the files that include the document under this context,
	// in workspace order. An entry is empty for a tree whose root is the document.
	includedFrom []occurrenceRef
}

// wholeDocument is the offset that stands for the end of the document.
const wholeDocument = 1 << 30

// occurrenceReadings returns one reading per distinct include context of the
// document, across every include tree of the workspace. The first is the reading
// all single-context features use. Occurrences whose parse contexts are equal
// read the buffer identically and collapse into one reading, so a document
// included once, or always under the same context, costs nothing extra.
func (s *Server) occurrenceReadings(docURI uri.URI, content string) []occurrenceReading {
	journal, parseErrs := s.cachedJournal(docURI, content)
	primary := occurrenceReading{
		journal:   journal,
		parseErrs: parseErrs,
		formatsAt: func(offset int) map[string]formatter.CommodityFormat {
			if offset >= wholeDocument {
				return s.commodityFormatsForDocument(docURI)
			}
			return s.commodityFormatsAt(docURI, journal, offset)
		},
	}
	path := uriToPath(docURI)

	if tree := s.getWorkspaceResolved(docURI); tree != nil {
		primary.tree, primary.id = tree, firstOccurrenceIDForPath(tree, path)
		if occurrence := tree.Occurrence(primary.id); occurrence != nil {
			primary.context, primary.hasContext = occurrence.InitialContext, true
			primary.includedFrom = []occurrenceRef{{tree, *occurrence}}
		}
	}
	readings := []occurrenceReading{primary}
	if s.workspace == nil || path == "" {
		return readings
	}

	doc := s.cachedParse(docURI, content)
	used := make(map[readingKey]bool)
	defer func() {
		doc.othersMu.Lock()
		for key := range doc.others {
			if !used[key] {
				delete(doc.others, key)
			}
		}
		doc.othersMu.Unlock()
	}()

	for _, tree := range s.workspace.GetIncludeTreesForFile(path) {
		if tree.Resolved == nil {
			continue
		}
		for _, occurrence := range tree.Resolved.OccurrencesForPath(path) {
			if tree.Resolved == primary.tree && occurrence.ID == primary.id {
				continue
			}
			if index := indexOfEqualReading(readings, tree.Resolved, occurrence.InitialContext); index >= 0 {
				readings[index].includedFrom = append(readings[index].includedFrom, occurrenceRef{tree.Resolved, occurrence})
				continue
			}
			key := readingKey{tree.Resolved, occurrence.ID}
			other, ok := s.otherReadingFor(doc, key, content)
			if !ok {
				continue
			}
			used[key] = true
			formatsAt := tree.Resolved.FormatsAtFunc(occurrence.ID)
			readings = append(readings, occurrenceReading{
				context:      occurrence.InitialContext,
				hasContext:   true,
				tree:         tree.Resolved,
				id:           occurrence.ID,
				journal:      other.journal,
				parseErrs:    other.parseErrs,
				other:        other,
				formatsAt:    formatsAt,
				includedFrom: []occurrenceRef{{tree.Resolved, occurrence}},
			})
		}
	}
	return readings
}

// otherReadingFor returns the cached parse of content under the occurrence,
// parsing it on first use.
func (s *Server) otherReadingFor(doc *cachedDoc, key readingKey, content string) (*otherReading, bool) {
	doc.othersMu.Lock()
	defer doc.othersMu.Unlock()
	if other, ok := doc.others[key]; ok {
		return other, true
	}
	journal, parseErrs, ok := key.tree.ParseOccurrence(key.id, content)
	if !ok {
		return nil, false
	}
	other := &otherReading{journal: journal, parseErrs: parseErrs}
	if doc.others == nil {
		doc.others = make(map[readingKey]*otherReading)
	}
	doc.others[key] = other
	return other, true
}

// occurrenceRef is one inclusion of a document. Its label costs file system
// lookups, so it is computed only for the messages that show it.
type occurrenceRef struct {
	tree       *include.ResolvedJournal
	occurrence include.JournalOccurrence
}

// labelsOf returns the distinct non-empty labels of the inclusions.
func labelsOf(refs []occurrenceRef) []string {
	var labels []string
	for _, ref := range refs {
		if label := occurrenceLabel(ref.tree, ref.occurrence); label != "" && !slices.Contains(labels, label) {
			labels = append(labels, label)
		}
	}
	return labels
}

// includeLabel names the include directive that created an occurrence, as
// `parent.journal:12`. The root of a tree has no directive and an empty label.
func includeLabel(occurrence include.JournalOccurrence) string {
	if occurrence.Via.ParentID == 0 {
		return ""
	}
	return fmt.Sprintf("%s:%d", filepath.Base(occurrence.Via.SourcePath), occurrence.Via.IncludeRange.Start.Line)
}

// occurrenceLabel is includeLabel made unambiguous: when another inclusion of
// the same file sits at the same directive (a file reached through two parents
// that both include it from line 1), the label also names as many of the
// parents, nearest first, as it takes to tell them apart: `mid.journal:1 via
// a.journal:1`.
func occurrenceLabel(tree *include.ResolvedJournal, occurrence include.JournalOccurrence) string {
	short := includeLabel(occurrence)
	if short == "" {
		return short
	}

	var group []include.JournalOccurrence
	for _, other := range tree.OccurrencesForPath(occurrence.Path) {
		if includeLabel(other) == short {
			group = append(group, other)
		}
	}
	if len(group) < 2 {
		return short
	}

	ancestry := func(of include.JournalOccurrence) []string {
		var chain []string
		for parent := tree.Occurrence(of.Via.ParentID); parent != nil && parent.Via.ParentID != 0; parent = tree.Occurrence(parent.Via.ParentID) {
			chain = append(chain, includeLabel(*parent))
		}
		return chain
	}
	own := ancestry(occurrence)
	for depth := 1; depth <= len(own); depth++ {
		unique := true
		for _, other := range group {
			if other.ID == occurrence.ID {
				continue
			}
			otherChain := ancestry(other)
			if slices.Equal(own[:depth], otherChain[:min(depth, len(otherChain))]) {
				unique = false
				break
			}
		}
		if unique {
			return short + " via " + strings.Join(own[:depth], " via ")
		}
	}
	if len(own) == 0 {
		return short
	}
	return short + " via " + strings.Join(own, " via ")
}

// indexOfEqualReading finds the reading of the same include tree whose context
// equals candidate. Equal contexts parse alike, but the readings of different
// trees are checked against different declarations, so they stay apart.
func indexOfEqualReading(readings []occurrenceReading, tree *include.ResolvedJournal, candidate parser.Context) int {
	for index := range readings {
		if readings[index].hasContext && readings[index].tree == tree && readings[index].context.Equal(candidate) {
			return index
		}
	}
	return -1
}

// everywhere reports whether check holds for the document after edits at every
// reading. A document with one reading is the caller's own business: its edit is
// built from that reading's values, so there is nothing to compare it against.
func everywhere(content string, readings []occurrenceReading, edits []protocol.TextEdit, check func(edited *ast.Journal) bool) bool {
	for index := range readings {
		journal, ok := readings[index].parseEdited(content, edits)
		if !ok || !check(journal) {
			return false
		}
	}
	return true
}

// parseEdited reads content with edits applied the way this reading reads it.
func (r *occurrenceReading) parseEdited(content string, edits []protocol.TextEdit) (*ast.Journal, bool) {
	if r.tree == nil {
		return nil, false
	}
	edited := applyEdits(content, edits)
	journal, _, ok := r.tree.ParseOccurrence(r.id, edited)
	return journal, ok
}

// formatReadings returns the readings in the form the formatter takes.
func formatReadings(readings []occurrenceReading) []formatter.Reading {
	result := make([]formatter.Reading, len(readings))
	for index := range readings {
		result[index] = formatter.Reading{Journal: readings[index].journal, Formats: readings[index].formatsAt(wholeDocument)}
	}
	return result
}

// formatEdits is the document's formatting under every occurrence. The edits
// keep each occurrence's values; see formatter.FormatDocumentInReadings.
func (s *Server) formatEdits(docURI uri.URI, content string) []protocol.TextEdit {
	settings := s.getSettings()
	return formatter.FormatDocumentInReadings(formatReadings(s.occurrenceReadings(docURI, content)), content, formatterOptionsFrom(settings.Formatting))
}

// applyEdits applies non-overlapping edits to content, last first so earlier
// positions stay valid.
func applyEdits(content string, edits []protocol.TextEdit) string {
	ordered := slices.Clone(edits)
	slices.SortStableFunc(ordered, func(a, b protocol.TextEdit) int {
		if a.Range.Start.Line != b.Range.Start.Line {
			return cmp.Compare(b.Range.Start.Line, a.Range.Start.Line)
		}
		return cmp.Compare(b.Range.Start.Character, a.Range.Start.Character)
	})
	for _, edit := range ordered {
		content = applyChange(content, edit.Range, edit.NewText)
	}
	return content
}

// declarationsOf collects what an include tree declares, for the readings that
// belong to a tree other than the one the workspace answers declarations for.
func declarationsOf(tree *include.ResolvedJournal) analyzer.ExternalDeclarations {
	external := analyzer.ExternalDeclarations{Accounts: map[string]bool{}, Commodities: map[string]bool{}}
	for _, directive := range tree.AllDirectives() {
		switch d := directive.(type) {
		case ast.AccountDirective:
			external.Accounts[d.Account.Name] = true
		case ast.CommodityDirective:
			external.Commodities[d.Commodity.Symbol] = true
		}
	}
	return external
}

// analysisOf returns the analysis of a non-primary reading, reusing the last one
// when it was reached under the same declarations.
func (s *Server) analysisOf(other *otherReading, external analyzer.ExternalDeclarations) *analyzer.AnalysisResult {
	other.analysisMu.Lock()
	defer other.analysisMu.Unlock()
	if other.analysis != nil && sameDeclarations(other.analysisExternal, external) {
		return other.analysis
	}
	other.analysis = s.analyzeJournal(other.journal, external)
	other.analysisExternal = external
	return other.analysis
}

// mergeReadingDiagnostics joins the diagnostics of the readings. One that every
// reading reports is kept as it is; one that only some readings report says
// which includes it holds for, since the same line is fine under the others.
func mergeReadingDiagnostics(readings []occurrenceReading, perReading [][]protocol.Diagnostic) []protocol.Diagnostic {
	if len(readings) == 1 {
		return perReading[0]
	}
	labels := make([][]string, len(readings))
	for index := range readings {
		labels[index] = labelsOf(readings[index].includedFrom)
	}
	return mergeQualified(perReading, labels)
}

// mergeQualified joins per-source diagnostics, labels[i] naming the includes of
// source i. A diagnostic held by fewer than all sources gets a qualifier.
func mergeQualified(perSource [][]protocol.Diagnostic, labels [][]string) []protocol.Diagnostic {
	if len(perSource) == 1 {
		return perSource[0]
	}

	type key struct {
		rng     protocol.Range
		code    string
		message string
	}
	keyOf := func(d protocol.Diagnostic) key {
		return key{d.Range, fmt.Sprint(d.Code), fmt.Sprint(d.Message)}
	}

	holders := make(map[key][]int)
	var order []protocol.Diagnostic
	for index, diagnostics := range perSource {
		for _, d := range diagnostics {
			k := keyOf(d)
			if _, seen := holders[k]; !seen {
				order = append(order, d)
			}
			if h := holders[k]; len(h) == 0 || h[len(h)-1] != index {
				holders[k] = append(h, index)
			}
		}
	}

	merged := make([]protocol.Diagnostic, 0, len(order))
	for _, d := range order {
		held := holders[keyOf(d)]
		if len(held) < len(perSource) {
			d.Message = protocol.String(fmt.Sprint(d.Message) + " (" + occurrenceQualifier(labels, held) + ")")
		}
		merged = append(merged, d)
	}
	return merged
}

// occurrenceQualifier names the includes of the sources at the given indexes.
func occurrenceQualifier(labelsOf [][]string, held []int) string {
	const shown = 3
	var labels []string
	for _, index := range held {
		for _, label := range labelsOf[index] {
			if label != "" && !slices.Contains(labels, label) {
				labels = append(labels, label)
			}
		}
	}
	if len(labels) == 0 {
		return "when this file is the journal root"
	}
	if len(labels) > shown {
		return "when included from " + strings.Join(labels[:shown], ", ") + ", ..."
	}
	return "when included from " + strings.Join(labels, ", ")
}
