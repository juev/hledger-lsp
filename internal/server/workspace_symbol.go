package server

import (
	"context"
	"strings"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/ast"
)

func (s *Server) WorkspaceSymbol(ctx context.Context, params *protocol.WorkspaceSymbolParams) ([]protocol.SymbolInformation, error) {
	query := strings.ToLower(params.Query)

	type symbolKey struct {
		name      string
		kind      protocol.SymbolKind
		uri       uri.URI
		startLine uint32
		startChar uint32
	}
	seen := make(map[symbolKey]bool)
	mappers := s.newMapperCache()
	var symbols []protocol.SymbolInformation

	addSymbols := func(journal *ast.Journal, docURI uri.URI) {
		for _, sym := range extractSymbols(mappers, journal, docURI, query) {
			key := symbolKey{
				name:      sym.Name,
				kind:      sym.Kind,
				uri:       sym.Location.URI,
				startLine: sym.Location.Range.Start.Line,
				startChar: sym.Location.Range.Start.Character,
			}
			if !seen[key] {
				seen[key] = true
				symbols = append(symbols, sym)
			}
		}
	}

	coveredPaths := make(map[string]bool)

	if s.workspace != nil {
		for _, tree := range s.workspace.AllTrees() {
			if tree.Resolved == nil {
				continue
			}
			for i := range tree.Resolved.Occurrences {
				occ := &tree.Resolved.Occurrences[i]
				if occ.Journal == nil {
					continue
				}
				coveredPaths[occ.Path] = true
				addSymbols(occ.Journal, pathToURI(occ.Path))
			}
		}
	}

	s.documents.Range(func(key, value any) bool {
		docURI := key.(uri.URI)
		if path := uriToPath(docURI); path != "" && coveredPaths[path] {
			return true
		}
		content := value.(string)
		journal, _ := s.cachedJournal(docURI, content)
		if journal == nil {
			return true
		}
		addSymbols(journal, docURI)
		return true
	})

	return symbols, nil
}

func extractSymbols(mappers *mapperCache, journal *ast.Journal, uri uri.URI, query string) []protocol.SymbolInformation {
	var symbols []protocol.SymbolInformation

	for _, dir := range journal.Directives {
		switch d := dir.(type) {
		case ast.AccountDirective:
			if matchesQuery(d.Account.Name, query) {
				symbols = append(symbols, protocol.SymbolInformation{
					BaseSymbolInformation: protocol.BaseSymbolInformation{Name: d.Account.Name, Kind: protocol.SymbolKindClass},
					Location: protocol.Location{
						URI:   uri,
						Range: mappers.rangeIn(uriToPath(uri), d.Account.Range),
					},
				})
			}
		case ast.CommodityDirective:
			if matchesQuery(d.Commodity.Symbol, query) {
				symbols = append(symbols, protocol.SymbolInformation{
					BaseSymbolInformation: protocol.BaseSymbolInformation{Name: d.Commodity.Symbol, Kind: protocol.SymbolKindEnum},
					Location: protocol.Location{
						URI:   uri,
						Range: mappers.rangeIn(uriToPath(uri), d.Commodity.Range),
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
					BaseSymbolInformation: protocol.BaseSymbolInformation{Name: payee, Kind: protocol.SymbolKindFunction},
					Location: protocol.Location{
						URI:   uri,
						Range: mappers.rangeIn(uriToPath(uri), payeeRange(tx, payee)),
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
