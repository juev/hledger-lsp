package server

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/formatter"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/lsputil"
)

type HoverContext int

const (
	HoverUnknown HoverContext = iota
	HoverAccount
	HoverAmount
	HoverPayee
	HoverCommodity
	HoverDate
	HoverTag
	HoverTagValue
)

type hoverElement struct {
	context     HoverContext
	rng         ast.Range
	account     *ast.Account
	amount      *ast.Amount
	cost        *ast.Cost
	payee       string
	transaction *ast.Transaction
	tagName     string
	tagValue    string
}

func (s *Server) Hover(ctx context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	if !s.featureEnabled(func(f featureSettings) bool { return f.Hover }) {
		return nil, nil
	}

	doc, ok := s.getJournalDoc(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	journal, _ := s.cachedJournal(params.TextDocument.URI, doc)

	element := findElementAtPosition(journal, runePosition(doc, params.Position))
	if element == nil || element.context == HoverUnknown {
		return nil, nil
	}

	var balances analyzer.AccountBalances
	var allTransactions []ast.Transaction
	var commodityFormats map[string]formatter.CommodityFormat
	var defSymbol string

	if resolved := s.getWorkspaceResolved(params.TextDocument.URI); resolved != nil {
		allTransactions = resolved.AllTransactions()
		balances = analyzer.CalculateAccountBalancesFromTransactions(allTransactions)
		if len(resolved.Occurrences) > 0 {
			occID := firstOccurrenceIDForPath(resolved, uriToPath(params.TextDocument.URI))
			commodityFormats = resolved.FormatsAt(occID, element.rng.Start.Offset)
			defSymbol = resolved.DefaultCommodityAt(occID, element.rng.Start.Offset)
		} else {
			directives := resolved.AllDirectives()
			commodityFormats = formatter.ExtractCommodityFormats(directives)
			defSymbol = defaultCommoditySymbol(directives)
		}
	} else {
		allTransactions = journal.Transactions
		balances = s.cachedBalances(params.TextDocument.URI, doc)
		commodityFormats = formatter.ExtractCommodityFormats(journal.Directives)
		defSymbol = defaultCommoditySymbol(journal.Directives)
	}

	content := buildHoverContentWithTransactions(element, balances, allTransactions, commodityFormats, defSymbol)
	if element.context == HoverAmount {
		if varied := s.amountHoverAcrossOccurrences(params.TextDocument.URI, doc, params.Position, element); varied != "" {
			content = varied
		}
	}
	if content == "" {
		return nil, nil
	}

	hoverRange := astRangeToLSP(lsputil.NewPositionMapper(doc), element.rng)
	return &protocol.Hover{
		Contents: documentationMarkupContent(content, s.clientCapabilities.hoverContentFormats),
		Range:    &hoverRange,
	}, nil
}

// amountHoverAcrossOccurrences returns the hover of an amount when the include
// contexts of the document read it as different values, and "" when they all
// read it alike (the common case, which keeps the ordinary hover). Each distinct
// reading is listed with the includes that give it. The values are written from
// the parsed quantity: the text of the amount is the same at every occurrence,
// which is why a reading can differ at all.
func (s *Server) amountHoverAcrossOccurrences(docURI uri.URI, doc string, position protocol.Position, primary *hoverElement) string {
	readings := s.occurrenceReadings(docURI, doc)
	if len(readings) < 2 {
		return ""
	}

	type group struct {
		content string
		labels  []string
	}
	var groups []group
	index := make(map[string]int)
	for i := range readings {
		element := primary
		if i > 0 {
			element = findElementAtPosition(readings[i].journal, runePosition(doc, position))
			if element == nil || element.context != HoverAmount || element.amount == nil {
				return ""
			}
		}
		key := hoverAmountKey(element)
		at, seen := index[key]
		if !seen {
			defaultSymbol := ""
			if readings[i].tree != nil {
				defaultSymbol = readings[i].tree.DefaultCommodityAt(readings[i].id, element.rng.Start.Offset)
			}
			formats := readings[i].formatsAt(element.rng.Start.Offset)
			groups = append(groups, group{content: buildAmountHoverWith(valueOnly(element.amount), valueOnlyCost(element.cost), func(amount *ast.Amount, symbol string) string {
				return styledAmountForHover(amount, formats, symbol)
			}, defaultSymbol)})
			at = len(groups) - 1
			index[key] = at
		}
		for _, label := range labelsOf(readings[i].includedFrom) {
			if label != "" && !slices.Contains(groups[at].labels, label) {
				groups[at].labels = append(groups[at].labels, label)
			}
		}
	}
	if len(groups) < 2 {
		return ""
	}

	parts := make([]string, 0, len(groups))
	for _, g := range groups {
		heading := "when this file is the journal root"
		if len(g.labels) > 0 {
			heading = "when included from " + strings.Join(g.labels, ", ")
		}
		parts = append(parts, "_"+heading+"_\n\n"+g.content)
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// styledAmountForHover writes an amount the way the occurrence's commodity styles
// write it, as the inlay hints do, so a decimal mark of that occurrence shows in
// the value: 1.5 under a comma mark reads `1,5`. The ordinary hover keeps the
// text as written, which is why it cannot tell the readings apart.
func styledAmountForHover(amount *ast.Amount, formats map[string]formatter.CommodityFormat, defaultSymbol string) string {
	display := withSymbolNamed(amount)
	if display.Commodity.Symbol == "" {
		display.Commodity.Symbol = defaultSymbol
	}
	return formatter.FormatAmount(&display, formats)
}

// withSymbolNamed copies an amount for text that names its commodity symbol even
// when the journal writes it bare. Such text is read with the mark of amounts
// written with that symbol, so an amount that inherited its symbol from the D
// directive takes that mark to read back as the same value.
func withSymbolNamed(amount *ast.Amount) ast.Amount {
	display := *amount
	if display.Commodity.Inferred && display.SymbolDecimalMark != "" {
		display.DecimalMark = display.SymbolDecimalMark
	}
	display.Commodity.Inferred = false
	return display
}

// hoverAmountKey identifies what an amount and its cost are worth.
func hoverAmountKey(element *hoverElement) string {
	key := element.amount.Quantity.String() + "|" + element.amount.Commodity.Symbol
	if element.cost != nil {
		key += "|" + element.cost.Amount.Quantity.String() + "|" + element.cost.Amount.Commodity.Symbol
	}
	return key
}

// valueOnly copies an amount without its written digits, so it is rendered from
// the quantity it was read as.
func valueOnly(amount *ast.Amount) *ast.Amount {
	copied := *amount
	copied.RawQuantity = ""
	return &copied
}

func valueOnlyCost(cost *ast.Cost) *ast.Cost {
	if cost == nil {
		return nil
	}
	copied := *cost
	copied.Amount = *valueOnly(&cost.Amount)
	return &copied
}

// firstOccurrenceIDForPath returns the ID of the first occurrence matching path,
// or 0 if not found.
func firstOccurrenceIDForPath(resolved *include.ResolvedJournal, path string) include.OccurrenceID {
	for i := range resolved.Occurrences {
		if resolved.Occurrences[i].Path == path {
			return resolved.Occurrences[i].ID
		}
	}
	return 0
}

func positionInRange(pos protocol.Position, rng ast.Range) bool {
	line := int(pos.Line) + 1
	col := int(pos.Character) + 1

	if line < rng.Start.Line || line > rng.End.Line {
		return false
	}

	if line == rng.Start.Line && col < rng.Start.Column {
		return false
	}

	if line == rng.End.Line && col > rng.End.Column {
		return false
	}

	return true
}

func findElementAtPosition(journal *ast.Journal, pos protocol.Position) *hoverElement {
	for i := range journal.Transactions {
		tx := &journal.Transactions[i]

		dateRange := computeDateRange(tx)
		if positionInRange(pos, dateRange) {
			return &hoverElement{
				context:     HoverDate,
				rng:         dateRange,
				transaction: tx,
			}
		}

		payee := getPayeeOrDescription(tx)
		if payee != "" {
			payeeRange := payeeRange(tx, payee)
			if positionInRange(pos, payeeRange) {
				return &hoverElement{
					context:     HoverPayee,
					rng:         payeeRange,
					payee:       payee,
					transaction: tx,
				}
			}
		}

		// Check transaction-level tags (in comments)
		for _, comment := range tx.Comments {
			if elem := findTagAtPosition(comment.Tags, pos); elem != nil {
				return elem
			}
		}

		for j := range tx.Postings {
			p := &tx.Postings[j]

			accountRange := computeAccountRange(&p.Account)
			if positionInRange(pos, accountRange) {
				return &hoverElement{
					context: HoverAccount,
					rng:     accountRange,
					account: &p.Account,
				}
			}

			if p.Amount != nil && positionInRange(pos, p.Amount.Range) {
				return &hoverElement{
					context: HoverAmount,
					rng:     p.Amount.Range,
					amount:  p.Amount,
					cost:    p.Cost,
				}
			}

			// Check posting-level tags
			if elem := findTagAtPosition(p.Tags, pos); elem != nil {
				return elem
			}
		}
	}

	return nil
}

func computeDateRange(tx *ast.Transaction) ast.Range {
	return tx.Date.Range
}

func computeAccountRange(account *ast.Account) ast.Range {
	// Account directives leave the end unset, which would otherwise travel to
	// the client as a range ending at character 0.
	return ensureRangeEnd(account.Range, account.Name)
}

func getPayeeOrDescription(tx *ast.Transaction) string {
	if tx.Payee != "" {
		return tx.Payee
	}
	return tx.Description
}

func payeeRange(tx *ast.Transaction, payee string) ast.Range {
	if tx.DescriptionRange != (ast.Range{}) {
		return tx.DescriptionRange
	}
	startCol := tx.Date.Range.End.Column + 1
	if tx.Status != ast.StatusNone {
		startCol += 2
	}
	payeeLen := utf8.RuneCountInString(payee)
	return ast.Range{
		Start: ast.Position{
			Line:   tx.Date.Range.Start.Line,
			Column: startCol,
		},
		End: ast.Position{
			Line:   tx.Date.Range.Start.Line,
			Column: startCol + payeeLen,
		},
	}
}

func buildHoverContentWithTransactions(element *hoverElement, balances analyzer.AccountBalances, transactions []ast.Transaction, commodityFormats map[string]formatter.CommodityFormat, defSymbol string) string {
	switch element.context {
	case HoverAccount:
		return buildAccountHoverWithTransactions(element.account.Name, balances, transactions, commodityFormats, defSymbol)
	case HoverAmount:
		return buildAmountHover(element.amount, element.cost, commodityFormats, defSymbol)
	case HoverPayee:
		return buildPayeeHoverWithTransactions(element.payee, transactions)
	case HoverDate:
		return buildDateHover(element.transaction)
	case HoverTag:
		return buildTagHover(element.tagName, transactions)
	case HoverTagValue:
		return buildTagValueHover(element.tagName, element.tagValue, transactions)
	default:
		return ""
	}
}

func buildAccountHoverWithTransactions(accountName string, balances analyzer.AccountBalances, transactions []ast.Transaction, commodityFormats map[string]formatter.CommodityFormat, defSymbol string) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "**Account:** `%s`\n\n", accountName)

	if commodityBalances, ok := balances[accountName]; ok && len(commodityBalances) > 0 {
		displayFormats := make(map[string]formatter.CommodityFormat, len(commodityFormats))
		maps.Copy(displayFormats, commodityFormats)
		inferredFormats := inferredCommodityFormatsFromTransactions(transactions)
		for sym, inferred := range inferredFormats {
			if _, hasSpecific := displayFormats[sym]; hasSpecific {
				continue
			}
			if _, hasDefault := displayFormats[""]; hasDefault {
				continue
			}
			displayFormats[sym] = inferred
		}

		displayBalances := make(map[string]decimal.Decimal, len(commodityBalances))
		maps.Copy(displayBalances, commodityBalances)

		if defSymbol != "" {
			if emptyBal, hasEmpty := displayBalances[""]; hasEmpty {
				displayBalances[defSymbol] = displayBalances[defSymbol].Add(emptyBal)
				delete(displayBalances, "")
			}
		}

		sb.WriteString("**Balance:**\n")

		commodities := make([]string, 0, len(displayBalances))
		for c := range displayBalances {
			commodities = append(commodities, c)
		}
		sort.Strings(commodities)

		for _, c := range commodities {
			bal := displayBalances[c]
			if format, ok := configuredCommodityFormat(c, commodityFormats); ok && format.DecimalPlaces > 0 {
				bal = bal.Round(int32(format.DecimalPlaces))
			}
			fmt.Fprintf(&sb, "- %s\n", formatter.FormatBalance(bal, c, displayFormats))
		}
		sb.WriteString("\n")
	}

	postingCount := countPostingsForAccountInTransactions(accountName, transactions)
	fmt.Fprintf(&sb, "**Postings:** %d", postingCount)

	return sb.String()
}

func enrichCommodityFormatsFromTransactions(formats map[string]formatter.CommodityFormat, transactions []ast.Transaction) {
	if _, hasDefault := formats[""]; hasDefault {
		return
	}
	for sym, inferred := range inferredCommodityFormatsFromTransactions(transactions) {
		if _, configured := formats[sym]; !configured {
			formats[sym] = inferred
		}
	}
}

func inferredCommodityFormatsFromTransactions(transactions []ast.Transaction) map[string]formatter.CommodityFormat {
	formats := make(map[string]formatter.CommodityFormat)
	for i := range transactions {
		for j := range transactions[i].Postings {
			amt := transactions[i].Postings[j].Amount
			if amt == nil || amt.Commodity.Symbol == "" {
				continue
			}
			sym := amt.Commodity.Symbol
			existing, ok := formats[sym]
			if !ok {
				existing = formatter.CommodityFormat{
					Position:     amt.Commodity.Position,
					SpaceBetween: formatter.DefaultSpaceBetween(amt.Commodity.Position, sym),
				}
			}
			dp := decimalPlaces(amt)
			if dp > existing.DecimalPlaces {
				existing.DecimalPlaces = dp
				existing.HasDecimal = true
				existing.DecimalMark = '.'
			}
			formats[sym] = existing
		}
	}
	return formats
}

func configuredCommodityFormat(symbol string, configured map[string]formatter.CommodityFormat) (formatter.CommodityFormat, bool) {
	if format, ok := configured[symbol]; ok {
		return format, true
	}
	format, ok := configured[""]
	return format, ok
}

// decimalPlaces returns the number of decimal places in an amount. The parsed
// quantity keeps its written digits, and unlike the raw text it knows which
// separator was the decimal mark: `15,000` can have none.
func decimalPlaces(amt *ast.Amount) int {
	exp := amt.Quantity.Exponent()
	if exp < 0 {
		return int(-exp)
	}
	return 0
}

func defaultCommoditySymbol(directives []ast.Directive) string {
	var symbol string
	for _, dir := range directives {
		if d, ok := dir.(ast.DefaultCommodityDirective); ok {
			symbol = d.Symbol
		}
	}
	return symbol
}

func buildAmountHover(amount *ast.Amount, cost *ast.Cost, commodityFormats map[string]formatter.CommodityFormat, defaultSymbol string) string {
	return buildAmountHoverWith(amount, cost, func(a *ast.Amount, symbol string) string {
		return formatAmountForHover(a, commodityFormats, symbol)
	}, defaultSymbol)
}

// buildAmountHoverWith lays out the amount hover, rendering each amount with
// render; the default symbol applies to the amount itself, not to its cost.
func buildAmountHoverWith(amount *ast.Amount, cost *ast.Cost, render func(*ast.Amount, string) string, defaultSymbol string) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "**Amount:** %s", render(amount, defaultSymbol))

	if cost != nil {
		costFormatted := render(&cost.Amount, "")
		if cost.IsTotal {
			fmt.Fprintf(&sb, "\n\n**Total cost:** @@ %s", costFormatted)
		} else {
			fmt.Fprintf(&sb, "\n\n**Unit cost:** @ %s", costFormatted)
		}
	}

	return sb.String()
}

