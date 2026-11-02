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
	primaryEffects := s.cachedPostingEffects(params.TextDocument.URI, content)

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
	emit := func(posting *ast.Posting, effectAt func(reading int) (analyzer.PostingEffect, bool), balanceAt func(occurrence int) map[string]decimal.Decimal) {
		offset := posting.Range.Start.Offset
		if settings.InlayHints.InferredAmounts {
			var values []hintValue
			for index := range readings {
				if effect, ok := effectAt(index); ok && len(effect.InferredAmounts) > 0 {
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
				if effect, ok := effectAt(index); ok && len(effect.CostContribution) > 0 {
					values = addHintValue(values, formatAmounts(effect.CostContribution, readings[index].formatsAt(offset)), readings[index].includedFrom)
				}
			}
			if len(values) > 0 {
				hints = appendHintInRange(hints, params.Range, valuedHint(mapper.ByteToLSP(posting.Cost.Range.End.Offset), "cost: ", values))
			}
		}
		var values []hintValue
		for index, occurrence := range occurrences {
			if amounts := balanceAt(index); len(amounts) > 0 {
				values = addHintValue(values, formatAmounts(amounts, occurrence.formatsAt(offset)), []occurrenceRef{occurrence.ref})
			}
		}
		if len(values) > 0 {
			hints = appendHintInRange(hints, params.Range, valuedHint(mapper.ByteToLSP(posting.Range.End.Offset), "balance: ", values))
		}
	}

	if len(readings) == 1 {
		// The document is read one way: walk its effects directly, and only the
		// postings the client asked about.
		for _, effect := range primaryEffects {
			posting := effectPosting(readings[0].journal, effect)
			// Commodity formats are rebuilt per posting, so a posting the client
			// did not ask about must not reach them.
			if posting == nil || !postingTouchesRange(mapper, posting, params.Range) {
				continue
			}
			ref := analyzer.PostingRef{TransactionIndex: effect.TransactionIndex, PostingIndex: effect.PostingIndex}
			emit(posting, func(int) (analyzer.PostingEffect, bool) { return effect, true },
				func(occurrence int) map[string]decimal.Decimal { return occurrences[occurrence].balances[ref] })
		}
		return hints, nil
	}

	// Several readings parse the same text, but a context can drop or keep a
	// posting that another does not, so the postings of different readings are
	// matched by where they are written, never by their position in the journal.
	// The balances of an inclusion are matched the same way.
	located := make([]map[int]locatedEffect, len(readings))
	var offsets []int
	known := make(map[int]*ast.Posting)
	for index := range readings {
		if readings[index].other == nil {
			located[index] = locateEffects(readings[index].journal, primaryEffects)
		} else {
			located[index] = readings[index].other.locatedEffects()
		}
		for offset, entry := range located[index] {
			if _, seen := known[offset]; !seen {
				known[offset] = entry.posting
				offsets = append(offsets, offset)
			}
		}
	}
	slices.Sort(offsets)
	balancesByOffset := make([]map[int]map[string]decimal.Decimal, len(occurrences))
	for index, occurrence := range occurrences {
		balancesByOffset[index] = s.balancesByOffset(params.TextDocument.URI, content, occurrence)
	}
	for _, offset := range offsets {
		posting := known[offset]
		if !postingTouchesRange(mapper, posting, params.Range) {
			continue
		}
		emit(posting, func(index int) (analyzer.PostingEffect, bool) {
			entry, ok := located[index][offset]
			return entry.effect, ok
		}, func(occurrence int) map[string]decimal.Decimal { return balancesByOffset[occurrence][offset] })
	}
	return hints, nil
}

// locatedEffect is a posting effect with the posting it belongs to.
type locatedEffect struct {
	posting *ast.Posting
	effect  analyzer.PostingEffect
}

// locateEffects keys the effects of a journal by the offset their posting starts at.
func locateEffects(journal *ast.Journal, effects []analyzer.PostingEffect) map[int]locatedEffect {
	located := make(map[int]locatedEffect, len(effects))
	for _, effect := range effects {
		if posting := effectPosting(journal, effect); posting != nil {
			located[posting.Range.Start.Offset] = locatedEffect{posting, effect}
		}
	}
	return located
}

