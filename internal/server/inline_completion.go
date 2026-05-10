package server

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/formatter"
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

	// Compute alignment columns from the current document so ghost text
	// targets the same columns that Format Document would produce. Parsing
	// here is independent of the payee-template path (which is cached and
	// may not reflect the latest content); for inline completion latency,
	// this is the same Parse cost as the cache-miss path of getPayeeTemplates.
	journal, _ := parser.Parse(content)
	commodityFormats := formatter.ExtractCommodityFormats(journal.Directives)
	alignment := formatter.ComputeAlignment(journal, commodityFormats, formatterOptionsFrom(settings.Formatting))

	insertText := buildInlinePostingsText(postings, settings.Formatting, alignment)

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

func buildInlinePostingsText(postings []analyzer.PostingTemplate, formatting formattingSettings, alignment formatter.AlignmentInfo) string {
	var sb strings.Builder
	indent := strings.Repeat(" ", formatting.IndentSize)

	for i, p := range postings {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(indent)
		sb.WriteString(p.Account)

		amountText := renderInlineAmount(p)
		if amountText != "" {
			spaces := inlineAmountSpacesFromAlignment(p, amountText, alignment, formatting, indent)
			sb.WriteString(strings.Repeat(" ", spaces))
			sb.WriteString(amountText)
		}
	}

	return sb.String()
}

func renderInlineAmount(p analyzer.PostingTemplate) string {
	if p.Amount == "" && p.Commodity == "" {
		return ""
	}

	var sb strings.Builder
	if p.CommodityLeft && p.Commodity != "" {
		sb.WriteString(p.Commodity)
	}
	sb.WriteString(p.Amount)
	if !p.CommodityLeft && p.Commodity != "" {
		sb.WriteString(" ")
		sb.WriteString(p.Commodity)
	}
	return sb.String()
}

// inlineAmountSpacesFromAlignment returns the gap between the rendered account
// and the rendered amount such that the amount lands on the same column the
// formatter would produce for this document. Falls back to 2 spaces when
// alignment is disabled or no usable column was computed.
func inlineAmountSpacesFromAlignment(p analyzer.PostingTemplate, amountText string, alignment formatter.AlignmentInfo, formatting formattingSettings, indent string) int {
	const minSpaces = 2
	if !formatting.AlignAmounts {
		return minSpaces
	}

	accountEnd := utf8.RuneCountInString(indent) + utf8.RuneCountInString(p.Account)

	switch formatting.AmountAlignmentMode {
	case "decimal":
		if alignment.DecimalCol > 0 {
			prefix := inlineDecimalPrefix(amountText)
			return max(alignment.DecimalCol-accountEnd-prefix, minSpaces)
		}
	case "left":
		// fall through to AccountCol below
	default:
		// "right" (default): prefer end-column anchoring when available.
		if alignment.AmountEndCol > 0 {
			amountLen := utf8.RuneCountInString(amountText)
			return max(alignment.AmountEndCol-accountEnd-amountLen, minSpaces)
		}
	}

	if alignment.AccountCol > 0 {
		return max(alignment.AccountCol-accountEnd, minSpaces)
	}
	return minSpaces
}

func inlineDecimalPrefix(amountText string) int {
	if idx := strings.Index(amountText, "."); idx >= 0 {
		return utf8.RuneCountInString(amountText[:idx])
	}
	fields := strings.Fields(amountText)
	if len(fields) == 0 {
		return utf8.RuneCountInString(amountText)
	}
	return utf8.RuneCountInString(fields[0])
}