func formatAmountForHover(amount *ast.Amount, commodityFormats map[string]formatter.CommodityFormat, defaultSymbol string) string {
	// Hover names the commodity the amount belongs to even when the journal
	// writes it bare, so an inherited default commodity is never hidden.
	displayAmount := withSymbolNamed(amount)

	if displayAmount.Commodity.Symbol == "" {
		if defaultSymbol == "" {
			return formatter.FormatAmount(&displayAmount, nil)
		}
		displayAmount.Commodity.Symbol = defaultSymbol
	}

	if _, ok := commodityFormats[displayAmount.Commodity.Symbol]; ok {
		return formatter.FormatAmount(&displayAmount, commodityFormats)
	}
	return formatter.FormatAmount(&displayAmount, nil)
}

func buildPayeeHoverWithTransactions(payee string, transactions []ast.Transaction) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "**Payee:** %s\n\n", payee)

	count := 0
	for i := range transactions {
		tx := &transactions[i]
		if tx.Payee == payee || tx.Description == payee {
			count++
		}
	}

	fmt.Fprintf(&sb, "**Transactions:** %d", count)

	return sb.String()
}

func buildDateHover(tx *ast.Transaction) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "**Date:** %04d-%02d-%02d\n\n", tx.Date.Year, tx.Date.Month, tx.Date.Day)

	payee := getPayeeOrDescription(tx)
	if payee != "" {
		fmt.Fprintf(&sb, "**Payee:** %s\n\n", payee)
	}

	fmt.Fprintf(&sb, "**Postings:** %d", len(tx.Postings))

	return sb.String()
}

