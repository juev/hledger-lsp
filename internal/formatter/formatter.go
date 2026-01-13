package formatter

import (
	"strings"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/ast"
)

const defaultIndent = "    "
const minSpaces = 2

func FormatDocument(journal *ast.Journal, content string) []protocol.TextEdit {
	if len(journal.Transactions) == 0 {
		return nil
	}

	var edits []protocol.TextEdit

	for i := range journal.Transactions {
		tx := &journal.Transactions[i]
		txEdits := formatTransaction(tx, content)
		edits = append(edits, txEdits...)
	}

	return edits
}

func formatTransaction(tx *ast.Transaction, content string) []protocol.TextEdit {
	if len(tx.Postings) == 0 {
		return nil
	}

	alignCol := CalculateAlignmentColumn(tx.Postings)
	var edits []protocol.TextEdit

	for i := range tx.Postings {
		posting := &tx.Postings[i]
		formatted := FormatPosting(posting, alignCol)

		edit := protocol.TextEdit{
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      uint32(posting.Range.Start.Line - 1),
					Character: 0,
				},
				End: protocol.Position{
					Line:      uint32(posting.Range.Start.Line - 1),
					Character: uint32(getLineLength(content, posting.Range.Start.Line-1)),
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
		accountLen := len(p.Account.Name)
		switch p.Virtual {
		case ast.VirtualBalanced, ast.VirtualUnbalanced:
			accountLen += 2
		}
		if accountLen > maxLen {
			maxLen = accountLen
		}
	}
	return len(defaultIndent) + maxLen + minSpaces
}

func FormatPosting(posting *ast.Posting, alignCol int) string {
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
		currentLen := sb.Len()
		spaces := alignCol - currentLen
		if spaces < minSpaces {
			spaces = minSpaces
		}
		sb.WriteString(strings.Repeat(" ", spaces))

		if posting.Amount.Commodity.Position == ast.CommodityLeft {
			sb.WriteString(posting.Amount.Commodity.Symbol)
		}

		sb.WriteString(posting.Amount.Quantity.String())

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
		sb.WriteString(posting.Cost.Amount.Quantity.String())
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
		sb.WriteString(posting.BalanceAssertion.Amount.Quantity.String())
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

func getLineLength(content string, line int) int {
	lines := strings.Split(content, "\n")
	if line < 0 || line >= len(lines) {
		return 0
	}
	return len(lines[line])
}
