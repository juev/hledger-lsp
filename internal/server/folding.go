package server

import (
	"context"
	"strings"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/parser"
)

func (s *Server) FoldingRanges(ctx context.Context, params *protocol.FoldingRangeParams) ([]protocol.FoldingRange, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	if doc == "" {
		return []protocol.FoldingRange{}, nil
	}

	var ranges []protocol.FoldingRange

	ranges = append(ranges, findTransactionFolds(doc)...)
	ranges = append(ranges, findDirectiveFolds(doc)...)
	ranges = append(ranges, findCommentBlockFolds(doc)...)

	return ranges, nil
}

func findTransactionFolds(content string) []protocol.FoldingRange {
	journal, _ := parser.Parse(content)
	var ranges []protocol.FoldingRange

	for i := range journal.Transactions {
		tx := &journal.Transactions[i]

		if len(tx.Postings) == 0 {
			continue
		}

		startLine := uint32(tx.Range.Start.Line - 1)
		endLine := uint32(tx.Range.End.Line - 1)

		if endLine > startLine {
			ranges = append(ranges, protocol.FoldingRange{
				StartLine: startLine,
				EndLine:   endLine,
				Kind:      protocol.RegionFoldingRange,
			})
		}
	}

	return ranges
}

func findDirectiveFolds(content string) []protocol.FoldingRange {
	lines := strings.Split(content, "\n")
	var ranges []protocol.FoldingRange

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		if !isDirectiveLine(line) {
			continue
		}

		startLine := i
		endLine := i

		for j := i + 1; j < len(lines); j++ {
			nextLine := lines[j]
			if strings.HasPrefix(nextLine, " ") || strings.HasPrefix(nextLine, "\t") {
				if strings.TrimSpace(nextLine) != "" {
					endLine = j
				}
			} else {
				break
			}
		}

		if endLine > startLine {
			ranges = append(ranges, protocol.FoldingRange{
				StartLine: uint32(startLine),
				EndLine:   uint32(endLine),
				Kind:      protocol.RegionFoldingRange,
			})
		}
	}

	return ranges
}

func isDirectiveLine(line string) bool {
	directives := []string{
		"account ", "commodity ", "decimal-mark ", "include ", "alias ",
		"payee ", "P ", "D ", "Y ", "tag ",
	}

	trimmed := strings.TrimLeft(line, " \t")
	for _, d := range directives {
		if strings.HasPrefix(trimmed, d) {
			return true
		}
	}
	return false
}

func findCommentBlockFolds(content string) []protocol.FoldingRange {
	lines := strings.Split(content, "\n")
	var ranges []protocol.FoldingRange

	i := 0
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])

		if !strings.HasPrefix(line, ";") && !strings.HasPrefix(line, "#") {
			i++
			continue
		}

		startLine := i
		endLine := i

		for j := i + 1; j < len(lines); j++ {
			nextLine := strings.TrimSpace(lines[j])
			if strings.HasPrefix(nextLine, ";") || strings.HasPrefix(nextLine, "#") {
				endLine = j
			} else {
				break
			}
		}

		if endLine > startLine {
			ranges = append(ranges, protocol.FoldingRange{
				StartLine: uint32(startLine),
				EndLine:   uint32(endLine),
				Kind:      protocol.CommentFoldingRange,
			})
		}

		i = endLine + 1
	}

	return ranges
}
