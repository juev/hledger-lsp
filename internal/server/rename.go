package server

import (
	"context"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/parser"
)

func (s *Server) PrepareRename(ctx context.Context, params *protocol.PrepareRenameParams) (*protocol.Range, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	journal, _ := parser.Parse(doc)
	target := findDefinitionTarget(journal, params.Position)
	if target == nil || target.context == DefContextUnknown {
		return nil, nil
	}

	return target.symbolRange, nil
}

func (s *Server) Rename(ctx context.Context, params *protocol.RenameParams) (*protocol.WorkspaceEdit, error) {
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

	locations := findReferences(target, resolved, currentPath, journal, true)
	if len(locations) == 0 {
		return nil, nil
	}

	changes := make(map[protocol.DocumentURI][]protocol.TextEdit)
	for _, loc := range locations {
		changes[loc.URI] = append(changes[loc.URI], protocol.TextEdit{
			Range:   loc.Range,
			NewText: params.NewName,
		})
	}

	return &protocol.WorkspaceEdit{
		Changes: changes,
	}, nil
}
