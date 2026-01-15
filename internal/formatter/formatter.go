package formatter

import (
	"strings"
	"unicode/utf8"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/lsputil"
)

const defaultIndent = "    "
const minSpaces = 2

type AlignmentInfo struct {
	AccountCol          int
	BalanceAssertionCol int
}

func FormatDocument(journal *ast.Journal, content string) []protocol.TextEdit {
	commodityFormats := extractCommodityFormats(journal)
	return FormatDocumentWithFormats(journal, content, commodityFormats)
}

func FormatDocumentWithFormats(journal *ast.Journal, content string, commodityFormats map[string]NumberFormat) []protocol.TextEdit {
	if len(journal.Transactions) == 0 {
		return nil
	}

	if commodityFormats == nil {
		commodityFormats = extractCommodityFormats(journal)
	}

	globalAccountCol := CalculateGlobalAlignmentColumn(journal.Transactions)

	mapper := lsputil.NewPositionMapper(content)
	var edits []protocol.TextEdit

	for i := range journal.Transactions {
		tx := &journal.Transactions[i]
		txEdits := formatTransaction(tx, mapper, commodityFormats, globalAccountCol)
		edits = append(edits, txEdits...)
	}

	return edits
}

func extractCommodityFormats(journal *ast.Journal) map[string]NumberFormat {
	formats := make(map[string]NumberFormat)
	for _, dir := range journal.Directives {
		if cd, ok := dir.(ast.CommodityDirective); ok {
			if cd.Format != "" {
				formats[cd.Commodity.Symbol] = ParseNumberFormat(cd.Format)
			}
		}
	}
	return formats
}

func formatTransaction(tx *ast.Transaction, mapper *lsputil.PositionMapper, commodityFormats map[string]NumberFormat, globalAccountCol int) []protocol.TextEdit {
	if len(tx.Postings) == 0 {
		return nil
	}

	alignment := CalculateAlignmentWithGlobal(tx.Postings, commodityFormats, globalAccountCol)
	var edits []protocol.TextEdit

	for i := range tx.Postings {
		posting := &tx.Postings[i]
		formatted := FormatPostingWithAlignment(posting, alignment, commodityFormats)
		line := posting.Range.Start.Line - 1

		edit := protocol.TextEdit{
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      uint32(line),
					Character: 0,
				},
				End: protocol.Position{
					Line:      uint32(line),
					Character: uint32(mapper.LineUTF16Len(line)),
				},
			},
			NewText: formatted,
		}
		edits = append(edits, edit)
	}

	return edits
}

func CalculateAlignmentColumn(postings []ast.Posting) int {
	maxLen := 0
	for _, p := range postings {
		accountLen := utf8.RuneCountInString(p.Account.Name)
		switch p.Virtual {
		case ast.VirtualBalanced, ast.VirtualUnbalanced:
			accountLen += 2
		}
		if accountLen > maxLen {
			maxLen = accountLen
		}
	}
	return utf8.RuneCountInString(defaultIndent) + maxLen + minSpaces
}

func CalculateGlobalAlignmentColumn(transactions []ast.Transaction) int {
	maxLen := 0
	for i := range transactions {
		for _, p := range transactions[i].Postings {
			accountLen := utf8.RuneCountInString(p.Account.Name)
			switch p.Virtual {
			case ast.VirtualBalanced, ast.VirtualUnbalanced:
				accountLen += 2
			}
			if accountLen > maxLen {
				maxLen = accountLen
			}
		}
	}
	return utf8.RuneCountInString(defaultIndent) + maxLen + minSpaces
}

func CalculateAlignment(postings []ast.Posting, commodityFormats map[string]NumberFormat) AlignmentInfo {
	accountCol := CalculateAlignmentColumn(postings)
	return CalculateAlignmentWithGlobal(postings, commodityFormats, accountCol)
}

func CalculateAlignmentWithGlobal(postings []ast.Posting, commodityFormats map[string]NumberFormat, accountCol int) AlignmentInfo {

	hasBalanceAssertion := false
	maxAmountCostLen := 0
	for i := range postings {
		p := &postings[i]
		if p.BalanceAssertion != nil {
			hasBalanceAssertion = true
		}
		if p.Amount != nil {
			amountCostLen := calculateAmountCostLen(p, commodityFormats)
			maxAmountCostLen = max(maxAmountCostLen, amountCostLen)
		}
	}

	if !hasBalanceAssertion {
		return AlignmentInfo{AccountCol: accountCol, BalanceAssertionCol: 0}
	}

	return AlignmentInfo{
		AccountCol:          accountCol,
		BalanceAssertionCol: accountCol + maxAmountCostLen + minSpaces,
	}
}

