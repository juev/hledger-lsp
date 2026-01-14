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

func FormatDocument(journal *ast.Journal, content string) []protocol.TextEdit {
	if len(journal.Transactions) == 0 {
		return nil
	}

	commodityFormats := extractCommodityFormats(journal)
	mapper := lsputil.NewPositionMapper(content)
	var edits []protocol.TextEdit

	for i := range journal.Transactions {
		tx := &journal.Transactions[i]
		txEdits := formatTransaction(tx, mapper, commodityFormats)
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

func formatTransaction(tx *ast.Transaction, mapper *lsputil.PositionMapper, commodityFormats map[string]NumberFormat) []protocol.TextEdit {
	if len(tx.Postings) == 0 {
		return nil
	}

	alignCol := CalculateAlignmentColumn(tx.Postings)
	var edits []protocol.TextEdit

	for i := range tx.Postings {
		posting := &tx.Postings[i]
		formatted := FormatPostingWithFormats(posting, alignCol, commodityFormats)
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

func FormatPosting(posting *ast.Posting, alignCol int) string {
	return FormatPostingWithFormats(posting, alignCol, nil)
}

func FormatPostingWithFormats(posting *ast.Posting, alignCol int, commodityFormats map[string]NumberFormat) string {
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
		spaces := alignCol - currentLen
		if spaces < minSpaces {
			spaces = minSpaces
		}
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
		if posting.BalanceAssertion.IsStrict {
			sb.WriteString(" == ")
		} else {
			sb.WriteString(" = ")
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

func formatAmountQuantity(amount *ast.Amount, commodityFormats map[string]NumberFormat) string {
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
