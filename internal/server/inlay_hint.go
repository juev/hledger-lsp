package server

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/shopspring/decimal"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/formatter"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/lsputil"
)

// InlayHint returns amount inference and optional balance and cost facts for a
// journal document. Semantic facts are cached per exact document content; LSP
// ranges and settings are applied for each request.
func (s *Server) InlayHint(_ context.Context, params *protocol.InlayHintParams) ([]protocol.InlayHint, error) {
	if params == nil {
		return nil, nil
	}
	settings := s.getSettings()
	if !settings.Features.InlayHints {
		return nil, nil
	}
	content, ok := s.getJournalDoc(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	path := uriToPath(params.TextDocument.URI)
	readings := s.occurrenceReadings(params.TextDocument.URI, content)
	effectsByReading := make([]map[analyzer.PostingRef]analyzer.PostingEffect, len(readings))
	var refs []analyzer.PostingRef
	known := make(map[analyzer.PostingRef]bool)
	for index := range readings {
		var effects []analyzer.PostingEffect
		if readings[index].other == nil {
			effects = s.cachedPostingEffects(params.TextDocument.URI, content)
		} else {
			effects = readings[index].other.postingEffects()
		}
		effectsByReading[index] = make(map[analyzer.PostingRef]analyzer.PostingEffect, len(effects))
		for _, effect := range effects {
			ref := analyzer.PostingRef{TransactionIndex: effect.TransactionIndex, PostingIndex: effect.PostingIndex}
			effectsByReading[index][ref] = effect
			if !known[ref] {
				known[ref] = true
				refs = append(refs, ref)
			}
		}
	}
	if len(readings) > 1 {
		sort.Slice(refs, func(i, j int) bool {
			if refs[i].TransactionIndex != refs[j].TransactionIndex {
				return refs[i].TransactionIndex < refs[j].TransactionIndex
			}
			return refs[i].PostingIndex < refs[j].PostingIndex
		})
	}

	// Replaying the balances walks the whole include tree, so it runs only when
	// the result will be read, and is reused until the document or its tree moves.
	// A balance depends on where in the journal the file sits, so every inclusion
	// of the file has its own, not one per include context.
	var occurrences []balanceOccurrence
	if settings.InlayHints.RunningBalances {
		occurrences = s.balanceOccurrences(params.TextDocument.URI, content, path)
	}

	mapper := lsputil.NewPositionMapper(content)
	hints := make([]protocol.InlayHint, 0)
	for _, ref := range refs {
		posting, ok := postingOf(readings, effectsByReading, ref)
		if !ok {
			continue
		}
		// Commodity formats below are rebuilt per posting, so a posting the client
		// did not ask about must not reach them.
		if !postingTouchesRange(mapper, posting, params.Range) {
			continue
		}
		offset := posting.Range.Start.Offset
		if settings.InlayHints.InferredAmounts {
			var values []hintValue
			for index := range readings {
				if effect, ok := effectsByReading[index][ref]; ok && len(effect.InferredAmounts) > 0 {
					values = addHintValue(values, formatAmounts(effect.InferredAmounts, readings[index].formatsAt(offset)), readings[index].includedFrom)
				}
			}
			if len(values) > 0 {
				// Anchored past the end of the line rather than after the account
				// name: an inlay hint takes up room in the rendered line, so a hint
				// placed mid-line pushes the alignment padding — and the cursor
				// waiting at the amount column — to the right.
				accountEnd := mapper.ByteToLSP(posting.Account.Range.End.Offset)
				hints = appendHintInRange(hints, params.Range, valuedHint(mapper.LineEndPosition(int(accountEnd.Line)), "= ", values))
			}
		}
		if settings.InlayHints.CostExpansion && posting.Cost != nil {
			var values []hintValue
			for index := range readings {
				if effect, ok := effectsByReading[index][ref]; ok && len(effect.CostContribution) > 0 {
					values = addHintValue(values, formatAmounts(effect.CostContribution, readings[index].formatsAt(offset)), readings[index].includedFrom)
				}
			}
			if len(values) > 0 {
				hints = appendHintInRange(hints, params.Range, valuedHint(mapper.ByteToLSP(posting.Cost.Range.End.Offset), "cost: ", values))
			}
		}
		var values []hintValue
		for _, occurrence := range occurrences {
			if amounts := occurrence.balances[ref]; len(amounts) > 0 {
				formats := occurrence.formatsAt(offset)
				values = addHintValue(values, formatAmounts(amounts, formats), []string{occurrence.label})
			}
		}
		if len(values) > 0 {
			hints = appendHintInRange(hints, params.Range, valuedHint(mapper.ByteToLSP(posting.Range.End.Offset), "balance: ", values))
		}
	}
	return hints, nil
}

// postingOf returns the posting a ref names, from the first reading that has an
// effect for it. The readings parse the same text, so the posting sits at the
// same place in each.
func postingOf(readings []occurrenceReading, effects []map[analyzer.PostingRef]analyzer.PostingEffect, ref analyzer.PostingRef) (*ast.Posting, bool) {
	for index := range readings {
		if _, ok := effects[index][ref]; !ok {
			continue
		}
		journal := readings[index].journal
		if ref.TransactionIndex < len(journal.Transactions) && ref.PostingIndex < len(journal.Transactions[ref.TransactionIndex].Postings) {
			return &journal.Transactions[ref.TransactionIndex].Postings[ref.PostingIndex], true
		}
	}
	return nil, false
}

// balanceOccurrence is one inclusion of the document with its replayed balances.
type balanceOccurrence struct {
	label     string
	balances  map[analyzer.PostingRef]map[string]decimal.Decimal
	formatsAt func(offset int) map[string]formatter.CommodityFormat
}

// balanceOccurrences replays the journal once per inclusion of the document, in
// every include tree that owns it. A document that no tree owns has none, unless
// the server has no workspace at all, in which case it is replayed on its own.
func (s *Server) balanceOccurrences(docURI uri.URI, content, path string) []balanceOccurrence {
	used := make(map[readingKey]bool)
	defer func() { s.keepRunningBalances(docURI, content, used) }()

	if s.workspace == nil {
		used[readingKey{}] = true
		return []balanceOccurrence{{
			balances: s.cachedRunningBalances(docURI, content, nil, 0),
			formatsAt: func(offset int) map[string]formatter.CommodityFormat {
				journal, _ := s.cachedJournal(docURI, content)
				return localCommodityFormatsAt(journal, offset)
			},
		}}
	}

	var result []balanceOccurrence
	for _, tree := range s.workspace.GetIncludeTreesForFile(path) {
		if tree.Resolved == nil {
			continue
		}
		resolved := tree.Resolved
		for _, occurrence := range resolved.OccurrencesForCanonical(path) {
			id := occurrence.ID
			used[readingKey{tree: resolved, id: id}] = true
			result = append(result, balanceOccurrence{
				label:    occurrenceLabel(resolved, occurrence),
				balances: s.cachedRunningBalances(docURI, content, resolved, id),
				formatsAt: func(offset int) map[string]formatter.CommodityFormat {
					return resolved.FormatsAt(id, offset)
				},
			})
		}
	}
	return result
}

// hintValue is one value a hint can show, with the includes that give it.
type hintValue struct {
	text   string
	labels []string
}

// addHintValue records value for the includes labelled, merging equal values.
func addHintValue(values []hintValue, text string, labels []string) []hintValue {
	for index := range values {
		if values[index].text == text {
			for _, label := range labels {
				if label != "" && !slices.Contains(values[index].labels, label) {
					values[index].labels = append(values[index].labels, label)
				}
			}
			return values
		}
	}
	kept := make([]string, 0, len(labels))
	for _, label := range labels {
		if label != "" {
			kept = append(kept, label)
		}
	}
	return append(values, hintValue{text: text, labels: kept})
}

// maxHintValues is how many distinct values a hint shows before it elides the
// rest; the tooltip lists all of them.
const maxHintValues = 3

// valuedHint builds a hint with prefix and the values joined by " | ". A single
// value is the whole label and has no tooltip, which is how every hint of a file
// read one way looks. With several, the tooltip names the includes behind each.
func valuedHint(position protocol.Position, prefix string, values []hintValue) protocol.InlayHint {
	hint := protocol.InlayHint{
		Position:    position,
		Kind:        protocol.InlayHintKindType,
		PaddingLeft: boolPtr(true),
	}
	if len(values) == 1 {
		hint.Label = protocol.String(prefix + values[0].text)
		return hint
	}

	shown := make([]string, 0, maxHintValues+1)
	for index, value := range values {
		if index == maxHintValues {
			shown = append(shown, "…")
			break
		}
		shown = append(shown, value.text)
	}
	hint.Label = protocol.String(prefix + strings.Join(shown, " | "))

	lines := make([]string, 0, len(values))
	for _, value := range values {
		line := value.text
		if len(value.labels) > 0 {
			line += " (when included from " + strings.Join(value.labels, ", ") + ")"
		} else {
			line += " (when this file is the journal root)"
		}
		lines = append(lines, line)
	}
	hint.Tooltip = protocol.String(strings.Join(lines, "\n"))
	return hint
}

// journalTransactions presents a lone journal the way a resolved one would.
func journalTransactions(journal *ast.Journal) []include.SourcedTransaction {
	sourced := make([]include.SourcedTransaction, len(journal.Transactions))
	for i := range journal.Transactions {
		sourced[i] = include.SourcedTransaction{Index: i, Transaction: journal.Transactions[i]}
	}
	return sourced
}

func appendHintInRange(hints []protocol.InlayHint, requestRange protocol.Range, hint protocol.InlayHint) []protocol.InlayHint {
	if positionInProtocolRange(hint.Position, requestRange) {
		return append(hints, hint)
	}
	return hints
}

// postingTouchesRange reports whether any line of posting lies in rng. Every hint
// for a posting sits on one of its lines, so this only ever discards postings
// that could not produce a visible hint.
func postingTouchesRange(mapper *lsputil.PositionMapper, posting *ast.Posting, rng protocol.Range) bool {
	first := mapper.ByteToLSP(posting.Range.Start.Offset).Line
	last := mapper.ByteToLSP(posting.Range.End.Offset).Line
	return last >= rng.Start.Line && first <= rng.End.Line
}

func positionInProtocolRange(position protocol.Position, rng protocol.Range) bool {
	if position.Line < rng.Start.Line || position.Line > rng.End.Line {
		return false
	}
	if position.Line == rng.Start.Line && position.Character < rng.Start.Character {
		return false
	}
	return position.Line != rng.End.Line || position.Character < rng.End.Character
}

// commodityFormatsAt returns the formats in effect at offset in the document:
// those of its include tree, or its own directives when it has none.
func (s *Server) commodityFormatsAt(docURI uri.URI, journal *ast.Journal, offset int) map[string]formatter.CommodityFormat {
	if resolved := s.getWorkspaceResolved(docURI); resolved != nil {
		if id := firstOccurrenceIDForPath(resolved, uriToPath(docURI)); id != 0 {
			return resolved.FormatsAt(id, offset)
		}
	}
	return localCommodityFormatsAt(journal, offset)
}

func localCommodityFormatsAt(journal *ast.Journal, offset int) map[string]formatter.CommodityFormat {
	directives := make([]ast.Directive, 0, len(journal.Directives))
	for _, directive := range journal.Directives {
		if directive.GetRange().Start.Offset <= offset {
			directives = append(directives, directive)
		}
	}
	return formatter.ExtractCommodityFormats(directives)
}

func formatAmounts(amounts map[string]decimal.Decimal, formats map[string]formatter.CommodityFormat) string {
	commodities := make([]string, 0, len(amounts))
	for commodity := range amounts {
		commodities = append(commodities, commodity)
	}
	sort.Strings(commodities)
	formatted := make([]string, 0, len(commodities))
	for _, commodity := range commodities {
		formatted = append(formatted, formatter.FormatBalance(amounts[commodity], commodity, formats))
	}
	return strings.Join(formatted, ", ")
}
