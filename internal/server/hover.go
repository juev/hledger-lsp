package server

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/lsputil"
	"github.com/juev/hledger-lsp/internal/parser"
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
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	journal, _ := parser.Parse(doc)

	element := findElementAtPosition(journal, params.Position)
	if element == nil || element.context == HoverUnknown {
		return nil, nil
	}

	var balances analyzer.AccountBalances
	var allTransactions []ast.Transaction

	if resolved := s.getWorkspaceResolved(params.TextDocument.URI); resolved != nil {
		allTransactions = resolved.AllTransactions()
		balances = analyzer.CalculateAccountBalancesFromTransactions(allTransactions)
	} else {
		allTransactions = journal.Transactions
		balances = analyzer.CalculateAccountBalances(journal)
	}

	content := buildHoverContentWithTransactions(element, balances, allTransactions)
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
			payeeRange := estimatePayeeRange(tx, payee)
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
	start := tx.Date.Range.Start
	return ast.Range{
		Start: start,
		End: ast.Position{
			Line:   start.Line,
			Column: start.Column + 10,
			Offset: start.Offset + 10,
		},
	}
}

func computeAccountRange(account *ast.Account) ast.Range {
	start := account.Range.Start
	nameLen := lsputil.UTF16Len(account.Name)
	return ast.Range{
		Start: start,
		End: ast.Position{
			Line:   start.Line,
			Column: start.Column + nameLen,
			Offset: start.Offset + len(account.Name),
		},
	}
}

func getPayeeOrDescription(tx *ast.Transaction) string {
	if tx.Payee != "" {
		return tx.Payee
	}
	return tx.Description
}

func estimatePayeeRange(tx *ast.Transaction, payee string) ast.Range {
	startCol := tx.Date.Range.Start.Column + 11
	if tx.Status != ast.StatusNone {
		startCol += 2
	}

	payeeLen := lsputil.UTF16Len(payee)
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

func buildHoverContentWithTransactions(element *hoverElement, balances analyzer.AccountBalances, transactions []ast.Transaction) string {
	switch element.context {
	case HoverAccount:
		return buildAccountHoverWithTransactions(element.account.Name, balances, transactions)
	case HoverAmount:
		return buildAmountHover(element.amount, element.cost)
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

func buildAccountHoverWithTransactions(accountName string, balances analyzer.AccountBalances, transactions []ast.Transaction) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "**Account:** `%s`\n\n", accountName)

	if commodityBalances, ok := balances[accountName]; ok && len(commodityBalances) > 0 {
		sb.WriteString("**Balance:**\n")

		commodities := make([]string, 0, len(commodityBalances))
		for c := range commodityBalances {
			commodities = append(commodities, c)
		}
		sort.Strings(commodities)

		for _, c := range commodities {
			bal := commodityBalances[c]
			fmt.Fprintf(&sb, "- %s %s\n", bal.String(), c)
		}
		sb.WriteString("\n")
	}

	postingCount := countPostingsForAccountInTransactions(accountName, transactions)
	fmt.Fprintf(&sb, "**Postings:** %d", postingCount)

	return sb.String()
}

func buildAmountHover(amount *ast.Amount, cost *ast.Cost) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "**Amount:** %s %s", amount.Quantity.String(), amount.Commodity.Symbol)

	if cost != nil {
		if cost.IsTotal {
			fmt.Fprintf(&sb, "\n\n**Total cost:** @@ %s %s", cost.Amount.Quantity.String(), cost.Amount.Commodity.Symbol)
		} else {
			fmt.Fprintf(&sb, "\n\n**Unit cost:** @ %s %s", cost.Amount.Quantity.String(), cost.Amount.Commodity.Symbol)
		}
	}

	return sb.String()
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

func astRangeToProtocol(rng ast.Range) *protocol.Range {
	return &protocol.Range{
		Start: protocol.Position{
			Line:      uint32(rng.Start.Line - 1),
			Character: uint32(rng.Start.Column - 1),
		},
		End: protocol.Position{
			Line:      uint32(rng.End.Line - 1),
			Character: uint32(rng.End.Column - 1),
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
