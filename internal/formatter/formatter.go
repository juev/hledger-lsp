package formatter

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
	"github.com/shopspring/decimal"
	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/lsputil"
)

const defaultIndentSize = 4
const minSpaces = 2
const minAssertionSpaces = 1

// widthCondition is a fixed runewidth condition so formatting is deterministic
// regardless of the server's locale. Full-width CJK characters always occupy
// two cells; ambiguous-width characters are treated as narrow (the common
// default for Western fonts/editors).
var widthCondition = runewidth.NewCondition()

// displayWidth returns the number of terminal cells s occupies when rendered:
// full-width CJK characters count as two cells, unlike utf8.RuneCountInString
// which counts them as one. Use it for visual alignment column math; keep
// RuneCountInString for positional/UTF-16 arithmetic.
func displayWidth(s string) int {
	return widthCondition.StringWidth(s)
}

// Amount alignment targets for postings with cost notation (`@`/`@@`).
// They select which amount anchors alignment:
//   - AlignTargetCost: the cost (second) amount — hledger 1.x behavior (default).
//   - AlignTargetPosting: the posting (first) amount — hledger 2.x print behavior;
//     the lot price and cost annotation then trail freely after it.
const (
	AlignTargetCost    = "cost"
	AlignTargetPosting = "posting"
)

// normalizeAlignTarget maps a raw option value to a known target, defaulting
// to AlignTargetCost for empty or unknown input.
func normalizeAlignTarget(target string) string {
	if target == AlignTargetPosting {
		return AlignTargetPosting
	}
	return AlignTargetCost
}

var defaultIndent = strings.Repeat(" ", defaultIndentSize)

type Options struct {
	IndentSize            int
	AlignAmounts          bool
	MinAlignmentColumn    int
	AmountAlignmentColumn int
	AmountAlignmentMode   string
	// AmountAlignmentTarget selects the alignment anchor for cost-notation
	// postings: "" / "cost" (default) anchor on the cost amount; "posting"
	// anchors on the posting amount. Normalized via normalizeAlignTarget.
	AmountAlignmentTarget string
}

func DefaultOptions() Options {
	return Options{IndentSize: defaultIndentSize, AlignAmounts: true}
}

type AlignmentInfo struct {
	AccountCol int
	DecimalCol int
	// AmountEndCol, when > 0, enables true right-alignment: amounts are
	// padded so that the column right after the last rune of the rendered
	// amount equals AmountEndCol. Used when AmountAlignmentMode = "right"
	// and every amount in the document has a right-side commodity (issue
	// #25). Zero disables end-column alignment.
	AmountEndCol int
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
		alignment := ComputeAlignment(journal, commodityFormats, opts)

		for i := range journal.Transactions {
			tx := &journal.Transactions[i]
			for j := range tx.Postings {
				postingLines[tx.Postings[j].Range.Start.Line-1] = true
			}
			txEdits := formatTransactionWithOpts(tx, mapper, commodityFormats, alignment.AccountCol, alignment.DecimalCol, alignment.AmountEndCol, opts)
			edits = append(edits, txEdits...)
		}
	}

	trimEdits := trimTrailingSpacesEdits(content, mapper, postingLines)
	edits = append(edits, trimEdits...)

	return edits
}

