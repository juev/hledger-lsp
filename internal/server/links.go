package server

import (
	"context"
	"path/filepath"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/parser"
)

func (s *Server) DocumentLink(ctx context.Context, params *protocol.DocumentLinkParams) ([]protocol.DocumentLink, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	if doc == "" {
		return []protocol.DocumentLink{}, nil
	}

	journal, _ := parser.Parse(doc)
	if journal == nil || len(journal.Includes) == 0 {
		return []protocol.DocumentLink{}, nil
	}

	currentPath := uriToPath(params.TextDocument.URI)
	currentDir := filepath.Dir(currentPath)

	var links []protocol.DocumentLink

	for _, inc := range journal.Includes {
		includePath := inc.Path
		if !filepath.IsAbs(includePath) {
			includePath = filepath.Join(currentDir, includePath)
		}
		includePath = filepath.Clean(includePath)

		target := protocol.DocumentURI("file://" + includePath)

		links = append(links, protocol.DocumentLink{
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      uint32(inc.Range.Start.Line - 1),
					Character: uint32(inc.Range.Start.Column - 1),
				},
				End: protocol.Position{
					Line:      uint32(inc.Range.End.Line - 1),
					Character: uint32(inc.Range.End.Column - 1),
				},
			},
			Target: target,
		})
	}

	return links, nil
}
