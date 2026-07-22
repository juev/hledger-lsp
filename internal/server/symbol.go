package server

import (
	"context"
	"fmt"
	"sort"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/filetype"
	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/juev/hledger-lsp/internal/rules"
)

func (s *Server) DocumentSymbol(
	ctx context.Context,
	params *protocol.DocumentSymbolParams,
) ([]any, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	if filetype.IsRules(string(params.TextDocument.URI)) {
		return rulesDocumentSymbols(doc), nil
	}

	journal, _ := parser.Parse(doc)
	if journal == nil {
		return []any{}, nil
	}

	var symbols []any

	symbols = append(symbols, groupTransactionsByMonth(journal.Transactions)...)

	for _, dir := range journal.Directives {
		symbols = append(symbols, directiveToSymbol(dir))
	}

	for _, inc := range journal.Includes {
		symbols = append(symbols, includeToSymbol(inc))
	}

	return symbols, nil
}

func groupTransactionsByMonth(transactions []ast.Transaction) []any {
	if len(transactions) == 0 {
		return nil
	}

	type monthGroup struct {
		label    string
		first    ast.Transaction
		last     ast.Transaction
		children []protocol.DocumentSymbol
	}

	groups := make(map[string]*monthGroup)
	var order []string

	for _, tx := range transactions {
		key := fmt.Sprintf("%04d-%02d", tx.Date.Year, tx.Date.Month)
		g, ok := groups[key]
		if !ok {
			g = &monthGroup{label: key, first: tx, last: tx}
			groups[key] = g
			order = append(order, key)
		}
		g.last = tx
		g.children = append(g.children, transactionToSymbol(tx))
	}

	sort.Strings(order)

	result := make([]any, 0, len(order))
	for _, key := range order {
		g := groups[key]
		rng := *astRangeToProtocol(ast.Range{
			Start: g.first.Range.Start,
			End:   g.last.Range.End,
		})
		result = append(result, protocol.DocumentSymbol{
			Name:           g.label,
			Kind:           protocol.SymbolKindNamespace,
			Range:          rng,
			SelectionRange: rng,
			Children:       g.children,
		})
	}
	return result
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

func rulesDocumentSymbols(doc string) []any {
	rf, _ := rules.Parse(doc)
	syms := rules.Symbols(rf)
	result := make([]any, 0, len(syms))
	for _, sym := range syms {
		rng := *astRangeToProtocol(sym.Range)
		result = append(result, protocol.DocumentSymbol{
			Name:           sym.Name,
			Kind:           protocol.SymbolKind(sym.Kind),
			Range:          rng,
			SelectionRange: rng,
		})
	}
	return result
}