func calculateAmountCostLen(posting *ast.Posting, commodityFormats map[string]NumberFormat) int {
	if posting.Amount == nil {
		return 0
	}

	length := 0

	if posting.Amount.Commodity.Position == ast.CommodityLeft {
		length += utf8.RuneCountInString(posting.Amount.Commodity.Symbol)
	}

	length += utf8.RuneCountInString(formatAmountQuantity(posting.Amount, commodityFormats))

	if posting.Amount.Commodity.Position == ast.CommodityRight {
		length += 1 + utf8.RuneCountInString(posting.Amount.Commodity.Symbol)
	}

	if posting.Cost != nil {
		if posting.Cost.IsTotal {
			length += 4 // " @@ "
		} else {
			length += 3 // " @ "
		}
		if posting.Cost.Amount.Commodity.Position == ast.CommodityLeft {
			length += utf8.RuneCountInString(posting.Cost.Amount.Commodity.Symbol)
		}
		length += utf8.RuneCountInString(formatAmountQuantity(&posting.Cost.Amount, commodityFormats))
		if posting.Cost.Amount.Commodity.Position == ast.CommodityRight {
			length += 1 + utf8.RuneCountInString(posting.Cost.Amount.Commodity.Symbol)
		}
	}

	return length
}

func FormatPostingWithAlignment(posting *ast.Posting, alignment AlignmentInfo, commodityFormats map[string]NumberFormat) string {
	var sb strings.Builder

	sb.WriteString(defaultIndent)

	switch posting.Status {
	case ast.StatusCleared:
		sb.WriteString("* ")
	case ast.StatusPending:
		sb.WriteString("! ")
	}

	switch posting.Virtual {
	case ast.VirtualUnbalanced:
		sb.WriteString("(")
	case ast.VirtualBalanced:
		sb.WriteString("[")
	}

	sb.WriteString(posting.Account.Name)

	switch posting.Virtual {
	case ast.VirtualUnbalanced:
		sb.WriteString(")")
	case ast.VirtualBalanced:
		sb.WriteString("]")
	}

	if posting.Amount != nil {
		currentLen := utf8.RuneCountInString(sb.String())
		spaces := max(alignment.AccountCol-currentLen, minSpaces)
		sb.WriteString(strings.Repeat(" ", spaces))

		if posting.Amount.Commodity.Position == ast.CommodityLeft {
			sb.WriteString(posting.Amount.Commodity.Symbol)
		}

		sb.WriteString(formatAmountQuantity(posting.Amount, commodityFormats))

		if posting.Amount.Commodity.Position == ast.CommodityRight {
			sb.WriteString(" ")
			sb.WriteString(posting.Amount.Commodity.Symbol)
		}
	}

	if posting.Cost != nil {
		if posting.Cost.IsTotal {
			sb.WriteString(" @@ ")
		} else {
			sb.WriteString(" @ ")
		}
		if posting.Cost.Amount.Commodity.Position == ast.CommodityLeft {
			sb.WriteString(posting.Cost.Amount.Commodity.Symbol)
		}
		sb.WriteString(formatAmountQuantity(&posting.Cost.Amount, commodityFormats))
		if posting.Cost.Amount.Commodity.Position == ast.CommodityRight {
			sb.WriteString(" ")
			sb.WriteString(posting.Cost.Amount.Commodity.Symbol)
		}
	}

	if posting.BalanceAssertion != nil {
		if alignment.BalanceAssertionCol > 0 {
			currentLen := utf8.RuneCountInString(sb.String())
			spaces := max(alignment.BalanceAssertionCol-currentLen, minSpaces)
			sb.WriteString(strings.Repeat(" ", spaces))
		}

		if posting.BalanceAssertion.IsStrict {
			sb.WriteString("== ")
		} else {
			sb.WriteString("= ")
		}
		if posting.BalanceAssertion.Amount.Commodity.Position == ast.CommodityLeft {
			sb.WriteString(posting.BalanceAssertion.Amount.Commodity.Symbol)
		}
		sb.WriteString(formatAmountQuantity(&posting.BalanceAssertion.Amount, commodityFormats))
		if posting.BalanceAssertion.Amount.Commodity.Position == ast.CommodityRight {
			sb.WriteString(" ")
			sb.WriteString(posting.BalanceAssertion.Amount.Commodity.Symbol)
		}
	}

	if posting.Comment != "" {
		sb.WriteString("  ; ")
		sb.WriteString(posting.Comment)
	}

	return sb.String()
}

func FormatPosting(posting *ast.Posting, alignCol int) string {
	return FormatPostingWithAlignment(posting, AlignmentInfo{AccountCol: alignCol}, nil)
}

// formatAmountQuantity returns formatted quantity string.
// Priority: commodity directive format > original raw format > decimal string.
func formatAmountQuantity(amount *ast.Amount, commodityFormats map[string]NumberFormat) string {
	if amount == nil {
		return ""
	}
	if commodityFormats != nil {
		if format, ok := commodityFormats[amount.Commodity.Symbol]; ok {
			return FormatNumber(amount.Quantity, format)
		}
	}
	if amount.RawQuantity != "" {
		return amount.RawQuantity
	}
	return amount.Quantity.String()
}
