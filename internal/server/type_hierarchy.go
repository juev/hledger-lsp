package server

import (
	"context"
	"sort"
	"strings"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/lsputil"
	"github.com/juev/hledger-lsp/internal/textutil"
)

type typeHierarchyData struct {
	Account  string  `json:"account"`
	Origin   uri.URI `json:"origin"`
	RootPath string  `json:"rootPath,omitempty"`
}

type typeHierarchyCandidate struct {
	name        string
	uri         uri.URI
	rangeValue  protocol.Range
	declaration bool
}

type typeHierarchyPreparedName struct {
	name     string
	rootPath string
}

type typeHierarchyAccountIdentity struct {
	name   string
	offset int
}

func (s *Server) prepareTypeHierarchy(_ context.Context, params *protocol.TypeHierarchyPrepareParams) ([]protocol.TypeHierarchyItem, error) {
	if params == nil {
		return nil, nil
	}
	doc, ok := s.getJournalDoc(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	doc = textutil.NormalizeLineEndings(doc)
	journal, _ := s.cachedJournal(params.TextDocument.URI, doc)
	mapper := lsputil.NewPositionMapper(doc)
	for _, account := range hierarchyAccounts(journal) {
		itemRange := hierarchyRange(mapper, account)
		if positionInProtocolRange(params.Position, itemRange) {
			names := s.typeHierarchyPreparedNames(params.TextDocument.URI, account)
			items := make([]protocol.TypeHierarchyItem, 0, len(names))
			for _, prepared := range names {
				candidate := typeHierarchyCandidate{name: prepared.name, uri: params.TextDocument.URI, rangeValue: itemRange}
				items = append(items, s.typeHierarchyItem(candidate, params.TextDocument.URI, prepared.rootPath))
			}
			return items, nil
		}
	}
	return nil, nil
}

func (s *Server) typeHierarchyPreparedNames(docURI uri.URI, account ast.Account) []typeHierarchyPreparedName {
	path := uriToPath(docURI)
	resolvedTrees := s.typeHierarchyTrees(docURI)
	if len(resolvedTrees) == 0 {
		resolvedTrees = []typeHierarchyTree{{resolved: s.GetResolved(docURI)}}
	}

	names := make(map[typeHierarchyPreparedName]struct{})
	for _, tree := range resolvedTrees {
		if tree.resolved == nil || len(tree.resolved.Occurrences) == 0 {
			continue
		}
		for _, occurrence := range tree.resolved.Occurrences {
			if occurrence.Path != path || occurrence.Journal == nil {
				continue
			}
			for _, contextual := range hierarchyAccounts(occurrence.Journal) {
				if contextual.Name == account.Name && contextual.Range.Start.Offset == account.Range.Start.Offset {
					names[typeHierarchyPreparedName{name: contextual.GetResolvedName(), rootPath: tree.rootPath}] = struct{}{}
				}
			}
		}
	}
	if len(names) == 0 {
		names[typeHierarchyPreparedName{name: account.GetResolvedName()}] = struct{}{}
	}
	result := make([]typeHierarchyPreparedName, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].name != result[j].name {
			return result[i].name < result[j].name
		}
		return result[i].rootPath < result[j].rootPath
	})
	return result
}

func (s *Server) typeHierarchySupertypes(_ context.Context, params *protocol.TypeHierarchySupertypesParams) ([]protocol.TypeHierarchyItem, error) {
	if params == nil {
		return nil, nil
	}
	data, ok := decodeTypeHierarchyData(params.Item.Data)
	if !ok {
		return nil, nil
	}
	parent, ok := hierarchyParent(data.Account)
	if !ok {
		return nil, nil
	}
	candidates := s.typeHierarchyCandidates(data.Origin, data.RootPath)
	if !hasHierarchyName(candidates, parent) {
		return nil, nil
	}
	candidate := candidateForName(candidates, parent)
	candidate.name = parent
	return []protocol.TypeHierarchyItem{s.typeHierarchyItem(candidate, data.Origin, data.RootPath)}, nil
}

