package server

import (
	"context"
	"fmt"
	"sort"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/filetype"
	"github.com/juev/hledger-lsp/internal/lsputil"
	"github.com/juev/hledger-lsp/internal/rules"
)

func (s *Server) documentSymbols(
	_ context.Context,
	params *protocol.DocumentSymbolParams,
) []protocol.DocumentSymbol {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil
	}

	if filetype.IsRules(string(params.TextDocument.URI)) {
		return rulesDocumentSymbols(doc)
	}

	journal, _ := s.cachedJournal(params.TextDocument.URI, doc)
	if journal == nil {
		return nil
	}

	mapper := lsputil.NewPositionMapper(doc)
	symbols := make([]protocol.DocumentSymbol, 0, len(journal.Transactions)+len(journal.Directives)+len(journal.Includes))

	symbols = append(symbols, groupTransactionsByMonth(mapper, journal.Transactions)...)

	for _, dir := range journal.Directives {
		symbols = append(symbols, directiveToSymbol(mapper, dir))
	}

	for _, inc := range journal.Includes {
		symbols = append(symbols, includeToSymbol(mapper, inc))
	}

	return symbols
}

func groupTransactionsByMonth(mapper *lsputil.PositionMapper, transactions []ast.Transaction) []protocol.DocumentSymbol {
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
		g.children = append(g.children, transactionToSymbol(mapper, tx))
	}

	sort.Strings(order)

	result := make([]protocol.DocumentSymbol, 0, len(order))
	for _, key := range order {
		g := groups[key]
		rng := astRangeToLSP(mapper, ast.Range{
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

func includeToSymbol(mapper *lsputil.PositionMapper, inc ast.Include) protocol.DocumentSymbol {
	rng := astRangeToLSP(mapper, inc.Range)
	return protocol.DocumentSymbol{
		Name:           "include " + inc.Path,
		Kind:           protocol.SymbolKindModule,
		Range:          rng,
		SelectionRange: rng,
	}
}

func transactionToSymbol(mapper *lsputil.PositionMapper, tx ast.Transaction) protocol.DocumentSymbol {
	name := formatTransactionName(tx)
	rng := astRangeToLSP(mapper, tx.Range)

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

func directiveToSymbol(mapper *lsputil.PositionMapper, dir ast.Directive) protocol.DocumentSymbol {
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

	rng := astRangeToLSP(mapper, dir.GetRange())
	return protocol.DocumentSymbol{
		Name:           name,
		Kind:           kind,
		Range:          rng,
		SelectionRange: rng,
	}
}

func rulesDocumentSymbols(doc string) []protocol.DocumentSymbol {
	rf, _ := rules.Parse(doc)
	syms := rules.Symbols(rf)
	result := make([]protocol.DocumentSymbol, 0, len(syms))
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

func flattenDocumentSymbols(documentURI uri.URI, symbols []protocol.DocumentSymbol) []protocol.SymbolInformation {
	result := make([]protocol.SymbolInformation, 0, len(symbols))
	for _, symbol := range symbols {
		flattenDocumentSymbol(documentURI, symbol, nil, &result)
	}
	return result
}

func flattenDocumentSymbol(documentURI uri.URI, symbol protocol.DocumentSymbol, containerName *string, result *[]protocol.SymbolInformation) {
	*result = append(*result, protocol.SymbolInformation{
		BaseSymbolInformation: protocol.BaseSymbolInformation{
			Name:          symbol.Name,
			Kind:          symbol.Kind,
			Tags:          symbol.Tags,
			ContainerName: containerName,
		},
		Location: protocol.Location{URI: documentURI, Range: symbol.Range},
	})

	for _, child := range symbol.Children {
		flattenDocumentSymbol(documentURI, child, &symbol.Name, result)
	}
}
