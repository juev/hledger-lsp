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

	if s.workspace != nil {
		if wsResolved := s.workspace.GetResolved(); wsResolved != nil {
			result = s.analyzer.AnalyzeResolved(wsResolved)
		}
	}

	if result == nil {
		if resolved := s.GetResolved(params.TextDocument.URI); resolved != nil {
			result = s.analyzer.AnalyzeResolved(resolved)
		} else {
			journal, _ := parser.Parse(doc)
			result = s.analyzer.Analyze(journal)
		}
	}

	completionCtx := determineCompletionContext(doc, params.Position, params.Context)
	counts := getCountsForContext(completionCtx, result)
	items := s.generateCompletionItems(completionCtx, result, doc, params.Position, counts)

	editRange := calculateTextEditRange(doc, params.Position, completionCtx)
	if editRange != nil {
		for i := range items {
			text := items[i].Label
			if items[i].InsertText != "" {
				text = items[i].InsertText
			}
			items[i].TextEdit = &protocol.TextEdit{
				Range:   *editRange,
				NewText: text,
			}
		}
	}

	query := extractQueryText(doc, params.Position, completionCtx)
	scored := filterAndScoreFuzzyMatch(items, query)
	items = rankCompletionItemsByScore(scored, counts, query)

	settings := s.getSettings()
	if settings.Completion.MaxResults > 0 && len(items) > settings.Completion.MaxResults {
		items = items[:settings.Completion.MaxResults]
	}

	return &protocol.CompletionList{
		IsIncomplete: true, // prevents VSCode from caching and re-sorting by fuzzy matching
		Items:        items,
	}, nil
}

func getCountsForContext(ctxType CompletionContextType, result *analyzer.AnalysisResult) map[string]int {
	switch ctxType {
	case ContextAccount:
		return result.AccountCounts
	case ContextPayee:
		return result.PayeeCounts
	case ContextCommodity:
		return result.CommodityCounts
	case ContextTagName:
		return result.TagCounts
	default:
		return nil
	}
}

func rankCompletionItemsByScore(scored []scoredItem, counts map[string]int, query string) []protocol.CompletionItem {
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		countI := 0
		countJ := 0
		if counts != nil {
			countI = counts[scored[i].item.Label]
			countJ = counts[scored[j].item.Label]
		}
		return countI > countJ
	})

	items := make([]protocol.CompletionItem, len(scored))
	for i, s := range scored {
		items[i] = s.item
		items[i].SortText = fmt.Sprintf("%06d_%s", i, s.item.Label)
		items[i].FilterText = query
	}
	return items
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

	if strings.HasPrefix(line, "account ") {
		return ContextAccount
	}
	if strings.HasPrefix(line, "commodity ") {
		return ContextCommodity
	}
	if strings.HasPrefix(line, "apply account ") {
		return ContextAccount
	}

	if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
		return determinePostingContext(line, pos)
	}

	if len(line) > 0 && line[0] >= '0' && line[0] <= '9' {
		return ContextPayee
	}

	return ContextDate
}

func determinePostingContext(line string, pos protocol.Position) CompletionContextType {
	byteCol := lsputil.UTF16OffsetToByteOffset(line, int(pos.Character))
	trimmed := strings.TrimLeft(line, " \t")
	indent := len(line) - len(trimmed)

	posInContent := byteCol - indent
	if posInContent < 0 {
		return ContextAccount
	}

	separatorIdx := findDoublespace(trimmed)
	if separatorIdx == -1 {
		return ContextAccount
	}

	if posInContent <= separatorIdx {
		return ContextAccount
	}

	afterSeparator := trimmed[separatorIdx:]
	afterAccount := strings.TrimLeft(afterSeparator, " ")
	skipSpaces := len(afterSeparator) - len(afterAccount)

	amountEnd := findAmountEnd(afterAccount)
	relativePos := posInContent - separatorIdx - skipSpaces

	if relativePos <= amountEnd {
		return ContextAccount
	}

	return ContextCommodity
}

func findDoublespace(s string) int {
	for i := 0; i < len(s)-1; i++ {
		if s[i] == ' ' && s[i+1] == ' ' {
			return i
		}
	}
	return -1
}

func findAmountEnd(s string) int {
	i := 0
	if i < len(s) && !isDigitOrSign(s[i]) {
		for i < len(s) && !isDigitOrSign(s[i]) && s[i] != ' ' {
			i++
		}
	}
	for i < len(s) && (s[i] == '-' || s[i] == '+') {
		i++
	}
	for i < len(s) && ((s[i] >= '0' && s[i] <= '9') || s[i] == '.' || s[i] == ',' || s[i] == '_') {
		i++
	}
	return i
}