func (s *Server) typeHierarchySubtypes(_ context.Context, params *protocol.TypeHierarchySubtypesParams) ([]protocol.TypeHierarchyItem, error) {
	if params == nil {
		return nil, nil
	}
	data, ok := decodeTypeHierarchyData(params.Item.Data)
	if !ok {
		return nil, nil
	}
	candidates := s.typeHierarchyCandidates(data.Origin, data.RootPath)
	children := make(map[string]struct{})
	for _, candidate := range candidates {
		if child, ok := hierarchyDirectChild(data.Account, candidate.name); ok {
			children[child] = struct{}{}
		}
	}
	if len(children) == 0 || !hasHierarchyName(candidates, data.Account) {
		return nil, nil
	}
	names := make([]string, 0, len(children))
	for name := range children {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]protocol.TypeHierarchyItem, 0, len(names))
	for _, name := range names {
		candidate := candidateForName(candidates, name)
		candidate.name = name
		items = append(items, s.typeHierarchyItem(candidate, data.Origin, data.RootPath))
	}
	return items, nil
}

type typeHierarchyTree struct {
	rootPath string
	resolved *include.ResolvedJournal
}

func (s *Server) typeHierarchyTrees(origin uri.URI) []typeHierarchyTree {
	if s.workspace == nil {
		return nil
	}
	path := uriToPath(origin)
	if path == "" {
		return nil
	}
	trees := s.workspace.GetIncludeTreesForFile(path)
	result := make([]typeHierarchyTree, 0, len(trees))
	for _, tree := range trees {
		result = append(result, typeHierarchyTree{rootPath: tree.RootPath, resolved: tree.Resolved})
	}
	return result
}

func (s *Server) typeHierarchyCandidates(origin uri.URI, rootPath string) []typeHierarchyCandidate {
	var resolved *include.ResolvedJournal
	if rootPath != "" {
		for _, tree := range s.typeHierarchyTrees(origin) {
			if tree.rootPath == rootPath {
				resolved = tree.resolved
				break
			}
		}
		if resolved == nil {
			return nil
		}
	} else {
		resolved = s.getWorkspaceResolved(origin)
	}
	if resolved == nil {
		resolved = s.GetResolved(origin)
	}
	var occurrences []include.JournalOccurrence
	if resolved != nil && len(resolved.Occurrences) > 0 {
		occurrences = resolved.Occurrences
	} else if resolved != nil {
		if resolved.Primary != nil {
			occurrences = append(occurrences, include.JournalOccurrence{Path: uriToPath(origin), Journal: resolved.Primary})
		}
		for _, path := range resolved.FileOrder {
			if journal := resolved.Files[path]; journal != nil {
				occurrences = append(occurrences, include.JournalOccurrence{Path: path, Journal: journal})
			}
		}
	} else if doc, ok := s.GetDocument(origin); ok {
		journal, _ := s.cachedJournal(origin, textutil.NormalizeLineEndings(doc))
		occurrences = append(occurrences, include.JournalOccurrence{Path: uriToPath(origin), Journal: journal})
	}

	var candidates []typeHierarchyCandidate
	mappers := make(map[uri.URI]*lsputil.PositionMapper)
	for _, occurrence := range occurrences {
		if occurrence.Journal == nil {
			continue
		}
		docURI := origin
		if occurrence.Path != "" {
			docURI = pathToURI(occurrence.Path)
		}
		mapper := mappers[docURI]
		if mapper == nil {
			content, ok := s.documentSource(docURI)
			if !ok {
				continue
			}
			mapper = lsputil.NewPositionMapper(content)
			mappers[docURI] = mapper
		}
		declarations := hierarchyDeclarationSet(occurrence.Journal)
		for _, account := range hierarchyAccounts(occurrence.Journal) {
			name := account.GetResolvedName()
			if name == "" {
				continue
			}
			candidates = append(candidates, typeHierarchyCandidate{name: name, uri: docURI, rangeValue: hierarchyRange(mapper, account), declaration: declarations[hierarchyAccountIdentity(account)]})
		}
	}
	return candidates
}

