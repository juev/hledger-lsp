package server

import (
	"context"
	"strings"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/filetype"
	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/juev/hledger-lsp/internal/rules"
)

func (s *Server) FoldingRanges(ctx context.Context, params *protocol.FoldingRangeParams) ([]protocol.FoldingRange, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	if doc == "" {
		return []protocol.FoldingRange{}, nil
	}

	if filetype.IsRules(string(params.TextDocument.URI)) {
		return rulesFoldingRanges(doc), nil
	}

	var ranges []protocol.FoldingRange

	ranges = append(ranges, findTransactionFolds(doc)...)
	ranges = append(ranges, findDirectiveFolds(doc)...)
	ranges = append(ranges, findCommentBlockFolds(doc)...)
	ranges = append(ranges, findCommentDirectiveFolds(doc)...)

	return ranges, nil
}

func rulesFoldingRanges(doc string) []protocol.FoldingRange {
	rf, _ := rules.Parse(doc)
	ruleRanges := rules.FoldingRanges(rf)
	result := make([]protocol.FoldingRange, 0, len(ruleRanges))
	for _, r := range ruleRanges {
		kind := protocol.RegionFoldingRange
		if r.Kind == rules.FoldingKindComment {
			kind = protocol.CommentFoldingRange
		}
		result = append(result, protocol.FoldingRange{
			StartLine: r.StartLine,
			EndLine:   r.EndLine,
			Kind:      kind,
		})
	}
	return result
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

func findCommentDirectiveFolds(content string) []protocol.FoldingRange {
	lines := strings.Split(content, "\n")
	var ranges []protocol.FoldingRange

	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])

		// Match "comment" directive: exactly "comment" or "comment" followed by whitespace
		if trimmed != "comment" && !strings.HasPrefix(trimmed, "comment ") && !strings.HasPrefix(trimmed, "comment\t") {
			continue
		}

		startLine := i
		endLine := len(lines) - 1 // default: unterminated → fold to end of file

		for j := i + 1; j < len(lines); j++ {
			endTrimmed := strings.TrimSpace(lines[j])
			if endTrimmed == "end comment" || strings.HasPrefix(endTrimmed, "end comment ") || strings.HasPrefix(endTrimmed, "end comment\t") {
				endLine = j
				break
			}
		}

		// Skip trailing empty lines for unterminated blocks
		for endLine > startLine && strings.TrimSpace(lines[endLine]) == "" {
			endLine--
		}

		if endLine > startLine {
			ranges = append(ranges, protocol.FoldingRange{
				StartLine: uint32(startLine),
				EndLine:   uint32(endLine),
				Kind:      protocol.CommentFoldingRange,
			})
		}

		i = endLine
	}

	return ranges
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
