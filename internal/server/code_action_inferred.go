package server

import (
	"fmt"
	"slices"
	"strings"

	"github.com/shopspring/decimal"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/formatter"
	"github.com/juev/hledger-lsp/internal/lsputil"
)

// insertInferredAmountKind marks the action that writes the amount hledger
// would infer for an amountless posting into the document. Clients bind it to a
// key by asking for this kind explicitly.
const insertInferredAmountKind = protocol.CodeActionKind("quickfix.hledger.insertInferredAmount")

// getInferredAmountCodeActions offers the inferred balancing amount of an
// amountless posting as an edit aligned to the document's amount column. Unlike
// the UNBALANCED quickfix it is not driven by a diagnostic — such a transaction
// is valid hledger, there is simply nothing written down yet.
func (s *Server) getInferredAmountCodeActions(params *protocol.CodeActionParams) []protocol.CodeAction {
	doc, ok := s.getJournalDoc(params.TextDocument.URI)
	if !ok {
		return nil
	}
	journal, _ := s.cachedJournal(params.TextDocument.URI, doc)
	if journal == nil || len(journal.Transactions) == 0 {
		return nil
	}

	settings := s.getSettings()
	lines := splitLines(doc)

	// Alignment walks every transaction, and code actions are requested on
	// every cursor move, so pay for it only once a posting actually qualifies.
	var alignment formatter.AlignmentInfo
	alignmentComputed := false
	computeAlignment := func() formatter.AlignmentInfo {
		if !alignmentComputed {
			commodityFormats := formatter.ExtractCommodityFormats(journal.Directives)
			if workspaceFormats := s.commodityFormatsForDocument(params.TextDocument.URI); workspaceFormats != nil {
				commodityFormats = workspaceFormats
			}
			alignment = formatter.ComputeAmountAlignment(journal, doc, commodityFormats, formatterOptionsFrom(settings.Formatting))
			alignmentComputed = true
		}
		return alignment
	}

	// The occurrences are listed only once a posting qualifies, like the alignment.
	var readings []occurrenceReading
	readingsListed := false
	listReadings := func() []occurrenceReading {
		if !readingsListed {
			readings = s.occurrenceReadings(params.TextDocument.URI, doc)
			readingsListed = true
		}
		return readings
	}
	var readingEffects [][]analyzer.PostingEffect
	effectsOf := func(index int) []analyzer.PostingEffect {
		if readingEffects == nil {
			readingEffects = make([][]analyzer.PostingEffect, len(readings))
		}
		if readingEffects[index] == nil {
			readingEffects[index] = analyzer.CalculatePostingEffects(readings[index].journal)
		}
		return readingEffects[index]
	}

	actions := make([]protocol.CodeAction, 0, 1)
	for _, effect := range s.cachedPostingEffects(params.TextDocument.URI, doc) {
		// A single edit cannot express a balance spanning several commodities.
		if len(effect.InferredAmounts) != 1 {
			continue
		}
		if effect.TransactionIndex >= len(journal.Transactions) {
			continue
		}
		transaction := &journal.Transactions[effect.TransactionIndex]
		if effect.PostingIndex >= len(transaction.Postings) {
			continue
		}
		posting := &transaction.Postings[effect.PostingIndex]
		if !postingLineInRange(posting, params.Range) {
			continue
		}

		formats := s.commodityFormatsAt(params.TextDocument.URI, journal, posting.Range.Start.Offset)
		amountText := formatInferredAmount(transaction, effect.InferredAmounts, formats)
		buildEdit := func(text string) (protocol.TextEdit, bool) {
			return buildInferredAmountEdit(lines, posting, text, computeAlignment(), settings.Formatting)
		}
		edit, ok := buildEdit(amountText)
		if !ok {
			continue
		}
		if all := listReadings(); len(all) > 1 {
			amountText, edit, ok = inferredAmountForReadings(all, doc, effect, posting.Range.Start.Offset, amountText, buildEdit, effectsOf, settings.Diagnostics.BalanceTolerance)
			if !ok {
				continue
			}
		}

		actions = append(actions, protocol.CodeAction{
			Title: fmt.Sprintf("Insert inferred amount (%s)", amountText),
			Kind:  codeActionKind(insertInferredAmountKind),
			Edit: &protocol.WorkspaceEdit{
				Changes: map[uri.URI][]protocol.TextEdit{params.TextDocument.URI: {edit}},
			},
		})
	}

	return actions
}

