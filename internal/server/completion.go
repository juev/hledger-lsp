package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/filetype"
	"github.com/juev/hledger-lsp/internal/lsputil"
	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/juev/hledger-lsp/internal/rules"
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
	ContextDirective
)

const (
	directiveAccount      = "account "
	directiveApplyAccount = "apply account "
	directiveCommodity    = "commodity "
)

type directiveDef struct {
	label      string
	insertText string
	detail     string
}

var directiveCompletions = []directiveDef{
	{"account", "account ", "Directive"},
	{"alias", "alias ", "Directive"},
	{"apply account", "apply account ", "Directive"},
	{"comment", "comment\n", "Directive"},
	{"commodity", "commodity ", "Directive"},
	{"D", "D ", "Directive"},
	{"decimal-mark", "decimal-mark ", "Directive"},
	{"end apply account", "end apply account\n", "Directive"},
	{"end comment", "end comment\n", "Directive"},
	{"include", "include ", "Directive"},
	{"P", "P ", "Directive"},
	{"payee", "payee ", "Directive"},
	{"tag", "tag ", "Directive"},
	{"Y", "Y ", "Directive"},
	{"year", "year ", "Directive"},
}

func (s *Server) Completion(ctx context.Context, params *protocol.CompletionParams) (*protocol.CompletionList, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return &protocol.CompletionList{Items: []protocol.CompletionItem{}}, nil
	}

	if filetype.IsRules(string(params.TextDocument.URI)) {
		return s.rulesCompletion(doc, params)
	}

	var result *analyzer.AnalysisResult
	cursorLine := int(params.Position.Line)

	if resolved := s.getWorkspaceResolved(params.TextDocument.URI); resolved != nil {
		filtered := resolvedWithoutTransaction(resolved, cursorLine, params.TextDocument.URI)
		result = s.analyzer.AnalyzeResolved(filtered)
	} else {
		journal, _ := parser.Parse(doc)
		txIdx := findCurrentTransactionIndex(journal.Transactions, cursorLine)
		filtered := journalWithoutTransaction(journal, txIdx)
		result = s.analyzer.Analyze(filtered)
	}

	settings := s.getSettings()
	completionCtx := determineCompletionContext(doc, params.Position, params.Context)
	counts := getCountsForContext(completionCtx, result, settings.Completion)
	items := s.generateCompletionItems(completionCtx, result, doc, params.Position, counts, settings.Completion)
	attachResolveData(items, completionCtx, params.TextDocument.URI)

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

func getCountsForContext(ctxType CompletionContextType, result *analyzer.AnalysisResult, settings completionSettings) map[string]int {
	switch ctxType {
	case ContextAccount:
		return result.AccountCounts
	case ContextPayee:
		if settings.IncludeNotes {
			return result.DescriptionCounts
		}
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

	if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
		return determinePostingContext(line, pos)
	}

	if len(line) > 0 && line[0] >= '0' && line[0] <= '9' {
		wsIdx := indexFirstWhitespace(line)
		if wsIdx != -1 {
			byteCol := lsputil.UTF16OffsetToByteOffset(line, int(pos.Character))
			if byteCol > wsIdx {
				return ContextPayee
			}
		}
		return ContextDate
	}

	if len(line) > 0 && line[0] != ';' && line[0] != '#' && line[0] != '*' && line[0] != '~' && line[0] != '=' {
		return ContextDirective
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
	if relativePos <= parts.prefixEnd {
		return ContextCommodity
	}
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

func findPrefixCommodityEnd(s string) int {
	if len(s) == 0 || isDigitOrSign(s[0]) || s[0] == '(' {
		return 0
	}
	i := 0
	for i < len(s) && !isDigitOrSign(s[i]) && s[i] != ' ' && s[i] != '(' && s[i] != ')' {
		i++
	}
	return i
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
	prefixEnd    int
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
	parts.prefixEnd = findPrefixCommodityEnd(parts.afterAccount)
	parts.amountEnd = findAmountEnd(parts.afterAccount)

	return parts
}

func indexFirstWhitespace(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' {
			return i
		}
	}
	return -1
}

func isDigitOrSign(c byte) bool {
	return (c >= '0' && c <= '9') || c == '-' || c == '+'
}

