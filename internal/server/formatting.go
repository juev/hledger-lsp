package server

import (
	"context"
	"strings"

	"go.lsp.dev/protocol"
)

func (s *Server) OnTypeFormatting(ctx context.Context, params *protocol.DocumentOnTypeFormattingParams) ([]protocol.TextEdit, error) {
	if params.Ch != "\n" {
		return nil, nil
	}

	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	lines := strings.Split(doc, "\n")
	lineNum := int(params.Position.Line)

	if lineNum == 0 {
		return nil, nil
	}

	prevLine := lines[lineNum-1]

	ctx_ := analyzeLineContext(prevLine)

	switch ctx_ {
	case lineIsTransactionHeader, lineIsPosting:
		return indentEdit(params.Position, params.Options), nil
	default:
		return nil, nil
	}
}

type lineContext int

const (
	lineIsOther lineContext = iota
	lineIsEmpty
	lineIsTransactionHeader
	lineIsPosting
)

func analyzeLineContext(line string) lineContext {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return lineIsEmpty
	}
	if startsWithDate(trimmed) {
		return lineIsTransactionHeader
	}
	if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
		return lineIsPosting
	}
	return lineIsOther
}

func startsWithDate(line string) bool {
	if len(line) < 8 {
		return false
	}
	for i := range 4 {
		if line[i] < '0' || line[i] > '9' {
			return false
		}
	}
	if line[4] != '-' && line[4] != '/' && line[4] != '.' {
		return false
	}
	return true
}

func indentEdit(pos protocol.Position, opts protocol.FormattingOptions) []protocol.TextEdit {
	indent := strings.Repeat(" ", int(opts.TabSize))
	if !opts.InsertSpaces {
		indent = "\t"
	}
	return []protocol.TextEdit{{
		Range: protocol.Range{
			Start: protocol.Position{Line: pos.Line, Character: 0},
			End:   protocol.Position{Line: pos.Line, Character: 0},
		},
		NewText: indent,
	}}
}