// formatInferredAmount renders the single inferred amount the way the
// transaction already writes that commodity: same symbol position and at least
// as many decimal places as its siblings. The decimal mark and the quoting of
// the symbol come from the amounts of that commodity in the transaction; the
// commodity format only supplies the rest of the number style, and its own mark
// yields to the one in force where the transaction is written.
func formatInferredAmount(
	transaction *ast.Transaction,
	amounts map[string]decimal.Decimal,
	formats map[string]formatter.CommodityFormat,
) string {
	// The commodity may legitimately be the empty symbol: hledger journals that
	// track a single currency often write bare numbers.
	var commodity string
	var quantity decimal.Decimal
	found := false
	for symbol, value := range amounts {
		commodity, quantity, found = symbol, value, true
	}
	if !found {
		return ""
	}

	amount := &ast.Amount{
		Quantity:  quantity,
		Commodity: ast.Commodity{Symbol: commodity, Position: ast.CommodityRight},
	}
	places := -quantity.Exponent()
	for i := range transaction.Postings {
		sibling := transaction.Postings[i].Amount
		if sibling == nil || sibling.Commodity.Symbol != commodity {
			continue
		}
		amount.Commodity.Position = sibling.Commodity.Position
		if siblingPlaces := -sibling.Quantity.Exponent(); siblingPlaces > places {
			places = siblingPlaces
		}
		break
	}
	// A journal that never spells this commodity out — every amount inherits it
	// from the default commodity directive — keeps the inserted amount bare too.
	amount.Commodity.Inferred = !transactionWritesCommodity(transaction, commodity)
	amount.Commodity.Quoted = transactionQuotesCommodity(transaction, commodity)
	amount.DecimalMark = transactionDecimalMark(transaction, commodity, amount.Commodity.Inferred)
	if places > 0 {
		amount.RawQuantity = quantity.StringFixed(places)
		// The raw text is written as is when no commodity format declares the
		// number style, so it has to carry the mark in force itself.
		if amount.DecimalMark == "," {
			amount.RawQuantity = strings.Replace(amount.RawQuantity, ".", ",", 1)
		}
	}
	if quantity.IsNegative() && amount.Commodity.Position == ast.CommodityLeft {
		amount.SignBeforeCommodity = true
	}

	return formatter.FormatAmount(amount, formats)
}

// transactionDecimalMark returns the decimal mark in force for a commodity where
// the transaction is written. A directive cannot change between the postings of
// one transaction, so any amount of that commodity, a cost or an assertion
// included, carries it. Amounts of the same commodity written the other way
// (bare against explicit) are skipped: hledger resolves a bare number through
// the default commodity, which can use another mark. "" means the transaction
// holds no such amount and the commodity format decides.
func transactionDecimalMark(transaction *ast.Transaction, commodity string, inferred bool) string {
	for i := range transaction.Postings {
		for _, amt := range postingAmounts(&transaction.Postings[i]) {
			if amt != nil && amt.Commodity.Symbol == commodity && amt.Commodity.Inferred == inferred && amt.DecimalMark != "" {
				return amt.DecimalMark
			}
		}
	}
	return ""
}

// transactionQuotesCommodity reports whether the transaction writes the
// commodity symbol in quotes anywhere, a cost or an assertion included. A symbol
// with a space or a digit only parses back when it keeps its quotes.
func transactionQuotesCommodity(transaction *ast.Transaction, commodity string) bool {
	for i := range transaction.Postings {
		for _, amt := range postingAmounts(&transaction.Postings[i]) {
			if amt != nil && amt.Commodity.Symbol == commodity && !amt.Commodity.Inferred && amt.Commodity.Quoted {
				return true
			}
		}
	}
	return false
}

// transactionWritesCommodity reports whether the transaction spells the
// commodity symbol out anywhere: in a posting amount, a cost, a lot price or a
// balance assertion.
func transactionWritesCommodity(transaction *ast.Transaction, commodity string) bool {
	for i := range transaction.Postings {
		for _, amt := range postingAmounts(&transaction.Postings[i]) {
			if amt != nil && amt.Commodity.Symbol == commodity && !amt.Commodity.Inferred {
				return true
			}
		}
	}
	return false
}

// postingAmounts lists every amount a posting writes.
func postingAmounts(posting *ast.Posting) []*ast.Amount {
	amounts := []*ast.Amount{posting.Amount}
	if posting.Cost != nil {
		amounts = append(amounts, &posting.Cost.Amount)
	}
	if posting.LotPrice != nil {
		amounts = append(amounts, posting.LotPrice.Cost)
	}
	if posting.BalanceAssertion != nil {
		amounts = append(amounts, &posting.BalanceAssertion.Amount)
		if posting.BalanceAssertion.Cost != nil {
			amounts = append(amounts, &posting.BalanceAssertion.Cost.Amount)
		}
	}
	return amounts
}

