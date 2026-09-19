package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/filetype"
	"github.com/juev/hledger-lsp/internal/lsputil"
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
	directivePayee        = "payee "
	directiveTag          = "tag "
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

func (s *Server) completion(ctx context.Context, params *protocol.CompletionParams) (*protocol.CompletionList, error) {
	result, err := s.completionWithScope(ctx, params, false)
	if err != nil {
		return nil, err
	}
	return result.CompletionList, nil
}

func (s *Server) completionWithScope(ctx context.Context, params *protocol.CompletionParams, allAccounts bool) (*ScopedCompletionResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	response := &ScopedCompletionResult{CompletionList: &protocol.CompletionList{Items: []protocol.CompletionItem{}}}
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return response, nil
	}

	if filetype.IsRules(string(params.TextDocument.URI)) {
		response.CompletionList = s.rulesCompletion(doc, params)
		return response, nil
	}

	var result *analyzer.AnalysisResult
	var transactions []ast.Transaction
	cursorLine := int(params.Position.Line)

	if resolved := s.getWorkspaceResolved(params.TextDocument.URI); resolved != nil {
		filtered := resolvedWithoutTransaction(resolved, cursorLine, params.TextDocument.URI)
		result = s.analyzer.AnalyzeResolved(filtered)
		transactions = filtered.AllTransactions()
	} else {
		journal, _ := s.cachedJournal(params.TextDocument.URI, doc)
		txIdx := findCurrentTransactionIndex(journal.Transactions, cursorLine)
		filtered := journalWithoutTransaction(journal, txIdx)
		result = s.analyzer.Analyze(filtered)
		transactions = filtered.Transactions
	}

	settings := s.getSettings()
	completionCtx := determineCompletionContext(doc, params.Position)
	displayCounts := getCountsForContext(completionCtx, result, settings.Completion)

	// Ranking counts may be boosted by the payee on the line; the counts shown in
	// the popup must stay the real usage counts.
	rankCounts := displayCounts
	var recency map[string]string
	if completionCtx == ContextAccount {
		if payee := extractPayeeForCompletion(doc, cursorLine); payee != "" {
			rankCounts = payeeBoostedCounts(result, payee, displayCounts)
			recency = payeeRecency(result, payee)
		}
	}

	items := s.generateCompletionItems(completionCtx, result, doc, params.Position, displayCounts, settings.Completion)
	isAccount := completionCtx == ContextAccount || completionCtx == ContextUnknown
	var balances analyzer.AccountBalances
	if isAccount {
		balances = analyzer.CalculateAccountBalancesFromTransactions(transactions)
		// The non-zero filter keeps the list short, but closed or unused accounts
		// stay unreachable without a way to turn it off.
		if !allAccounts && settings.Completion.AccountScope != accountScopeAll {
			items = filterNonzeroAccountCompletions(items, balances)
		}
	}
	attachResolveData(items, completionCtx, params.TextDocument.URI)

	editRange := calculateTextEditRange(doc, params.Position, completionCtx)
	if isAccount {
		response.AccountRange = editRange
	}
	if editRange != nil {
		for i := range items {
			text := items[i].Label
			if insertText, ok := items[i].InsertText.Get(); ok {
				text = insertText
			}
			items[i].TextEdit = &protocol.TextEdit{
				Range:   *editRange,
				NewText: text,
			}
		}
	}

	query := extractQueryText(doc, params.Position, completionCtx)
	scored := filterAndScoreFuzzyMatch(items, query, settings.Completion.FuzzyMatching)
	items = rankCompletionItemsByScore(scored, rankCounts, query, recency)
	if isAccount && allAccounts {
		// Preserve the ordinary ranking within each balance group.
		sort.SliceStable(items, func(i, j int) bool {
			return hasNonzeroAccountBalance(balances[items[i].Label]) && !hasNonzeroAccountBalance(balances[items[j].Label])
		})
		for i := range items {
			items[i].SortText.Set(fmt.Sprintf("%06d_%s", i, items[i].Label))
		}
	}

	if (!isAccount || !allAccounts) && settings.Completion.MaxResults > 0 && len(items) > settings.Completion.MaxResults {
		items = items[:settings.Completion.MaxResults]
	}

	response.CompletionList = &protocol.CompletionList{
		IsIncomplete: true, // prevents VSCode from caching and re-sorting by fuzzy matching
		Items:        items,
	}
	return response, nil
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

