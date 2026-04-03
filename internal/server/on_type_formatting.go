package server

import (
	"context"
	"strings"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/formatter"
	"github.com/juev/hledger-lsp/internal/lsputil"
	"github.com/juev/hledger-lsp/internal/parser"
)

type lineKind int

const (
	lineEmpty lineKind = iota
	lineWhitespaceOnly
	lineTransactionHeader
	linePosting
	lineDirective
	lineComment
	lineOther
)

var directiveKeywords = map[string]struct{}{
	"account": {}, "alias": {}, "apply": {}, "assert": {}, "bucket": {}, "capture": {},
	"check": {}, "comment": {}, "commodity": {}, "D": {}, "decimal-mark": {}, "def": {},
	"define": {}, "end": {}, "eval": {}, "expr": {}, "include": {}, "payee": {}, "P": {},
	"tag": {}, "test": {}, "Y": {}, "year": {},
}

func classifyLine(line string) lineKind {
	if len(line) == 0 {
		return lineEmpty
	}

	if strings.TrimSpace(line) == "" {
		return lineWhitespaceOnly
	}

	first := line[0]

	if first == ' ' || first == '\t' {
		return linePosting
	}

	if first == ';' || first == '#' || first == '*' {
		return lineComment
	}

	if first >= '0' && first <= '9' || first == '~' || first == '=' {
		return lineTransactionHeader
	}

	word := line
	if idx := strings.IndexAny(line, " \t"); idx != -1 {
		word = line[:idx]
	}
	if _, ok := directiveKeywords[word]; ok {
		return lineDirective
	}

	return lineOther
}

func (s *Server) OnTypeFormatting(ctx context.Context, params *protocol.DocumentOnTypeFormattingParams) ([]protocol.TextEdit, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	switch params.Ch {
	case "\n":
		return s.onTypeNewline(doc, params)
	case "\t":
		return s.onTypeTab(doc, params)
	default:
		return nil, nil
	}
}

func (s *Server) onTypeNewline(doc string, params *protocol.DocumentOnTypeFormattingParams) ([]protocol.TextEdit, error) {
	line := int(params.Position.Line)
	if line == 0 {
		return nil, nil
	}

	lines := splitLines(doc)
	if line-1 >= len(lines) {
		return nil, nil
	}
	prevLine := lines[line-1]

	settings := s.getSettings()
	indent := strings.Repeat(" ", settings.Formatting.IndentSize)

	kind := classifyLine(prevLine)
	var newIndent string
	switch kind {
	case lineTransactionHeader, linePosting:
		newIndent = indent
	default:
		newIndent = ""
	}

	var currentLineContent string
	if line < len(lines) {
		currentLineContent = lines[line]
	}

	if currentLineContent == newIndent {
		return nil, nil
	}

	var currentLineLen uint32
	if strings.TrimSpace(currentLineContent) == "" {
		currentLineLen = uint32(lsputil.UTF16Len(currentLineContent))
	}

	return []protocol.TextEdit{{
		Range: protocol.Range{
			Start: protocol.Position{Line: uint32(line), Character: 0},
			End:   protocol.Position{Line: uint32(line), Character: currentLineLen},
		},
		NewText: newIndent,
	}}, nil
}

func (s *Server) onTypeTab(doc string, params *protocol.DocumentOnTypeFormattingParams) ([]protocol.TextEdit, error) {
	line := int(params.Position.Line)
	lines := splitLines(doc)

	if line >= len(lines) {
		return nil, nil
	}

	if classifyLine(lines[line]) != linePosting {
		return nil, nil
	}

	alignCol := s.getAlignmentColumn(doc, params.TextDocument.URI)
	if alignCol <= 0 {
		return nil, nil
	}

	// onTypeTab inserts spaces to align cursor to the global alignment column.
	// Note: alignAmount command in VS Code does NOT insert Tab before calling this.
	// params.Position is the current cursor position WITHOUT Tab in the document.
	// We insert spaces at current position (empty range = insertion), not replace Tab.
	cursorChar := int(params.Position.Character)

	if cursorChar >= alignCol {
		return nil, nil
	}

	spacesNeeded := alignCol - cursorChar

	return []protocol.TextEdit{{
		Range: protocol.Range{
			Start: protocol.Position{Line: uint32(line), Character: uint32(cursorChar)},
			End:   protocol.Position{Line: uint32(line), Character: uint32(cursorChar)},
		},
		NewText: strings.Repeat(" ", spacesNeeded),
	}}, nil
}

func (s *Server) getAlignmentColumn(doc string, uri protocol.DocumentURI) int {
	if cached, ok := s.alignmentCache.Load(uri); ok {
		if v, ok := cached.(int); ok {
			return v
		}
	}

	journal, _ := parser.Parse(doc)
	if len(journal.Transactions) == 0 {
		return 0
	}

	settings := s.getSettings()
	alignCol := formatter.CalculateGlobalAlignmentColumnWithIndent(journal.Transactions, settings.Formatting.IndentSize)
	if settings.Formatting.MinAlignmentColumn > 0 && alignCol < settings.Formatting.MinAlignmentColumn-1 {
		alignCol = settings.Formatting.MinAlignmentColumn - 1
	}

	if settings.Formatting.AmountAlignmentMode == "decimal" {
		commodityFormats := formatter.ExtractCommodityFormats(journal.Directives)
		if s.workspace != nil {
			if wf := s.workspace.GetCommodityFormatsForFile(uriToPath(uri)); wf != nil {
				commodityFormats = wf
			}
		}
		decimalCol := formatter.CalculateGlobalDecimalCol(journal.Transactions, commodityFormats, alignCol)
		if decimalCol > 0 {
			alignCol = decimalCol
		}
	}

	s.alignmentCache.Store(uri, alignCol)
	return alignCol
}