// postingLineInRange reports whether the posting sits on one of the lines the
// client asked about. Line granularity is deliberate: the request usually
// carries a bare cursor position, which no range covering the amount column
// would overlap.
func postingLineInRange(posting *ast.Posting, requested protocol.Range) bool {
	line := posting.Account.Range.End.Line - 1
	return line >= int(requested.Start.Line) && line <= int(requested.End.Line)
}

// buildInferredAmountEdit replaces whatever whitespace follows the account name
// with padding that puts the amount on the document's amount column. Anything
// else on the line — a posting comment — is preserved after two spaces.
func buildInferredAmountEdit(
	lines []string,
	posting *ast.Posting,
	amountText string,
	alignment formatter.AlignmentInfo,
	formatting formattingSettings,
) (protocol.TextEdit, bool) {
	if amountText == "" || posting.Amount != nil {
		return protocol.TextEdit{}, false
	}

	lineIndex := posting.Account.Range.End.Line - 1
	if lineIndex < 0 || lineIndex >= len(lines) {
		return protocol.TextEdit{}, false
	}
	line := strings.TrimSuffix(lines[lineIndex], "\r")

	accountEnd := posting.Account.Range.End.Column - 1
	accountEndByte := lsputil.UTF16OffsetToByteOffset(line, lsputil.RuneOffsetToUTF16Offset(line, accountEnd))
	if accountEndByte < 0 || accountEndByte > len(line) {
		return protocol.TextEdit{}, false
	}

	rest := line[accountEndByte:]
	trailing := strings.TrimLeft(rest, " \t")
	suffix := ""
	if trailing != "" {
		suffix = "  "
	}

	newText := strings.Repeat(" ", amountSpacesFromAlignment(accountEnd, amountText, alignment, formatting)) + amountText + suffix
	startCharacter := lsputil.RuneOffsetToUTF16Offset(line, accountEnd)

	return protocol.TextEdit{
		Range: protocol.Range{
			Start: protocol.Position{Line: uint32(lineIndex), Character: uint32(startCharacter)},
			End:   protocol.Position{Line: uint32(lineIndex), Character: uint32(startCharacter + len(rest) - len(trailing))},
		},
		NewText: newText,
	}, true
}

// inferredAmountForReadings picks the text to write for an inferred amount when
// the document is read under several contexts. The amount is inferred at each
// occurrence from what that occurrence reads, and the text is read back at each
// one, so it is offered only when it balances the transaction at every
// occurrence. Candidates are the text each occurrence's own formats produce and
// the number as plain digits, which reads the same under any decimal mark.
func inferredAmountForReadings(
	readings []occurrenceReading,
	doc string,
	effect analyzer.PostingEffect,
	offset int,
	firstText string,
	buildEdit func(text string) (protocol.TextEdit, bool),
	effectsOf func(index int) []analyzer.PostingEffect,
	tolerance float64,
) (string, protocol.TextEdit, bool) {
	candidates := []string{firstText}
	var plain []string
	addTo := func(list *[]string, text string) {
		if text != "" && !slices.Contains(candidates, text) && !slices.Contains(*list, text) {
			*list = append(*list, text)
		}
	}
	var styled []string
	for index := range readings {
		reading := &readings[index]
		if effect.TransactionIndex >= len(reading.journal.Transactions) {
			return "", protocol.TextEdit{}, false
		}
		var inferred map[string]decimal.Decimal
		for _, other := range effectsOf(index) {
			if other.TransactionIndex == effect.TransactionIndex && other.PostingIndex == effect.PostingIndex {
				inferred = other.InferredAmounts
			}
		}
		// A reading that infers several commodities, or none, has no single
		// amount that a single edit could write for it.
		if len(inferred) != 1 {
			return "", protocol.TextEdit{}, false
		}
		transaction := &reading.journal.Transactions[effect.TransactionIndex]
		if index > 0 {
			addTo(&styled, formatInferredAmount(transaction, inferred, reading.formatsAt(offset)))
		}
		addTo(&plain, formatInferredAmount(transaction, inferred, map[string]formatter.CommodityFormat{}))
	}

	candidates = append(append(candidates, styled...), plain...)
	for _, text := range candidates {
		edit, ok := buildEdit(text)
		if !ok {
			continue
		}
		balanced := everywhere(doc, readings, []protocol.TextEdit{edit}, func(edited *ast.Journal) bool {
			return effect.TransactionIndex < len(edited.Transactions) &&
				analyzer.CheckBalance(&edited.Transactions[effect.TransactionIndex], decimal.NewFromFloat(tolerance)).Balanced
		})
		if balanced {
			return text, edit, true
		}
	}
	return "", protocol.TextEdit{}, false
}
