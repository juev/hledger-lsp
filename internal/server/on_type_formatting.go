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
		if prevEdit := s.formatPreviousPostingLine(doc, params.TextDocument.URI, line-1); prevEdit != nil {
			return []protocol.TextEdit{*prevEdit}, nil
		}
		return nil, nil
	}

	var currentLineLen uint32
	if strings.TrimSpace(currentLineContent) == "" {
		currentLineLen = uint32(lsputil.UTF16Len(currentLineContent))
	}

	edits := make([]protocol.TextEdit, 0, 2)
	if prevEdit := s.formatPreviousPostingLine(doc, params.TextDocument.URI, line-1); prevEdit != nil {
		edits = append(edits, *prevEdit)
	}

	edits = append(edits, protocol.TextEdit{
		Range: protocol.Range{
			Start: protocol.Position{Line: uint32(line), Character: 0},
			End:   protocol.Position{Line: uint32(line), Character: currentLineLen},
		},
		NewText: newIndent,
	})
	return edits, nil
}

func (s *Server) formatPreviousPostingLine(doc string, uri protocol.DocumentURI, line int) *protocol.TextEdit {
	lines := splitLines(doc)
	if line < 0 || line >= len(lines) || classifyLine(lines[line]) != linePosting {
		return nil
	}

	journal, _ := parser.Parse(doc)
	var commodityFormats map[string]formatter.CommodityFormat
	if s.workspace != nil {
		commodityFormats = s.workspace.GetCommodityFormatsForFile(uriToPath(uri))
	}

	settings := s.getSettings()
	opts := formatter.Options{
		IndentSize:            settings.Formatting.IndentSize,
		AlignAmounts:          settings.Formatting.AlignAmounts,
		MinAlignmentColumn:    settings.Formatting.MinAlignmentColumn,
		AmountAlignmentColumn: settings.Formatting.AmountAlignmentColumn,
		AmountAlignmentMode:   settings.Formatting.AmountAlignmentMode,
	}
	for _, edit := range formatter.FormatDocumentWithOptions(journal, doc, commodityFormats, opts) {
		if int(edit.Range.Start.Line) == line {
			if edit.NewText == lines[line] {
				return nil
			}
			lineEdit := edit
			return &lineEdit
		}
	}
	return nil
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
	//
	// alignCol is rune-based (matches lexer.Position.Column units), but
	// params.Position.Character arrives in LSP UTF-16 units. For BMP characters
	// (ASCII, Latin, Cyrillic, CJK) the two are 1:1. For supplementary characters
	// (emoji, codepoint > 0xFFFF) one rune = 2 UTF-16 units, so we must convert
	// the cursor position to runes before comparing with alignCol.
	cursorRune := lsputil.UTF16OffsetToRuneOffset(lines[line], int(params.Position.Character))

	if cursorRune >= alignCol {
		return nil, nil
	}

	spacesNeeded := alignCol - cursorRune

	// TextEdit Range stays in LSP UTF-16 (the inserted ASCII spaces have
	// identical length in runes and UTF-16, so cursor advances correctly).
	return []protocol.TextEdit{{
		Range: protocol.Range{
			Start: protocol.Position{Line: uint32(line), Character: params.Position.Character},
			End:   protocol.Position{Line: uint32(line), Character: params.Position.Character},
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
	// Smart detection: if the file already has hand-aligned amounts, use the
	// most common existing column as the base. This preserves the file's visual
	// layout for new postings via Tab and full document formatting.
	if detected := formatter.DetectExistingAmountColumn(journal.Transactions); detected > alignCol {
		alignCol = detected
	}
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
