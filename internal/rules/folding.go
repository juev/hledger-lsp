package rules

import "github.com/juev/hledger-lsp/internal/ast"

const (
	FoldingKindRegion  = "region"
	FoldingKindComment = "comment"
)

// FoldingRange represents a range that can be folded.
type FoldingRange struct {
	StartLine uint32
	EndLine   uint32
	Kind      string // FoldingKindRegion or FoldingKindComment
}

// FoldingRanges returns folding ranges for a parsed rules file.
func FoldingRanges(rf *RulesFile) []FoldingRange {
	if rf == nil {
		return []FoldingRange{}
	}

	ranges := []FoldingRange{}

	regions := make([]ast.Range, 0, len(rf.IfBlocks)+len(rf.IfTables))
	for _, block := range rf.IfBlocks {
		regions = append(regions, block.Range)
	}
	for _, table := range rf.IfTables {
		regions = append(regions, table.Range)
	}
	for _, r := range regions {
		startLine := uint32(max(0, r.Start.Line-1))
		endLine := uint32(max(0, r.End.Line-1))
		if endLine > startLine {
			ranges = append(ranges, FoldingRange{
				StartLine: startLine,
				EndLine:   endLine,
				Kind:      FoldingKindRegion,
			})
		}
	}

	ranges = append(ranges, commentFoldsFromAST(rf)...)

	return ranges
}

// commentFoldsFromAST groups consecutive comment lines from the AST into folding ranges.
func commentFoldsFromAST(rf *RulesFile) []FoldingRange {
	if len(rf.Comments) == 0 {
		return nil
	}

	var ranges []FoldingRange
	i := 0
	for i < len(rf.Comments) {
		startLine := rf.Comments[i].Range.Start.Line
		endLine := startLine

		j := i + 1
		for j < len(rf.Comments) && rf.Comments[j].Range.Start.Line == rf.Comments[j-1].Range.Start.Line+1 {
			endLine = rf.Comments[j].Range.Start.Line
			j++
		}

		if endLine > startLine {
			ranges = append(ranges, FoldingRange{
				StartLine: uint32(startLine - 1),
				EndLine:   uint32(endLine - 1),
				Kind:      FoldingKindComment,
			})
		}

		i = j
	}

	return ranges
}