func countPostingsForAccountInTransactions(accountName string, transactions []ast.Transaction) int {
	count := 0
	for i := range transactions {
		for j := range transactions[i].Postings {
			if transactions[i].Postings[j].Account.Name == accountName {
				count++
			}
		}
	}
	return count
}

func ensureRangeEnd(rng ast.Range, name string) ast.Range {
	if rng.End.Line == 0 && rng.End.Column == 0 && rng.Start.Line > 0 {
		rng.End = ast.Position{
			Line: rng.Start.Line,
			// The lexer counts columns in runes; converting to the UTF-16 units
			// LSP wants happens at the protocol boundary, not here.
			Column: rng.Start.Column + utf8.RuneCountInString(name),
			Offset: rng.Start.Offset + len(name),
		}
	}
	return rng
}

// astRangeToProtocol reads lexer columns as LSP characters, which only holds
// while the line has no character outside the BMP. Prefer astRangeToLSP, which
// goes through byte offsets and a PositionMapper.
func astRangeToProtocol(rng ast.Range) *protocol.Range {
	return &protocol.Range{
		Start: protocol.Position{
			Line:      uint32(max(0, rng.Start.Line-1)),
			Character: uint32(max(0, rng.Start.Column-1)),
		},
		End: protocol.Position{
			Line:      uint32(max(0, rng.End.Line-1)),
			Character: uint32(max(0, rng.End.Column-1)),
		},
	}
}

