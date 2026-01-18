package server

import (
	"context"
	"encoding/json"
	"strings"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/parser"
)

type InlineCompletionTriggerKind int

const (
	InlineCompletionTriggerInvoked   InlineCompletionTriggerKind = 1
	InlineCompletionTriggerAutomatic InlineCompletionTriggerKind = 2
)

type InlineCompletionParams struct {
	TextDocument protocol.TextDocumentIdentifier `json:"textDocument"`
	Position     protocol.Position               `json:"position"`
	Context      InlineCompletionContext         `json:"context"`
}

type InlineCompletionContext struct {
	TriggerKind InlineCompletionTriggerKind `json:"triggerKind"`
}

type InlineCompletionItem struct {
	InsertText string          `json:"insertText"`
	FilterText string          `json:"filterText,omitempty"`
	Range      *protocol.Range `json:"range,omitempty"`
}

type InlineCompletionList struct {
	Items []InlineCompletionItem `json:"items"`
}

type transactionContext struct {
	InTransaction bool
	PayeeLine     int
	PostingIndex  int
	CurrentPayee  string
	HasTemplate   bool
}

func (s *Server) InlineCompletion(_ context.Context, params json.RawMessage) (*InlineCompletionList, error) {
	var p InlineCompletionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	doc, ok := s.GetDocument(p.TextDocument.URI)
	if !ok {
		return &InlineCompletionList{Items: []InlineCompletionItem{}}, nil
	}

	txCtx := findTransactionContext(doc, int(p.Position.Line))
	if !txCtx.InTransaction {
		return &InlineCompletionList{Items: []InlineCompletionItem{}}, nil
	}

	if txCtx.PostingIndex != 0 {
		return &InlineCompletionList{Items: []InlineCompletionItem{}}, nil
	}

	lines := strings.Split(doc, "\n")
	var currentLineIndent uint32
	if int(p.Position.Line) < len(lines) {
		currentLine := lines[p.Position.Line]
		trimmed := strings.TrimLeft(currentLine, " \t")
		if trimmed != "" {
			return &InlineCompletionList{Items: []InlineCompletionItem{}}, nil
		}
		currentLineIndent = uint32(getLineIndentation(currentLine))
	}

	if txCtx.CurrentPayee == "" {
		return &InlineCompletionList{Items: []InlineCompletionItem{}}, nil
	}

	var result *analyzer.AnalysisResult
	if resolved := s.getWorkspaceResolved(p.TextDocument.URI); resolved != nil {
		result = s.analyzer.AnalyzeResolved(resolved)
	} else {
		journal, _ := parser.Parse(doc)
		result = s.analyzer.Analyze(journal)
	}

	postings, ok := result.PayeeTemplates[txCtx.CurrentPayee]
	if !ok || len(postings) == 0 {
		return &InlineCompletionList{Items: []InlineCompletionItem{}}, nil
	}

	template := buildInlineTemplate(postings)

	return &InlineCompletionList{
		Items: []InlineCompletionItem{
			{
				InsertText: template,
				Range: &protocol.Range{
					Start: protocol.Position{Line: p.Position.Line, Character: currentLineIndent},
					End:   protocol.Position{Line: p.Position.Line, Character: p.Position.Character},
				},
			},
		},
	}, nil
}

func getLineIndentation(line string) int {
	for i, c := range line {
		if c != ' ' && c != '\t' {
			return i
		}
	}
	return len(line)
}

func isLineIndented(line string) bool {
	if len(line) == 0 {
		return false
	}
	if line[0] == '\t' {
		return true
	}
	if len(line) >= 2 && line[0] == ' ' && line[1] == ' ' {
		return true
	}
	return false
}

func findTransactionContext(content string, currentLine int) transactionContext {
	lines := strings.Split(content, "\n")
	ctx := transactionContext{
		InTransaction: false,
		PayeeLine:     -1,
		PostingIndex:  -1,
	}

	if currentLine >= len(lines) {
		return ctx
	}

	for i := currentLine; i >= 0; i-- {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		isIndented := isLineIndented(line)

		if trimmed == "" {
			if !isIndented && i != currentLine {
				return ctx
			}
			continue
		}

		if len(trimmed) > 0 && trimmed[0] >= '0' && trimmed[0] <= '9' {
			ctx.InTransaction = true
			ctx.PayeeLine = i
			ctx.CurrentPayee = extractPayeeFromLine(trimmed)
			ctx.PostingIndex = countPostingsBetween(lines, i+1, currentLine)
			return ctx
		}

		if !isIndented {
			return ctx
		}
	}

	return ctx
}

func extractPayeeFromLine(line string) string {
	spaceIdx := strings.Index(line, " ")
	if spaceIdx == -1 {
		return ""
	}

	afterDate := strings.TrimSpace(line[spaceIdx+1:])

	if len(afterDate) > 0 && (afterDate[0] == '*' || afterDate[0] == '!') {
		afterDate = strings.TrimSpace(afterDate[1:])
	}

	if commentIdx := strings.Index(afterDate, ";"); commentIdx != -1 {
		afterDate = strings.TrimSpace(afterDate[:commentIdx])
	}

	return afterDate
}

func countPostingsBetween(lines []string, startLine, endLine int) int {
	count := 0
	for i := startLine; i < endLine && i < len(lines); i++ {
		line := lines[i]
		if isLineIndented(line) {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && !strings.HasPrefix(trimmed, ";") {
				count++
			}
		}
	}
	return count
}

func buildInlineTemplate(postings []analyzer.PostingTemplate) string {
	var sb strings.Builder

	for i, p := range postings {
		if i > 0 {
			sb.WriteString("\n    ")
		}

		sb.WriteString(p.Account)

		if p.Amount != "" || p.Commodity != "" {
			sb.WriteString("  ")
			if p.CommodityLeft && p.Commodity != "" {
				sb.WriteString(p.Commodity)
				sb.WriteString(p.Amount)
			} else if p.Amount != "" {
				sb.WriteString(p.Amount)
				if p.Commodity != "" {
					sb.WriteString(" ")
					sb.WriteString(p.Commodity)
				}
			}
		}
	}

	return sb.String()
}
