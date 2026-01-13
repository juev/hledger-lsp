package server

import (
	"context"
	"fmt"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/parser"
)

func (s *Server) DocumentSymbol(
	ctx context.Context,
	params *protocol.DocumentSymbolParams,
) ([]any, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	journal, _ := parser.Parse(doc)
	if journal == nil {
		return []any{}, nil
	}

	var symbols []any

	for _, tx := range journal.Transactions {
		symbols = append(symbols, transactionToSymbol(tx))
	}

	for _, dir := range journal.Directives {
		symbols = append(symbols, directiveToSymbol(dir))
	}

	for _, inc := range journal.Includes {
		symbols = append(symbols, includeToSymbol(inc))
	}

	return symbols, nil
}

func includeToSymbol(inc ast.Include) protocol.DocumentSymbol {
	rng := *astRangeToProtocol(inc.Range)
	return protocol.DocumentSymbol{
		Name:           "include " + inc.Path,
		Kind:           protocol.SymbolKindModule,
		Range:          rng,
		SelectionRange: rng,
	}
}

func transactionToSymbol(tx ast.Transaction) protocol.DocumentSymbol {
	name := formatTransactionName(tx)
	rng := *astRangeToProtocol(tx.Range)

	return protocol.DocumentSymbol{
		Name:           name,
		Kind:           protocol.SymbolKindFunction,
		Range:          rng,
		SelectionRange: rng,
	}
}

func formatTransactionName(tx ast.Transaction) string {
	date := fmt.Sprintf("%04d-%02d-%02d", tx.Date.Year, tx.Date.Month, tx.Date.Day)
	if tx.Description != "" {
		return date + " " + tx.Description
	}
	return date
}

func directiveToSymbol(dir ast.Directive) protocol.DocumentSymbol {
	var name string
	var kind protocol.SymbolKind

	switch d := dir.(type) {
	case ast.AccountDirective:
		name = "account " + d.Account.Name
		kind = protocol.SymbolKindClass
	case ast.CommodityDirective:
		name = "commodity " + d.Commodity.Symbol
		kind = protocol.SymbolKindEnum
	case ast.Include:
		name = "include " + d.Path
		kind = protocol.SymbolKindModule
	case ast.PriceDirective:
		name = fmt.Sprintf("P %04d-%02d-%02d %s",
			d.Date.Year, d.Date.Month, d.Date.Day, d.Commodity.Symbol)
		kind = protocol.SymbolKindConstant
	default:
		name = "directive"
		kind = protocol.SymbolKindVariable
	}

	rng := *astRangeToProtocol(dir.GetRange())
	return protocol.DocumentSymbol{
		Name:           name,
		Kind:           kind,
		Range:          rng,
		SelectionRange: rng,
	}
}
