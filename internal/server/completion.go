package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/lsputil"
	"github.com/juev/hledger-lsp/internal/parser"
)

type CompletionContextType int

const (
	ContextUnknown CompletionContextType = iota
	ContextAccount
	ContextPayee
	ContextCommodity
	ContextTagName
	ContextTagValue
	ContextDate
)

func (s *Server) Completion(ctx context.Context, params *protocol.CompletionParams) (*protocol.CompletionList, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return &protocol.CompletionList{Items: []protocol.CompletionItem{}}, nil
	}

	var result *analyzer.AnalysisResult
	if resolved := s.GetResolved(params.TextDocument.URI); resolved != nil {
		result = s.analyzer.AnalyzeResolved(resolved)
	} else {
		journal, _ := parser.Parse(doc)
		result = s.analyzer.Analyze(journal)
	}

	completionCtx := determineCompletionContext(doc, params.Position, params.Context)
	items := generateCompletionItems(completionCtx, result, doc, params.Position)
	settings := s.getSettings()
	if settings.Completion.MaxResults > 0 && len(items) > settings.Completion.MaxResults {
		items = items[:settings.Completion.MaxResults]
	}

	return &protocol.CompletionList{
		IsIncomplete: false,
		Items:        items,
	}, nil
}

func determineCompletionContext(content string, pos protocol.Position, ctx *protocol.CompletionContext) CompletionContextType {
	lines := strings.Split(content, "\n")
	if int(pos.Line) >= len(lines) {
		return ContextDate
	}

	line := lines[pos.Line]

	if tagCtx := determineTagContext(line, pos); tagCtx != ContextUnknown {
		return tagCtx
	}

	if ctx != nil && ctx.TriggerCharacter == ":" {
		return ContextAccount
	}

	if ctx != nil && (ctx.TriggerCharacter == "@" || ctx.TriggerCharacter == "=") {
		return ContextCommodity
	}

	if line == "" {
		return ContextDate
	}

	if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
		return ContextAccount
	}

	if len(line) > 0 && line[0] >= '0' && line[0] <= '9' {
		return ContextPayee
	}

	return ContextDate
}

func determineTagContext(line string, pos protocol.Position) CompletionContextType {
	semicolonIdx := strings.Index(line, ";")
	if semicolonIdx == -1 {
		return ContextUnknown
	}

	bytePos := lsputil.UTF16OffsetToByteOffset(line, int(pos.Character))
	if bytePos <= semicolonIdx {
		return ContextUnknown
	}

	afterSemicolon := line[semicolonIdx+1:]
	cursorInComment := bytePos - semicolonIdx - 1
	if cursorInComment < 0 || cursorInComment > len(afterSemicolon) {
		cursorInComment = len(afterSemicolon)
	}

	beforeCursor := afterSemicolon[:cursorInComment]

	lastColon := strings.LastIndex(beforeCursor, ":")
	lastComma := strings.LastIndex(beforeCursor, ",")

	if lastColon == -1 {
		return ContextTagName
	}

	if lastComma > lastColon {
		afterComma := strings.TrimSpace(beforeCursor[lastComma+1:])
		if strings.Contains(afterComma, ":") {
			return ContextTagValue
		}
		return ContextTagName
	}

	return ContextTagValue
}

func generateCompletionItems(ctxType CompletionContextType, result *analyzer.AnalysisResult, content string, pos protocol.Position) []protocol.CompletionItem {
	var items []protocol.CompletionItem

	switch ctxType {
	case ContextAccount:
		prefix := extractAccountPrefix(content, pos)
		accounts := getAccountsForPrefix(result.Accounts, prefix)
		for _, acc := range accounts {
			items = append(items, protocol.CompletionItem{
				Label:  acc,
				Kind:   protocol.CompletionItemKindVariable,
				Detail: "Account",
			})
		}

	case ContextPayee:
		for _, payee := range result.Payees {
			item := protocol.CompletionItem{
				Label:  payee,
				Kind:   protocol.CompletionItemKindClass,
				Detail: "Payee",
			}
			if postings, ok := result.PayeeTemplates[payee]; ok && len(postings) > 0 {
				item.InsertText = buildPayeeTemplate(payee, postings)
				item.Detail = "Payee (with template)"
			}
			items = append(items, item)
		}

	case ContextCommodity:
		for _, commodity := range result.Commodities {
			items = append(items, protocol.CompletionItem{
				Label:  commodity,
				Kind:   protocol.CompletionItemKindEnum,
				Detail: "Commodity",
			})
		}

	case ContextTagName:
		for _, tagName := range result.Tags {
			items = append(items, protocol.CompletionItem{
				Label:      tagName,
				Kind:       protocol.CompletionItemKindProperty,
				Detail:     "Tag",
				InsertText: tagName + ":",
			})
		}

	case ContextTagValue:
		lines := strings.Split(content, "\n")
		if int(pos.Line) < len(lines) {
			line := lines[pos.Line]
			tagName := extractCurrentTagName(line, int(pos.Character))
			if values, ok := result.TagValues[tagName]; ok {
				for _, value := range values {
					items = append(items, protocol.CompletionItem{
						Label:  value,
						Kind:   protocol.CompletionItemKindValue,
						Detail: "Tag value for " + tagName,
					})
				}
			}
		}

	case ContextDate:
		items = generateDateCompletionItems(result.Dates)

	default:
		for _, acc := range result.Accounts.All {
			items = append(items, protocol.CompletionItem{
				Label:  acc,
				Kind:   protocol.CompletionItemKindVariable,
				Detail: "Account",
			})
		}
	}

	return items
}

