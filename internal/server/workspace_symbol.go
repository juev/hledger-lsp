package server

import (
	"context"
	"strings"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/parser"
)

func (s *Server) WorkspaceSymbol(ctx context.Context, params *protocol.WorkspaceSymbolParams) ([]protocol.SymbolInformation, error) {
	query := strings.ToLower(params.Query)

	var symbols []protocol.SymbolInformation

	s.documents.Range(func(key, value any) bool {
		uri := key.(protocol.DocumentURI)
		content := value.(string)

		journal, _ := parser.Parse(content)
		if journal == nil {
			return true
		}

		symbols = append(symbols, extractSymbols(journal, uri, query)...)
		return true
	})

	return symbols, nil
}

func extractSymbols(journal *ast.Journal, uri protocol.DocumentURI, query string) []protocol.SymbolInformation {
	var symbols []protocol.SymbolInformation

	for _, dir := range journal.Directives {
		switch d := dir.(type) {
		case ast.AccountDirective:
			if matchesQuery(d.Account.Name, query) {
				symbols = append(symbols, protocol.SymbolInformation{
					Name: d.Account.Name,
					Kind: protocol.SymbolKindClass,
					Location: protocol.Location{
						URI:   uri,
						Range: *astRangeToProtocol(d.Account.Range),
					},
				})
			}
		case ast.CommodityDirective:
			if matchesQuery(d.Commodity.Symbol, query) {
				symbols = append(symbols, protocol.SymbolInformation{
					Name: d.Commodity.Symbol,
					Kind: protocol.SymbolKindEnum,
					Location: protocol.Location{
						URI:   uri,
						Range: *astRangeToProtocol(d.Commodity.Range),
					},
				})
			}
		}
	}

	seen := make(map[string]bool)
	for i := range journal.Transactions {
		tx := &journal.Transactions[i]
		payee := getPayeeOrDescription(tx)
		if payee != "" && !seen[payee] {
			if matchesQuery(payee, query) {
				seen[payee] = true
				symbols = append(symbols, protocol.SymbolInformation{
					Name: payee,
					Kind: protocol.SymbolKindFunction,
					Location: protocol.Location{
						URI:   uri,
						Range: *astRangeToProtocol(estimatePayeeRange(tx, payee)),
					},
				})
			}
		}
	}

	return symbols
}

func matchesQuery(name, query string) bool {
	if query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(name), query)
}