// ComputeAlignment derives the alignment columns used to render postings,
// matching the semantics of FormatDocumentWithOptions. Returns a zero
// AlignmentInfo when alignment is disabled or the journal has no transactions.
//
// Reused by both the formatter and inline completion (ghost text) so the
// columns ghost text targets always match what Format Document would produce
// for the same document.
func ComputeAlignment(journal *ast.Journal, commodityFormats map[string]CommodityFormat, opts Options) AlignmentInfo {
	if journal == nil || len(journal.Transactions) == 0 || !opts.AlignAmounts {
		return AlignmentInfo{}
	}

	indentSize := opts.IndentSize
	if indentSize <= 0 {
		indentSize = defaultIndentSize
	}

	target := normalizeAlignTarget(opts.AmountAlignmentTarget)

	naturalAccountCol := CalculateGlobalAlignmentColumnWithIndent(journal.Transactions, indentSize)
	globalAccountCol := naturalAccountCol
	if opts.MinAlignmentColumn > 0 && globalAccountCol < opts.MinAlignmentColumn-1 {
		globalAccountCol = opts.MinAlignmentColumn - 1
	}
	globalDecimalCol := 0
	globalAmountEndCol := 0

	switch opts.AmountAlignmentMode {
	case "decimal":
		if opts.AmountAlignmentColumn > 0 {
			globalDecimalCol = opts.AmountAlignmentColumn
		} else {
			globalDecimalCol = CalculateGlobalDecimalCol(journal.Transactions, commodityFormats, globalAccountCol, target)
			if detected := DetectExistingDecimalColumn(journal.Transactions, commodityFormats, target); detected > globalDecimalCol {
				globalDecimalCol = detected
			}
		}
	case "left":
		if opts.AmountAlignmentColumn > 0 {
			globalAccountCol = opts.AmountAlignmentColumn
		} else if detected := DetectExistingAmountColumn(journal.Transactions); detected > globalAccountCol {
			globalAccountCol = detected
		}
	default:
		// Smart detection: if the file already has hand-aligned
		// amounts, use the most common existing start column as the
		// base. Decimal mode uses decimal-target detection instead.
		if detected := DetectExistingAmountColumn(journal.Transactions); detected > globalAccountCol {
			globalAccountCol = detected
		}
		if opts.AmountAlignmentColumn > 0 && allAmountsCommodityRight(journal.Transactions) {
			globalAmountEndCol = opts.AmountAlignmentColumn
			break
		}
		// Right-alignment: when every amount has a right-side
		// commodity, anchor by end column so the commodity symbol
		// (USD / EUR / …) aligns at the rightmost column even with
		// mixed-sign amounts. Mixed commodity positions fall back to
		// start-column alignment (issue #25). An explicit
		// MinAlignmentColumn also falls back to start-column —
		// that setting is a start-column constraint by definition
		// and takes priority over automatic end-column anchoring.
		if opts.MinAlignmentColumn <= 0 && allAmountsCommodityRight(journal.Transactions) {
			if endCol := DetectExistingAmountEndColumn(journal.Transactions, commodityFormats, target); endCol > 0 {
				naturalEndCol := naturalAccountCol + calculateGlobalAlignmentTargetLen(journal.Transactions, commodityFormats, target)
				globalAmountEndCol = max(naturalEndCol, endCol)
			}
		}
	}

	return AlignmentInfo{
		AccountCol:   globalAccountCol,
		DecimalCol:   globalDecimalCol,
		AmountEndCol: globalAmountEndCol,
	}
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

func formatTransactionWithOpts(tx *ast.Transaction, mapper *lsputil.PositionMapper, commodityFormats map[string]CommodityFormat, globalAccountCol int, globalDecimalCol int, globalAmountEndCol int, opts Options) []protocol.TextEdit {
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
		if globalAmountEndCol > 0 {
			alignment.AmountEndCol = globalAmountEndCol
		}
	}

	target := normalizeAlignTarget(opts.AmountAlignmentTarget)

	for i := range tx.Postings {
		posting := &tx.Postings[i]
		formatted := formatPostingWithOpts(posting, alignment, commodityFormats, indent, opts.AlignAmounts, target)
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
	accountLen := displayWidth(p.Account.Name)
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
	return displayWidth(defaultIndent) + maxLen + minSpaces
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
	return displayWidth(defaultIndent) + maxLen + minSpaces
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

func selectModalColumn(columns []int) int {
	counts := make(map[int]int)
	bestCol := 0
	bestCount := 0
	for _, col := range columns {
		if col <= 0 {
			continue
		}
		counts[col]++
		if counts[col] > bestCount || (counts[col] == bestCount && col > bestCol) {
			bestCol = col
			bestCount = counts[col]
		}
	}
	return bestCol
}

// DetectExistingAmountColumn returns the most common 0-indexed rune column
// where an amount currently begins across all postings, or 0 if no posting has
// an amount. Ties choose the larger column to avoid compressing hand-formatted
// files. Used for "smart" alignment detection: when a file already has a
// dominant visual layout, this column becomes a floor for new postings via Tab
// and full document formatting.
//
// AST positions from the parser are 1-indexed runes; this function returns
// 0-indexed runes for direct compatibility with CalculateGlobalAlignmentColumnWithIndent.
func DetectExistingAmountColumn(transactions []ast.Transaction) int {
	var columns []int
	for i := range transactions {
		for j := range transactions[i].Postings {
			p := &transactions[i].Postings[j]
			if p.Amount == nil || p.Amount.Range.Start.Column <= 0 {
				continue
			}
			col := p.Amount.Range.Start.Column - 1 // 1-indexed → 0-indexed
			columns = append(columns, col)
		}
	}
	return selectModalColumn(columns)
}

// DetectExistingAmountEndColumn returns the most common 0-indexed rune column
// right after the last rune of any commodity-right alignment target across all
// postings, or 0 if no such amount exists. Ties choose the larger column.
//
// The end column is computed as `(Amount.Range.Start.Column - 1) +
// renderedAlignmentTargetLen` rather than read from `Amount.Range.End.Column`,
// because the parser extends `Amount.Range.End` through trailing whitespace up
// to the next token (e.g. `=` of a balance assertion), which would shift the
// detected column one or two positions too far right and break format
// idempotency.
//
// Used for right-alignment of commodity-on-right amounts (issue #25):
// aligning by end column keeps the commodity symbol (USD / EUR / …) in a
// fixed column regardless of sign or integer-part width. Commodity-left
// amounts are ignored — those are aligned by start column via the sibling
// DetectExistingAmountColumn so the commodity symbol (e.g. $) stays put.
func DetectExistingAmountEndColumn(transactions []ast.Transaction, commodityFormats map[string]CommodityFormat, target string) int {
	var columns []int
	for i := range transactions {
		for j := range transactions[i].Postings {
			p := &transactions[i].Postings[j]
			if p.Amount == nil || p.Amount.Commodity.Position != ast.CommodityRight {
				continue
			}
			if p.Amount.Range.Start.Column <= 0 {
				continue
			}
			startCol := p.Amount.Range.Start.Column - 1
			endCol := startCol + calculateAlignmentTargetLen(p, commodityFormats, target)
			columns = append(columns, endCol)
		}
	}
	return selectModalColumn(columns)
}

func DetectExistingDecimalColumn(transactions []ast.Transaction, commodityFormats map[string]CommodityFormat, target string) int {
	var columns []int
	for i := range transactions {
		for j := range transactions[i].Postings {
			p := &transactions[i].Postings[j]
			if p.Amount == nil || p.Amount.Range.Start.Column <= 0 {
				continue
			}
			startCol := p.Amount.Range.Start.Column - 1
			columns = append(columns, startCol+calculateAlignmentTargetDecimalPrefix(p, commodityFormats, target))
		}
	}
	return selectModalColumn(columns)
}

// allAmountsCommodityRight reports whether every posting that carries an
// amount has a right-position commodity. Used as a guard for end-column
// alignment — mixed positions fall back to start-column semantics so
// commodity-left amounts (`$10.00`) keep their `$` column aligned.
func allAmountsCommodityRight(transactions []ast.Transaction) bool {
	seenAny := false
	for i := range transactions {
		for j := range transactions[i].Postings {
			p := &transactions[i].Postings[j]
			if p.Amount == nil {
				continue
			}
			seenAny = true
			if p.Amount.Commodity.Symbol == "" {
				continue
			}
			if p.Amount.Commodity.Position != ast.CommodityRight {
				return false
			}
		}
	}
	return seenAny
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
func CalculateAlignmentWithGlobal(_ []ast.Posting, _ map[string]CommodityFormat, accountCol int) AlignmentInfo {
	return AlignmentInfo{AccountCol: accountCol}
}

// CalculateGlobalDecimalCol computes the column where decimal points should align,
// based on the maximum prefix length (chars before decimal point) across all postings.
// The alignment anchor depends on target: with AlignTargetCost the cost amount's
// decimal prefix is used for cost-notation postings; with AlignTargetPosting only
// the posting amount's decimal prefix is considered and the cost annotation trails.
func CalculateGlobalDecimalCol(transactions []ast.Transaction, commodityFormats map[string]CommodityFormat, accountCol int, target string) int {
	maxPrefix := 0
	for i := range transactions {
		for j := range transactions[i].Postings {
			p := &transactions[i].Postings[j]
			if p.Amount != nil {
				prefix := calculateAlignmentTargetDecimalPrefix(p, commodityFormats, target)
				maxPrefix = max(maxPrefix, prefix)
			}
		}
	}
	if maxPrefix > 0 {
		return accountCol + maxPrefix
	}
	return 0
}

func calculateAlignmentTargetLen(posting *ast.Posting, commodityFormats map[string]CommodityFormat, target string) int {
	if posting.Amount == nil {
		return 0
	}

	length := calculateSingleAmountLen(posting.Amount, commodityFormats)
	if target == AlignTargetPosting {
		return length
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

func calculateGlobalAlignmentTargetLen(transactions []ast.Transaction, commodityFormats map[string]CommodityFormat, target string) int {
	maxLen := 0
	for i := range transactions {
		for j := range transactions[i].Postings {
			maxLen = max(maxLen, calculateAlignmentTargetLen(&transactions[i].Postings[j], commodityFormats, target))
		}
	}
	return maxLen
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

func calculateAlignmentTargetDecimalPrefix(posting *ast.Posting, commodityFormats map[string]CommodityFormat, target string) int {
	if posting.Amount == nil {
		return 0
	}

	prefix := calculateSingleAmountDecimalPrefix(posting.Amount, commodityFormats)
	if target == AlignTargetPosting || posting.Cost == nil {
		return prefix
	}

	amountLen := calculateSingleAmountLen(posting.Amount, commodityFormats)
	separatorLen := 3 // " @ "
	if posting.Cost.IsTotal {
		separatorLen = 4 // " @@ "
	}
	return amountLen + separatorLen + calculateSingleAmountDecimalPrefix(&posting.Cost.Amount, commodityFormats)
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
		symbolWidth := displayWidth(symbol)
		if amount.SignBeforeCommodity && len(qty) > 0 && (qty[0] == '-' || qty[0] == '+') {
			prefixBeforeQty = 1 + symbolWidth // sign + symbol
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
			prefixBeforeQty = symbolWidth
			if spaceBetween {
				prefixBeforeQty++
			}
		}
	}

	if qtyDecimalIdx >= 0 {
		return prefixBeforeQty + qtyDecimalIdx
	}

	// No decimal — prefix is the full integer part
	return prefixBeforeQty + displayWidth(effectiveQty)
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
	symbolLen := displayWidth(commoditySymbolDisplay(&amount.Commodity))
	qtyLen := displayWidth(formatAmountQuantity(amount, commodityFormats))
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
	return formatPostingWithOpts(posting, alignment, commodityFormats, defaultIndent, true, AlignTargetCost)
}

func FormatPosting(posting *ast.Posting, alignCol int) string {
	return FormatPostingWithAlignment(posting, AlignmentInfo{AccountCol: alignCol}, nil)
}

func formatPostingWithOpts(posting *ast.Posting, alignment AlignmentInfo, commodityFormats map[string]CommodityFormat, indent string, alignAmounts bool, target string) string {
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
		switch {
		case alignAmounts && alignment.DecimalCol > 0:
			currentLen := displayWidth(sb.String())
			prefix := calculateAlignmentTargetDecimalPrefix(posting, commodityFormats, target)
			spaces = max(alignment.DecimalCol-currentLen-prefix, minSpaces)
		case alignAmounts && alignment.AmountEndCol > 0:
			currentLen := displayWidth(sb.String())
			amountLen := calculateAlignmentTargetLen(posting, commodityFormats, target)
			spaces = max(alignment.AmountEndCol-currentLen-amountLen, minSpaces)
		case alignAmounts && alignment.AccountCol > 0:
			currentLen := displayWidth(sb.String())
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
		spaces := minAssertionSpaces
		if posting.Amount == nil {
			spaces = minSpaces
			if alignAmounts && alignment.AccountCol > 0 {
				currentLen := displayWidth(sb.String())
				spaces = max(alignment.AccountCol-currentLen, minSpaces)
			}
		}
		sb.WriteString(strings.Repeat(" ", spaces))

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
