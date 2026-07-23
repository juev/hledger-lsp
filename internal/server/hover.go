package server

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"
	"go.lsp.dev/protocol"

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
	doc, ok := s.getJournalDoc(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	journal, _ := s.cachedJournal(params.TextDocument.URI, doc)

	element := findElementAtPosition(journal, params.Position)
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
	if content == "" {
		return nil, nil
	}

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: content,
		},
		Range: astRangeToProtocol(element.rng),
	}, nil
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
	return account.Range
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
		enrichCommodityFormatsFromTransactions(displayFormats, transactions)

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
			fmt.Fprintf(&sb, "- %s\n", formatter.FormatBalance(bal, c, displayFormats))
		}
		sb.WriteString("\n")
	}

	postingCount := countPostingsForAccountInTransactions(accountName, transactions)
	fmt.Fprintf(&sb, "**Postings:** %d", postingCount)

	return sb.String()
}

func enrichCommodityFormatsFromTransactions(formats map[string]formatter.CommodityFormat, transactions []ast.Transaction) {
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
}

// decimalPlaces returns the number of decimal places in an amount,
// preferring RawQuantity string parsing over Decimal.Exponent().
func decimalPlaces(amt *ast.Amount) int {
	if amt.RawQuantity != "" {
		if idx := strings.LastIndexByte(amt.RawQuantity, '.'); idx >= 0 {
			return len(amt.RawQuantity) - idx - 1
		}
		if idx := strings.LastIndexByte(amt.RawQuantity, ','); idx >= 0 {
			// comma as decimal mark (e.g., "25,70")
			after := amt.RawQuantity[idx+1:]
			if len(after) <= 3 {
				return len(after)
			}
		}
		return 0
	}
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
	var sb strings.Builder

	fmt.Fprintf(&sb, "**Amount:** %s", formatAmountForHover(amount, commodityFormats, defaultSymbol))

	if cost != nil {
		costFormatted := formatAmountForHover(&cost.Amount, commodityFormats, "")
		if cost.IsTotal {
			fmt.Fprintf(&sb, "\n\n**Total cost:** @@ %s", costFormatted)
		} else {
			fmt.Fprintf(&sb, "\n\n**Unit cost:** @ %s", costFormatted)
		}
	}

	return sb.String()
}

func formatAmountForHover(amount *ast.Amount, commodityFormats map[string]formatter.CommodityFormat, defaultSymbol string) string {
	if amount.Commodity.Symbol != "" {
		if _, ok := commodityFormats[amount.Commodity.Symbol]; ok {
			return formatter.FormatAmount(amount, commodityFormats)
		}
		return formatter.FormatAmount(amount, nil)
	}

	if defaultSymbol != "" {
		displayAmount := *amount
		displayAmount.Commodity.Symbol = defaultSymbol
		return formatter.FormatAmount(&displayAmount, commodityFormats)
	}

	return formatter.FormatAmount(amount, nil)
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
			Line:   rng.Start.Line,
			Column: rng.Start.Column + lsputil.UTF16Len(name),
			Offset: rng.Start.Offset + len(name),
		}
	}
	return rng
}

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
