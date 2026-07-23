package server

import (
	"context"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/ast"
)

func (s *Server) DocumentHighlight(_ context.Context, params *protocol.DocumentHighlightParams) ([]protocol.DocumentHighlight, error) {
	doc, ok := s.getJournalDoc(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	journal, _ := s.cachedJournal(params.TextDocument.URI, doc)

	target := findDefinitionTarget(journal, params.Position)
	if target == nil || target.context == DefContextUnknown {
		return nil, nil
	}

	switch target.context {
	case DefContextAccount:
		return findAccountHighlights(journal, target.name), nil
	case DefContextCommodity:
		return findCommodityHighlights(journal, target.name), nil
	case DefContextPayee:
		return findPayeeHighlights(journal, target.name), nil
	default:
		return nil, nil
	}
}

func findAccountHighlights(journal *ast.Journal, name string) []protocol.DocumentHighlight {
	var highlights []protocol.DocumentHighlight

	for _, dir := range journal.Directives {
		if ad, ok := dir.(ast.AccountDirective); ok && ad.Account.Name == name {
			highlights = append(highlights, protocol.DocumentHighlight{
				Range: *astRangeToProtocol(ensureRangeEnd(ad.Account.Range, ad.Account.Name)),
				Kind:  protocol.DocumentHighlightKindText,
			})
		}
	}

	for i := range journal.Transactions {
		tx := &journal.Transactions[i]
		for j := range tx.Postings {
			p := &tx.Postings[j]
			if p.Account.Name == name {
				highlights = append(highlights, protocol.DocumentHighlight{
					Range: *astRangeToProtocol(computeAccountRange(&p.Account)),
					Kind:  protocol.DocumentHighlightKindRead,
				})
			}
		}
	}

	if len(highlights) == 0 {
		return nil
	}
	return highlights
}

func findCommodityHighlights(journal *ast.Journal, symbol string) []protocol.DocumentHighlight {
	var highlights []protocol.DocumentHighlight

	for _, dir := range journal.Directives {
		if cd, ok := dir.(ast.CommodityDirective); ok && cd.Commodity.Symbol == symbol {
			highlights = append(highlights, protocol.DocumentHighlight{
				Range: *astRangeToProtocol(ensureRangeEnd(cd.Commodity.Range, cd.Commodity.Symbol)),
				Kind:  protocol.DocumentHighlightKindText,
			})
		}
	}

	for i := range journal.Transactions {
		tx := &journal.Transactions[i]
		for j := range tx.Postings {
			p := &tx.Postings[j]
			if p.Amount != nil && p.Amount.Commodity.Symbol == symbol {
				highlights = append(highlights, protocol.DocumentHighlight{
					Range: *astRangeToProtocol(p.Amount.Commodity.Range),
					Kind:  protocol.DocumentHighlightKindRead,
				})
			}
		}
	}

	if len(highlights) == 0 {
		return nil
	}
	return highlights
}

func findPayeeHighlights(journal *ast.Journal, payee string) []protocol.DocumentHighlight {
	var highlights []protocol.DocumentHighlight

	for i := range journal.Transactions {
		tx := &journal.Transactions[i]
		txPayee := getPayeeOrDescription(tx)
		if txPayee == payee {
			highlights = append(highlights, protocol.DocumentHighlight{
				Range: *astRangeToProtocol(payeeRange(tx, payee)),
				Kind:  protocol.DocumentHighlightKindRead,
			})
		}
	}

	if len(highlights) == 0 {
		return nil
	}
	return highlights
}
