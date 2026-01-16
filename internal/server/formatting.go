package server

import (
	"context"
	"strings"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/lsputil"
)

const defaultAmountColumn = 48

func (s *Server) OnTypeFormatting(ctx context.Context, params *protocol.DocumentOnTypeFormattingParams) ([]protocol.TextEdit, error) {
	switch params.Ch {
	case "\n":
		return s.handleEnterFormatting(params)
	case "\t":
		return s.handleTabFormatting(params)
	default:
		return nil, nil
	}
}

func (s *Server) handleEnterFormatting(params *protocol.DocumentOnTypeFormattingParams) ([]protocol.TextEdit, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	lines := strings.Split(doc, "\n")
	lineNum := int(params.Position.Line)

	if lineNum == 0 || lineNum-1 >= len(lines) {
		return nil, nil
	}

	prevLine := lines[lineNum-1]
	lineCtx := analyzeLineContext(prevLine)

	currentLine := ""
	if lineNum < len(lines) {
		currentLine = lines[lineNum]
	}

	currentWhitespaceLen := len(currentLine) - len(strings.TrimLeft(currentLine, " \t"))
	currentWhitespaceLenUTF16 := lsputil.UTF16Len(currentLine[:currentWhitespaceLen])

	switch lineCtx {
	case lineIsTransactionHeader, lineIsPostingWithoutAmount:
		return replaceIndentEdit(params.Position, params.Options, currentWhitespaceLenUTF16), nil
	case lineIsPostingWithAmount:
		if currentWhitespaceLenUTF16 > 0 {
			return removeIndentEdit(params.Position, currentWhitespaceLenUTF16), nil
		}
		return nil, nil
	default:
		return nil, nil
	}
}

func (s *Server) handleTabFormatting(params *protocol.DocumentOnTypeFormattingParams) ([]protocol.TextEdit, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	lines := strings.Split(doc, "\n")
	lineNum := int(params.Position.Line)
	charPosUTF16 := int(params.Position.Character)

	if lineNum >= len(lines) {
		return nil, nil
	}

	line := lines[lineNum]

	if !isPostingLine(line) {
		return nil, nil
	}

	lineWithoutTab := strings.TrimSuffix(line, "\t")
	if hasAmount(lineWithoutTab) {
		return nil, nil
	}

	lineLenUTF16 := lsputil.UTF16Len(line)
	if charPosUTF16 != lineLenUTF16 {
		return nil, nil
	}

	accountEndColUTF16 := lsputil.UTF16Len(lineWithoutTab)
	targetCol := calculateAmountColumn(lines)

	if accountEndColUTF16 >= targetCol {
		targetCol = accountEndColUTF16 + 2
	}

	spacesNeeded := targetCol - accountEndColUTF16
	if spacesNeeded < 2 {
		spacesNeeded = 2
	}

	return []protocol.TextEdit{{
		Range: protocol.Range{
			Start: protocol.Position{Line: params.Position.Line, Character: uint32(charPosUTF16 - 1)},
			End:   protocol.Position{Line: params.Position.Line, Character: uint32(charPosUTF16)},
		},
		NewText: strings.Repeat(" ", spacesNeeded),
	}}, nil
}

func isPostingLine(line string) bool {
	return strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
}

func hasAmount(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	return strings.Contains(trimmed, "  ")
}

func calculateAmountColumn(lines []string) int {
	maxAmountCol := 0
	for _, line := range lines {
		if !isPostingLine(line) {
			continue
		}
		trimmed := strings.TrimLeft(line, " \t")
		indentUTF16 := lsputil.UTF16Len(line) - lsputil.UTF16Len(trimmed)

		idx := strings.Index(trimmed, "  ")
		if idx <= 0 {
			continue
		}

		rest := trimmed[idx:]
		amountStart := -1
		for i, ch := range rest {
			if ch != ' ' {
				amountStart = i
				break
			}
		}

		if amountStart > 0 {
			prefixBeforeAmount := trimmed[:idx+amountStart]
			col := indentUTF16 + lsputil.UTF16Len(prefixBeforeAmount)
			if col > maxAmountCol {
				maxAmountCol = col
			}
		}
	}
	if maxAmountCol == 0 {
		return defaultAmountColumn
	}
	return maxAmountCol
}

type lineContext int

const (
	lineIsOther lineContext = iota
	lineIsEmpty
	lineIsTransactionHeader
	lineIsPostingWithAmount
	lineIsPostingWithoutAmount
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
		if hasAmount(line) {
			return lineIsPostingWithAmount
		}
		return lineIsPostingWithoutAmount
	}
	return lineIsOther
}

func startsWithDate(line string) bool {
	if len(line) < 5 {
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

func replaceIndentEdit(pos protocol.Position, opts protocol.FormattingOptions, existingWhitespaceLen int) []protocol.TextEdit {
	indent := strings.Repeat(" ", int(opts.TabSize))
	if !opts.InsertSpaces {
		indent = "\t"
	}
	return []protocol.TextEdit{{
		Range: protocol.Range{
			Start: protocol.Position{Line: pos.Line, Character: 0},
			End:   protocol.Position{Line: pos.Line, Character: uint32(existingWhitespaceLen)},
		},
		NewText: indent,
	}}
}

func removeIndentEdit(pos protocol.Position, existingWhitespaceLen int) []protocol.TextEdit {
	return []protocol.TextEdit{{
		Range: protocol.Range{
			Start: protocol.Position{Line: pos.Line, Character: 0},
			End:   protocol.Position{Line: pos.Line, Character: uint32(existingWhitespaceLen)},
		},
		NewText: "",
	}}
}
