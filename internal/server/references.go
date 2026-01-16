package server

import (
	"context"
	"sort"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/parser"
)

func (s *Server) References(ctx context.Context, params *protocol.ReferenceParams) ([]protocol.Location, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	journal, _ := parser.Parse(doc)

	target := findDefinitionTarget(journal, params.Position)
	if target == nil || target.context == DefContextUnknown {
		return nil, nil
	}

	resolved := s.getWorkspaceResolved(params.TextDocument.URI)
	currentPath := uriToPath(params.TextDocument.URI)

	return findReferences(target, resolved, currentPath, journal, params.Context.IncludeDeclaration), nil
}

func findReferences(target *definitionTarget, resolved *include.ResolvedJournal, currentPath string, currentJournal *ast.Journal, includeDeclaration bool) []protocol.Location {
	switch target.context {
	case DefContextAccount:
		return findAccountReferences(target.name, resolved, currentPath, currentJournal, includeDeclaration)
	case DefContextCommodity:
		return findCommodityReferences(target.name, resolved, currentPath, currentJournal, includeDeclaration)
	case DefContextPayee:
		// Payees don't have declarations (no directive), so includeDeclaration is ignored
		return findPayeeReferences(target.name, resolved, currentPath, currentJournal)
	default:
		return nil
	}
}

func findAccountReferences(name string, resolved *include.ResolvedJournal, currentPath string, currentJournal *ast.Journal, includeDeclaration bool) []protocol.Location {
	journals := allJournalsWithPaths(resolved, currentPath, currentJournal)
	var locations []protocol.Location

	for _, filePath := range sortedJournalPaths(journals) {
		journal := journals[filePath]

		if includeDeclaration {
			for _, dir := range journal.Directives {
				if ad, ok := dir.(ast.AccountDirective); ok {
					if ad.Account.Name == name {
						locations = append(locations, protocol.Location{
							URI:   pathToURI(filePath),
							Range: *astRangeToProtocol(computeAccountRange(&ad.Account)),
						})
					}
				}
			}
		}

		for i := range journal.Transactions {
			tx := &journal.Transactions[i]
			for j := range tx.Postings {
				p := &tx.Postings[j]
				if p.Account.Name == name {
					locations = append(locations, protocol.Location{
						URI:   pathToURI(filePath),
						Range: *astRangeToProtocol(computeAccountRange(&p.Account)),
					})
				}
			}
		}
	}

	return sortAndDedup(locations)
}

func findCommodityReferences(symbol string, resolved *include.ResolvedJournal, currentPath string, currentJournal *ast.Journal, includeDeclaration bool) []protocol.Location {
	journals := allJournalsWithPaths(resolved, currentPath, currentJournal)
	var locations []protocol.Location

	for _, filePath := range sortedJournalPaths(journals) {
		journal := journals[filePath]

		if includeDeclaration {
			for _, dir := range journal.Directives {
				if cd, ok := dir.(ast.CommodityDirective); ok {
					if cd.Commodity.Symbol == symbol {
						locations = append(locations, protocol.Location{
							URI:   pathToURI(filePath),
							Range: *astRangeToProtocol(cd.Commodity.Range),
						})
					}
				}
			}
		}

		for i := range journal.Transactions {
			tx := &journal.Transactions[i]
			for j := range tx.Postings {
				p := &tx.Postings[j]
				if p.Amount != nil && p.Amount.Commodity.Symbol == symbol {
					locations = append(locations, protocol.Location{
						URI:   pathToURI(filePath),
						Range: *astRangeToProtocol(p.Amount.Commodity.Range),
					})
				}
			}
		}
	}

	return sortAndDedup(locations)
}

func findPayeeReferences(payee string, resolved *include.ResolvedJournal, currentPath string, currentJournal *ast.Journal) []protocol.Location {
	journals := allJournalsWithPaths(resolved, currentPath, currentJournal)
	var locations []protocol.Location

	for _, filePath := range sortedJournalPaths(journals) {
		journal := journals[filePath]

		for i := range journal.Transactions {
			tx := &journal.Transactions[i]
			txPayee := getPayeeOrDescription(tx)
			if txPayee == payee {
				locations = append(locations, protocol.Location{
					URI:   pathToURI(filePath),
					Range: *astRangeToProtocol(estimatePayeeRange(tx, payee)),
				})
			}
		}
	}

	return sortAndDedup(locations)
}

func sortAndDedup(locations []protocol.Location) []protocol.Location {
	if len(locations) == 0 {
		return nil
	}

	sort.Slice(locations, func(i, j int) bool {
		if locations[i].URI != locations[j].URI {
			return locations[i].URI < locations[j].URI
		}
		if locations[i].Range.Start.Line != locations[j].Range.Start.Line {
			return locations[i].Range.Start.Line < locations[j].Range.Start.Line
		}
		return locations[i].Range.Start.Character < locations[j].Range.Start.Character
	})

	result := make([]protocol.Location, 0, len(locations))
	for i, loc := range locations {
		if i == 0 || !locationsEqual(loc, locations[i-1]) {
			result = append(result, loc)
		}
	}

	return result
}

func locationsEqual(a, b protocol.Location) bool {
	return a.URI == b.URI &&
		a.Range.Start.Line == b.Range.Start.Line &&
		a.Range.Start.Character == b.Range.Start.Character &&
		a.Range.End.Line == b.Range.End.Line &&
		a.Range.End.Character == b.Range.End.Character
}
