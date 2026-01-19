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

const (
	directiveAccount      = "account "
	directiveApplyAccount = "apply account "
	directiveCommodity    = "commodity "
)

func (s *Server) Completion(ctx context.Context, params *protocol.CompletionParams) (*protocol.CompletionList, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return &protocol.CompletionList{Items: []protocol.CompletionItem{}}, nil
	}

	var result *analyzer.AnalysisResult

	if resolved := s.getWorkspaceResolved(params.TextDocument.URI); resolved != nil {
		result = s.analyzer.AnalyzeResolved(resolved)
	} else {
		journal, _ := parser.Parse(doc)
		result = s.analyzer.Analyze(journal)
	}

	settings := s.getSettings()
	completionCtx := determineCompletionContext(doc, params.Position, params.Context)
	counts := getCountsForContext(completionCtx, result)
	items := s.generateCompletionItems(completionCtx, result, doc, params.Position, counts, settings.Completion)

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
	scored := filterAndScoreFuzzyMatch(items, query, settings.Completion.FuzzyMatching)
	items = rankCompletionItemsByScore(scored, counts, query)

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

	if strings.HasPrefix(line, directiveAccount) {
		return ContextAccount
	}
	if strings.HasPrefix(line, directiveCommodity) {
		return ContextCommodity
	}
	if strings.HasPrefix(line, directiveApplyAccount) {
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
	parts := parsePosting(line)

	posInContent := byteCol - parts.indent
	if posInContent < 0 {
		return ContextAccount
	}

	if parts.separatorIdx == -1 {
		return ContextAccount
	}

	if posInContent <= parts.separatorIdx {
		return ContextAccount
	}

	relativePos := posInContent - parts.separatorIdx - parts.skipSpaces
	if relativePos <= parts.amountEnd {
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
	if i < len(s) && s[i] == '(' {
		i++
	}
	if i < len(s) && !isDigitOrSign(s[i]) {
		for i < len(s) && !isDigitOrSign(s[i]) && s[i] != ' ' && s[i] != ')' {
			i++
		}
	}
	for i < len(s) && (s[i] == '-' || s[i] == '+') {
		i++
	}
	for i < len(s) && ((s[i] >= '0' && s[i] <= '9') || s[i] == '.' || s[i] == ',' || s[i] == '_') {
		i++
	}
	if i < len(s) && s[i] == ')' {
		i++
	}
	return i
}

type postingParts struct {
	indent       int
	account      string
	separatorIdx int
	afterAccount string
	skipSpaces   int
	amountEnd    int
}

func parsePosting(line string) postingParts {
	trimmed := strings.TrimLeft(line, " \t")
	indent := len(line) - len(trimmed)

	parts := postingParts{
		indent:       indent,
		separatorIdx: -1,
	}

	parts.separatorIdx = findDoublespace(trimmed)
	if parts.separatorIdx == -1 {
		parts.account = trimmed
		return parts
	}

	parts.account = trimmed[:parts.separatorIdx]
	afterSeparator := trimmed[parts.separatorIdx:]
	parts.afterAccount = strings.TrimLeft(afterSeparator, " ")
	parts.skipSpaces = len(afterSeparator) - len(parts.afterAccount)
	parts.amountEnd = findAmountEnd(parts.afterAccount)

	return parts
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

func (s *Server) generateCompletionItems(ctxType CompletionContextType, result *analyzer.AnalysisResult, content string, pos protocol.Position, counts map[string]int, settings completionSettings) []protocol.CompletionItem {
	var items []protocol.CompletionItem

	switch ctxType {
	case ContextAccount:
		prefix := extractAccountPrefix(content, pos)
		accounts := getAccountsForPrefix(result.Accounts, prefix)
		for _, acc := range accounts {
			items = append(items, protocol.CompletionItem{
				Label:  acc,
				Kind:   protocol.CompletionItemKindVariable,
				Detail: formatDetailWithCount("Account", acc, counts, settings.ShowCounts),
			})
		}

	case ContextPayee:
		for _, payee := range result.Payees {
			postings := result.PayeeTemplates[payee]
			hasTemplate := len(postings) > 0

			item := protocol.CompletionItem{
				Label:  payee,
				Kind:   protocol.CompletionItemKindClass,
				Detail: formatPayeeDetailWithCount(payee, counts, hasTemplate, settings.ShowCounts),
			}

			if hasTemplate && s.snippetSupport && settings.Snippets {
				formattingSettings := s.getSettings().Formatting
				item.InsertText = buildPayeeSnippetTemplate(payee, postings, formattingSettings.IndentSize)
				item.InsertTextFormat = protocol.InsertTextFormatSnippet
			}

			items = append(items, item)
		}

	case ContextCommodity:
		for _, commodity := range result.Commodities {
			items = append(items, protocol.CompletionItem{
				Label:  commodity,
				Kind:   protocol.CompletionItemKindEnum,
				Detail: formatDetailWithCount("Commodity", commodity, counts, settings.ShowCounts),
			})
		}

	case ContextTagName:
		for _, tagName := range result.Tags {
			items = append(items, protocol.CompletionItem{
				Label:      tagName,
				Kind:       protocol.CompletionItemKindProperty,
				Detail:     formatDetailWithCount("Tag", tagName, counts, settings.ShowCounts),
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
		items = generateDateCompletionItems(result.Dates, content, int(pos.Line))

	default:
		for _, acc := range result.Accounts.All {
			items = append(items, protocol.CompletionItem{
				Label:  acc,
				Kind:   protocol.CompletionItemKindVariable,
				Detail: formatDetailWithCount("Account", acc, counts, settings.ShowCounts),
			})
		}
	}

	return items
}

func formatDetailWithCount(baseDetail, label string, counts map[string]int, showCounts bool) string {
	if !showCounts || counts == nil {
		return baseDetail
	}
	count := counts[label]
	if count > 0 {
		return fmt.Sprintf("%s (%d)", baseDetail, count)
	}
	return baseDetail
}

func formatPayeeDetailWithCount(payee string, counts map[string]int, hasTemplate, showCounts bool) string {
	count := 0
	if showCounts && counts != nil {
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
func generateDateCompletionItems(historicalDates []string, content string, cursorLine int) []protocol.CompletionItem {
	var items []protocol.CompletionItem
	now := time.Now()

	format := detectDateFormat(content, cursorLine)
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
		reformatted := reformatDateString(date, format)
		if seen[reformatted] {
			continue
		}
		seen[reformatted] = true
		items = append(items, protocol.CompletionItem{
			Label:    reformatted,
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

func detectDateFormat(content string, cursorLine int) DateFormat {
	lines := strings.Split(content, "\n")
	maxLinesToCheck := 50

	if cursorLine >= len(lines) {
		cursorLine = len(lines) - 1
	}
	if cursorLine < 0 {
		cursorLine = 0
	}

	for i := cursorLine; i >= 0 && cursorLine-i < maxLinesToCheck; i-- {
		trimmed := strings.TrimSpace(lines[i])
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

	for i := cursorLine + 1; i < len(lines) && i-cursorLine < maxLinesToCheck; i++ {
		trimmed := strings.TrimSpace(lines[i])
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

func reformatDateString(dateStr string, f DateFormat) string {
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return dateStr
	}
	return formatDateWithFormat(t, f)
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
		if strings.HasPrefix(line, directiveAccount) {
			startByte = len(directiveAccount)
		} else if strings.HasPrefix(line, directiveApplyAccount) {
			startByte = len(directiveApplyAccount)
		} else {
			trimmed := strings.TrimLeft(line[:byteCol], " \t")
			startByte = byteCol - len(trimmed)
		}
	case ContextCommodity:
		if strings.HasPrefix(line, directiveCommodity) {
			startByte = len(directiveCommodity)
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
	parts := parsePosting(line)
	if parts.separatorIdx == -1 {
		return byteCol
	}

	commodityStart := parts.indent + parts.separatorIdx + parts.skipSpaces + parts.amountEnd

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
		if after, found := strings.CutPrefix(beforeCursor, directiveAccount); found {
			return after
		}
		if after, found := strings.CutPrefix(beforeCursor, directiveApplyAccount); found {
			return after
		}
		trimmed := strings.TrimLeft(beforeCursor, " \t")
		return trimmed

	case ContextPayee:
		_, after, found := strings.Cut(beforeCursor, " ")
		if !found {
			return ""
		}
		return strings.TrimLeft(after, " ")

	case ContextCommodity:
		if after, found := strings.CutPrefix(beforeCursor, directiveCommodity); found {
			return after
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

func fuzzyMatchScoreBySegments(accountName, pattern string) int {
	if pattern == "" {
		return fuzzyScoreEmptyPattern
	}

	segments := strings.Split(accountName, ":")
	bestScore := 0

	for _, segment := range segments {
		if score := fuzzyMatchScore(segment, pattern); score > bestScore {
			bestScore = score
		}
	}

	return bestScore
}

func filterAndScoreFuzzyMatch(items []protocol.CompletionItem, query string, fuzzyEnabled bool) []scoredItem {
	if query == "" {
		result := make([]scoredItem, len(items))
		for i, item := range items {
			result[i] = scoredItem{item: item, score: fuzzyScoreEmptyPattern}
		}
		return result
	}

	if !fuzzyEnabled {
		return filterByPrefix(items, query)
	}

	queryForSegment := strings.TrimSuffix(query, ":")

	var result []scoredItem
	for _, item := range items {
		if strings.Contains(item.Label, ":") {
			if score := fuzzyMatchScoreBySegments(item.Label, queryForSegment); score > 0 {
				result = append(result, scoredItem{item: item, score: score})
				continue
			}
		}
		if score := fuzzyMatchScore(item.Label, query); score > 0 {
			result = append(result, scoredItem{item: item, score: score})
		}
	}
	return result
}

func filterByPrefix(items []protocol.CompletionItem, query string) []scoredItem {
	queryLower := strings.ToLower(query)
	var result []scoredItem
	for _, item := range items {
		if strings.HasPrefix(strings.ToLower(item.Label), queryLower) {
			result = append(result, scoredItem{item: item, score: fuzzyScoreEmptyPattern})
		}
	}
	return result
}

func buildPayeeSnippetTemplate(payee string, postings []analyzer.PostingTemplate, indentSize int) string {
	var sb strings.Builder

	indent := strings.Repeat(" ", indentSize)

	sb.WriteString(payee)

	tabstopNum := 1
	for i, p := range postings {
		sb.WriteString("\n")
		sb.WriteString(indent)
		sb.WriteString(p.Account)

		if p.Amount != "" || p.Commodity != "" {
			sb.WriteString("  ")
			if p.CommodityLeft && p.Commodity != "" {
				sb.WriteString(p.Commodity)
			}
			sb.WriteString(fmt.Sprintf("${%d:%s}", tabstopNum, p.Amount))
			tabstopNum++
			if !p.CommodityLeft && p.Commodity != "" {
				sb.WriteString(" ")
				sb.WriteString(p.Commodity)
			}
		} else if i < len(postings)-1 {
			sb.WriteString(fmt.Sprintf("  ${%d}", tabstopNum))
			tabstopNum++
		}
	}

	sb.WriteString("$0")
	return sb.String()
}