func isDigitOrSign(c byte) bool {
	return (c >= '0' && c <= '9') || c == '-' || c == '+'
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

func (s *Server) generateCompletionItems(ctxType CompletionContextType, result *analyzer.AnalysisResult, content string, pos protocol.Position, counts map[string]int) []protocol.CompletionItem {
	var items []protocol.CompletionItem

	switch ctxType {
	case ContextAccount:
		prefix := extractAccountPrefix(content, pos)
		accounts := getAccountsForPrefix(result.Accounts, prefix)
		for _, acc := range accounts {
			items = append(items, protocol.CompletionItem{
				Label:  acc,
				Kind:   protocol.CompletionItemKindVariable,
				Detail: formatDetailWithCount("Account", acc, counts),
			})
		}

	case ContextPayee:
		for _, payee := range result.Payees {
			hasTemplate := false
			item := protocol.CompletionItem{
				Label: payee,
				Kind:  protocol.CompletionItemKindClass,
			}
			if postings, ok := result.PayeeTemplates[payee]; ok && len(postings) > 0 {
				if s.snippetSupport {
					item.InsertText = buildPayeeSnippet(payee, postings)
					item.InsertTextFormat = protocol.InsertTextFormatSnippet
				} else {
					item.InsertText = buildPayeeTemplate(payee, postings)
				}
				hasTemplate = true
			}
			item.Detail = formatPayeeDetailWithCount(payee, counts, hasTemplate)
			items = append(items, item)
		}

	case ContextCommodity:
		for _, commodity := range result.Commodities {
			items = append(items, protocol.CompletionItem{
				Label:  commodity,
				Kind:   protocol.CompletionItemKindEnum,
				Detail: formatDetailWithCount("Commodity", commodity, counts),
			})
		}

	case ContextTagName:
		for _, tagName := range result.Tags {
			items = append(items, protocol.CompletionItem{
				Label:      tagName,
				Kind:       protocol.CompletionItemKindProperty,
				Detail:     formatDetailWithCount("Tag", tagName, counts),
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
		items = generateDateCompletionItems(result.Dates, content)

	default:
		for _, acc := range result.Accounts.All {
			items = append(items, protocol.CompletionItem{
				Label:  acc,
				Kind:   protocol.CompletionItemKindVariable,
				Detail: formatDetailWithCount("Account", acc, counts),
			})
		}
	}

	return items
}

func formatDetailWithCount(baseDetail, label string, counts map[string]int) string {
	if counts == nil {
		return baseDetail
	}
	count := counts[label]
	if count > 0 {
		return fmt.Sprintf("%s (%d)", baseDetail, count)
	}
	return baseDetail
}

func formatPayeeDetailWithCount(payee string, counts map[string]int, hasTemplate bool) string {
	count := 0
	if counts != nil {
		count = counts[payee]
	}

	if count > 0 && hasTemplate {
		return fmt.Sprintf("Payee (%d) + template", count)
	}
	if count > 0 {
		return fmt.Sprintf("Payee (%d)", count)
	}
	if hasTemplate {
		return "Payee + template"
	}
	return "Payee"
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
func generateDateCompletionItems(historicalDates []string, content string) []protocol.CompletionItem {
	var items []protocol.CompletionItem
	now := time.Now()

	format := detectDateFormat(content)
	today := formatDateWithFormat(now, format)
	yesterday := formatDateWithFormat(now.AddDate(0, 0, -1), format)
	tomorrow := formatDateWithFormat(now.AddDate(0, 0, 1), format)

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

type DateFormat struct {
	Separator    string
	HasYear      bool
	LeadingZeros bool
}

var defaultDateFormat = DateFormat{Separator: "-", HasYear: true, LeadingZeros: true}

func detectDateFormat(content string) DateFormat {
	lines := strings.Split(content, "\n")
	maxLinesToCheck := 100
	for i, line := range lines {
		if i >= maxLinesToCheck {
			break
		}
		trimmed := strings.TrimSpace(line)
		if len(trimmed) < 5 {
			continue
		}

		if trimmed[0] < '0' || trimmed[0] > '9' {
			continue
		}

		if format, ok := parseDateFormat(trimmed); ok {
			return format
		}
	}
	return defaultDateFormat
}

func parseDateFormat(line string) (DateFormat, bool) {
	for _, sep := range []string{"-", "/", "."} {
		if format, ok := tryParseDateWithSep(line, sep); ok {
			return format, true
		}
	}
	return DateFormat{}, false
}

func tryParseDateWithSep(line string, sep string) (DateFormat, bool) {
	parts := strings.SplitN(line, sep, 4)
	if len(parts) < 2 {
		return DateFormat{}, false
	}

	first := parts[0]
	if len(first) == 4 && isAllDigits(first) {
		if len(parts) >= 3 && isAllDigits(parts[1]) && len(parts[2]) >= 2 {
			dayPart := strings.SplitN(parts[2], " ", 2)[0]
			if isAllDigits(dayPart) {
				leadingZeros := len(parts[1]) == 2 && len(dayPart) == 2
				return DateFormat{Separator: sep, HasYear: true, LeadingZeros: leadingZeros}, true
			}
		}
	}

	if len(first) <= 2 && isAllDigits(first) {
		if len(parts) >= 2 && len(parts[1]) >= 2 {
			dayPart := strings.SplitN(parts[1], " ", 2)[0]
			if isAllDigits(dayPart) {
				leadingZeros := len(first) == 2 && len(dayPart) == 2
				return DateFormat{Separator: sep, HasYear: false, LeadingZeros: leadingZeros}, true
			}
		}
	}

	return DateFormat{}, false
}

func isAllDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

func formatDateWithFormat(t time.Time, f DateFormat) string {
	month := int(t.Month())
	day := t.Day()

	var monthStr, dayStr string
	if f.LeadingZeros {
		monthStr = fmt.Sprintf("%02d", month)
		dayStr = fmt.Sprintf("%02d", day)
	} else {
		monthStr = fmt.Sprintf("%d", month)
		dayStr = fmt.Sprintf("%d", day)
	}

	if f.HasYear {
		return fmt.Sprintf("%04d%s%s%s%s", t.Year(), f.Separator, monthStr, f.Separator, dayStr)
	}
	return monthStr + f.Separator + dayStr
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

func escapeSnippetText(s string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"$", "\\$",
		"}", "\\}",
	)
	return replacer.Replace(s)
}

func calculateTextEditRange(content string, pos protocol.Position, ctxType CompletionContextType) *protocol.Range {
	lines := strings.Split(content, "\n")
	if int(pos.Line) >= len(lines) {
		return nil
	}
	line := lines[pos.Line]
	byteCol := lsputil.UTF16OffsetToByteOffset(line, int(pos.Character))
	if byteCol > len(line) {
		byteCol = len(line)
	}

	var startByte int
	switch ctxType {
	case ContextAccount:
		if strings.HasPrefix(line, "account ") {
			startByte = 8
		} else if strings.HasPrefix(line, "apply account ") {
			startByte = 14
		} else {
			trimmed := strings.TrimLeft(line[:byteCol], " \t")
			startByte = byteCol - len(trimmed)
		}
	case ContextCommodity:
		if strings.HasPrefix(line, "commodity ") {
			startByte = 10
		} else {
			startByte = findCommodityStart(line, byteCol)
		}
	case ContextPayee:
		spaceIdx := strings.Index(line[:byteCol], " ")
		if spaceIdx != -1 {
			startByte = spaceIdx + 1
			for startByte < byteCol && (line[startByte] == ' ' || line[startByte] == '*' || line[startByte] == '!') {
				startByte++
			}
		}
	default:
		return nil
	}

	startChar := lsputil.ByteOffsetToUTF16(line, startByte)
	return &protocol.Range{
		Start: protocol.Position{Line: pos.Line, Character: uint32(startChar)},
		End:   pos,
	}
}

func findCommodityStart(line string, byteCol int) int {
	trimmed := strings.TrimLeft(line, " \t")
	indent := len(line) - len(trimmed)

	separatorIdx := findDoublespace(trimmed)
	if separatorIdx == -1 {
		return byteCol
	}

	afterSeparator := trimmed[separatorIdx:]
	afterAccount := strings.TrimLeft(afterSeparator, " ")
	skipSpaces := len(afterSeparator) - len(afterAccount)

	amountEnd := findAmountEnd(afterAccount)
	commodityStart := indent + separatorIdx + skipSpaces + amountEnd

	for commodityStart < len(line) && line[commodityStart] == ' ' {
		commodityStart++
	}

	return commodityStart
}

func extractQueryText(content string, pos protocol.Position, ctxType CompletionContextType) string {
	lines := strings.Split(content, "\n")
	if int(pos.Line) >= len(lines) {
		return ""
	}

	line := lines[pos.Line]
	byteCol := lsputil.UTF16OffsetToByteOffset(line, int(pos.Character))
	if byteCol > len(line) {
		byteCol = len(line)
	}

	beforeCursor := line[:byteCol]

	switch ctxType {
	case ContextAccount:
		if strings.HasPrefix(beforeCursor, "account ") {
			return strings.TrimPrefix(beforeCursor, "account ")
		}
		if strings.HasPrefix(beforeCursor, "apply account ") {
			return strings.TrimPrefix(beforeCursor, "apply account ")
		}
		trimmed := strings.TrimLeft(beforeCursor, " \t")
		return trimmed

	case ContextPayee:
		spaceIdx := strings.Index(beforeCursor, " ")
		if spaceIdx == -1 {
			return ""
		}
		return strings.TrimLeft(beforeCursor[spaceIdx+1:], " ")

	case ContextCommodity:
		if strings.HasPrefix(beforeCursor, "commodity ") {
			return strings.TrimPrefix(beforeCursor, "commodity ")
		}
		trimmed := strings.TrimLeft(beforeCursor, " \t")
		separatorIdx := findDoublespace(trimmed)
		if separatorIdx == -1 {
			return ""
		}
		afterAccount := strings.TrimLeft(trimmed[separatorIdx:], " ")
		amountEnd := findAmountEnd(afterAccount)
		if amountEnd >= len(afterAccount) {
			return ""
		}
		return strings.TrimLeft(afterAccount[amountEnd:], " ")

	default:
		return ""
	}
}

func fuzzyMatch(text, pattern string) bool {
	return fuzzyMatchScore(text, pattern) > 0
}

const (
	fuzzyScoreEmptyPattern     = 1000 // score when pattern is empty (all items match)
	fuzzyScoreBaseMatch        = 10   // base score per matched character
	fuzzyScoreConsecutiveBonus = 5    // bonus increment for consecutive matches
	fuzzyScoreWordBoundary     = 15   // bonus for match at word boundary (after ':' or start)
)

func fuzzyMatchScore(text, pattern string) int {
	if pattern == "" {
		return fuzzyScoreEmptyPattern
	}

	text = strings.ToLower(text)
	pattern = strings.ToLower(pattern)

	textRunes := []rune(text)
	patternRunes := []rune(pattern)

	j := 0
	score := 0
	lastMatchIdx := -1
	consecutiveBonus := 0

	for i := 0; i < len(textRunes) && j < len(patternRunes); i++ {
		if textRunes[i] == patternRunes[j] {
			score += fuzzyScoreBaseMatch

			if lastMatchIdx == i-1 {
				consecutiveBonus += fuzzyScoreConsecutiveBonus
				score += consecutiveBonus
			} else {
				consecutiveBonus = 0
			}

			if i == 0 || textRunes[i-1] == ':' {
				score += fuzzyScoreWordBoundary
			}

			lastMatchIdx = i
			j++
		}
	}

	if j < len(patternRunes) {
		return 0
	}

	return score
}

type scoredItem struct {
	item  protocol.CompletionItem
	score int
}

func filterAndScoreFuzzyMatch(items []protocol.CompletionItem, query string) []scoredItem {
	if query == "" {
		result := make([]scoredItem, len(items))
		for i, item := range items {
			result[i] = scoredItem{item: item, score: fuzzyScoreEmptyPattern}
		}
		return result
	}

	var result []scoredItem
	for _, item := range items {
		if score := fuzzyMatchScore(item.Label, query); score > 0 {
			result = append(result, scoredItem{item: item, score: score})
		}
	}
	return result
}

func buildPayeeSnippet(payee string, postings []analyzer.PostingTemplate) string {
	var sb strings.Builder
	sb.WriteString(payee)
	sb.WriteString("\n")

	tabstop := 1
	for _, p := range postings {
		sb.WriteString("    ")
		sb.WriteString(fmt.Sprintf("${%d:%s}", tabstop, escapeSnippetText(p.Account)))
		tabstop++
		if p.Amount != "" || p.Commodity != "" {
			sb.WriteString("  ")
			var amountStr string
			if p.CommodityLeft && p.Commodity != "" {
				amountStr = p.Commodity + p.Amount
			} else if p.Amount != "" {
				amountStr = p.Amount
				if p.Commodity != "" {
					amountStr += " " + p.Commodity
				}
			}
			sb.WriteString(fmt.Sprintf("${%d:%s}", tabstop, escapeSnippetText(amountStr)))
			tabstop++
		}
		sb.WriteString("\n")
	}
	sb.WriteString("$0")

	return sb.String()
}