func extractAccountPrefix(content string, pos protocol.Position) string {
	lines := strings.Split(content, "\n")
	if int(pos.Line) >= len(lines) {
		return ""
	}

	line := lines[pos.Line]
	byteCol := lsputil.UTF16OffsetToByteOffset(line, int(pos.Character))
	if byteCol > len(line) {
		byteCol = len(line)
	}

	beforeCursor := strings.TrimSpace(line[:byteCol])

	lastColon := strings.LastIndex(beforeCursor, ":")
	if lastColon == -1 {
		return ""
	}

	start := strings.LastIndexAny(beforeCursor[:lastColon], " \t")
	if start == -1 {
		return beforeCursor[:lastColon+1]
	}
	return beforeCursor[start+1 : lastColon+1]
}

func getAccountsForPrefix(accounts *analyzer.AccountIndex, prefix string) []string {
	if prefix == "" {
		return accounts.All
	}

	if accs, ok := accounts.ByPrefix[prefix]; ok {
		return accs
	}

	return accounts.All
}

func extractCurrentTagName(line string, pos int) string {
	bytePos := lsputil.UTF16OffsetToByteOffset(line, pos)

	semicolonIdx := strings.Index(line, ";")
	if semicolonIdx == -1 || bytePos <= semicolonIdx {
		return ""
	}

	afterSemicolon := line[semicolonIdx+1:]
	cursorInComment := bytePos - semicolonIdx - 1
	if cursorInComment < 0 || cursorInComment > len(afterSemicolon) {
		cursorInComment = len(afterSemicolon)
	}

	beforeCursor := afterSemicolon[:cursorInComment]

	lastColon := strings.LastIndex(beforeCursor, ":")
	if lastColon == -1 {
		return ""
	}

	lastComma := strings.LastIndex(beforeCursor[:lastColon], ",")
	start := lastComma + 1
	tagName := strings.TrimSpace(beforeCursor[start:lastColon])

	return tagName
}

// generateDateCompletionItems creates date suggestions with today/yesterday/tomorrow at top.
// Tests check detail strings ("today" etc.) not specific dates, making them time-independent.
func generateDateCompletionItems(historicalDates []string) []protocol.CompletionItem {
	var items []protocol.CompletionItem
	now := time.Now()

	today := formatDateForCompletion(now)
	yesterday := formatDateForCompletion(now.AddDate(0, 0, -1))
	tomorrow := formatDateForCompletion(now.AddDate(0, 0, 1))

	items = append(items, protocol.CompletionItem{
		Label:    today,
		Kind:     protocol.CompletionItemKindConstant,
		Detail:   "today",
		SortText: "0001",
	})
	items = append(items, protocol.CompletionItem{
		Label:    yesterday,
		Kind:     protocol.CompletionItemKindConstant,
		Detail:   "yesterday",
		SortText: "0002",
	})
	items = append(items, protocol.CompletionItem{
		Label:    tomorrow,
		Kind:     protocol.CompletionItemKindConstant,
		Detail:   "tomorrow",
		SortText: "0003",
	})

	sortedDates := make([]string, len(historicalDates))
	copy(sortedDates, historicalDates)
	sort.Sort(sort.Reverse(sort.StringSlice(sortedDates)))

	seen := map[string]bool{today: true, yesterday: true, tomorrow: true}
	for i, date := range sortedDates {
		if seen[date] {
			continue
		}
		seen[date] = true
		items = append(items, protocol.CompletionItem{
			Label:    date,
			Kind:     protocol.CompletionItemKindConstant,
			Detail:   "from history",
			SortText: fmt.Sprintf("%04d", 100+i),
		})
	}

	return items
}

func formatDateForCompletion(t time.Time) string {
	return fmt.Sprintf("%04d-%02d-%02d", t.Year(), int(t.Month()), t.Day())
}

func buildPayeeTemplate(payee string, postings []analyzer.PostingTemplate) string {
	var sb strings.Builder
	sb.WriteString(payee)
	sb.WriteString("\n")

	for _, p := range postings {
		sb.WriteString("    ")
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
		sb.WriteString("\n")
	}

	return sb.String()
}
