package server

import (
	"context"
	"encoding/json"
	"regexp"
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

// dateRegex matches transaction dates:
// - Full: YYYY-MM-DD, YYYY/MM/DD, YYYY.MM.DD (with or without leading zeros)
// - Short: MM-DD, M-D (year inferred from context)
// - Secondary date after = is handled separately by space detection
var dateRegex = regexp.MustCompile(`^(\d{4}[-/\.])?\d{1,2}[-/\.]\d{1,2}`)

func (s *Server) InlineCompletion(_ context.Context, params json.RawMessage) (*InlineCompletionList, error) {
	var p InlineCompletionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	content, ok := s.GetDocument(p.TextDocument.URI)
	if !ok {
		return &InlineCompletionList{Items: []InlineCompletionItem{}}, nil
	}

	settings := s.getSettings()
	if !settings.Features.InlineCompletion {
		return &InlineCompletionList{Items: []InlineCompletionItem{}}, nil
	}

	lines := strings.Split(content, "\n")
	lineNum := int(p.Position.Line)

	if lineNum >= len(lines) || strings.TrimSpace(lines[lineNum]) != "" {
		return &InlineCompletionList{Items: []InlineCompletionItem{}}, nil
	}

	if lineNum == 0 {
		return &InlineCompletionList{Items: []InlineCompletionItem{}}, nil
	}

	prevLine := lines[lineNum-1]
	if !isTransactionHeaderLine(prevLine) {
		return &InlineCompletionList{Items: []InlineCompletionItem{}}, nil
	}

	payee := extractPayeeFromHeader(prevLine)
	if payee == "" {
		return &InlineCompletionList{Items: []InlineCompletionItem{}}, nil
	}

	templates := s.getPayeeTemplates(p.TextDocument.URI, content)
	postings, ok := templates[payee]
	if !ok || len(postings) == 0 {
		return &InlineCompletionList{Items: []InlineCompletionItem{}}, nil
	}

	insertText := buildInlinePostingsText(postings, settings.Formatting.IndentSize)

	item := InlineCompletionItem{
		InsertText: insertText,
		Range: &protocol.Range{
			Start: protocol.Position{Line: p.Position.Line, Character: 0},
			End:   protocol.Position{Line: p.Position.Line, Character: p.Position.Character},
		},
	}

	return &InlineCompletionList{Items: []InlineCompletionItem{item}}, nil
}

func (s *Server) getPayeeTemplates(uri protocol.DocumentURI, content string) map[string][]analyzer.PostingTemplate {
	if cached, ok := s.payeeTemplatesCache.Load(uri); ok {
		if templates, ok := cached.(map[string][]analyzer.PostingTemplate); ok {
			return templates
		}
	}

	var result *analyzer.AnalysisResult
	if resolved := s.getWorkspaceResolved(uri); resolved != nil {
		result = s.analyzer.AnalyzeResolved(resolved)
	} else {
		journal, _ := parser.Parse(content)
		result = s.analyzer.Analyze(journal)
	}

	s.payeeTemplatesCache.Store(uri, result.PayeeTemplates)
	return result.PayeeTemplates
}

func isTransactionHeaderLine(line string) bool {
	if len(line) == 0 {
		return false
	}

	if !dateRegex.MatchString(line) {
		return false
	}

	spaceIdx := strings.Index(line, " ")
	if spaceIdx == -1 {
		return false
	}

	afterDate := strings.TrimSpace(line[spaceIdx:])
	return afterDate != ""
}

func extractPayeeFromHeader(line string) string {
	if len(line) == 0 {
		return ""
	}

	spaceIdx := strings.Index(line, " ")
	if spaceIdx == -1 {
		return ""
	}

	afterDate := strings.TrimSpace(line[spaceIdx:])
	if afterDate == "" {
		return ""
	}

	for len(afterDate) > 0 && (afterDate[0] == '*' || afterDate[0] == '!') {
		afterDate = strings.TrimSpace(afterDate[1:])
	}

	if len(afterDate) > 0 && afterDate[0] == '(' {
		closeIdx := strings.Index(afterDate, ")")
		if closeIdx != -1 {
			afterDate = strings.TrimSpace(afterDate[closeIdx+1:])
		}
	}

	if commentIdx := strings.Index(afterDate, ";"); commentIdx != -1 {
		afterDate = strings.TrimSpace(afterDate[:commentIdx])
	}

	if pipeIdx := strings.Index(afterDate, "|"); pipeIdx != -1 {
		afterDate = strings.TrimSpace(afterDate[:pipeIdx])
	}

	return afterDate
}

func buildInlinePostingsText(postings []analyzer.PostingTemplate, indentSize int) string {
	var sb strings.Builder
	indent := strings.Repeat(" ", indentSize)

	for i, p := range postings {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(indent)
		sb.WriteString(p.Account)

		if p.Amount != "" || p.Commodity != "" {
			sb.WriteString("  ")
			if p.CommodityLeft && p.Commodity != "" {
				sb.WriteString(p.Commodity)
			}
			sb.WriteString(p.Amount)
			if !p.CommodityLeft && p.Commodity != "" {
				sb.WriteString(" ")
				sb.WriteString(p.Commodity)
			}
		}
	}

	return sb.String()
}