func (s *Server) typeHierarchyItem(candidate typeHierarchyCandidate, origin uri.URI, rootPath string) protocol.TypeHierarchyItem {
	data, _ := protocol.Marshal(typeHierarchyData{Account: candidate.name, Origin: origin, RootPath: rootPath})
	return protocol.TypeHierarchyItem{Name: candidate.name, Kind: protocol.SymbolKindClass, URI: candidate.uri, Range: candidate.rangeValue, SelectionRange: candidate.rangeValue, Data: protocol.LSPAny(data)}
}

func hierarchyAccounts(journal *ast.Journal) []ast.Account {
	if journal == nil {
		return nil
	}
	accounts := make([]ast.Account, 0)
	for _, directive := range journal.Directives {
		if declaration, ok := directive.(ast.AccountDirective); ok {
			accounts = append(accounts, declaration.Account)
		}
	}
	appendPostings := func(postings []ast.Posting) {
		for _, posting := range postings {
			accounts = append(accounts, posting.Account)
		}
	}
	for _, transaction := range journal.Transactions {
		appendPostings(transaction.Postings)
	}
	for _, transaction := range journal.PeriodicTransactions {
		appendPostings(transaction.Postings)
	}
	for _, rule := range journal.AutoPostingRules {
		appendPostings(rule.Postings)
	}
	return accounts
}

func hierarchyDeclarationSet(journal *ast.Journal) map[typeHierarchyAccountIdentity]bool {
	declarations := make(map[typeHierarchyAccountIdentity]bool)
	for _, directive := range journal.Directives {
		if declaration, ok := directive.(ast.AccountDirective); ok {
			declarations[hierarchyAccountIdentity(declaration.Account)] = true
		}
	}
	return declarations
}

func hierarchyAccountIdentity(account ast.Account) typeHierarchyAccountIdentity {
	return typeHierarchyAccountIdentity{name: account.Name, offset: account.Range.Start.Offset}
}

func hierarchyRange(mapper *lsputil.PositionMapper, account ast.Account) protocol.Range {
	start := account.Range.Start.Offset
	return protocol.Range{Start: mapper.ByteToLSP(start), End: mapper.ByteToLSP(start + len(account.Name))}
}

func decodeTypeHierarchyData(raw protocol.LSPAny) (typeHierarchyData, bool) {
	var data typeHierarchyData
	if len(raw) == 0 || protocol.Unmarshal(raw, &data) != nil || data.Account == "" || data.Origin == "" {
		return typeHierarchyData{}, false
	}
	return data, true
}

func hierarchyParent(name string) (string, bool) {
	index := strings.LastIndex(name, ":")
	if index <= 0 {
		return "", false
	}
	return name[:index], true
}

func hierarchyDirectChild(parent, name string) (string, bool) {
	prefix := parent + ":"
	if !strings.HasPrefix(name, prefix) {
		return "", false
	}
	part := strings.SplitN(strings.TrimPrefix(name, prefix), ":", 2)[0]
	if part == "" {
		return "", false
	}
	return prefix + part, true
}

func hasHierarchyName(candidates []typeHierarchyCandidate, name string) bool {
	for _, candidate := range candidates {
		if candidate.name == name || strings.HasPrefix(candidate.name, name+":") {
			return true
		}
	}
	return false
}

func candidateForName(candidates []typeHierarchyCandidate, name string) typeHierarchyCandidate {
	matching := make([]typeHierarchyCandidate, 0)
	for _, candidate := range candidates {
		if candidate.name == name || strings.HasPrefix(candidate.name, name+":") {
			matching = append(matching, candidate)
		}
	}
	sort.SliceStable(matching, func(i, j int) bool {
		iExact := matching[i].name == name
		jExact := matching[j].name == name
		if iExact != jExact {
			return iExact
		}
		if matching[i].declaration != matching[j].declaration {
			return matching[i].declaration
		}
		if matching[i].uri != matching[j].uri {
			return matching[i].uri < matching[j].uri
		}
		if matching[i].rangeValue.Start.Line != matching[j].rangeValue.Start.Line {
			return matching[i].rangeValue.Start.Line < matching[j].rangeValue.Start.Line
		}
		return matching[i].rangeValue.Start.Character < matching[j].rangeValue.Start.Character
	})
	return matching[0]
}