func findTagAtPosition(tags []ast.Tag, pos protocol.Position) *hoverElement {
	for _, tag := range tags {
		if !positionInRange(pos, tag.Range) {
			continue
		}

		// Determine if cursor is on tag name or tag value
		// Tag format: name:value
		// tag.Range.Start is at the beginning of name
		colonCol := tag.Range.Start.Column + lsputil.UTF16Len(tag.Name)
		cursorCol := int(pos.Character) + 1 // convert to 1-based

		if cursorCol <= colonCol {
			// Cursor is on tag name
			return &hoverElement{
				context: HoverTag,
				rng: ast.Range{
					Start: tag.Range.Start,
					End: ast.Position{
						Line:   tag.Range.Start.Line,
						Column: colonCol,
						Offset: tag.Range.Start.Offset + len(tag.Name),
					},
				},
				tagName: tag.Name,
			}
		}

		// Cursor is on tag value (after the colon)
		return &hoverElement{
			context: HoverTagValue,
			rng: ast.Range{
				Start: ast.Position{
					Line:   tag.Range.Start.Line,
					Column: colonCol + 1,
					Offset: tag.Range.Start.Offset + len(tag.Name) + 1,
				},
				End: tag.Range.End,
			},
			tagName:  tag.Name,
			tagValue: tag.Value,
		}
	}
	return nil
}

