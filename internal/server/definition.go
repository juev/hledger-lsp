package server

import (
	"context"
	"sort"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/lsputil"
)

type DefinitionContext int

const (
	DefContextUnknown DefinitionContext = iota
	DefContextAccount
	DefContextCommodity
	DefContextPayee
)

type definitionTarget struct {
	context     DefinitionContext
	name        string
	symbolRange *protocol.Range
}

func (s *Server) definition(_ context.Context, params *protocol.DefinitionParams) ([]protocol.Location, error) {
	doc, ok := s.getJournalDoc(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	journal, _ := s.cachedJournal(params.TextDocument.URI, doc)

	mapper := lsputil.NewPositionMapper(doc)
	target := findDefinitionTarget(mapper, journal, params.Position)
	if target == nil || target.context == DefContextUnknown {
		return nil, nil
	}

	resolved := s.getWorkspaceResolved(params.TextDocument.URI)
	currentPath := uriToPath(params.TextDocument.URI)

	location := findDefinitionLocation(target, resolved, currentPath, journal, s.newMapperCache())
	if location == nil {
		return nil, nil
	}

	return []protocol.Location{*location}, nil
}

func findDefinitionTarget(mapper *lsputil.PositionMapper, journal *ast.Journal, pos protocol.Position) *definitionTarget {
	for _, dir := range journal.Directives {
		switch d := dir.(type) {
		case ast.AccountDirective:
			accountRange := ensureRangeEnd(d.Account.Range, d.Account.Name)
			if positionInRange(pos, accountRange) {
				return &definitionTarget{
					context:     DefContextAccount,
					name:        d.Account.Name,
					symbolRange: rangePtr(astRangeToLSP(mapper, accountRange)),
				}
			}
		case ast.CommodityDirective:
			if d.Commodity.Symbol != "" {
				commodityRange := ensureRangeEnd(d.Commodity.Range, d.Commodity.Symbol)
				if positionInRange(pos, commodityRange) {
					return &definitionTarget{
						context:     DefContextCommodity,
						name:        d.Commodity.Symbol,
						symbolRange: rangePtr(astRangeToLSP(mapper, commodityRange)),
					}
				}
			}
		case ast.PayeeDirective:
			if d.Name != "" && positionInRange(pos, d.Range) {
				return &definitionTarget{
					context:     DefContextPayee,
					name:        d.Name,
					symbolRange: rangePtr(astRangeToLSP(mapper, d.Range)),
				}
			}
		}
	}

	for i := range journal.Transactions {
		tx := &journal.Transactions[i]

		payee := getPayeeOrDescription(tx)
		if payee != "" {
			payeeRange := payeeRange(tx, payee)
			if positionInRange(pos, payeeRange) {
				return &definitionTarget{
					context:     DefContextPayee,
					name:        payee,
					symbolRange: rangePtr(astRangeToLSP(mapper, payeeRange)),
				}
			}
		}

		for j := range tx.Postings {
			p := &tx.Postings[j]

			accountRange := computeAccountRange(&p.Account)
			if positionInRange(pos, accountRange) {
				return &definitionTarget{
					context:     DefContextAccount,
					name:        p.Account.Name,
					symbolRange: rangePtr(astRangeToLSP(mapper, accountRange)),
				}
			}

			if p.Amount != nil && p.Amount.Commodity.Symbol != "" {
				if positionInRange(pos, p.Amount.Commodity.Range) {
					return &definitionTarget{
						context:     DefContextCommodity,
						name:        p.Amount.Commodity.Symbol,
						symbolRange: rangePtr(astRangeToLSP(mapper, p.Amount.Commodity.Range)),
					}
				}
			}
		}
	}

	return nil
}

func findDefinitionLocation(target *definitionTarget, resolved *include.ResolvedJournal, currentPath string, currentJournal *ast.Journal, mappers *mapperCache) *protocol.Location {
	switch target.context {
	case DefContextAccount:
		return findAccountDefinitionResolved(target.name, resolved, currentPath, currentJournal, mappers)
	case DefContextCommodity:
		return findCommodityDefinitionResolved(target.name, resolved, currentPath, currentJournal, mappers)
	case DefContextPayee:
		return findPayeeDefinitionResolved(target.name, resolved, currentPath, currentJournal, mappers)
	default:
		return nil
	}
}

func findAccountDefinitionResolved(name string, resolved *include.ResolvedJournal, currentPath string, currentJournal *ast.Journal, mappers *mapperCache) *protocol.Location {
	journals := allJournalsWithPaths(resolved, currentPath, currentJournal)

	for _, filePath := range sortedJournalPaths(journals) {
		journal := journals[filePath]
		for _, dir := range journal.Directives {
			if ad, ok := dir.(ast.AccountDirective); ok {
				if ad.Account.Name == name {
					return &protocol.Location{
						URI:   pathToURI(filePath),
						Range: mappers.rangeIn(filePath, ad.Range),
					}
				}
			}
		}
	}

	return findFirstAccountUsageResolved(name, journals, mappers)
}

func findFirstAccountUsageResolved(name string, journals map[string]*ast.Journal, mappers *mapperCache) *protocol.Location {
	var earliest *protocol.Location
	var earliestDate *ast.Date

	for _, filePath := range sortedJournalPaths(journals) {
		journal := journals[filePath]
		for i := range journal.Transactions {
			tx := &journal.Transactions[i]
			for j := range tx.Postings {
				p := &tx.Postings[j]
				if p.Account.Name == name {
					if earliestDate == nil || compareDates(tx.Date, *earliestDate) < 0 {
						earliestDate = &tx.Date
						earliest = &protocol.Location{
							URI:   pathToURI(filePath),
							Range: mappers.rangeIn(filePath, computeAccountRange(&p.Account)),
						}
					}
				}
			}
		}
	}

	return earliest
}

func findCommodityDefinitionResolved(symbol string, resolved *include.ResolvedJournal, currentPath string, currentJournal *ast.Journal, mappers *mapperCache) *protocol.Location {
	journals := allJournalsWithPaths(resolved, currentPath, currentJournal)

	for _, filePath := range sortedJournalPaths(journals) {
		journal := journals[filePath]
		for _, dir := range journal.Directives {
			if cd, ok := dir.(ast.CommodityDirective); ok {
				if cd.Commodity.Symbol == symbol {
					return &protocol.Location{
						URI:   pathToURI(filePath),
						Range: mappers.rangeIn(filePath, cd.Range),
					}
				}
			}
		}
	}

	return findFirstCommodityUsageResolved(symbol, journals, mappers)
}

func findFirstCommodityUsageResolved(symbol string, journals map[string]*ast.Journal, mappers *mapperCache) *protocol.Location {
	var earliest *protocol.Location
	var earliestDate *ast.Date

	for _, filePath := range sortedJournalPaths(journals) {
		journal := journals[filePath]
		for i := range journal.Transactions {
			tx := &journal.Transactions[i]
			for j := range tx.Postings {
				p := &tx.Postings[j]
				if p.Amount != nil && p.Amount.Commodity.Symbol == symbol {
					if earliestDate == nil || compareDates(tx.Date, *earliestDate) < 0 {
						earliestDate = &tx.Date
						earliest = &protocol.Location{
							URI:   pathToURI(filePath),
							Range: mappers.rangeIn(filePath, p.Amount.Commodity.Range),
						}
					}
				}
			}
		}
	}

	return earliest
}

func findPayeeDefinitionResolved(payee string, resolved *include.ResolvedJournal, currentPath string, currentJournal *ast.Journal, mappers *mapperCache) *protocol.Location {
	journals := allJournalsWithPaths(resolved, currentPath, currentJournal)

	var earliest *protocol.Location
	var earliestDate *ast.Date

	for _, filePath := range sortedJournalPaths(journals) {
		journal := journals[filePath]
		for i := range journal.Transactions {
			tx := &journal.Transactions[i]
			txPayee := getPayeeOrDescription(tx)
			if txPayee == payee {
				if earliestDate == nil || compareDates(tx.Date, *earliestDate) < 0 {
					earliestDate = &tx.Date
					earliest = &protocol.Location{
						URI:   pathToURI(filePath),
						Range: mappers.rangeIn(filePath, tx.Range),
					}
				}
			}
		}
	}

	return earliest
}

func allJournalsWithPaths(resolved *include.ResolvedJournal, currentPath string, currentJournal *ast.Journal) map[string]*ast.Journal {
	result := make(map[string]*ast.Journal)

	if resolved != nil && len(resolved.Occurrences) > 0 {
		// Occurrence-aware: first occurrence wins for each path since source
		// locations are identical across occurrences of the same file.
		for i := range resolved.Occurrences {
			occ := &resolved.Occurrences[i]
			if occ.Journal != nil {
				if _, exists := result[occ.Path]; !exists {
					result[occ.Path] = occ.Journal
				}
			}
		}
		return result
	}

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

func pathToURI(path string) uri.URI {
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

func sortedJournalPaths(journals map[string]*ast.Journal) []string {
	paths := make([]string, 0, len(journals))
	for path := range journals {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
