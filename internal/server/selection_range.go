package server

import (
	"context"
	"strings"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/lsputil"
)

func (s *Server) SelectionRange(_ context.Context, params *protocol.SelectionRangeParams) ([]protocol.SelectionRange, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	journal, _ := s.cachedJournal(params.TextDocument.URI, doc)
	lines := strings.Split(doc, "\n")
	docRange := documentRange(lines)

	result := make([]protocol.SelectionRange, len(params.Positions))
	for i, pos := range params.Positions {
		result[i] = buildSelectionRange(journal, pos, docRange)
	}

	return result, nil
}

func documentRange(lines []string) protocol.Range {
	if len(lines) == 0 {
		return protocol.Range{}
	}
	lastLine := uint32(len(lines) - 1)
	lastChar := uint32(lsputil.UTF16Len(lines[len(lines)-1]))
	return protocol.Range{
		Start: protocol.Position{Line: 0, Character: 0},
		End:   protocol.Position{Line: lastLine, Character: lastChar},
	}
}

func buildSelectionRange(journal *ast.Journal, pos protocol.Position, docRange protocol.Range) protocol.SelectionRange {
	docSel := protocol.SelectionRange{Range: docRange}

	for i := range journal.Transactions {
		tx := &journal.Transactions[i]
		txRange := *astRangeToProtocol(tx.Range)
		if !rangeContainsPosition(txRange, pos) {
			continue
		}

		txSel := protocol.SelectionRange{Range: txRange, Parent: &docSel}

		if dateRange := *astRangeToProtocol(tx.Date.Range); rangeContainsPosition(dateRange, pos) {
			return protocol.SelectionRange{Range: dateRange, Parent: &txSel}
		}

		payee := getPayeeOrDescription(tx)
		if payee != "" {
			payeeRange := *astRangeToProtocol(payeeRange(tx, payee))
			if rangeContainsPosition(payeeRange, pos) {
				return protocol.SelectionRange{Range: payeeRange, Parent: &txSel}
			}
		}

		for j := range tx.Postings {
			p := &tx.Postings[j]
			postingRange := *astRangeToProtocol(p.Range)
			if !rangeContainsPosition(postingRange, pos) {
				continue
			}

			postingSel := protocol.SelectionRange{Range: postingRange, Parent: &txSel}

			accountRange := *astRangeToProtocol(p.Account.Range)
			if rangeContainsPosition(accountRange, pos) {
				segRange := findAccountSegmentRange(p.Account, pos)
				if segRange != nil && !rangesEqual(*segRange, accountRange) {
					accountSel := protocol.SelectionRange{Range: accountRange, Parent: &postingSel}
					return protocol.SelectionRange{Range: *segRange, Parent: &accountSel}
				}
				return protocol.SelectionRange{Range: accountRange, Parent: &postingSel}
			}

			if p.Amount != nil {
				amountRange := *astRangeToProtocol(p.Amount.Range)
				if rangeContainsPosition(amountRange, pos) {
					amountSel := protocol.SelectionRange{Range: amountRange, Parent: &postingSel}
					commodityRange := *astRangeToProtocol(p.Amount.Commodity.Range)
					if p.Amount.Commodity.Symbol != "" && rangeContainsPosition(commodityRange, pos) {
						return protocol.SelectionRange{Range: commodityRange, Parent: &amountSel}
					}
					return amountSel
				}
			}

			return postingSel
		}

		return txSel
	}

	for _, dir := range journal.Directives {
		dirRange := *astRangeToProtocol(dir.GetRange())
		if !rangeContainsPosition(dirRange, pos) {
			continue
		}

		dirSel := protocol.SelectionRange{Range: dirRange, Parent: &docSel}

		switch d := dir.(type) {
		case ast.AccountDirective:
			accountRange := *astRangeToProtocol(ensureRangeEnd(d.Account.Range, d.Account.Name))
			if rangeContainsPosition(accountRange, pos) {
				return protocol.SelectionRange{Range: accountRange, Parent: &dirSel}
			}
		case ast.CommodityDirective:
			commodityRange := *astRangeToProtocol(ensureRangeEnd(d.Commodity.Range, d.Commodity.Symbol))
			if rangeContainsPosition(commodityRange, pos) {
				return protocol.SelectionRange{Range: commodityRange, Parent: &dirSel}
			}
		}

		return dirSel
	}

	for _, comment := range journal.Comments {
		commentRange := *astRangeToProtocol(comment.Range)
		if rangeContainsPosition(commentRange, pos) {
			return protocol.SelectionRange{Range: commentRange, Parent: &docSel}
		}
	}

	return docSel
}

func findAccountSegmentRange(account ast.Account, pos protocol.Position) *protocol.Range {
	segments := strings.Split(account.Name, ":")
	if len(segments) <= 1 {
		return nil
	}

	startCol := account.Range.Start.Column
	startLine := account.Range.Start.Line

	cursorCol := int(pos.Character) + 1 // 1-based

	for _, seg := range segments {
		segLen := lsputil.UTF16Len(seg)
		endCol := startCol + segLen

		if startLine == int(pos.Line)+1 && cursorCol >= startCol && cursorCol <= endCol {
			return &protocol.Range{
				Start: protocol.Position{
					Line:      uint32(startLine - 1),
					Character: uint32(startCol - 1),
				},
				End: protocol.Position{
					Line:      uint32(startLine - 1),
					Character: uint32(endCol - 1),
				},
			}
		}
		startCol = endCol + 1 // skip ":"
	}

	return nil
}

func rangeContainsPosition(r protocol.Range, pos protocol.Position) bool {
	if pos.Line < r.Start.Line || pos.Line > r.End.Line {
		return false
	}
	if pos.Line == r.Start.Line && pos.Character < r.Start.Character {
		return false
	}
	if pos.Line == r.End.Line && pos.Character > r.End.Character {
		return false
	}
	return true
}

func rangesEqual(a, b protocol.Range) bool {
	return a.Start.Line == b.Start.Line &&
		a.Start.Character == b.Start.Character &&
		a.End.Line == b.End.Line &&
		a.End.Character == b.End.Character
}