func buildTagHover(tagName string, transactions []ast.Transaction) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "**Tag:** `%s`\n\n", tagName)

	count := countTagUsage(tagName, transactions)
	fmt.Fprintf(&sb, "**Usage:** %d\n\n", count)

	values := collectTagValues(tagName, transactions)
	if len(values) > 0 {
		sb.WriteString("**Values:**\n")
		for _, v := range values {
			if v == "" {
				fmt.Fprint(&sb, "- *(empty)*\n")
			} else {
				fmt.Fprintf(&sb, "- `%s`\n", v)
			}
		}
	}

	return sb.String()
}

func buildTagValueHover(tagName, tagValue string, transactions []ast.Transaction) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "**Tag:** `%s`\n", tagName)
	if tagValue == "" {
		sb.WriteString("**Value:** *(empty)*\n\n")
	} else {
		fmt.Fprintf(&sb, "**Value:** `%s`\n\n", tagValue)
	}

	count := countTagValueUsage(tagName, tagValue, transactions)
	fmt.Fprintf(&sb, "**Usage:** %d", count)

	return sb.String()
}

func forEachTag(transactions []ast.Transaction, fn func(ast.Tag)) {
	for i := range transactions {
		tx := &transactions[i]
		for _, comment := range tx.Comments {
			for _, tag := range comment.Tags {
				fn(tag)
			}
		}
		for j := range tx.Postings {
			for _, tag := range tx.Postings[j].Tags {
				fn(tag)
			}
		}
	}
}

func countTagUsage(tagName string, transactions []ast.Transaction) int {
	count := 0
	forEachTag(transactions, func(tag ast.Tag) {
		if tag.Name == tagName {
			count++
		}
	})
	return count
}

func countTagValueUsage(tagName, tagValue string, transactions []ast.Transaction) int {
	count := 0
	forEachTag(transactions, func(tag ast.Tag) {
		if tag.Name == tagName && tag.Value == tagValue {
			count++
		}
	})
	return count
}

func collectTagValues(tagName string, transactions []ast.Transaction) []string {
	valuesSet := make(map[string]struct{})
	forEachTag(transactions, func(tag ast.Tag) {
		if tag.Name == tagName {
			valuesSet[tag.Value] = struct{}{}
		}
	})

	values := make([]string, 0, len(valuesSet))
	for v := range valuesSet {
		values = append(values, v)
	}
	sort.Strings(values)
	return values
}
