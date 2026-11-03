package server

import (
	"context"

	"go.lsp.dev/protocol"
)

func (s *Server) RangeFormat(ctx context.Context, params *protocol.DocumentRangeFormattingParams) ([]protocol.TextEdit, error) {
	// CSV rules and imported data are different formats: journal formatting
	// must not touch them (full-document Format refuses them too).
	if s.isImportDocument(params.TextDocument.URI) {
		return nil, nil
	}

	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	filtered := filterEditsByRange(s.formatEdits(params.TextDocument.URI, doc), params.Range)
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
