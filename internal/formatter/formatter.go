package formatter

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/shopspring/decimal"
	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/lsputil"
)

const defaultIndentSize = 4
const minSpaces = 2

var defaultIndent = strings.Repeat(" ", defaultIndentSize)

type Options struct {
	IndentSize          int
	AlignAmounts        bool
	MinAlignmentColumn  int
	AmountAlignmentMode string
}

func DefaultOptions() Options {
	return Options{IndentSize: defaultIndentSize, AlignAmounts: true}
}

type AlignmentInfo struct {
	AccountCol          int
	BalanceAssertionCol int
	DecimalCol          int
}

func FormatDocument(journal *ast.Journal, content string) []protocol.TextEdit {
	commodityFormats := extractCommodityFormats(journal)
	return FormatDocumentWithFormats(journal, content, commodityFormats)
}

func FormatDocumentWithFormats(journal *ast.Journal, content string, commodityFormats map[string]CommodityFormat) []protocol.TextEdit {
	return FormatDocumentWithOptions(journal, content, commodityFormats, DefaultOptions())
}

func FormatDocumentWithOptions(journal *ast.Journal, content string, commodityFormats map[string]CommodityFormat, opts Options) []protocol.TextEdit {
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
		globalDecimalCol := 0
		if opts.AlignAmounts {
			globalAccountCol = CalculateGlobalAlignmentColumnWithIndent(journal.Transactions, opts.IndentSize)
			// Smart detection: if the file already has hand-aligned amounts,
			// use the widest existing column as the base. This preserves the
			// file's visual layout and keeps Format consistent with Tab.
			if detected := DetectExistingAmountColumn(journal.Transactions); detected > globalAccountCol {
				globalAccountCol = detected
			}
			if opts.MinAlignmentColumn > 0 && globalAccountCol < opts.MinAlignmentColumn-1 {
				globalAccountCol = opts.MinAlignmentColumn - 1
			}
			if opts.AmountAlignmentMode == "decimal" {
				globalDecimalCol = CalculateGlobalDecimalCol(journal.Transactions, commodityFormats, globalAccountCol)
			}
		}

		for i := range journal.Transactions {
			tx := &journal.Transactions[i]
			for j := range tx.Postings {
				postingLines[tx.Postings[j].Range.Start.Line-1] = true
			}
			txEdits := formatTransactionWithOpts(tx, mapper, commodityFormats, globalAccountCol, globalDecimalCol, opts)
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

// ExtractCommodityFormats builds a commodity symbol → format map from directives.
// The empty string key ("") holds the default format from the D directive,
// or from the decimal-mark directive as a fallback when no D directive is present.
func ExtractCommodityFormats(directives []ast.Directive) map[string]CommodityFormat {
	formats := make(map[string]CommodityFormat)
	var defaultFormat *CommodityFormat
	var decimalMarkFormat *CommodityFormat

	for _, dir := range directives {
		switch d := dir.(type) {
		case ast.CommodityDirective:
			if d.Format != "" {
				formats[d.Commodity.Symbol] = ParseCommodityFormat(d.Format, d.Commodity.Symbol)
			}
		case ast.DefaultCommodityDirective:
			if d.Format != "" {
				cf := ParseCommodityFormat(d.Format, d.Symbol)
				defaultFormat = &cf
				if d.Symbol != "" {
					formats[d.Symbol] = cf
				}
			}
		case ast.DecimalMarkDirective:
			var decMark rune
			var thousandsSep string
			if d.Mark == "," {
				decMark = ','
				thousandsSep = "."
			} else {
				decMark = '.'
				thousandsSep = ","
			}
			cf := CommodityFormat{
				NumberFormat: NumberFormat{
					DecimalMark:  decMark,
					ThousandsSep: thousandsSep,
					HasDecimal:   true,
				},
				Position:     ast.CommodityRight,
				SpaceBetween: true,
			}
			decimalMarkFormat = &cf
		}
	}

	if defaultFormat != nil {
		formats[""] = *defaultFormat
	} else if decimalMarkFormat != nil {
		formats[""] = *decimalMarkFormat
	}

	return formats
}

func extractCommodityFormats(journal *ast.Journal) map[string]CommodityFormat {
	return ExtractCommodityFormats(journal.Directives)
}

func formatTransactionWithOpts(tx *ast.Transaction, mapper *lsputil.PositionMapper, commodityFormats map[string]CommodityFormat, globalAccountCol int, globalDecimalCol int, opts Options) []protocol.TextEdit {
	if len(tx.Postings) == 0 {
		return nil
	}

	indent := strings.Repeat(" ", opts.IndentSize)
	var edits []protocol.TextEdit

	var alignment AlignmentInfo
	if opts.AlignAmounts {
		alignment = CalculateAlignmentWithGlobal(tx.Postings, commodityFormats, globalAccountCol)
		if globalDecimalCol > 0 {
			alignment.DecimalCol = globalDecimalCol
		}
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

// CalculateGlobalAlignmentColumnWithIndent returns the column at which amounts
// should be aligned, using the given indentSize instead of the default indent.
func CalculateGlobalAlignmentColumnWithIndent(transactions []ast.Transaction, indentSize int) int {
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

// DetectExistingAmountColumn returns the leftmost (minimum) 0-indexed rune
// column where an amount currently begins across all postings, or 0 if no
// posting has an amount. Used for "smart" alignment detection: when a
// hand-formatted file has amounts wider than formula natural would compute,
// this column becomes a floor for new postings via Tab and full document
// formatting, preserving the file's visual layout.
//
// MIN (leftmost) is chosen because:
//   - For right-align mode, the leftmost amount is the widest one. All other
//     amounts in a consistently-formatted file end at the same column, so the
//     widest amount's start column is the canonical accountCol.
//   - For decimal mode, the leftmost amount is the one with the longest
//     integer part. After formatting, narrower amounts shift right to align
//     decimal points; the widest amount's start column stays put. MIN gives
//     idempotent format runs (MAX would drift wider on each iteration).
//
// AST positions from the parser are 1-indexed runes; this function returns
// 0-indexed runes for direct compatibility with CalculateGlobalAlignmentColumnWithIndent.
func DetectExistingAmountColumn(transactions []ast.Transaction) int {
	minCol := -1
	for i := range transactions {
		for j := range transactions[i].Postings {
			p := &transactions[i].Postings[j]
			if p.Amount == nil || p.Amount.Range.Start.Column <= 0 {
				continue
			}
			col := p.Amount.Range.Start.Column - 1 // 1-indexed → 0-indexed
			if minCol < 0 || col < minCol {
				minCol = col
			}
		}
	}
	if minCol < 0 {
		return 0
	}
	return minCol
}

// CalculateAlignment calculates alignment for a single transaction's postings.
// For consistent file-wide alignment, use CalculateAlignmentWithGlobal with
// a pre-calculated global column from CalculateGlobalAlignmentColumn.
func CalculateAlignment(postings []ast.Posting, commodityFormats map[string]CommodityFormat) AlignmentInfo {
	accountCol := CalculateAlignmentColumn(postings)
	return CalculateAlignmentWithGlobal(postings, commodityFormats, accountCol)
}

// CalculateAlignmentWithGlobal calculates alignment using a provided account column.
// Use this with CalculateGlobalAlignmentColumn for file-wide consistent alignment.
func CalculateAlignmentWithGlobal(postings []ast.Posting, commodityFormats map[string]CommodityFormat, accountCol int) AlignmentInfo {

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

// CalculateGlobalDecimalCol computes the column where decimal points should align,
// based on the maximum prefix length (chars before decimal point) across all postings.
// Only the posting amount's decimal prefix is considered — cost annotations (@ or @@)
// follow after the posting amount and do not participate in alignment.
func CalculateGlobalDecimalCol(transactions []ast.Transaction, commodityFormats map[string]CommodityFormat, accountCol int) int {
	maxPrefix := 0
	for i := range transactions {
		for j := range transactions[i].Postings {
			p := &transactions[i].Postings[j]
			if p.Amount != nil {
				prefix := calculateAmountDecimalPrefix(p, commodityFormats)
				maxPrefix = max(maxPrefix, prefix)
			}
		}
	}
	if maxPrefix > 0 {
		return accountCol + maxPrefix
	}
	return 0
}

func calculateAmountCostLen(posting *ast.Posting, commodityFormats map[string]CommodityFormat) int {
	if posting.Amount == nil {
		return 0
	}

	length := calculateSingleAmountLen(posting.Amount, commodityFormats)

	if posting.LotPrice != nil {
		length += calculateLotPriceLen(posting.LotPrice, commodityFormats)
	}

	if posting.Cost != nil {
		if posting.Cost.IsTotal {
			length += 4 // " @@ "
		} else {
			length += 3 // " @ "
		}
		length += calculateSingleAmountLen(&posting.Cost.Amount, commodityFormats)
	}

	return length
}

func calculateLotPriceLen(lot *ast.LotPrice, commodityFormats map[string]CommodityFormat) int {
	length := 0
	if lot.Cost != nil {
		if lot.IsTotal {
			length += 5 // " {{" + "}}"
		} else {
			length += 3 // " {" + "}"
		}
		length += calculateSingleAmountLen(lot.Cost, commodityFormats)
	}
	if lot.Date != "" {
		length += 3 + utf8.RuneCountInString(lot.Date) // " [" + date + "]"
	}
	if lot.Label != "" {
		length += 3 + utf8.RuneCountInString(lot.Label) // " (" + label + ")"
	}
	return length
}

// calculateAmountDecimalPrefix returns the number of characters in the rendered
// posting amount BEFORE the decimal point. For amounts without a decimal part,
// returns the length up to where the decimal point would be (end of integer part).
// This includes sign, commodity symbol, space, and integer part of the number.
func calculateAmountDecimalPrefix(posting *ast.Posting, commodityFormats map[string]CommodityFormat) int {
	if posting.Amount == nil {
		return 0
	}
	return calculateSingleAmountDecimalPrefix(posting.Amount, commodityFormats)
}

// calculateSingleAmountDecimalPrefix returns the number of characters in the rendered
// amount BEFORE the decimal point. Works on a standalone Amount (not a Posting).
//
// The calculation is based on the structural layout of the rendered amount
// (not by searching for the decimal mark in the rendered string, which would
// fail when the commodity symbol also contains the decimal mark character,
// e.g. quoted commodities like "VWXY.Z").
func calculateSingleAmountDecimalPrefix(amount *ast.Amount, commodityFormats map[string]CommodityFormat) int {
	qty := formatAmountQuantity(amount, commodityFormats)
	mark := resolveDecimalMark(amount, commodityFormats)
	qtyDecimalIdx := strings.LastIndexFunc(qty, func(r rune) bool { return r == mark })

	position, spaceBetween := resolveCommodityDisplay(amount, commodityFormats)
	symbol := commoditySymbolDisplay(&amount.Commodity)

	// Calculate characters before the quantity digits in the rendered string.
	// Right-commodity: qty comes first → prefixBeforeQty = 0
	// Left-commodity:  symbol [space] qty → prefixBeforeQty = len(symbol) [+1]
	// Left + SignBeforeCommodity: sign symbol [space] qty_without_sign
	var prefixBeforeQty int
	effectiveQty := qty

	if position == ast.CommodityLeft {
		symbolRuneLen := utf8.RuneCountInString(symbol)
		if amount.SignBeforeCommodity && len(qty) > 0 && (qty[0] == '-' || qty[0] == '+') {
			prefixBeforeQty = 1 + symbolRuneLen // sign + symbol
			if spaceBetween {
				prefixBeforeQty++
			}
			effectiveQty = qty[1:]
			if qtyDecimalIdx > 0 {
				qtyDecimalIdx-- // adjust for removed sign byte
			} else {
				qtyDecimalIdx = -1
			}
		} else {
			prefixBeforeQty = symbolRuneLen
			if spaceBetween {
				prefixBeforeQty++
			}
		}
	}

	if qtyDecimalIdx >= 0 {
		return prefixBeforeQty + qtyDecimalIdx
	}

	// No decimal — prefix is the full integer part
	return prefixBeforeQty + utf8.RuneCountInString(effectiveQty)
}

func resolveDecimalMark(amount *ast.Amount, commodityFormats map[string]CommodityFormat) rune {
	if commodityFormats != nil {
		if cf, ok := commodityFormats[amount.Commodity.Symbol]; ok {
			return cf.DecimalMark
		}
		if cf, ok := commodityFormats[""]; ok {
			return cf.DecimalMark
		}
	}
	return '.'
}

func calculateSingleAmountLen(amount *ast.Amount, commodityFormats map[string]CommodityFormat) int {
	_, spaceBetween := resolveCommodityDisplay(amount, commodityFormats)
	symbolLen := utf8.RuneCountInString(commoditySymbolDisplay(&amount.Commodity))
	qtyLen := utf8.RuneCountInString(formatAmountQuantity(amount, commodityFormats))
	length := qtyLen

	if symbolLen > 0 {
		length += symbolLen
		if spaceBetween {
			length++
		}
	}

	return length
}

func FormatPostingWithAlignment(posting *ast.Posting, alignment AlignmentInfo, commodityFormats map[string]CommodityFormat) string {
	return formatPostingWithOpts(posting, alignment, commodityFormats, defaultIndent, true)
}

func FormatPosting(posting *ast.Posting, alignCol int) string {
	return FormatPostingWithAlignment(posting, AlignmentInfo{AccountCol: alignCol}, nil)
}

func formatPostingWithOpts(posting *ast.Posting, alignment AlignmentInfo, commodityFormats map[string]CommodityFormat, indent string, alignAmounts bool) string {
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
		if alignAmounts && alignment.DecimalCol > 0 {
			currentLen := utf8.RuneCountInString(sb.String())
			// Always align the posting amount's decimal point, regardless of cost.
			prefix := calculateAmountDecimalPrefix(posting, commodityFormats)
			spaces = max(alignment.DecimalCol-currentLen-prefix, minSpaces)
		} else if alignAmounts && alignment.AccountCol > 0 {
			currentLen := utf8.RuneCountInString(sb.String())
			spaces = max(alignment.AccountCol-currentLen, minSpaces)
		}
		sb.WriteString(strings.Repeat(" ", spaces))

		writeAmountWithSign(&sb, posting.Amount, commodityFormats)
	}

	if posting.LotPrice != nil {
		writeLotPrice(&sb, posting.LotPrice, commodityFormats)
	}

	if posting.Cost != nil {
		if posting.Cost.IsTotal {
			sb.WriteString(" @@ ")
		} else {
			sb.WriteString(" @ ")
		}
		writeAmountWithSign(&sb, &posting.Cost.Amount, commodityFormats)
	}

	if posting.BalanceAssertion != nil {
		if alignAmounts && alignment.BalanceAssertionCol > 0 {
			currentLen := utf8.RuneCountInString(sb.String())
			spaces := max(alignment.BalanceAssertionCol-currentLen, minSpaces)
			sb.WriteString(strings.Repeat(" ", spaces))
		} else {
			sb.WriteString(strings.Repeat(" ", minSpaces))
		}

		switch {
		case posting.BalanceAssertion.IsStrict && posting.BalanceAssertion.IsInclusive:
			sb.WriteString("==* ")
		case posting.BalanceAssertion.IsStrict:
			sb.WriteString("== ")
		case posting.BalanceAssertion.IsInclusive:
			sb.WriteString("=* ")
		default:
			sb.WriteString("= ")
		}
		writeAmountWithSign(&sb, &posting.BalanceAssertion.Amount, commodityFormats)
		if posting.BalanceAssertion.LotPrice != nil {
			writeLotPrice(&sb, posting.BalanceAssertion.LotPrice, commodityFormats)
		}
		if posting.BalanceAssertion.Cost != nil {
			if posting.BalanceAssertion.Cost.IsTotal {
				sb.WriteString(" @@ ")
			} else {
				sb.WriteString(" @ ")
			}
			writeAmountWithSign(&sb, &posting.BalanceAssertion.Cost.Amount, commodityFormats)
		}
	}

	if posting.Comment != "" {
		sb.WriteString("  ; ")
		sb.WriteString(strings.TrimLeft(posting.Comment, " \t"))
	}

	return sb.String()
}

func resolveCommodityDisplay(amount *ast.Amount, commodityFormats map[string]CommodityFormat) (position ast.CommodityPosition, spaceBetween bool) {
	position = amount.Commodity.Position
	spaceBetween = DefaultSpaceBetween(position, amount.Commodity.Symbol)

	if commodityFormats != nil {
		if cf, ok := commodityFormats[amount.Commodity.Symbol]; ok {
			return cf.Position, cf.SpaceBetween
		}
	}
	return position, spaceBetween
}

// DefaultSpaceBetween returns the default spacing rule for a commodity.
// Right-position commodities always get a space. Left-position commodities
// get a space only for word commodities (not ending with a currency symbol).
// An empty symbol means there is no commodity at all (plain numbers like
// `-8`), so there is nothing to space against — return false.
func DefaultSpaceBetween(position ast.CommodityPosition, symbol string) bool {
	if symbol == "" {
		return false
	}
	if position == ast.CommodityRight {
		return true
	}
	return !IsSymbolCommodity(symbol)
}

// IsSymbolCommodity returns true if the commodity symbol ends with a Unicode
// currency character (Sc category), e.g. "$", "AU$", "¥". Word commodities
// like "USD", "AAPL", "RUB" return false.
func IsSymbolCommodity(symbol string) bool {
	if symbol == "" {
		return false
	}
	lastRune, _ := utf8.DecodeLastRuneInString(symbol)
	return lastRune != utf8.RuneError && unicode.Is(unicode.Sc, lastRune)
}

func commoditySymbolDisplay(c *ast.Commodity) string {
	if c.Quoted {
		return `"` + c.Symbol + `"`
	}
	return c.Symbol
}

func writeLotPrice(sb *strings.Builder, lot *ast.LotPrice, commodityFormats map[string]CommodityFormat) {
	if lot.Cost != nil {
		if lot.IsTotal {
			sb.WriteString(" {{")
		} else {
			sb.WriteString(" {")
		}
		writeAmountWithSign(sb, lot.Cost, commodityFormats)
		if lot.IsTotal {
			sb.WriteString("}}")
		} else {
			sb.WriteString("}")
		}
	}

	if lot.Date != "" {
		sb.WriteString(" [")
		sb.WriteString(lot.Date)
		sb.WriteString("]")
	}

	if lot.Label != "" {
		sb.WriteString(" (")
		sb.WriteString(lot.Label)
		sb.WriteString(")")
	}
}

func writeAmountWithSign(sb *strings.Builder, amount *ast.Amount, commodityFormats map[string]CommodityFormat) {
	qty := formatAmountQuantity(amount, commodityFormats)
	position, spaceBetween := resolveCommodityDisplay(amount, commodityFormats)
	symbol := commoditySymbolDisplay(&amount.Commodity)

	if position == ast.CommodityLeft {
		if amount.SignBeforeCommodity && len(qty) > 0 && (qty[0] == '-' || qty[0] == '+') {
			sb.WriteByte(qty[0])
			sb.WriteString(symbol)
			if spaceBetween {
				sb.WriteString(" ")
			}
			sb.WriteString(qty[1:])
		} else {
			sb.WriteString(symbol)
			if spaceBetween {
				sb.WriteString(" ")
			}
			sb.WriteString(qty)
		}
	} else {
		sb.WriteString(qty)
		if symbol != "" {
			if spaceBetween {
				sb.WriteString(" ")
			}
			sb.WriteString(symbol)
		}
	}
}

// FormatAmount renders an amount as a string with commodity symbol, respecting
// position, spacing, and number format from commodityFormats.
// Pass nil commodityFormats to use the amount's raw formatting.
func FormatAmount(amount *ast.Amount, commodityFormats map[string]CommodityFormat) string {
	var sb strings.Builder
	writeAmountWithSign(&sb, amount, commodityFormats)
	return sb.String()
}

// FormatBalance renders a decimal balance with a commodity symbol, respecting
// commodity formats for position, spacing, and number formatting.
// If commodity is empty, the raw quantity is returned.
func FormatBalance(quantity decimal.Decimal, commodity string, commodityFormats map[string]CommodityFormat) string {
	if commodity == "" {
		return quantity.String()
	}

	amount := &ast.Amount{
		Quantity:  quantity,
		Commodity: ast.Commodity{Symbol: commodity, Position: ast.CommodityRight},
	}

	if commodityFormats != nil {
		if cf, ok := commodityFormats[commodity]; ok {
			amount.Commodity.Position = cf.Position
		} else if cf, ok := commodityFormats[""]; ok {
			amount.Commodity.Position = cf.Position
		}
	}

	if quantity.IsNegative() && amount.Commodity.Position == ast.CommodityLeft {
		amount.SignBeforeCommodity = true
	}

	return FormatAmount(amount, commodityFormats)
}

func formatAmountQuantity(amount *ast.Amount, commodityFormats map[string]CommodityFormat) string {
	if amount == nil {
		return ""
	}
	if commodityFormats != nil {
		if cf, ok := commodityFormats[amount.Commodity.Symbol]; ok {
			return FormatNumber(amount.Quantity, cf.NumberFormat)
		}
		if cf, ok := commodityFormats[""]; ok {
			if cf.DecimalPlaces == 0 && amount.RawQuantity != "" {
				return amount.RawQuantity
			}
			return FormatNumber(amount.Quantity, cf.NumberFormat)
		}
	}
	if amount.RawQuantity != "" {
		return amount.RawQuantity
	}
	return amount.Quantity.String()
}