func rankCompletionItemsByScore(scored []scoredItem, counts map[string]int, query string, recency map[string]string) []protocol.CompletionItem {
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
		if countI != countJ {
			return countI > countJ
		}
		if recency != nil {
			return recency[scored[i].item.Label] > recency[scored[j].item.Label]
		}
		return false
	})

	items := make([]protocol.CompletionItem, len(scored))
	for i, s := range scored {
		items[i] = s.item
		items[i].SortText.Set(fmt.Sprintf("%06d_%s", i, s.item.Label))
		items[i].FilterText.Set(query)
	}
	return items
}

const payeeBoostMultiplier = 100

func extractPayeeForCompletion(doc string, cursorLine int) string {
	lines := strings.Split(doc, "\n")
	for i := cursorLine; i >= 0; i-- {
		line := lines[i]
		if len(line) == 0 {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			continue
		}
		if isTransactionHeaderLine(line) {
			return extractPayeeFromHeader(line)
		}
		return ""
	}
	return ""
}

func payeeBoostedCounts(result *analyzer.AnalysisResult, payee string, globalCounts map[string]int) map[string]int {
	boosted := make(map[string]int, len(globalCounts))
	for k, v := range globalCounts {
		boosted[k] = v
	}
	prefix := payee + "::"
	for key, count := range result.PayeeAccountPairUsage {
		if account, ok := strings.CutPrefix(key, prefix); ok {
			boosted[account] += count * payeeBoostMultiplier
		}
	}
	return boosted
}

func payeeRecency(result *analyzer.AnalysisResult, payee string) map[string]string {
	recency := make(map[string]string)
	prefix := payee + "::"
	for key, date := range result.PayeeAccountLastUsed {
		if account, ok := strings.CutPrefix(key, prefix); ok {
			recency[account] = date
		}
	}
	return recency
}

func determineCompletionContext(content string, pos protocol.Position) CompletionContextType {
	lines := strings.Split(content, "\n")
	if int(pos.Line) >= len(lines) {
		return ContextDate
	}

	line := lines[pos.Line]

	if tagCtx := determineTagContext(line, pos); tagCtx != ContextUnknown {
		return tagCtx
	}

	if line == "" {
		return ContextDate
	}

	// An indented line is a posting. The trigger character only decides which
	// field of that posting the cursor sits in, so a ':', '=' or '@' inside a
	// transaction header or a directive line cannot open the wrong completion.
	if line[0] == ' ' || line[0] == '\t' {
		return determinePostingContext(line, pos)
	}

	switch {
	case strings.HasPrefix(line, directiveAccount):
		return ContextAccount
	case strings.HasPrefix(line, directiveApplyAccount):
		return ContextAccount
	case strings.HasPrefix(line, directiveCommodity):
		return ContextCommodity
	case strings.HasPrefix(line, directivePayee):
		return ContextPayee
	case strings.HasPrefix(line, directiveTag):
		return ContextTagName
	}

	if isPriceDirectiveLine(line) {
		return determinePriceDirectiveContext(line, pos)
	}

	if line[0] >= '0' && line[0] <= '9' {
		wsIdx := indexFirstWhitespace(line)
		if wsIdx != -1 {
			byteCol := lsputil.UTF16OffsetToByteOffset(line, int(pos.Character))
			if byteCol > wsIdx {
				return ContextPayee
			}
		}
		return ContextDate
	}

	if line[0] != ';' && line[0] != '#' && line[0] != '*' && line[0] != '~' && line[0] != '=' {
		return ContextDirective
	}

	return ContextDate
}

// isPriceDirectiveLine reports whether a line starts a P (price) directive.
// Unlike other directives a P line holds two commodity positions: the priced
// commodity and the price commodity.
func isPriceDirectiveLine(line string) bool {
	if line == "" || line[0] != 'P' {
		return false
	}
	if len(line) > 1 && line[1] != ' ' && line[1] != '\t' {
		return false
	}
	return true
}

