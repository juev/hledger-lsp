package server

import (
	"context"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/formatter"
)

func (s *Server) RangeFormat(ctx context.Context, params *protocol.DocumentRangeFormattingParams) ([]protocol.TextEdit, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	journal, _ := s.cachedJournal(params.TextDocument.URI, doc)
	commodityFormats := s.commodityFormatsForDocument(params.TextDocument.URI)

	settings := s.getSettings()
	opts := formatterOptionsFrom(settings.Formatting)

	allEdits := formatter.FormatDocumentWithOptions(journal, doc, commodityFormats, opts)

	filtered := filterEditsByRange(allEdits, params.Range)
	if len(filtered) == 0 {
		return nil, nil
	}
	return filtered, nil
}

func filterEditsByRange(edits []protocol.TextEdit, r protocol.Range) []protocol.TextEdit {
	var result []protocol.TextEdit
	for _, edit := range edits {
		if rangesOverlap(edit.Range, r) {
			result = append(result, edit)
		}
	}
	return result
}

func rangesOverlap(a, b protocol.Range) bool {
	if positionBefore(a.End, b.Start) {
		return false
	}
	if positionBefore(b.End, a.Start) {
		return false
	}
	return true
}

func positionBefore(a, b protocol.Position) bool {
	if a.Line < b.Line {
		return true
	}
	if a.Line == b.Line && a.Character < b.Character {
		return true
	}
	return false
}
