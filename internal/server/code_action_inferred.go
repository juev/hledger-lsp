package server

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

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
			alignment = formatter.ComputeAlignment(journal, commodityFormats, formatterOptionsFrom(settings.Formatting))
			alignmentComputed = true
		}
		return alignment
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

		formats := localCommodityFormatsAt(journal, posting.Range.Start.Offset)
		amountText := formatInferredAmount(transaction, effect.InferredAmounts, formats)
		edit, ok := buildInferredAmountEdit(lines, posting, amountText, computeAlignment(), settings.Formatting)
		if !ok {
			continue
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
// as many decimal places as its siblings. A commodity directive still wins,
// because FormatAmount prefers a declared format over the raw quantity.
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
		amount.Commodity.Quoted = sibling.Commodity.Quoted
		if siblingPlaces := -sibling.Quantity.Exponent(); siblingPlaces > places {
			places = siblingPlaces
		}
		break
	}
	if places > 0 {
		amount.RawQuantity = quantity.StringFixed(places)
	}
	if quantity.IsNegative() && amount.Commodity.Position == ast.CommodityLeft {
		amount.SignBeforeCommodity = true
	}

	return formatter.FormatAmount(amount, formats)
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
