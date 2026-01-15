package server

import (
	"context"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/parser"
)

type DefinitionContext int

const (
	DefContextUnknown DefinitionContext = iota
	DefContextAccount
	DefContextCommodity
	DefContextPayee
)

type definitionTarget struct {
	context DefinitionContext
	name    string
}

func (s *Server) Definition(ctx context.Context, params *protocol.DefinitionParams) ([]protocol.Location, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	journal, _ := parser.Parse(doc)

	target := findDefinitionTarget(journal, params.Position)
	if target == nil || target.context == DefContextUnknown {
		return nil, nil
	}

	resolved := s.GetResolved(params.TextDocument.URI)
	currentPath := uriToPath(params.TextDocument.URI)

	location := findDefinitionLocation(target, resolved, currentPath, journal)
	if location == nil {
		return nil, nil
	}

	return []protocol.Location{*location}, nil
}

func findDefinitionTarget(journal *ast.Journal, pos protocol.Position) *definitionTarget {
	for i := range journal.Transactions {
		tx := &journal.Transactions[i]

		payee := getPayeeOrDescription(tx)
		if payee != "" {
			payeeRange := estimatePayeeRange(tx, payee)
			if positionInRange(pos, payeeRange) {
				return &definitionTarget{
					context: DefContextPayee,
					name:    payee,
				}
			}
		}

		for j := range tx.Postings {
			p := &tx.Postings[j]

			accountRange := computeAccountRange(&p.Account)
			if positionInRange(pos, accountRange) {
				return &definitionTarget{
					context: DefContextAccount,
					name:    p.Account.Name,
				}
			}

			if p.Amount != nil && p.Amount.Commodity.Symbol != "" {
				if positionInRange(pos, p.Amount.Commodity.Range) {
					return &definitionTarget{
						context: DefContextCommodity,
						name:    p.Amount.Commodity.Symbol,
					}
				}
			}
		}
	}

	return nil
}

func findDefinitionLocation(target *definitionTarget, resolved *include.ResolvedJournal, currentPath string, currentJournal *ast.Journal) *protocol.Location {
	switch target.context {
	case DefContextAccount:
		return findAccountDefinitionResolved(target.name, resolved, currentPath, currentJournal)
	case DefContextCommodity:
		return findCommodityDefinitionResolved(target.name, resolved, currentPath, currentJournal)
	case DefContextPayee:
		return findPayeeDefinitionResolved(target.name, resolved, currentPath, currentJournal)
	default:
		return nil
	}
}

func findAccountDefinitionResolved(name string, resolved *include.ResolvedJournal, currentPath string, currentJournal *ast.Journal) *protocol.Location {
	journals := allJournalsWithPaths(resolved, currentPath, currentJournal)

	for filePath, journal := range journals {
		for _, dir := range journal.Directives {
			if ad, ok := dir.(ast.AccountDirective); ok {
				if ad.Account.Name == name {
					return &protocol.Location{
						URI:   pathToURI(filePath),
						Range: *astRangeToProtocol(ad.Range),
					}
				}
			}
		}
	}

	return findFirstAccountUsageResolved(name, journals)
}

func findFirstAccountUsageResolved(name string, journals map[string]*ast.Journal) *protocol.Location {
	var earliest *protocol.Location
	var earliestDate *ast.Date

	for filePath, journal := range journals {
		for i := range journal.Transactions {
			tx := &journal.Transactions[i]
			for j := range tx.Postings {
				p := &tx.Postings[j]
				if p.Account.Name == name {
					if earliestDate == nil || compareDates(tx.Date, *earliestDate) < 0 {
						earliestDate = &tx.Date
						earliest = &protocol.Location{
							URI:   pathToURI(filePath),
							Range: *astRangeToProtocol(computeAccountRange(&p.Account)),
						}
					}
				}
			}
		}
	}

	return earliest
}

func findCommodityDefinitionResolved(symbol string, resolved *include.ResolvedJournal, currentPath string, currentJournal *ast.Journal) *protocol.Location {
	journals := allJournalsWithPaths(resolved, currentPath, currentJournal)

	for filePath, journal := range journals {
		for _, dir := range journal.Directives {
			if cd, ok := dir.(ast.CommodityDirective); ok {
				if cd.Commodity.Symbol == symbol {
					return &protocol.Location{
						URI:   pathToURI(filePath),
						Range: *astRangeToProtocol(cd.Range),
					}
				}
			}
		}
	}

	return findFirstCommodityUsageResolved(symbol, journals)
}

func findFirstCommodityUsageResolved(symbol string, journals map[string]*ast.Journal) *protocol.Location {
	var earliest *protocol.Location
	var earliestDate *ast.Date

	for filePath, journal := range journals {
		for i := range journal.Transactions {
			tx := &journal.Transactions[i]
			for j := range tx.Postings {
				p := &tx.Postings[j]
				if p.Amount != nil && p.Amount.Commodity.Symbol == symbol {
					if earliestDate == nil || compareDates(tx.Date, *earliestDate) < 0 {
						earliestDate = &tx.Date
						earliest = &protocol.Location{
							URI:   pathToURI(filePath),
							Range: *astRangeToProtocol(p.Amount.Commodity.Range),
						}
					}
				}
			}
		}
	}

	return earliest
}

func findPayeeDefinitionResolved(payee string, resolved *include.ResolvedJournal, currentPath string, currentJournal *ast.Journal) *protocol.Location {
	journals := allJournalsWithPaths(resolved, currentPath, currentJournal)

	var earliest *protocol.Location
	var earliestDate *ast.Date

	for filePath, journal := range journals {
		for i := range journal.Transactions {
			tx := &journal.Transactions[i]
			txPayee := getPayeeOrDescription(tx)
			if txPayee == payee {
				if earliestDate == nil || compareDates(tx.Date, *earliestDate) < 0 {
					earliestDate = &tx.Date
					earliest = &protocol.Location{
						URI:   pathToURI(filePath),
						Range: *astRangeToProtocol(tx.Range),
					}
				}
			}
		}
	}

	return earliest
}

func allJournalsWithPaths(resolved *include.ResolvedJournal, currentPath string, currentJournal *ast.Journal) map[string]*ast.Journal {
	result := make(map[string]*ast.Journal)

	if resolved != nil {
		for path, journal := range resolved.Files {
			result[path] = journal
		}
		if resolved.Primary != nil && currentPath != "" {
			result[currentPath] = resolved.Primary
		}
	} else if currentJournal != nil && currentPath != "" {
		result[currentPath] = currentJournal
	}

	return result
}

func pathToURI(path string) protocol.DocumentURI {
	return uri.File(path)
}

func compareDates(a, b ast.Date) int {
	if a.Year != b.Year {
		return a.Year - b.Year
	}
	if a.Month != b.Month {
		return a.Month - b.Month
	}
	return a.Day - b.Day
}