// balancesByOffset keys the running balances of an inclusion by the offset their
// posting starts at in the inclusion's own journal, reusing the result until the
// document or its tree moves.
func (s *Server) balancesByOffset(docURI uri.URI, content string, occurrence balanceOccurrence) map[int]map[string]decimal.Decimal {
	doc := s.cachedParse(docURI, content)
	key := readingKey{tree: occurrence.ref.tree, id: occurrence.ref.occurrence.ID}

	doc.runningMu.Lock()
	defer doc.runningMu.Unlock()
	if byOffset, ok := doc.runningOffsets[key]; ok {
		return byOffset
	}
	byOffset := make(map[int]map[string]decimal.Decimal, len(occurrence.balances))
	journal := occurrence.ref.occurrence.Journal
	for ref, amounts := range occurrence.balances {
		if journal == nil || ref.TransactionIndex >= len(journal.Transactions) || ref.PostingIndex >= len(journal.Transactions[ref.TransactionIndex].Postings) {
			continue
		}
		byOffset[journal.Transactions[ref.TransactionIndex].Postings[ref.PostingIndex].Range.Start.Offset] = amounts
	}
	if doc.runningOffsets == nil {
		doc.runningOffsets = make(map[readingKey]map[int]map[string]decimal.Decimal)
	}
	doc.runningOffsets[key] = byOffset
	return byOffset
}

// effectPosting returns the posting an effect belongs to, or nil when the effect
// no longer matches the journal.
func effectPosting(journal *ast.Journal, effect analyzer.PostingEffect) *ast.Posting {
	if effect.TransactionIndex >= len(journal.Transactions) {
		return nil
	}
	transaction := &journal.Transactions[effect.TransactionIndex]
	if effect.PostingIndex >= len(transaction.Postings) {
		return nil
	}
	return &transaction.Postings[effect.PostingIndex]
}

// balanceOccurrence is one inclusion of the document with its replayed balances.
type balanceOccurrence struct {
	ref       occurrenceRef
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
			// The same formats as the inferred amount of the line, so the two hints
			// of one posting are written alike.
			formatsAt: func(offset int) map[string]formatter.CommodityFormat {
				journal, _ := s.cachedJournal(docURI, content)
				return s.commodityFormatsAt(docURI, journal, offset)
			},
		}}
	}

	var result []balanceOccurrence
	for _, tree := range s.workspace.GetIncludeTreesForFile(path) {
		if tree.Resolved == nil {
			continue
		}
		resolved := tree.Resolved
		occurrences := resolved.OccurrencesForCanonical(path)
		ids := make([]include.OccurrenceID, len(occurrences))
		for index, occurrence := range occurrences {
			ids[index] = occurrence.ID
			used[readingKey{tree: resolved, id: occurrence.ID}] = true
		}
		replays := s.cachedRunningBalancesOf(docURI, content, resolved, ids)
		for _, occurrence := range occurrences {
			result = append(result, balanceOccurrence{
				ref:       occurrenceRef{resolved, occurrence},
				balances:  replays[occurrence.ID],
				formatsAt: resolved.FormatsAtFunc(occurrence.ID),
			})
		}
	}
	return result
}

// hintValue is one value a hint can show, with the inclusions that give it.
type hintValue struct {
	text string
	refs []occurrenceRef
}

// addHintValue records value for the inclusions, merging equal values. The
// inclusions are named only when a tooltip is built.
func addHintValue(values []hintValue, text string, refs []occurrenceRef) []hintValue {
	for index := range values {
		if values[index].text == text {
			values[index].refs = append(values[index].refs, refs...)
			return values
		}
	}
	return append(values, hintValue{text: text, refs: slices.Clone(refs)})
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
		if labels := labelsOf(value.refs); len(labels) > 0 {
			line += " (when included from " + strings.Join(labels, ", ") + ")"
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