// determinePriceDirectiveContext maps the cursor inside a `P DATE [TIME]
// COMMODITY PRICE` line onto a completion context: the keyword, the date, or one
// of the two commodity positions.
func determinePriceDirectiveContext(line string, pos protocol.Position) CompletionContextType {
	byteCol := lsputil.UTF16OffsetToByteOffset(line, int(pos.Character))
	if byteCol > len(line) {
		byteCol = len(line)
	}

	before := line[:byteCol]
	trimmed := strings.TrimRight(before, " \t")
	afterToken := len(before) > len(trimmed)
	fields := strings.Fields(trimmed)

	switch {
	case len(fields) <= 1:
		if afterToken {
			// "P " — the date comes next.
			return ContextDate
		}
		// "P" — the directive keyword is still being typed.
		return ContextDirective
	case len(fields) == 2:
		if afterToken {
			// "P 2024-01-15 " — the priced commodity comes next.
			return ContextCommodity
		}
		// Inside the date token.
		return ContextDate
	default:
		// After the date (and optional time): the priced commodity or the price.
		return ContextCommodity
	}
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

// postingStartOffset returns the byte offset where a posting's account name
// begins: after the indentation, an optional status mark ('*' or '!') followed by
// whitespace, and an optional virtual-posting bracket ('(' or '[').
//
// hledger writes postings as "[STATUS] ACCOUNT AMOUNT" and wraps virtual
// accounts in brackets, so the marker must not become part of the completion
// query or of the range the client replaces.
func postingStartOffset(line string, byteCol int) int {
	if byteCol > len(line) {
		byteCol = len(line)
	}

	offset := 0
	for offset < byteCol && (line[offset] == ' ' || line[offset] == '\t') {
		offset++
	}

	if offset+1 < byteCol && (line[offset] == '*' || line[offset] == '!') &&
		(line[offset+1] == ' ' || line[offset+1] == '\t') {
		offset++
		for offset < byteCol && (line[offset] == ' ' || line[offset] == '\t') {
			offset++
		}
	}

	if offset < byteCol && (line[offset] == '(' || line[offset] == '[') {
		offset++
	}

	return offset
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

// payeeStartOffset returns the byte offset where the payee text of a transaction
// header begins, skipping the date, an optional status mark and an optional
// transaction code (hledger: DATE [STATUS] [(CODE)] DESCRIPTION). Only the bytes
// before byteCol are inspected, so a header the user is still typing works too:
// an unterminated code is left in place, because the cursor is still inside it.
func payeeStartOffset(line string, byteCol int) int {
	if byteCol > len(line) {
		byteCol = len(line)
	}
	head := line[:byteCol]

	wsIdx := indexFirstWhitespace(head)
	if wsIdx == -1 {
		return byteCol
	}

	start := skipSpacesAndTabs(head, wsIdx+1)
	if start < len(head) && (head[start] == '*' || head[start] == '!') {
		start = skipSpacesAndTabs(head, start+1)
	}
	if start < len(head) && head[start] == '(' {
		if closeIdx := strings.IndexByte(head[start:], ')'); closeIdx != -1 {
			start = skipSpacesAndTabs(head, start+closeIdx+1)
		}
	}

	return start
}

func skipSpacesAndTabs(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
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
		applyPrefix := activeAccountPrefix(content, int(pos.Line))

		// The typed prefix is relative inside an apply-account block, while the
		// index is keyed by resolved names.
		lookup := prefix
		if applyPrefix != "" && lookup != "" {
			lookup = applyPrefix + ":" + lookup
		}

		accounts := getAccountsForPrefix(result.Accounts, lookup)
		for _, acc := range accounts {
			item := protocol.CompletionItem{
				Label:  acc,
				Kind:   protocol.CompletionItemKindVariable,
				Detail: protocol.NewOptional(formatDetailWithCount("Account", acc, counts, settings.ShowCounts)),
			}
			// Inside an apply-account block the written form is relative: inserting
			// the resolved name would produce business:business:checking. The label
			// keeps the resolved name so balance filtering, usage counts and
			// matching still work on it.
			if written := relativeAccountName(acc, applyPrefix); written != acc {
				item.InsertText = protocol.NewOptional(written)
			}
			items = append(items, item)
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
				Detail: protocol.NewOptional(formatPayeeDetailWithCount(payee, counts, settings.ShowCounts)),
			})
		}

	case ContextCommodity:
		for _, commodity := range result.Commodities {
			display := commodityDisplaySymbol(commodity)
			items = append(items, protocol.CompletionItem{
				Label: display,
				Kind:  protocol.CompletionItemKindEnum,
				// Filter on the raw symbol so a client-side prefix filter still
				// matches when the inserted text gains quotes.
				FilterText: protocol.NewOptional(commodity),
				Detail:     protocol.NewOptional(formatDetailWithCount("Commodity", commodity, counts, settings.ShowCounts)),
			})
		}

	case ContextTagName:
		for _, tagName := range result.Tags {
			items = append(items, protocol.CompletionItem{
				Label:      tagName,
				Kind:       protocol.CompletionItemKindProperty,
				Detail:     protocol.NewOptional(formatDetailWithCount("Tag", tagName, counts, settings.ShowCounts)),
				InsertText: protocol.NewOptional(tagName + ":"),
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
						Detail: protocol.NewOptional("Tag value for " + tagName),
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
				Detail:     protocol.NewOptional(d.detail),
				InsertText: protocol.NewOptional(d.insertText),
			})
		}

	default:
		for _, acc := range result.Accounts.All {
			items = append(items, protocol.CompletionItem{
				Label:  acc,
				Kind:   protocol.CompletionItemKindVariable,
				Detail: protocol.NewOptional(formatDetailWithCount("Account", acc, counts, settings.ShowCounts)),
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

	// Skip the indentation and any posting marker before looking for the prefix.
	accountStart := postingStartOffset(line, byteCol)
	if accountStart > byteCol {
		accountStart = byteCol
	}
	beforeCursor := strings.TrimSpace(line[accountStart:byteCol])

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

// activeAccountPrefix returns the account prefix introduced by `apply account`
// directives above the cursor line, joined with colons. hledger prepends it to
// every posting account inside the block, so completion must offer the name as it
// is written there rather than the fully resolved name.
func activeAccountPrefix(content string, line int) string {
	lines := strings.Split(content, "\n")
	if line > len(lines) {
		line = len(lines)
	}

	var stack []string
	for i := 0; i < line; i++ {
		trimmed := strings.TrimSpace(lines[i])
		switch {
		case strings.HasPrefix(trimmed, directiveApplyAccount):
			name := strings.TrimSpace(strings.TrimPrefix(trimmed, directiveApplyAccount))
			if comment := strings.Index(name, ";"); comment != -1 {
				name = strings.TrimSpace(name[:comment])
			}
			if name != "" {
				stack = append(stack, name)
			}
		case trimmed == "end apply account":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	return strings.Join(stack, ":")
}

// relativeAccountName strips the active apply-account prefix from a resolved
// account name, falling back to the full name when the account is outside the
// block.
func relativeAccountName(account, applyPrefix string) string {
	if applyPrefix == "" {
		return account
	}
	if relative, ok := strings.CutPrefix(account, applyPrefix+":"); ok && relative != "" {
		return relative
	}
	return account
}

// needsQuoting reports whether a commodity symbol must be written in double
// quotes. hledger accepts a bare symbol only when every rune is a letter, a
// currency symbol, '/' or '_'; spaces, dots, hyphens and digits require quotes.
// Verified against hledger 1.52.4: bare "TEST.A", "ACME-Inc", "123abc" and
// "green apples" fail to load, while quoted forms of any symbol are accepted.
func needsQuoting(symbol string) bool {
	if symbol == "" {
		return false
	}
	for _, r := range symbol {
		if unicode.IsLetter(r) || unicode.Is(unicode.Sc, r) || r == '/' || r == '_' {
			continue
		}
		return true
	}
	return false
}

// commodityDisplaySymbol returns the symbol as it must be written in a journal.
func commodityDisplaySymbol(symbol string) string {
	if needsQuoting(symbol) {
		return "\"" + symbol + "\""
	}
	return symbol
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
		Detail:   protocol.NewOptional("today"),
		SortText: protocol.NewOptional("0001"),
	})
	items = append(items, protocol.CompletionItem{
		Label:    yesterday,
		Kind:     protocol.CompletionItemKindConstant,
		Detail:   protocol.NewOptional("yesterday"),
		SortText: protocol.NewOptional("0002"),
	})
	items = append(items, protocol.CompletionItem{
		Label:    tomorrow,
		Kind:     protocol.CompletionItemKindConstant,
		Detail:   protocol.NewOptional("tomorrow"),
		SortText: protocol.NewOptional("0003"),
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
			Detail:   protocol.NewOptional("from history"),
			SortText: protocol.NewOptional(fmt.Sprintf("%04d", 100+i)),
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
			startByte = postingStartOffset(line, byteCol)
		}
	case ContextCommodity:
		if strings.HasPrefix(line, directiveCommodity) {
			startByte = len(directiveCommodity)
		} else {
			startByte = findCommodityStart(line, byteCol)
		}
	case ContextPayee:
		startByte = payeeStartOffset(line, byteCol)
	case ContextTagName:
		if strings.HasPrefix(line, directiveTag) {
			startByte = len(directiveTag)
			break
		}

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

// commodityOffsetAfterMarker returns the byte offset of the commodity inside the
// text that follows a posting amount. It skips the price or assertion marker and,
// for the total form ("@@ 100 EUR"), the total amount written before the
// commodity. It returns -1 when the text holds no marker.
func commodityOffsetAfterMarker(text string) int {
	offset := 0
	skipSpaces := func() {
		for offset < len(text) && (text[offset] == ' ' || text[offset] == '\t') {
			offset++
		}
	}

	skipSpaces()
	switch {
	case strings.HasPrefix(text[offset:], "@@"):
		offset += 2
	case strings.HasPrefix(text[offset:], "=="):
		offset += 2
	case strings.HasPrefix(text[offset:], "@"), strings.HasPrefix(text[offset:], "="):
		offset++
	default:
		return -1
	}

	skipSpaces()
	amountStart := offset
	for offset < len(text) && (isDigitOrSign(text[offset]) || text[offset] == '.' || text[offset] == ',') {
		offset++
	}
	if offset > amountStart {
		skipSpaces()
	}

	return offset
}

// trimAmountMarker removes a leading price or assertion marker from the text that
// follows an amount, so the completion query is the commodity the user is typing
// rather than "@ $", "@@ 100 " or "= $".
func trimAmountMarker(text string) string {
	if offset := commodityOffsetAfterMarker(text); offset >= 0 {
		return text[offset:]
	}
	return strings.TrimLeft(text, " \t")
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
	// Skip the price or assertion marker: after "@", "@@" or "=" the user is
	// typing a commodity, and the replaced range must cover only that text.
	if offset := commodityOffsetAfterMarker(line[commodityStart:]); offset >= 0 {
		return commodityStart + offset
	}

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
		// Skip the indentation and any posting marker, so "* exp" queries "exp"
		// and "(assets:c" queries "assets:c".
		start := postingStartOffset(line, byteCol)
		if start > byteCol {
			start = byteCol
		}
		return line[start:byteCol]

	case ContextPayee:
		return beforeCursor[payeeStartOffset(line, byteCol):]

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
		return trimAmountMarker(parts.afterAccount[parts.amountEnd:])

	case ContextDate:
		if len(beforeCursor) > 0 && beforeCursor[0] >= '0' && beforeCursor[0] <= '9' {
			return beforeCursor
		}
		return ""

	case ContextTagName:
		if after, found := strings.CutPrefix(beforeCursor, directiveTag); found {
			return after
		}

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

func (s *Server) rulesCompletion(doc string, params *protocol.CompletionParams) *protocol.CompletionList {
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
			Detail: protocol.NewOptional(ri.Detail),
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
	}
}