type commentCursorInfo struct {
	semicolonIdx    int
	afterSemicolon  string
	cursorInComment int
	beforeCursor    string
}

func parseCommentCursor(line string, bytePos int) (commentCursorInfo, bool) {
	semicolonIdx := strings.Index(line, ";")
	if semicolonIdx == -1 || bytePos <= semicolonIdx {
		return commentCursorInfo{}, false
	}

	afterSemicolon := line[semicolonIdx+1:]
	cursorInComment := bytePos - semicolonIdx - 1
	if cursorInComment < 0 || cursorInComment > len(afterSemicolon) {
		cursorInComment = len(afterSemicolon)
	}

	return commentCursorInfo{
		semicolonIdx:    semicolonIdx,
		afterSemicolon:  afterSemicolon,
		cursorInComment: cursorInComment,
		beforeCursor:    afterSemicolon[:cursorInComment],
	}, true
}

func determineTagContext(line string, pos protocol.Position) CompletionContextType {
	bytePos := lsputil.UTF16OffsetToByteOffset(line, int(pos.Character))
	info, ok := parseCommentCursor(line, bytePos)
	if !ok {
		return ContextUnknown
	}

	lastColon := strings.LastIndex(info.beforeCursor, ":")
	lastComma := strings.LastIndex(info.beforeCursor, ",")

	if lastColon == -1 {
		return ContextTagName
	}

	if lastComma > lastColon {
		afterComma := strings.TrimSpace(info.beforeCursor[lastComma+1:])
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
		payees := result.Payees
		if settings.IncludeNotes {
			payees = result.Descriptions
		}
		for _, payee := range payees {
			items = append(items, protocol.CompletionItem{
				Label:  payee,
				Kind:   protocol.CompletionItemKindClass,
				Detail: formatPayeeDetailWithCount(payee, counts, settings.ShowCounts),
			})
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
		typedPrefix := extractDateTypedPrefix(content, pos)
		items = generateDateCompletionItems(result.Dates, content, int(pos.Line), typedPrefix)

	case ContextDirective:
		for _, d := range directiveCompletions {
			items = append(items, protocol.CompletionItem{
				Label:      d.label,
				Kind:       protocol.CompletionItemKindKeyword,
				Detail:     d.detail,
				InsertText: d.insertText,
			})
		}

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

func formatPayeeDetailWithCount(payee string, counts map[string]int, showCounts bool) string {
	if showCounts && counts != nil {
		if count := counts[payee]; count > 0 {
			return fmt.Sprintf("Payee (%d)", count)
		}
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
	info, ok := parseCommentCursor(line, bytePos)
	if !ok {
		return ""
	}

	lastColon := strings.LastIndex(info.beforeCursor, ":")
	if lastColon == -1 {
		return ""
	}

	lastComma := strings.LastIndex(info.beforeCursor[:lastColon], ",")
	start := lastComma + 1
	return strings.TrimSpace(info.beforeCursor[start:lastColon])
}

// generateDateCompletionItems creates date suggestions with today/yesterday/tomorrow at top.
// Tests check detail strings ("today" etc.) not specific dates, making them time-independent.
func generateDateCompletionItems(historicalDates []string, content string, cursorLine int, typedPrefix string) []protocol.CompletionItem {
	var items []protocol.CompletionItem
	now := time.Now()

	format := detectDateFormat(content, cursorLine)
	if override := detectFormatFromTypedText(typedPrefix); override != nil {
		format = *override
	}
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

func detectFormatFromTypedText(typed string) *DateFormat {
	if len(typed) >= 4 && isAllDigits(typed[:4]) {
		sep := "-"
		if len(typed) > 4 {
			sep = string(typed[4])
		}
		return &DateFormat{Separator: sep, HasYear: true, LeadingZeros: true}
	}
	return nil
}

func extractDateTypedPrefix(content string, pos protocol.Position) string {
	lines := strings.Split(content, "\n")
	if int(pos.Line) >= len(lines) {
		return ""
	}
	line := lines[pos.Line]
	byteCol := lsputil.UTF16OffsetToByteOffset(line, int(pos.Character))
	if byteCol > len(line) {
		byteCol = len(line)
	}
	prefix := line[:byteCol]
	if len(prefix) > 0 && prefix[0] >= '0' && prefix[0] <= '9' {
		return prefix
	}
	return ""
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
		wsIdx := indexFirstWhitespace(line[:byteCol])
		if wsIdx != -1 {
			startByte = wsIdx + 1
			for startByte < byteCol && (line[startByte] == ' ' || line[startByte] == '\t' || line[startByte] == '*' || line[startByte] == '!') {
				startByte++
			}
		}
	case ContextTagName:
		info, ok := parseCommentCursor(line, byteCol)
		if !ok {
			return nil
		}
		lastComma := strings.LastIndex(info.beforeCursor, ",")
		if lastComma != -1 {
			seg := info.beforeCursor[lastComma+1:]
			trimmed := strings.TrimLeft(seg, " ")
			startByte = info.semicolonIdx + 1 + lastComma + 1 + (len(seg) - len(trimmed))
		} else {
			trimmed := strings.TrimLeft(info.beforeCursor, " ")
			startByte = info.semicolonIdx + 1 + (info.cursorInComment - len(trimmed))
		}
	case ContextTagValue:
		info, ok := parseCommentCursor(line, byteCol)
		if !ok {
			return nil
		}
		lastColon := strings.LastIndex(info.beforeCursor, ":")
		if lastColon == -1 {
			return nil
		}
		seg := info.beforeCursor[lastColon+1:]
		trimmed := strings.TrimLeft(seg, " ")
		startByte = info.semicolonIdx + 1 + lastColon + 1 + (len(seg) - len(trimmed))
	case ContextDate:
		if len(line) > 0 && line[0] >= '0' && line[0] <= '9' {
			startByte = 0
			break
		}
		return nil
	case ContextDirective:
		startByte = 0
	default:
		return nil
	}

	endByte := findTokenEnd(line, byteCol, ctxType)
	startChar := lsputil.ByteOffsetToUTF16(line, startByte)
	endChar := lsputil.ByteOffsetToUTF16(line, endByte)
	return &protocol.Range{
		Start: protocol.Position{Line: pos.Line, Character: uint32(startChar)},
		End:   protocol.Position{Line: pos.Line, Character: uint32(endChar)},
	}
}

func findTokenEnd(line string, byteCol int, ctxType CompletionContextType) int {
	if byteCol >= len(line) {
		return len(line)
	}

	rest := line[byteCol:]

	switch ctxType {
	case ContextAccount:
		for i := 0; i < len(rest); i++ {
			if rest[i] == '\t' || rest[i] == ';' {
				return byteCol + i
			}
			if i+1 < len(rest) && rest[i] == ' ' && rest[i+1] == ' ' {
				return byteCol + i
			}
		}
	case ContextPayee:
		end := len(rest)
		for i := 0; i < len(rest); i++ {
			if rest[i] == '|' || rest[i] == ';' {
				end = i
				break
			}
		}
		for end > 0 && rest[end-1] == ' ' {
			end--
		}
		return byteCol + end
	case ContextCommodity, ContextDate:
		for i := 0; i < len(rest); i++ {
			if rest[i] == ' ' || rest[i] == '\t' {
				return byteCol + i
			}
		}
	case ContextTagName:
		for i := 0; i < len(rest); i++ {
			if rest[i] == ':' || rest[i] == ',' {
				return byteCol + i
			}
		}
	case ContextTagValue:
		for i := 0; i < len(rest); i++ {
			if rest[i] == ',' {
				return byteCol + i
			}
		}
	case ContextDirective:
		return len(line)
	}

	return len(line)
}

func findCommodityStart(line string, byteCol int) int {
	parts := parsePosting(line)
	if parts.separatorIdx == -1 {
		return byteCol
	}

	afterAccountStart := parts.indent + parts.separatorIdx + parts.skipSpaces
	relativeCol := byteCol - afterAccountStart

	if relativeCol <= parts.prefixEnd {
		return afterAccountStart
	}

	commodityStart := afterAccountStart + parts.amountEnd
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
		wsIdx := indexFirstWhitespace(beforeCursor)
		if wsIdx == -1 {
			return ""
		}
		return strings.TrimLeft(beforeCursor[wsIdx+1:], " \t")

	case ContextCommodity:
		if after, found := strings.CutPrefix(beforeCursor, directiveCommodity); found {
			return after
		}
		parts := parsePosting(line)
		if parts.separatorIdx == -1 {
			return ""
		}
		afterAccountStart := parts.indent + parts.separatorIdx + parts.skipSpaces
		relativeCol := byteCol - afterAccountStart
		isPrefixComplete := parts.prefixEnd > 0 &&
			parts.prefixEnd < len(parts.afterAccount) &&
			isDigitOrSign(parts.afterAccount[parts.prefixEnd])
		if relativeCol <= parts.prefixEnd && !isPrefixComplete {
			if relativeCol <= 0 {
				return ""
			}
			return parts.afterAccount[:relativeCol]
		}
		if parts.amountEnd >= len(parts.afterAccount) {
			return ""
		}
		return strings.TrimLeft(parts.afterAccount[parts.amountEnd:], " ")

	case ContextDate:
		if len(beforeCursor) > 0 && beforeCursor[0] >= '0' && beforeCursor[0] <= '9' {
			return beforeCursor
		}
		return ""

	case ContextTagName:
		info, ok := parseCommentCursor(line, byteCol)
		if !ok {
			return ""
		}
		lastComma := strings.LastIndex(info.beforeCursor, ",")
		if lastComma != -1 {
			return strings.TrimSpace(info.beforeCursor[lastComma+1:])
		}
		return strings.TrimSpace(info.beforeCursor)

	case ContextTagValue:
		info, ok := parseCommentCursor(line, byteCol)
		if !ok {
			return ""
		}
		lastColon := strings.LastIndex(info.beforeCursor, ":")
		if lastColon == -1 {
			return ""
		}
		return strings.TrimSpace(info.beforeCursor[lastColon+1:])

	case ContextDirective:
		return beforeCursor

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

func rulesTextEditRange(line string, lineNum, col int) *protocol.Range {
	byteCol := lsputil.UTF16OffsetToByteOffset(line, col)
	if byteCol > len(line) {
		byteCol = len(line)
	}
	byteStart := byteCol
	for byteStart > 0 && line[byteStart-1] != ' ' && line[byteStart-1] != '\t' {
		byteStart--
	}
	return &protocol.Range{
		Start: protocol.Position{Line: uint32(lineNum), Character: uint32(lsputil.ByteOffsetToUTF16(line, byteStart))},
		End:   protocol.Position{Line: uint32(lineNum), Character: uint32(lsputil.ByteOffsetToUTF16(line, byteCol))},
	}
}

func (s *Server) rulesCompletion(doc string, params *protocol.CompletionParams) (*protocol.CompletionList, error) {
	lines := strings.Split(doc, "\n")
	line := ""
	lineNum := int(params.Position.Line)
	col := int(params.Position.Character)
	if lineNum < len(lines) {
		line = lines[lineNum]
	}

	var workspaceAccounts []string
	if s.workspace != nil {
		snap := s.workspace.IndexSnapshot()
		if snap.Accounts != nil {
			workspaceAccounts = snap.Accounts.All
		}
	}

	// Resolve the transitive include closure so completion can see `fields`
	// declared in included .rules files (issue #24). The primary file's
	// content comes from the editor; child includes go through the loader's
	// ContentGetter (which prefers open documents over disk).
	var resolvedIncludes *rules.ResolvedRules
	if s.rulesLoader != nil {
		if path := uriToPath(params.TextDocument.URI); path != "" {
			resolvedIncludes, _ = s.rulesLoader.LoadFromContent(path, doc)
		}
	}

	rulesItems := rules.Complete(doc, lineNum, col, workspaceAccounts, resolvedIncludes)
	editRange := rulesTextEditRange(line, lineNum, col)

	items := make([]protocol.CompletionItem, len(rulesItems))
	for i, ri := range rulesItems {
		items[i] = protocol.CompletionItem{
			Label:  ri.Label,
			Detail: ri.Detail,
			Kind:   protocol.CompletionItemKind(ri.Kind),
			TextEdit: &protocol.TextEdit{
				Range:   *editRange,
				NewText: ri.Label,
			},
		}
	}

	return &protocol.CompletionList{
		IsIncomplete: true,
		Items:        items,
	}, nil
}
