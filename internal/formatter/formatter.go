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
// DefaultTabSize is the tab stop used when a client does not report one.
const DefaultTabSize = 4

// DisplayWidth returns the terminal cell width of s: full-width CJK characters
// and other East Asian wide runes count as two cells. Alignment arithmetic uses
// this metric, never the rune count.
func DisplayWidth(s string) int {
	return displayWidth(s)
}

// DisplayWidthInLine returns the display column of the first byteOffset bytes of
// line, expanding tab characters to the next tab stop of tabSize cells. Cursor
// positions in tab-indented journals need this to land on the alignment column.
func DisplayWidthInLine(line string, byteOffset, tabSize int) int {
	if byteOffset > len(line) {
		byteOffset = len(line)
	}
	if byteOffset < 0 {
		byteOffset = 0
	}
	if tabSize <= 0 {
		tabSize = DefaultTabSize
	}

	column := 0
	for _, r := range line[:byteOffset] {
		if r == '\t' {
			column += tabSize - column%tabSize
			continue
		}
		column += runewidth.RuneWidth(r)
	}
	return column
}

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
	// CommentColumn, when > 0, is the display column where inline posting
	// comments start. It preserves a hand-aligned comment column instead of
	// pulling every comment to two spaces after its amount.
	CommentColumn int
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

	postings := AllPostings(journal)
	if len(postings) > 0 {
		alignment := ComputeAlignment(journal, content, commodityFormats, opts)

		for i := range postings {
			postingLines[postings[i].Range.Start.Line-1] = true
		}

		for i := range journal.Transactions {
			tx := &journal.Transactions[i]
			txEdits := formatTransactionWithOpts(tx, mapper, commodityFormats, alignment.AccountCol, alignment.DecimalCol, alignment.AmountEndCol, alignment.CommentColumn, opts)
			edits = append(edits, txEdits...)
		}
		for i := range journal.PeriodicTransactions {
			tx := &journal.PeriodicTransactions[i]
			txEdits := formatPostingsWithOpts(tx.Postings, mapper, commodityFormats, alignment.AccountCol, alignment.DecimalCol, alignment.AmountEndCol, alignment.CommentColumn, opts)
			edits = append(edits, txEdits...)
		}
		for i := range journal.AutoPostingRules {
			rule := &journal.AutoPostingRules[i]
			txEdits := formatPostingsWithOpts(rule.Postings, mapper, commodityFormats, alignment.AccountCol, alignment.DecimalCol, alignment.AmountEndCol, alignment.CommentColumn, opts)
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
func ComputeAlignment(journal *ast.Journal, content string, commodityFormats map[string]CommodityFormat, opts Options) AlignmentInfo {
	return computeAlignment(journal, content, commodityFormats, opts)
}

func computeAlignment(journal *ast.Journal, content string, commodityFormats map[string]CommodityFormat, opts Options) AlignmentInfo {
	if journal == nil || !opts.AlignAmounts {
		return AlignmentInfo{}
	}
	postings := AllPostings(journal)
	if len(postings) == 0 {
		return AlignmentInfo{}
	}
	lines := strings.Split(content, "\n")

	indentSize := opts.IndentSize
	if indentSize <= 0 {
		indentSize = defaultIndentSize
	}

	target := normalizeAlignTarget(opts.AmountAlignmentTarget)

	naturalAccountCol := CalculateGlobalAlignmentColumnWithIndent(postings, indentSize)
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
			globalDecimalCol = CalculateGlobalDecimalCol(postings, commodityFormats, globalAccountCol, target)
			if detected := detectExistingDecimalColumn(lines, postings, commodityFormats, target); detected > globalDecimalCol {
				globalDecimalCol = detected
			}
		}
	case "left":
		if opts.AmountAlignmentColumn > 0 {
			globalAccountCol = opts.AmountAlignmentColumn
		} else if detected := detectExistingAmountColumn(lines, postings); detected > globalAccountCol {
			globalAccountCol = detected
		}
	default:
		// Smart detection: if the file already has hand-aligned
		// amounts, use the most common existing start column as the
		// base. Decimal mode uses decimal-target detection instead.
		if detected := detectExistingAmountColumn(lines, postings); detected > globalAccountCol {
			globalAccountCol = detected
		}
		if opts.AmountAlignmentColumn > 0 && allAmountsCommodityRight(postings) {
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
		if opts.MinAlignmentColumn <= 0 && allAmountsCommodityRight(postings) {
			if endCol := detectExistingAmountEndColumn(lines, postings, commodityFormats, target); endCol > 0 {
				naturalEndCol := naturalAccountCol + calculateGlobalAlignmentTargetLen(postings, commodityFormats, target)
				globalAmountEndCol = max(naturalEndCol, endCol)
			}
		}
	}

	alignment := AlignmentInfo{
		AccountCol:   globalAccountCol,
		DecimalCol:   globalDecimalCol,
		AmountEndCol: globalAmountEndCol,
	}
	alignment.CommentColumn = commentColumn(lines, postings)
	return alignment
}

// AllPostings returns every posting of a journal in document order: regular
// transactions first, then periodic transactions and auto posting rules. hledger
// formats and folds those blocks like ordinary postings, so alignment must take
// their account names into account too.
func AllPostings(journal *ast.Journal) []ast.Posting {
	if journal == nil {
		return nil
	}

	total := len(journal.Transactions) + len(journal.PeriodicTransactions) + len(journal.AutoPostingRules)
	result := make([]ast.Posting, 0, total)
	for i := range journal.Transactions {
		result = append(result, journal.Transactions[i].Postings...)
	}
	for i := range journal.PeriodicTransactions {
		result = append(result, journal.PeriodicTransactions[i].Postings...)
	}
	for i := range journal.AutoPostingRules {
		result = append(result, journal.AutoPostingRules[i].Postings...)
	}
	return result
}

// commentColumn preserves a hand-aligned block of inline comments: when at least
// two comments already share a column, formatting keeps them there instead of
// pulling each one to two spaces after its own amount. A lone comment keeps the
// default two-space gap, so a one-off comment does not end up detached from its
// posting.
//
// The writer still enforces a two-space minimum, so a preserved column can never
// make a comment collide with the amount, assertion or cost text.
func commentColumn(lines []string, postings []ast.Posting) int {
	var columns []int
	for i := range postings {
		if postings[i].Comment == "" {
			continue
		}
		if column := displayColumn(lines, postings[i].CommentRange.Start.Line, postings[i].CommentRange.Start.Column); column > 0 {
			columns = append(columns, column)
		}
	}
	if len(columns) < 2 {
		return 0
	}

	detected := selectModalColumn(columns)
	if columnCount(columns, detected) < 2 {
		return 0
	}
	return detected
}

func columnCount(columns []int, column int) int {
	count := 0
	for _, candidate := range columns {
		if candidate == column {
			count++
		}
	}
	return count
}

// displayColumn converts a 1-indexed rune column on a line into a display column,
// which is the metric alignment arithmetic uses.
func displayColumn(lines []string, line, column int) int {
	if column <= 1 || line <= 0 || line > len(lines) {
		return 0
	}

	text := lines[line-1]
	runes := []rune(text)
	if column-1 > len(runes) {
		column = len(runes) + 1
	}
	return displayWidth(string(runes[:column-1]))
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

func formatTransactionWithOpts(tx *ast.Transaction, mapper *lsputil.PositionMapper, commodityFormats map[string]CommodityFormat, globalAccountCol int, globalDecimalCol int, globalAmountEndCol int, globalCommentCol int, opts Options) []protocol.TextEdit {
	return formatPostingsWithOpts(tx.Postings, mapper, commodityFormats, globalAccountCol, globalDecimalCol, globalAmountEndCol, globalCommentCol, opts)
}

// formatPostingsWithOpts renders a list of postings that share one alignment,
// which is how transactions, periodic transactions and auto posting rules are
// formatted.
func formatPostingsWithOpts(postings []ast.Posting, mapper *lsputil.PositionMapper, commodityFormats map[string]CommodityFormat, globalAccountCol int, globalDecimalCol int, globalAmountEndCol int, globalCommentCol int, opts Options) []protocol.TextEdit {
	if len(postings) == 0 {
		return nil
	}

	indent := strings.Repeat(" ", opts.IndentSize)
	var edits []protocol.TextEdit

	var alignment AlignmentInfo
	if opts.AlignAmounts {
		alignment = CalculateAlignmentWithGlobal(postings, commodityFormats, globalAccountCol)
		if globalDecimalCol > 0 {
			alignment.DecimalCol = globalDecimalCol
		}
		if globalAmountEndCol > 0 {
			alignment.AmountEndCol = globalAmountEndCol
		}
	}
	if globalCommentCol > 0 {
		alignment.CommentColumn = globalCommentCol
	}

	target := normalizeAlignTarget(opts.AmountAlignmentTarget)

	for i := range postings {
		posting := &postings[i]
		if posting.UnparsedTail.End.Offset > posting.UnparsedTail.Start.Offset {
			// The parser did not understand part of this line (for example the
			// `:=` balance assignment hledger 1.52 rejects). Rebuilding the line
			// from the AST would silently drop that text, so leave it alone.
			continue
		}
		formatted := appendPostingComment(formatPostingBody(posting, alignment, commodityFormats, indent, opts.AlignAmounts, target), posting, alignment)
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
func CalculateGlobalAlignmentColumnWithIndent(postings []ast.Posting, indentSize int) int {
	maxLen := 0
	for i := range postings {
		if accountLen := calculateAccountDisplayLength(&postings[i]); accountLen > maxLen {
			maxLen = accountLen
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

// DetectExistingAmountColumn returns the most common display (terminal cell)
// column where an amount currently begins across all postings, or 0 if no
// posting has an amount. Ties choose the larger column to avoid compressing
// hand-formatted files. Used for "smart" alignment detection: when a file
// already has a dominant visual layout, this column becomes a floor for new
// postings via Tab and full document formatting.
//
// The parser reports 1-indexed rune columns, so the content is needed to convert
// a column into display cells: a CJK or emoji account name before the amount
// occupies more cells than it has runes.
func DetectExistingAmountColumn(content string, postings []ast.Posting) int {
	return detectExistingAmountColumn(strings.Split(content, "\n"), postings)
}

func detectExistingAmountColumn(lines []string, postings []ast.Posting) int {
	var columns []int
	for i := range postings {
		p := &postings[i]
		if p.Amount == nil || p.Amount.Range.Start.Column <= 0 {
			continue
		}
		columns = append(columns, displayColumn(lines, p.Amount.Range.Start.Line, p.Amount.Range.Start.Column))
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
func DetectExistingAmountEndColumn(content string, postings []ast.Posting, commodityFormats map[string]CommodityFormat, target string) int {
	return detectExistingAmountEndColumn(strings.Split(content, "\n"), postings, commodityFormats, target)
}

func detectExistingAmountEndColumn(lines []string, postings []ast.Posting, commodityFormats map[string]CommodityFormat, target string) int {
	var columns []int
	for i := range postings {
		p := &postings[i]
		if p.Amount == nil || p.Amount.Commodity.Position != ast.CommodityRight {
			continue
		}
		if p.Amount.Range.Start.Column <= 0 {
			continue
		}
		startCol := displayColumn(lines, p.Amount.Range.Start.Line, p.Amount.Range.Start.Column)
		endCol := startCol + calculateAlignmentTargetLen(p, commodityFormats, target)
		columns = append(columns, endCol)
	}
	return selectModalColumn(columns)
}

func DetectExistingDecimalColumn(content string, postings []ast.Posting, commodityFormats map[string]CommodityFormat, target string) int {
	return detectExistingDecimalColumn(strings.Split(content, "\n"), postings, commodityFormats, target)
}

func detectExistingDecimalColumn(lines []string, postings []ast.Posting, commodityFormats map[string]CommodityFormat, target string) int {
	var columns []int
	for i := range postings {
		p := &postings[i]
		if p.Amount == nil || p.Amount.Range.Start.Column <= 0 {
			continue
		}
		startCol := displayColumn(lines, p.Amount.Range.Start.Line, p.Amount.Range.Start.Column)
		columns = append(columns, startCol+calculateAlignmentTargetDecimalPrefix(p, commodityFormats, target))
	}
	return selectModalColumn(columns)
}

// allAmountsCommodityRight reports whether every posting that carries an
// amount has a right-position commodity. Used as a guard for end-column
// alignment — mixed positions fall back to start-column semantics so
// commodity-left amounts (`$10.00`) keep their `$` column aligned.
func allAmountsCommodityRight(postings []ast.Posting) bool {
	seenAny := false
	for i := range postings {
		p := &postings[i]
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
func CalculateGlobalDecimalCol(postings []ast.Posting, commodityFormats map[string]CommodityFormat, accountCol int, target string) int {
	maxPrefix := 0
	for i := range postings {
		if postings[i].Amount != nil {
			prefix := calculateAlignmentTargetDecimalPrefix(&postings[i], commodityFormats, target)
			maxPrefix = max(maxPrefix, prefix)
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

func calculateGlobalAlignmentTargetLen(postings []ast.Posting, commodityFormats map[string]CommodityFormat, target string) int {
	maxLen := 0
	for i := range postings {
		maxLen = max(maxLen, calculateAlignmentTargetLen(&postings[i], commodityFormats, target))
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
		// A bare amount is written in the default commodity's number format, so
		// that format also decides where its decimal mark sits; see
		// formatAmountQuantity.
		if cf, ok := commodityFormats[amount.Commodity.Symbol]; ok && !amount.Commodity.Inferred {
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

	return sb.String()
}

// formatPostingBody renders a posting without its inline comment, so the caller
// can place the comment at the document's comment column.
func formatPostingBody(posting *ast.Posting, alignment AlignmentInfo, commodityFormats map[string]CommodityFormat, indent string, alignAmounts bool, target string) string {
	body := formatPostingWithOpts(posting, alignment, commodityFormats, indent, alignAmounts, target)
	return body
}

// appendPostingComment appends the inline comment at alignment.CommentColumn
// (two spaces after the body when no column is known).
func appendPostingComment(body string, posting *ast.Posting, alignment AlignmentInfo) string {
	if posting.Comment == "" {
		return body
	}

	spaces := minSpaces
	if alignment.CommentColumn > 0 {
		spaces = max(alignment.CommentColumn-displayWidth(body), minSpaces)
	}

	var sb strings.Builder
	sb.WriteString(body)
	sb.WriteString(strings.Repeat(" ", spaces))
	sb.WriteString("; ")
	sb.WriteString(strings.TrimLeft(posting.Comment, " \t"))
	return sb.String()
}

func resolveCommodityDisplay(amount *ast.Amount, commodityFormats map[string]CommodityFormat) (position ast.CommodityPosition, spaceBetween bool) {
	// An amount written without a symbol renders bare, so a declared format for
	// the inherited commodity must not add a space where the symbol would be.
	if commoditySymbolDisplay(&amount.Commodity) == "" {
		return ast.CommodityRight, false
	}

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

// commoditySymbolDisplay returns the commodity text to render. An amount that
// was written without a symbol, inheriting the journal's default commodity,
// has no symbol text of its own and therefore stays bare when rendered.
func commoditySymbolDisplay(c *ast.Commodity) string {
	symbol := c.WrittenSymbol()
	if c.Quoted && symbol != "" {
		return `"` + symbol + `"`
	}
	return symbol
}

func writeLotPrice(sb *strings.Builder, lot *ast.LotPrice, commodityFormats map[string]CommodityFormat) {
	if lot.Cost != nil {
		if lot.IsTotal {
			sb.WriteString(" {{")
		} else {
			sb.WriteString(" {")
		}
		if lot.Fixed {
			// {=PRICE} / {{=PRICE}}: the amount is the whole lot cost, so the
			// `=` must survive formatting (hledger 1.52 accepts this form).
			sb.WriteString("=")
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
		// An amount written without a symbol is read with the default commodity
		// number format (the D directive), so it is rendered with that same
		// format. Writing a bare number in the style of the commodity it
		// inherits would change what the text means when it is read again.
		if cf, ok := commodityFormats[amount.Commodity.Symbol]; ok && !amount.Commodity.Inferred {
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
