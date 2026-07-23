package server

import (
	"context"
	"path/filepath"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/filetype"
	"github.com/juev/hledger-lsp/internal/rules"
)

func (s *Server) DocumentLink(ctx context.Context, params *protocol.DocumentLinkParams) ([]protocol.DocumentLink, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	if doc == "" {
		return []protocol.DocumentLink{}, nil
	}

	currentPath := uriToPath(params.TextDocument.URI)
	currentDir := filepath.Dir(currentPath)

	if filetype.IsRules(string(params.TextDocument.URI)) {
		return rulesDocumentLinks(doc, currentDir), nil
	}

	journal, _ := s.cachedJournal(params.TextDocument.URI, doc)
	if journal == nil || len(journal.Includes) == 0 {
		return []protocol.DocumentLink{}, nil
	}

	var links []protocol.DocumentLink

	for _, inc := range journal.Includes {
		includePath := inc.Path
		if !filepath.IsAbs(includePath) {
			includePath = filepath.Join(currentDir, includePath)
		}
		includePath = filepath.Clean(includePath)

		target := pathToURI(includePath)

		links = append(links, protocol.DocumentLink{
			Range:  *astRangeToProtocol(inc.Range),
			Target: target,
		})
	}

	return links, nil
}

func rulesDocumentLinks(doc, currentDir string) []protocol.DocumentLink {
	rf, _ := rules.Parse(doc)
	ruleLinks := rules.Links(rf, currentDir)
	result := make([]protocol.DocumentLink, 0, len(ruleLinks))
	for _, rl := range ruleLinks {
		target := pathToURI(rl.Path)
		result = append(result, protocol.DocumentLink{
			Range:  *astRangeToProtocol(rl.Range),
			Target: target,
		})
	}
	return result
}
