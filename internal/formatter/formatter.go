package formatter

import (
	"strings"
	"unicode/utf8"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/lsputil"
)

const defaultIndentSize = 4
const minSpaces = 2

var defaultIndent = strings.Repeat(" ", defaultIndentSize)

type Options struct {
	IndentSize         int
	AlignAmounts       bool
	MinAlignmentColumn int
}

func DefaultOptions() Options {
	return Options{IndentSize: defaultIndentSize, AlignAmounts: true}
}

type AlignmentInfo struct {
	AccountCol          int
	BalanceAssertionCol int
}

func FormatDocument(journal *ast.Journal, content string) []protocol.TextEdit {
	commodityFormats := extractCommodityFormats(journal)
	return FormatDocumentWithFormats(journal, content, commodityFormats)
}

func FormatDocumentWithFormats(journal *ast.Journal, content string, commodityFormats map[string]NumberFormat) []protocol.TextEdit {
	return FormatDocumentWithOptions(journal, content, commodityFormats, DefaultOptions())
}

func FormatDocumentWithOptions(journal *ast.Journal, content string, commodityFormats map[string]NumberFormat, opts Options) []protocol.TextEdit {
	if commodityFormats == nil {
		commodityFormats = extractCommodityFormats(journal)
	}

	if opts.IndentSize <= 0 {
		opts.IndentSize = defaultIndentSize
	}

	mapper := lsputil.NewPositionMapper(content)
	var edits []protocol.TextEdit

	postingLines := make(map[int]bool)

	if len(journal.Transactions) > 0 {
		globalAccountCol := 0
		if opts.AlignAmounts {
			globalAccountCol = calculateGlobalAlignmentColumnWithIndent(journal.Transactions, opts.IndentSize)
			if opts.MinAlignmentColumn > 0 && globalAccountCol < opts.MinAlignmentColumn {
				globalAccountCol = opts.MinAlignmentColumn
			}
		}

		for i := range journal.Transactions {
			tx := &journal.Transactions[i]
			for j := range tx.Postings {
				postingLines[tx.Postings[j].Range.Start.Line-1] = true
			}
			txEdits := formatTransactionWithOpts(tx, mapper, commodityFormats, globalAccountCol, opts)
			edits = append(edits, txEdits...)
		}
	}

	trimEdits := trimTrailingSpacesEdits(content, mapper, postingLines)
	edits = append(edits, trimEdits...)

	return edits
}

func trimTrailingSpacesEdits(content string, mapper *lsputil.PositionMapper, postingLines map[int]bool) []protocol.TextEdit {
	lines := strings.Split(content, "\n")
	var edits []protocol.TextEdit

	for lineNum, line := range lines {
		if postingLines[lineNum] {
			continue
		}

		trimmed := strings.TrimRight(line, " \t")
		if len(trimmed) == len(line) {
			continue
		}

		trimmedUTF16Len := lsputil.UTF16Len(trimmed)
		lineUTF16Len := mapper.LineUTF16Len(lineNum)

		edit := protocol.TextEdit{
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      uint32(lineNum),
					Character: uint32(trimmedUTF16Len),
				},
				End: protocol.Position{
					Line:      uint32(lineNum),
					Character: uint32(lineUTF16Len),
				},
			},
			NewText: "",
		}
		edits = append(edits, edit)
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

func formatTransactionWithOpts(tx *ast.Transaction, mapper *lsputil.PositionMapper, commodityFormats map[string]NumberFormat, globalAccountCol int, opts Options) []protocol.TextEdit {
	if len(tx.Postings) == 0 {
		return nil
	}

	indent := strings.Repeat(" ", opts.IndentSize)
	var edits []protocol.TextEdit

	var alignment AlignmentInfo
	if opts.AlignAmounts {
		alignment = CalculateAlignmentWithGlobal(tx.Postings, commodityFormats, globalAccountCol)
	}

	for i := range tx.Postings {
		posting := &tx.Postings[i]
		formatted := formatPostingWithOpts(posting, alignment, commodityFormats, indent, opts.AlignAmounts)
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

func calculateAccountDisplayLength(p *ast.Posting) int {
	accountLen := utf8.RuneCountInString(p.Account.Name)
	switch p.Virtual {
	case ast.VirtualBalanced, ast.VirtualUnbalanced:
		accountLen += 2
	}
	return accountLen
}

func CalculateAlignmentColumn(postings []ast.Posting) int {
	maxLen := 0
	for i := range postings {
		if accountLen := calculateAccountDisplayLength(&postings[i]); accountLen > maxLen {
			maxLen = accountLen
		}
	}
	return utf8.RuneCountInString(defaultIndent) + maxLen + minSpaces
}

func CalculateGlobalAlignmentColumn(transactions []ast.Transaction) int {
	maxLen := 0
	for i := range transactions {
		for j := range transactions[i].Postings {
			if accountLen := calculateAccountDisplayLength(&transactions[i].Postings[j]); accountLen > maxLen {
				maxLen = accountLen
			}
		}
	}
	return utf8.RuneCountInString(defaultIndent) + maxLen + minSpaces
}

func calculateGlobalAlignmentColumnWithIndent(transactions []ast.Transaction, indentSize int) int {
	maxLen := 0
	for i := range transactions {
		for j := range transactions[i].Postings {
			if accountLen := calculateAccountDisplayLength(&transactions[i].Postings[j]); accountLen > maxLen {
				maxLen = accountLen
			}
		}
	}
	return indentSize + maxLen + minSpaces
}

// CalculateAlignment calculates alignment for a single transaction's postings.
// For consistent file-wide alignment, use CalculateAlignmentWithGlobal with
// a pre-calculated global column from CalculateGlobalAlignmentColumn.
func CalculateAlignment(postings []ast.Posting, commodityFormats map[string]NumberFormat) AlignmentInfo {
	accountCol := CalculateAlignmentColumn(postings)
	return CalculateAlignmentWithGlobal(postings, commodityFormats, accountCol)
}

// CalculateAlignmentWithGlobal calculates alignment using a provided account column.
// Use this with CalculateGlobalAlignmentColumn for file-wide consistent alignment.
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
	return formatPostingWithOpts(posting, alignment, commodityFormats, defaultIndent, true)
}

func FormatPosting(posting *ast.Posting, alignCol int) string {
	return FormatPostingWithAlignment(posting, AlignmentInfo{AccountCol: alignCol}, nil)
}

func formatPostingWithOpts(posting *ast.Posting, alignment AlignmentInfo, commodityFormats map[string]NumberFormat, indent string, alignAmounts bool) string {
	var sb strings.Builder

	sb.WriteString(indent)

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
		spaces := minSpaces
		if alignAmounts && alignment.AccountCol > 0 {
			currentLen := utf8.RuneCountInString(sb.String())
			spaces = max(alignment.AccountCol-currentLen, minSpaces)
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
		if alignAmounts && alignment.BalanceAssertionCol > 0 {
			currentLen := utf8.RuneCountInString(sb.String())
			spaces := max(alignment.BalanceAssertionCol-currentLen, minSpaces)
			sb.WriteString(strings.Repeat(" ", spaces))
		} else {
			sb.WriteString(strings.Repeat(" ", minSpaces))
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
