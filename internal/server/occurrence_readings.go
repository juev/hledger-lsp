package server

import (
	"cmp"
	"slices"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

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
	// includedFrom lists the files that include the document under this context,
	// in workspace order. An entry is empty for a tree whose root is the document.
	includedFrom []string
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
			primary.includedFrom = []string{occurrence.Via.SourcePath}
		}
	}
	readings := []occurrenceReading{primary}
	if s.workspace == nil || path == "" {
		return readings
	}

	for _, tree := range s.workspace.GetIncludeTreesForFile(path) {
		if tree.Resolved == nil {
			continue
		}
		for _, occurrence := range tree.Resolved.OccurrencesForPath(path) {
			if tree.Resolved == primary.tree && occurrence.ID == primary.id {
				continue
			}
			if index := indexOfEqualContext(readings, occurrence.InitialContext); index >= 0 {
				readings[index].includedFrom = append(readings[index].includedFrom, occurrence.Via.SourcePath)
				continue
			}
			journal, parseErrs, ok := tree.Resolved.ParseOccurrence(occurrence.ID, content)
			if !ok {
				continue
			}
			resolved, id := tree.Resolved, occurrence.ID
			readings = append(readings, occurrenceReading{
				context:    occurrence.InitialContext,
				hasContext: true,
				tree:       tree.Resolved,
				id:         occurrence.ID,
				journal:    journal,
				parseErrs:  parseErrs,
				formatsAt: func(offset int) map[string]formatter.CommodityFormat {
					return resolved.FormatsAt(id, offset)
				},
				includedFrom: []string{occurrence.Via.SourcePath},
			})
		}
	}
	return readings
}

func indexOfEqualContext(readings []occurrenceReading, candidate parser.Context) int {
	for index := range readings {
		if readings[index].hasContext && readings[index].context.Equal(candidate) {
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
