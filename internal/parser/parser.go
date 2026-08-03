package parser

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"github.com/juev/hledger-lsp/internal/ast"
)

type ParseError struct {
	Message string
	Pos     Position
	End     Position
}

// Context contains parse-affecting state inherited by an included journal.
// Its fields stay private so callers can only pass snapshots returned by parser.
type Context struct {
	defaultYear                 int
	decimalMark                 string
	decimalMarkExplicit         bool
	defaultCommodityDecimalMark string
	defaultCommoditySymbol      string
	commodityDecimalMarks       map[string]string
	accountPrefixes             []string
	basicAliases                []basicAlias
}

type basicAlias struct {
	original string
	alias    string
}

// ContextExports is the portion of a child context that an include may merge
// into its parent: declared commodity styles only.
type ContextExports struct {
	commodityDecimalMarks map[string]string
}

type IncludeResolver func(IncludeSite) ContextExports

type IncludeSite struct {
	Include ast.Include
	Context Context
}

type LocalItemKind uint8

const (
	LocalItemTransaction LocalItemKind = iota
	LocalItemPeriodicTransaction
	LocalItemAutoPostingRule
	LocalItemDirective
	LocalItemComment
	LocalItemInclude
)

type LocalItem struct {
	Kind  LocalItemKind
	Index int
}

type ContextCheckpoint struct {
	Offset  int
	Context Context
}

type ParseResult struct {
	Journal      *ast.Journal
	Items        []LocalItem
	IncludeSites []IncludeSite
	Checkpoints  []ContextCheckpoint
	Exports      ContextExports
}

func (e ParseError) Error() string {
	return fmt.Sprintf("%d:%d-%d:%d: %s", e.Pos.Line, e.Pos.Column, e.End.Line, e.End.Column, e.Message)
}

type Parser struct {
	lexer                        *Lexer
	current                      Token
	errors                       []ParseError
	defaultYear                  int
	decimalMark                  string
	decimalMarkExplicit          bool
	commodityDecimalMarks        map[string]string
	defaultCommodityDecimalMark  string
	defaultCommoditySymbol       string
	inputLen                     int
	accountPrefixes              []string // stack for nested apply account directives
	basicAliases                 []basicAlias
	items                        []LocalItem
	includeSites                 []IncludeSite
	checkpoints                  []ContextCheckpoint
	resolveInclude               IncludeResolver
	initialCommodityDecimalMarks map[string]string
}

// Parse turns journal text into an AST. The input must already use LF line
// endings — see ParseWithContext.
func Parse(input string) (*ast.Journal, []ParseError) {
	result, errs := ParseWithContext(input, Context{}, nil)
	return result.Journal, errs
}

// ParseWithContext parses journal text that carries an outer parse context.
//
// The input must use LF line endings. Positions in the returned AST are
// offsets into exactly the string passed in, and callers map them back to LSP
// positions against that same string; normalizing here would instead give the
// AST a coordinate system of its own and shift every position on a CRLF
// document. Callers normalize on the way in — textutil.NormalizeLineEndings.
func ParseWithContext(input string, initial Context, resolve IncludeResolver) (ParseResult, []ParseError) {
	context := cloneContext(initial)
	p := &Parser{
		lexer:                        NewLexer(input),
		inputLen:                     len(input),
		defaultYear:                  context.defaultYear,
		decimalMark:                  context.decimalMark,
		decimalMarkExplicit:          context.decimalMarkExplicit,
		defaultCommodityDecimalMark:  context.defaultCommodityDecimalMark,
		defaultCommoditySymbol:       context.defaultCommoditySymbol,
		commodityDecimalMarks:        context.commodityDecimalMarks,
		accountPrefixes:              context.accountPrefixes,
		basicAliases:                 context.basicAliases,
		resolveInclude:               resolve,
		initialCommodityDecimalMarks: cloneStringMap(context.commodityDecimalMarks),
	}
	p.advance()
	journal := p.parseJournal()
	return ParseResult{
		Journal:      journal,
		Items:        append([]LocalItem(nil), p.items...),
		IncludeSites: append([]IncludeSite(nil), p.includeSites...),
		Checkpoints:  append([]ContextCheckpoint(nil), p.checkpoints...),
		Exports:      p.contextExports(),
	}, p.errors
}

func (p *Parser) parseJournal() *ast.Journal {
	estimatedTx := p.inputLen / 100
	journal := &ast.Journal{
		Transactions: make([]ast.Transaction, 0, estimatedTx),
	}

	for p.current.Type != TokenEOF {
		switch p.current.Type {
		case TokenNewline:
			p.advance()
		case TokenComment:
			journal.Comments = append(journal.Comments, p.parseComment())
			p.items = append(p.items, LocalItem{Kind: LocalItemComment, Index: len(journal.Comments) - 1})
		case TokenDate:
			tx := p.parseTransaction()
			if tx != nil {
				journal.Transactions = append(journal.Transactions, *tx)
				p.items = append(p.items, LocalItem{Kind: LocalItemTransaction, Index: len(journal.Transactions) - 1})
			}
		case TokenTilde:
			ptx := p.parsePeriodicTransaction()
			if ptx != nil {
				journal.PeriodicTransactions = append(journal.PeriodicTransactions, *ptx)
				p.items = append(p.items, LocalItem{Kind: LocalItemPeriodicTransaction, Index: len(journal.PeriodicTransactions) - 1})
			}
		case TokenAutoRule:
			rule := p.parseAutoPostingRule()
			if rule != nil {
				journal.AutoPostingRules = append(journal.AutoPostingRules, *rule)
				p.items = append(p.items, LocalItem{Kind: LocalItemAutoPostingRule, Index: len(journal.AutoPostingRules) - 1})
			}
		case TokenDirective:
			dir := p.parseDirective()
			if dir != nil {
				if inc, ok := dir.(ast.Include); ok {
					journal.Includes = append(journal.Includes, inc)
					p.items = append(p.items, LocalItem{Kind: LocalItemInclude, Index: len(journal.Includes) - 1})
					site := IncludeSite{Include: inc, Context: p.context()}
					p.includeSites = append(p.includeSites, site)
					if p.resolveInclude != nil {
						p.mergeExports(p.resolveInclude(site))
					}
				} else {
					journal.Directives = append(journal.Directives, dir)
					p.items = append(p.items, LocalItem{Kind: LocalItemDirective, Index: len(journal.Directives) - 1})
				}
			}
			p.checkpoint()
		case TokenIndent:
			// Whitespace-only lines: consume indent, newline handled by next iteration
			p.advance()
			if p.current.Type != TokenNewline && p.current.Type != TokenEOF {
				p.error("unexpected content: %s", p.current.Value)
				p.skipToNextLine()
			}
		default:
			if p.current.Value != "" {
				p.error("unexpected content: %s", p.current.Value)
			} else {
				p.error("unexpected content")
			}
			p.skipToNextLine()
		}
	}

	return journal
}

func (p *Parser) parseTransaction() *ast.Transaction {
	tx := &ast.Transaction{
		Postings: make([]ast.Posting, 0, 3),
	}
	tx.Range.Start = toASTPosition(p.current.Pos)

	date := p.parseDate()
	if date == nil {
		p.skipToNextLine()
		return nil
	}
	tx.Date = *date

	if p.current.Type == TokenEquals {
		p.advance()
		date2 := p.parseDate()
		if date2 != nil {
			tx.Date2 = date2
		}
	}

	if p.current.Type == TokenStatus {
		tx.Status = p.parseStatus()
	}

	if p.current.Type == TokenCode {
		tx.Code = p.current.Value
		p.advance()
	}

	if p.current.Type == TokenText {
		desc := p.current.Value
		descPos := p.current.Pos
		p.advance()

		if p.current.Type == TokenPipe {
			tx.Payee = strings.TrimSpace(desc)
			tx.DescriptionRange = descRange(descPos, tx.Payee)
			p.advance()
			tx.Note = p.parseNote()
			tx.Description = tx.Payee
			if tx.Note != "" {
				tx.Description = tx.Payee + " | " + tx.Note
			}
		} else {
			tx.Description = desc
			tx.DescriptionRange = descRange(descPos, desc)
		}
	}

	if p.current.Type == TokenComment {
		tx.Comments = append(tx.Comments, p.parseComment())
	}

	if p.current.Type == TokenNewline {
		p.advance()
	}

	for p.current.Type == TokenIndent {
		posting := p.parsePosting()
		if posting != nil {
			tx.Postings = append(tx.Postings, *posting)
		}
		if p.current.Type == TokenNewline {
			p.advance()
		}
	}

	tx.Range.End = toASTPosition(p.current.Pos)
	return tx
}

func (p *Parser) parsePeriodicTransaction() *ast.PeriodicTransaction {
	ptx := &ast.PeriodicTransaction{
		Postings: make([]ast.Posting, 0, 3),
	}
	ptx.Range.Start = toASTPosition(p.current.Pos)

	// Skip the ~ token
	p.advance()

	// Parse period expression (e.g., "monthly", "every 2 weeks")
	var periodPos Position
	if p.current.Type == TokenText {
		ptx.Period = strings.TrimSpace(p.current.Value)
		periodPos = p.current.Pos
		p.advance()
	} else {
		p.error("expected period expression after ~")
		p.skipToNextLine()
		return nil
	}

	// Parse optional status
	if p.current.Type == TokenStatus {
		ptx.Status = p.parseStatus()
	}

	// Parse optional code
	if p.current.Type == TokenCode {
		ptx.Code = p.current.Value
		p.advance()
	}

	// Parse optional description
	// Period text may include the description when separated by |
	// e.g. "~ monthly from 2024-01  Payee | note" — scanText returns
	// "monthly from 2024-01  Payee" as one token, then TokenPipe follows.
	switch p.current.Type {
	case TokenText:
		desc := p.current.Value
		descPos := p.current.Pos
		p.advance()

		if p.current.Type == TokenPipe {
			ptx.Payee = strings.TrimSpace(desc)
			ptx.DescriptionRange = descRange(descPos, ptx.Payee)
			p.advance()
			ptx.Note = p.parseNote()
			ptx.Description = ptx.Payee
			if ptx.Note != "" {
				ptx.Description = ptx.Payee + " | " + ptx.Note
			}
		} else {
			ptx.Description = desc
			ptx.DescriptionRange = descRange(descPos, desc)
		}
	case TokenPipe:
		if idx := strings.Index(ptx.Period, "  "); idx >= 0 {
			raw := ptx.Period[idx:]
			ptx.Payee = strings.TrimSpace(raw)
			leadingSpaces := len(raw) - len(strings.TrimLeft(raw, " \t"))
			payeeStart := idx + leadingSpaces
			payeePos := Position{
				Line:   periodPos.Line,
				Column: periodPos.Column + utf8.RuneCountInString(ptx.Period[:payeeStart]),
				Offset: periodPos.Offset + payeeStart,
			}
			ptx.DescriptionRange = descRange(payeePos, ptx.Payee)
			ptx.Period = strings.TrimSpace(ptx.Period[:idx])
		}
		p.advance()
		ptx.Note = p.parseNote()
		if ptx.Payee != "" {
			ptx.Description = ptx.Payee
			if ptx.Note != "" {
				ptx.Description = ptx.Payee + " | " + ptx.Note
			}
		}
	}

	// Parse optional comment
	if p.current.Type == TokenComment {
		ptx.Comments = append(ptx.Comments, p.parseComment())
	}

	// Skip newline
	if p.current.Type == TokenNewline {
		p.advance()
	}

	// Parse postings
	for p.current.Type == TokenIndent {
		posting := p.parsePosting()
		if posting != nil {
			ptx.Postings = append(ptx.Postings, *posting)
		}
		if p.current.Type == TokenNewline {
			p.advance()
		}
	}

	ptx.Range.End = toASTPosition(p.current.Pos)
	return ptx
}

func (p *Parser) parseAutoPostingRule() *ast.AutoPostingRule {
	rule := &ast.AutoPostingRule{
		Postings: make([]ast.Posting, 0, 2),
	}
	rule.Range.Start = toASTPosition(p.current.Pos)

	// Skip the = token
	p.advance()

	// Parse query (everything until newline)
	var queryParts []string
	for p.current.Type != TokenNewline && p.current.Type != TokenEOF && p.current.Type != TokenComment {
		switch p.current.Type {
		case TokenText, TokenAccount, TokenNumber:
			queryParts = append(queryParts, p.current.Value)
		case TokenColon:
			queryParts = append(queryParts, ":")
		}
		p.advance()
	}

	if len(queryParts) == 0 {
		p.error("expected query expression after =")
		p.skipToNextLine()
		return nil
	}

	rule.Query = strings.Join(queryParts, "")

	// Parse optional comment
	if p.current.Type == TokenComment {
		rule.Comments = append(rule.Comments, p.parseComment())
	}

	// Skip newline
	if p.current.Type == TokenNewline {
		p.advance()
	}

	// Parse postings
	for p.current.Type == TokenIndent {
		posting := p.parsePosting()
		if posting != nil {
			rule.Postings = append(rule.Postings, *posting)
		}
		if p.current.Type == TokenNewline {
			p.advance()
		}
	}

	rule.Range.End = toASTPosition(p.current.Pos)
	return rule
}

func (p *Parser) parseDate() *ast.Date {
	if p.current.Type != TokenDate {
		p.error("expected date")
		return nil
	}

	value := p.current.Value
	pos := p.current.Pos
	end := p.current.End
	p.advance()

	var sep byte
	for i := 0; i < len(value); i++ {
		if value[i] == '-' || value[i] == '/' || value[i] == '.' {
			sep = value[i]
			break
		}
	}

	parts := strings.Split(value, string(sep))

	switch len(parts) {
	case 2:
		if p.defaultYear == 0 {
			p.errorAt(pos, end, "partial date requires Y directive: %s", value)
			return nil
		}
		month, err := strconv.Atoi(parts[0])
		if err != nil {
			p.errorAt(pos, end, "invalid month: %s", parts[0])
			return nil
		}
		day, err := strconv.Atoi(parts[1])
		if err != nil {
			p.errorAt(pos, end, "invalid day: %s", parts[1])
			return nil
		}
		return &ast.Date{
			Year:  p.defaultYear,
			Month: month,
			Day:   day,
			Range: ast.Range{Start: toASTPosition(pos), End: toASTPosition(end)},
		}
	case 3:
		year, err := strconv.Atoi(parts[0])
		if err != nil {
			p.errorAt(pos, end, "invalid year: %s", parts[0])
			return nil
		}
		month, err := strconv.Atoi(parts[1])
		if err != nil {
			p.errorAt(pos, end, "invalid month: %s", parts[1])
			return nil
		}
		day, err := strconv.Atoi(parts[2])
		if err != nil {
			p.errorAt(pos, end, "invalid day: %s", parts[2])
			return nil
		}
		return &ast.Date{
			Year:  year,
			Month: month,
			Day:   day,
			Range: ast.Range{Start: toASTPosition(pos), End: toASTPosition(end)},
		}
	default:
		p.errorAt(pos, end, "invalid date format: %s", value)
		return nil
	}
}

func (p *Parser) parseStatus() ast.Status {
	status := ast.StatusNone
	if p.current.Type == TokenStatus {
		switch p.current.Value {
		case "*":
			status = ast.StatusCleared
		case "!":
			status = ast.StatusPending
		}
		p.advance()
	}
	return status
}

func (p *Parser) parsePosting() *ast.Posting {
	if p.current.Type != TokenIndent {
		return nil
	}
	p.advance()

	if p.current.Type == TokenComment {
		p.parseComment()
		return nil
	}

	if p.current.Type == TokenNewline || p.current.Type == TokenEOF {
		return nil
	}

	posting := &ast.Posting{}
	posting.Range.Start = toASTPosition(p.current.Pos)

	if p.current.Type == TokenStatus {
		posting.Status = p.parseStatus()
	}

	var closingToken TokenType
	switch p.current.Type {
	case TokenLBracket:
		posting.Virtual = ast.VirtualBalanced
		closingToken = TokenRBracket
		p.advance()
	case TokenLParen:
		posting.Virtual = ast.VirtualUnbalanced
		closingToken = TokenRParen
		p.advance()
	}

	if p.current.Type != TokenAccount {
		p.error("expected account name")
		p.skipToNextLine()
		return nil
	}

	originalName := p.current.Value
	resolvedName := p.resolveAccountName(originalName)

	posting.Account = ast.Account{
		Name:         originalName,
		ResolvedName: resolvedName,
		Range:        ast.Range{Start: toASTPosition(p.current.Pos), End: toASTPosition(p.current.End)},
	}
	p.advance()

	if closingToken != 0 && p.current.Type == closingToken {
		p.advance()
	}

	if isCommodityToken(p.current.Type) || p.current.Type == TokenNumber || p.current.Type == TokenSign {
		amount := p.parseAmount()
		if amount != nil {
			posting.Amount = amount
		}
	}

	p.parseLotAnnotations(posting)

	if p.current.Type == TokenEquals || p.current.Type == TokenDoubleEquals ||
		p.current.Type == TokenEqualsStar || p.current.Type == TokenDoubleEqualsStar {
		posting.BalanceAssertion = p.parseBalanceAssertion()
	}

	if p.current.Type == TokenComment {
		posting.Comment = p.current.Value
		posting.Tags = parseTags(p.current.Value, p.current.Pos)
		p.advance()
	}

	posting.Range.End = toASTPosition(p.current.Pos)
	return posting
}

func (p *Parser) parseAmount() *ast.Amount {
	amount := &ast.Amount{}
	amount.Range.Start = toASTPosition(p.current.Pos)

	sign := ""
	signBeforeCommodity := false
	if p.current.Type == TokenSign {
		sign = p.current.Value
		signBeforeCommodity = true
		p.advance()
	}

	if isCommodityToken(p.current.Type) {
		amount.Commodity = ast.Commodity{
			Symbol:   p.current.Value,
			Quoted:   p.current.Type == TokenQuotedCommodity,
			Position: ast.CommodityLeft,
			Range: ast.Range{
				Start: toASTPosition(p.current.Pos),
				End:   toASTPosition(p.current.End),
			},
		}
		if signBeforeCommodity && (sign == "-" || sign == "+") {
			amount.SignBeforeCommodity = true
		}
		p.advance()
	}

	if p.current.Type == TokenSign {
		if sign == "" {
			sign = p.current.Value
		}
		p.advance()
	}

	if p.current.Type != TokenNumber {
		p.error("expected number")
		p.skipToNextLine()
		return nil
	}

	rawNumberStr := p.current.Value
	if sign == "-" && !strings.HasPrefix(rawNumberStr, "-") {
		rawNumberStr = "-" + rawNumberStr
	}
	amount.RawQuantity = rawNumberStr
	rawTokenValue := p.current.Value
	p.advance()

	if amount.Commodity.Symbol == "" {
		isCommodity := isCommodityToken(p.current.Type) ||
			(p.current.Type == TokenText && isValidCommodityText(p.current.Value))
		if isCommodity {
			amount.Commodity = ast.Commodity{
				Symbol:   p.current.Value,
				Quoted:   p.current.Type == TokenQuotedCommodity,
				Position: ast.CommodityRight,
				Range: ast.Range{
					Start: toASTPosition(p.current.Pos),
					End:   toASTPosition(p.current.End),
				},
			}
			p.advance()
		}
	}

	numberStr := strings.ReplaceAll(rawNumberStr, " ", "")
	mark := p.resolveDecimalMark(amount.Commodity.Symbol)
	numberStr = normalizeNumber(numberStr, mark)

	qty, err := decimal.NewFromString(numberStr)
	if err != nil {
		p.error("invalid number: %s", rawTokenValue)
		return nil
	}
	amount.Quantity = qty

	amount.Range.End = toASTPosition(p.current.Pos)
	return amount
}

func (p *Parser) parseCost() *ast.Cost {
	cost := &ast.Cost{}
	cost.Range.Start = toASTPosition(p.current.Pos)

	if p.current.Type == TokenAtAt {
		cost.IsTotal = true
	}
	p.advance()

	amount := p.parseAmount()
	if amount == nil {
		return nil
	}
	cost.Amount = *amount
	cost.Range.End = toASTPosition(p.current.Pos)
	return cost
}

func (p *Parser) ensureLotPrice(posting *ast.Posting) *ast.LotPrice {
	if posting.LotPrice == nil {
		posting.LotPrice = &ast.LotPrice{}
		posting.LotPrice.Range.Start = toASTPosition(p.current.Pos)
	}
	return posting.LotPrice
}

func (p *Parser) parseLotAnnotations(posting *ast.Posting) {
	p.parseAnnotationLoop(
		func() *ast.LotPrice { return p.ensureLotPrice(posting) },
		func(c *ast.Cost) { posting.Cost = c },
	)
}

func (p *Parser) parseLotDate(lot *ast.LotPrice) {
	p.advance() // consume [

	switch p.current.Type {
	case TokenDate:
		lot.Date = p.current.Value
		lot.Range.End = toASTPosition(p.current.End)
		p.advance()
	case TokenNumber:
		date := p.current.Value
		p.advance()
		for p.current.Type == TokenSign || p.current.Type == TokenNumber {
			date += p.current.Value
			p.advance()
		}
		lot.Date = date
		lot.Range.End = toASTPosition(p.current.Pos)
	}

	if p.current.Type == TokenRBracket {
		lot.Range.End = toASTPosition(p.current.End)
		p.advance()
	}
}

func (p *Parser) looksLikeConsolidatedDate() bool {
	if p.current.Type != TokenNumber || len(p.current.Value) != 4 {
		return false
	}
	endOffset := p.current.End.Offset
	if endOffset < len(p.lexer.input) && p.lexer.input[endOffset] == '-' {
		return true
	}
	return false
}

func (p *Parser) parseConsolidatedDate() string {
	date := p.current.Value
	p.advance()
	for p.current.Type == TokenSign || p.current.Type == TokenNumber {
		date += p.current.Value
		p.advance()
	}
	date = strings.TrimRight(date, ",")
	return date
}

func (p *Parser) parseLotPriceInto(lot *ast.LotPrice) {
	if p.current.Type == TokenDoubleLBrace {
		lot.IsTotal = true
	}
	p.advance()

	closingToken := TokenRBrace
	if lot.IsTotal {
		closingToken = TokenDoubleRBrace
	}

	if p.current.Type == closingToken {
		lot.Range.End = toASTPosition(p.current.End)
		p.advance()
		return
	}

	if p.looksLikeConsolidatedDate() {
		lot.Date = p.parseConsolidatedDate()
	}

	if p.current.Type == TokenQuotedCommodity {
		lot.Label = p.current.Value
		p.advance()
		if p.current.Type == TokenText && strings.HasPrefix(p.current.Value, ",") {
			p.advance()
		}
	}

	if p.current.Type != closingToken {
		if isCommodityToken(p.current.Type) || p.current.Type == TokenNumber || p.current.Type == TokenSign {
			amount := p.parseAmount()
			if amount != nil {
				lot.Cost = amount
			}
		}
	}

	if p.current.Type == closingToken {
		lot.Range.End = toASTPosition(p.current.End)
		p.advance()
	}
}

func (p *Parser) parseBalanceAssertion() *ast.BalanceAssertion {
	ba := &ast.BalanceAssertion{}
	ba.Range.Start = toASTPosition(p.current.Pos)

	switch p.current.Type {
	case TokenDoubleEquals:
		ba.IsStrict = true
	case TokenEqualsStar:
		ba.IsInclusive = true
	case TokenDoubleEqualsStar:
		ba.IsStrict = true
		ba.IsInclusive = true
	}
	p.advance()

	amount := p.parseAmount()
	if amount == nil {
		return nil
	}
	ba.Amount = *amount
	p.parseBalanceAssertionAnnotations(ba)
	ba.Range.End = toASTPosition(p.current.Pos)
	return ba
}

func (p *Parser) parseBalanceAssertionAnnotations(ba *ast.BalanceAssertion) {
	p.parseAnnotationLoop(
		func() *ast.LotPrice {
			if ba.LotPrice == nil {
				ba.LotPrice = &ast.LotPrice{}
				ba.LotPrice.Range.Start = toASTPosition(p.current.Pos)
			}
			return ba.LotPrice
		},
		func(c *ast.Cost) { ba.Cost = c },
	)
}

func (p *Parser) parseAnnotationLoop(ensureLot func() *ast.LotPrice, setCost func(*ast.Cost)) {
	for {
		switch p.current.Type {
		case TokenLBrace, TokenDoubleLBrace:
			p.parseLotPriceInto(ensureLot())
		case TokenLBracket:
			p.parseLotDate(ensureLot())
		case TokenCode:
			lot := ensureLot()
			lot.Label = p.current.Value
			lot.Range.End = toASTPosition(p.current.End)
			p.advance()
		case TokenAt, TokenAtAt:
			setCost(p.parseCost())
		default:
			return
		}
	}
}

func (p *Parser) parseDirective() ast.Directive {
	directive := p.current.Value
	pos := p.current.Pos
	p.advance()

	switch directive {
	case "account":
		return p.parseAccountDirective(pos)
	case "commodity":
		return p.parseCommodityDirective(pos)
	case "include":
		return p.parseIncludeDirective(pos)
	case "P":
		return p.parsePriceDirective(pos)
	case "Y", "year":
		return p.parseYearDirective(pos)
	case "D":
		return p.parseDefaultCommodityDirective(pos)
	case "decimal-mark":
		return p.parseDecimalMarkDirective(pos)
	case "payee":
		return p.parsePayeeDirective(pos)
	case "tag":
		return p.parseTagDirective(pos)
	case "alias":
		return p.parseAliasDirective(pos)
	case "apply":
		return p.parseApplyDirective(pos)
	case "comment":
		return p.parseCommentBlock(pos)
	case "end":
		return p.parseEndDirective(pos)
	default:
		p.skipToNextLine()
		return nil
	}
}

func (p *Parser) parseAccountDirective(startPos Position) ast.Directive {
	if p.current.Type != TokenAccount && p.current.Type != TokenText {
		p.error("expected account name")
		p.skipToNextLine()
		return nil
	}

	accountName := p.current.Value
	accountPos := p.current.Pos
	p.advance()

	if p.current.Type == TokenText {
		accountName += " " + p.current.Value
		p.advance()
	}

	dir := ast.AccountDirective{
		Account: ast.Account{
			Name:         accountName,
			ResolvedName: p.resolveAccountName(accountName),
			Range:        ast.Range{Start: toASTPosition(accountPos)},
		},
		Range: ast.Range{Start: toASTPosition(startPos)},
	}

	if p.current.Type == TokenComment {
		dir.Comment = p.current.Value
		dir.Tags = parseTags(p.current.Value, p.current.Pos)
		p.advance()
	}

	for p.current.Type != TokenNewline && p.current.Type != TokenEOF {
		p.advance()
	}

	dir.Subdirs = p.parseSubdirectives()
	dir.Range.End = toASTPosition(p.current.Pos)

	return dir
}

func (p *Parser) parseCommodityDirective(startPos Position) ast.Directive {
	dir := ast.CommodityDirective{
		Range: ast.Range{Start: toASTPosition(startPos)},
	}

	// Handle inline format: "commodity $1000.00" (symbol first, then number)
	switch p.current.Type {
	case TokenCommodity, TokenQuotedCommodity:
		symbol := p.current.Value
		dir.Commodity = ast.Commodity{
			Symbol: symbol,
			Quoted: p.current.Type == TokenQuotedCommodity,
			Range: ast.Range{
				Start: toASTPosition(p.current.Pos),
				End:   toASTPosition(p.current.End),
			},
		}
		p.advance()

		// Collect number part for format (no space for currency symbols)
		if p.current.Type == TokenNumber {
			dir.Format = symbol + p.current.Value
			p.advance()
		}
	case TokenNumber:
		// Handle inline format: "commodity 1.000,00 USD" (number first, then symbol)
		number := p.current.Value
		p.advance()

		if isCommodityToken(p.current.Type) || p.current.Type == TokenText {
			dir.Commodity = ast.Commodity{
				Symbol: p.current.Value,
				Quoted: p.current.Type == TokenQuotedCommodity,
				Range: ast.Range{
					Start: toASTPosition(p.current.Pos),
					End:   toASTPosition(p.current.End),
				},
			}
			dir.Format = number + " " + p.current.Value
			p.advance()
		}
	case TokenText:
		dir.Commodity = ast.Commodity{
			Symbol: p.current.Value,
			Range: ast.Range{
				Start: toASTPosition(p.current.Pos),
				End:   toASTPosition(p.current.End),
			},
		}
		p.advance()
	}

	for p.current.Type != TokenNewline && p.current.Type != TokenEOF && p.current.Type != TokenComment {
		p.advance()
	}
	if p.current.Type == TokenComment {
		p.advance()
	}

	dir.Subdirs = p.parseSubdirectives()

	if format, ok := dir.Subdirs["format"]; ok {
		dir.Format = format
	}
	if note, ok := dir.Subdirs["note"]; ok {
		dir.Note = note
	}

	if dir.Format != "" && dir.Commodity.Symbol != "" {
		if mark := inferDecimalMarkFromFormat(dir.Format); mark != "" {
			p.commodityDecimalMarks[dir.Commodity.Symbol] = mark
		}
	}

	dir.Range.End = toASTPosition(p.current.Pos)
	return dir
}

func (p *Parser) parseIncludeDirective(startPos Position) ast.Directive {
	var path strings.Builder

	for p.current.Type != TokenNewline && p.current.Type != TokenEOF && p.current.Type != TokenComment {
		path.WriteString(p.current.Value)
		p.advance()
	}

	pathStr := strings.TrimSpace(path.String())
	if pathStr == "" {
		p.error("expected file path")
		p.skipToNextLine()
		return nil
	}

	inc := ast.Include{
		Path:  pathStr,
		Range: ast.Range{Start: toASTPosition(startPos)},
	}
	inc.Range.End = toASTPosition(p.current.Pos)
	p.skipToNextLine()
	return inc
}

func (p *Parser) parsePriceDirective(startPos Position) ast.Directive {
	dir := ast.PriceDirective{
		Range: ast.Range{Start: toASTPosition(startPos)},
	}

	date := p.parseDate()
	if date == nil {
		p.skipToNextLine()
		return nil
	}
	dir.Date = *date

	if isCommodityToken(p.current.Type) || p.current.Type == TokenText {
		dir.Commodity = ast.Commodity{
			Symbol: p.current.Value,
			Quoted: p.current.Type == TokenQuotedCommodity,
			Range: ast.Range{
				Start: toASTPosition(p.current.Pos),
				End:   toASTPosition(p.current.End),
			},
		}
		p.advance()
	} else {
		p.error("expected commodity")
		p.skipToNextLine()
		return nil
	}

	price := p.parseAmount()
	if price == nil {
		p.skipToNextLine()
		return nil
	}
	dir.Price = *price

	dir.Range.End = toASTPosition(p.current.Pos)
	p.skipToNextLine()
	return dir
}

func (p *Parser) parseSubdirectives() map[string]string {
	subdirs := make(map[string]string)

	for p.current.Type == TokenNewline {
		p.advance()

		if p.current.Type != TokenIndent {
			break
		}
		p.advance()

		if p.current.Type == TokenComment {
			p.advance()
			continue
		}

		if p.current.Type == TokenNewline || p.current.Type == TokenEOF {
			continue
		}

		if p.current.Type == TokenText {
			line := p.current.Value
			p.advance()

			spaceIdx := strings.Index(line, " ")
			if spaceIdx > 0 {
				name := line[:spaceIdx]
				value := strings.TrimSpace(line[spaceIdx+1:])
				subdirs[name] = value
			} else {
				subdirs[line] = ""
			}
			continue
		}

		name := ""
		if p.current.Type == TokenDirective {
			name = p.current.Value
			p.advance()
		} else {
			p.skipToNextLine()
			continue
		}

		var value strings.Builder
		for p.current.Type != TokenNewline && p.current.Type != TokenEOF && p.current.Type != TokenComment {
			value.WriteString(p.current.Value)
			if p.current.Type == TokenNumber || isCommodityToken(p.current.Type) || p.current.Type == TokenText {
				value.WriteString(" ")
			}
			p.advance()
		}

		subdirs[name] = strings.TrimSpace(value.String())
	}

	return subdirs
}

func (p *Parser) parseDefaultCommodityDirective(startPos Position) ast.Directive {
	dir := ast.DefaultCommodityDirective{
		Range: ast.Range{Start: toASTPosition(startPos)},
	}

	var numberStr string

	switch p.current.Type {
	case TokenCommodity, TokenQuotedCommodity:
		symbol := p.current.Value
		dir.Symbol = symbol
		p.advance()

		if p.current.Type == TokenNumber {
			numberStr = p.current.Value
			dir.Format = symbol + numberStr
			p.advance()
		}
	case TokenNumber:
		numberStr = p.current.Value
		p.advance()

		if isCommodityToken(p.current.Type) || p.current.Type == TokenText {
			dir.Symbol = p.current.Value
			dir.Format = numberStr + " " + p.current.Value
			p.advance()
		}
	}

	if !p.decimalMarkExplicit && numberStr != "" {
		if mark := inferDecimalMark(numberStr); mark != "" {
			p.defaultCommodityDecimalMark = mark
			p.defaultCommoditySymbol = dir.Symbol
		}
	}

	dir.Range.End = toASTPosition(p.current.Pos)
	p.skipToNextLine()
	return dir
}

func (p *Parser) parseDecimalMarkDirective(startPos Position) ast.Directive {
	dir := ast.DecimalMarkDirective{
		Range: ast.Range{Start: toASTPosition(startPos)},
	}

	if p.current.Type != TokenText && !isCommodityToken(p.current.Type) {
		p.error("expected decimal mark (. or ,)")
		p.skipToNextLine()
		return nil
	}

	mark := strings.TrimSpace(p.current.Value)
	if mark != "." && mark != "," {
		p.error("decimal mark must be . or ,")
		p.skipToNextLine()
		return nil
	}

	dir.Mark = mark
	p.decimalMark = mark
	p.decimalMarkExplicit = true
	p.advance()

	dir.Range.End = toASTPosition(p.current.Pos)
	p.skipToNextLine()
	return dir
}

func (p *Parser) parsePayeeDirective(startPos Position) ast.Directive {
	dir := ast.PayeeDirective{
		Range: ast.Range{Start: toASTPosition(startPos)},
	}

	// Collect all tokens until newline to form the payee name
	var parts []string
	for p.current.Type != TokenNewline && p.current.Type != TokenEOF && p.current.Type != TokenComment {
		if p.current.Type == TokenText || p.current.Type == TokenAccount {
			parts = append(parts, p.current.Value)
		}
		p.advance()
	}

	if len(parts) == 0 {
		p.error("expected payee name")
		return nil
	}

	dir.Name = strings.Join(parts, " ")
	dir.Range.End = toASTPosition(p.current.Pos)
	return dir
}

func (p *Parser) parseTagDirective(startPos Position) ast.Directive {
	dir := ast.TagDirective{
		Range: ast.Range{Start: toASTPosition(startPos)},
	}

	if p.current.Type != TokenText && p.current.Type != TokenAccount {
		p.error("expected tag name")
		p.skipToNextLine()
		return nil
	}

	dir.Name = strings.TrimSpace(p.current.Value)
	p.advance()

	dir.Range.End = toASTPosition(p.current.Pos)
	p.skipToNextLine()
	return dir
}

func (p *Parser) parseAliasDirective(startPos Position) ast.Directive {
	dir := ast.AliasDirective{
		Range: ast.Range{Start: toASTPosition(startPos)},
	}
	if p.current.Type == TokenAccount {
		if original, alias, ok := strings.Cut(p.current.Value, "="); ok {
			dir.Original = strings.TrimSpace(original)
			dir.Alias = strings.TrimSpace(alias)
			dir.Range.End = toASTPosition(p.current.End)
			p.advance()
			p.basicAliases = append(p.basicAliases, basicAlias{original: dir.Original, alias: dir.Alias})
			return dir
		}
	}

	// Collect tokens until we hit '=' sign
	var originalParts []string
	isRegex := false

	for p.current.Type != TokenEquals && p.current.Type != TokenNewline && p.current.Type != TokenEOF {
		switch p.current.Type {
		case TokenText, TokenAccount, TokenNumber:
			originalParts = append(originalParts, p.current.Value)
		case TokenColon:
			originalParts = append(originalParts, ":")
		}
		p.advance()
	}

	if len(originalParts) == 0 {
		p.error("expected alias pattern or name")
		p.skipToNextLine()
		return nil
	}

	original := strings.Join(originalParts, "")
	// Check if it's a regex pattern (starts and ends with /)
	if strings.HasPrefix(original, "/") && strings.HasSuffix(original, "/") {
		isRegex = true
		// Remove leading and trailing slashes
		original = original[1 : len(original)-1]
	}

	dir.Original = strings.TrimSpace(original)
	dir.IsRegex = isRegex

	// Expect '=' sign
	if p.current.Type != TokenEquals {
		p.error("expected = in alias directive")
		p.skipToNextLine()
		return nil
	}
	p.advance()

	// Parse the replacement/alias - collect all remaining tokens until newline
	var aliasParts []string
	for p.current.Type != TokenNewline && p.current.Type != TokenEOF && p.current.Type != TokenComment {
		switch p.current.Type {
		case TokenText, TokenAccount, TokenNumber:
			aliasParts = append(aliasParts, p.current.Value)
		case TokenColon:
			aliasParts = append(aliasParts, ":")
		}
		p.advance()
	}

	if len(aliasParts) == 0 {
		p.error("expected alias replacement")
		return nil
	}

	dir.Alias = strings.Join(aliasParts, "")
	dir.Range.End = toASTPosition(p.current.Pos)
	if !dir.IsRegex {
		p.basicAliases = append(p.basicAliases, basicAlias{original: dir.Original, alias: dir.Alias})
	}
	return dir
}

func (p *Parser) parseApplyDirective(_ Position) ast.Directive {
	// Expect "account" after "apply"
	if p.current.Type != TokenText && !isCommodityToken(p.current.Type) && p.current.Type != TokenAccount {
		p.error("expected 'account' after 'apply'")
		p.skipToNextLine()
		return nil
	}

	if strings.TrimSpace(p.current.Value) != "account" {
		p.error("expected 'account' after 'apply'")
		p.skipToNextLine()
		return nil
	}
	p.advance()

	// Parse prefix (rest of line)
	var prefixParts []string
	for p.current.Type != TokenNewline && p.current.Type != TokenEOF && p.current.Type != TokenComment {
		switch p.current.Type {
		case TokenText, TokenAccount:
			prefixParts = append(prefixParts, p.current.Value)
		case TokenColon:
			prefixParts = append(prefixParts, ":")
		}
		p.advance()
	}

	prefix := strings.Join(prefixParts, "")
	if prefix == "" {
		p.error("expected account prefix after 'apply account'")
		p.skipToNextLine()
		return nil
	}

	// Push to stack
	p.accountPrefixes = append(p.accountPrefixes, prefix)

	p.skipToNextLine()

	// Return nil for now (no AST type needed for minimal implementation)
	return nil
}

func (p *Parser) parseCommentBlock(startPos Position) ast.Directive {
	// Skip to next line after "comment"
	p.skipToNextLine()

	// Skip all tokens until we find "end comment"
	for p.current.Type != TokenEOF {
		// Check for "end" directive
		if p.current.Type == TokenDirective && p.current.Value == "end" {
			p.advance()
			// Check if the next token is "comment"
			if (p.current.Type == TokenText || p.current.Type == TokenAccount ||
				p.current.Type == TokenDirective || isCommodityToken(p.current.Type)) &&
				strings.TrimSpace(p.current.Value) == "comment" {
				// Found "end comment", skip to next line and return
				p.skipToNextLine()
				return nil
			}
		}
		// Not "end comment", keep skipping
		p.advance()
	}

	endPos := Position{Line: startPos.Line, Column: startPos.Column + len("comment"), Offset: startPos.Offset + len("comment")}
	p.errorAt(startPos, endPos, "unterminated 'comment' block (missing 'end comment')")
	return nil
}

func (p *Parser) parseEndDirective(_ Position) ast.Directive {
	// Check what we're ending
	if p.current.Type == TokenText || p.current.Type == TokenDirective || p.current.Type == TokenAccount || isCommodityToken(p.current.Type) {
		endType := strings.TrimSpace(p.current.Value)
		switch endType {
		case "comment":
			// This should be handled by parseCommentBlock
			// If we get here, it's an unexpected "end comment"
			p.error("unexpected 'end comment' without matching 'comment'")
			p.skipToNextLine()
			return nil
		case "apply":
			// Handle "end apply account"
			p.advance()
			if p.current.Type == TokenText || isCommodityToken(p.current.Type) || p.current.Type == TokenAccount {
				if strings.TrimSpace(p.current.Value) == "account" {
					// Pop from stack
					if len(p.accountPrefixes) > 0 {
						p.accountPrefixes = p.accountPrefixes[:len(p.accountPrefixes)-1]
					}
					p.skipToNextLine()
					return nil
				}
			}
			p.error("expected 'account' after 'end apply'")
			p.skipToNextLine()
			return nil
		case "aliases":
			p.basicAliases = nil
			p.skipToNextLine()
			return nil
		default:
			p.error("unknown 'end' directive type: %s", endType)
			p.skipToNextLine()
			return nil
		}
	}

	p.error("expected directive type after 'end'")
	p.skipToNextLine()
	return nil
}

func (p *Parser) parseYearDirective(startPos Position) ast.Directive {
	if p.current.Type != TokenNumber {
		p.error("expected year")
		p.skipToNextLine()
		return nil
	}

	year, err := strconv.Atoi(p.current.Value)
	if err != nil || year < 1 || year > 9999 {
		p.error("invalid year: %s", p.current.Value)
		p.skipToNextLine()
		return nil
	}

	p.defaultYear = year
	dir := ast.YearDirective{
		Year:  year,
		Range: ast.Range{Start: toASTPosition(startPos)},
	}
	p.advance()
	dir.Range.End = toASTPosition(p.current.Pos)
	p.skipToNextLine()
	return dir
}

func (p *Parser) parseNote() string {
	var parts []string
	for p.current.Type == TokenText || p.current.Type == TokenPipe {
		parts = append(parts, p.current.Value)
		p.advance()
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func (p *Parser) parseComment() ast.Comment {
	comment := ast.Comment{
		Text: p.current.Value,
		Range: ast.Range{
			Start: toASTPosition(p.current.Pos),
			End:   toASTPosition(p.current.End),
		},
		Tags: parseTags(p.current.Value, p.current.Pos),
	}
	p.advance()
	return comment
}

func parseTags(text string, basePos Position) []ast.Tag {
	if !strings.Contains(text, ":") {
		return nil
	}

	var tags []ast.Tag
	parts := strings.Split(text, ",")
	searchStart := 0

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		colonIdx := strings.Index(trimmed, ":")
		if colonIdx == -1 {
			continue
		}

		name := strings.TrimSpace(trimmed[:colonIdx])
		if name == "" || !isValidTagName(name) {
			continue
		}

		value := ""
		if colonIdx+1 < len(trimmed) {
			value = ExtractTagValue(trimmed[colonIdx+1:])
		}

		tagStart := strings.Index(text[searchStart:], name+":")
		if tagStart == -1 {
			continue
		}
		tagStart += searchStart

		tagEnd := tagStart + len(name) + 1
		if value != "" {
			valueStart := strings.Index(text[tagEnd:], value)
			if valueStart != -1 {
				tagEnd = tagEnd + valueStart + len(value)
			}
		}

		startCol := basePos.Column + 1 + utf8.RuneCountInString(text[:tagStart])
		endCol := basePos.Column + 1 + utf8.RuneCountInString(text[:tagEnd])

		tags = append(tags, ast.Tag{
			Name:  name,
			Value: value,
			Range: ast.Range{
				Start: ast.Position{Line: basePos.Line, Column: startCol, Offset: basePos.Offset + 1 + tagStart},
				End:   ast.Position{Line: basePos.Line, Column: endCol, Offset: basePos.Offset + 1 + tagEnd},
			},
		})

		searchStart = tagEnd
	}

	return tags
}

// ExtractTagValue extracts a tag value from raw text after the colon.
// Per hledger, a tag value runs until the next comma or end of line, so
// internal whitespace (including 2+ consecutive spaces) is part of the
// value. Comma splitting happens in the callers; here only surrounding
// whitespace is trimmed.
func ExtractTagValue(raw string) string {
	return strings.TrimSpace(raw)
}

func isValidTagName(name string) bool {
	for _, r := range name {
		isLower := r >= 'a' && r <= 'z'
		isUpper := r >= 'A' && r <= 'Z'
		isDigit := r >= '0' && r <= '9'
		isSpecial := r == '-' || r == '_'
		if !isLower && !isUpper && !isDigit && !isSpecial {
			return false
		}
	}
	return true
}

func (p *Parser) advance() {
	p.current = p.lexer.Next()
}

func (p *Parser) getAccountPrefix() string {
	if len(p.accountPrefixes) == 0 {
		return ""
	}
	return strings.Join(p.accountPrefixes, ":")
}

func (p *Parser) resolveAccountName(name string) string {
	resolvedName := name
	if prefix := p.getAccountPrefix(); prefix != "" {
		resolvedName = prefix + ":" + resolvedName
	}
	for i := len(p.basicAliases) - 1; i >= 0; i-- {
		alias := p.basicAliases[i]
		if resolvedName == alias.original {
			resolvedName = alias.alias
			continue
		}
		if strings.HasPrefix(resolvedName, alias.original+":") {
			resolvedName = alias.alias + strings.TrimPrefix(resolvedName, alias.original)
		}
	}
	return resolvedName
}

// ApplyContextExports returns a clone of initial with declared commodity styles
// from exports merged in. Other parse-affecting state remains local.
func ApplyContextExports(initial Context, exports ContextExports) Context {
	context := cloneContext(initial)
	for symbol, mark := range exports.commodityDecimalMarks {
		context.commodityDecimalMarks[symbol] = mark
	}
	return context
}

// MergeContextExports combines exports in source order. Later styles override
// earlier styles for the same commodity.
func MergeContextExports(exports ...ContextExports) ContextExports {
	merged := ContextExports{commodityDecimalMarks: make(map[string]string)}
	for _, export := range exports {
		for symbol, mark := range export.commodityDecimalMarks {
			merged.commodityDecimalMarks[symbol] = mark
		}
	}
	return merged
}

func (p *Parser) context() Context {
	return cloneContext(Context{
		defaultYear:                 p.defaultYear,
		decimalMark:                 p.decimalMark,
		decimalMarkExplicit:         p.decimalMarkExplicit,
		defaultCommodityDecimalMark: p.defaultCommodityDecimalMark,
		defaultCommoditySymbol:      p.defaultCommoditySymbol,
		commodityDecimalMarks:       p.commodityDecimalMarks,
		accountPrefixes:             p.accountPrefixes,
		basicAliases:                p.basicAliases,
	})
}

func (p *Parser) checkpoint() {
	p.checkpoints = append(p.checkpoints, ContextCheckpoint{
		Offset:  p.current.Pos.Offset,
		Context: p.context(),
	})
}

func (p *Parser) contextExports() ContextExports {
	exports := ContextExports{commodityDecimalMarks: make(map[string]string)}
	for symbol, mark := range p.commodityDecimalMarks {
		if initialMark, ok := p.initialCommodityDecimalMarks[symbol]; !ok || initialMark != mark {
			exports.commodityDecimalMarks[symbol] = mark
		}
	}
	return exports
}

func (p *Parser) mergeExports(exports ContextExports) {
	for symbol, mark := range exports.commodityDecimalMarks {
		p.commodityDecimalMarks[symbol] = mark
	}
}

func cloneContext(context Context) Context {
	return Context{
		defaultYear:                 context.defaultYear,
		decimalMark:                 context.decimalMark,
		decimalMarkExplicit:         context.decimalMarkExplicit,
		defaultCommodityDecimalMark: context.defaultCommodityDecimalMark,
		defaultCommoditySymbol:      context.defaultCommoditySymbol,
		commodityDecimalMarks:       cloneStringMap(context.commodityDecimalMarks),
		accountPrefixes:             append([]string(nil), context.accountPrefixes...),
		basicAliases:                append([]basicAlias(nil), context.basicAliases...),
	}
}

func cloneStringMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func (p *Parser) skipToNextLine() {
	for p.current.Type != TokenNewline && p.current.Type != TokenEOF {
		p.advance()
	}
	if p.current.Type == TokenNewline {
		p.advance()
	}
}

func (p *Parser) error(format string, args ...any) {
	p.errorAt(p.current.Pos, p.current.End, format, args...)
}

func (p *Parser) errorAt(pos, end Position, format string, args ...any) {
	p.errors = append(p.errors, ParseError{
		Message: fmt.Sprintf(format, args...),
		Pos:     pos,
		End:     end,
	})
}

func descRange(pos Position, text string) ast.Range {
	return ast.Range{
		Start: toASTPosition(pos),
		End: ast.Position{
			Line:   pos.Line,
			Column: pos.Column + utf8.RuneCountInString(text),
			Offset: pos.Offset + len(text),
		},
	}
}

func toASTPosition(pos Position) ast.Position {
	return ast.Position{
		Line:   pos.Line,
		Column: pos.Column,
		Offset: pos.Offset,
	}
}

func (p *Parser) resolveDecimalMark(commodity string) string {
	if p.decimalMarkExplicit {
		return p.decimalMark
	}
	if commodity != "" {
		if mark, ok := p.commodityDecimalMarks[commodity]; ok {
			return mark
		}
		if commodity == p.defaultCommoditySymbol {
			return p.defaultCommodityDecimalMark
		}
		return ""
	}
	return p.defaultCommodityDecimalMark
}

func inferDecimalMarkFromFormat(format string) string {
	var numberPart strings.Builder
	for _, r := range format {
		if r >= '0' && r <= '9' || r == '.' || r == ',' {
			numberPart.WriteRune(r)
		}
	}
	return inferDecimalMark(numberPart.String())
}

func inferDecimalMark(numberStr string) string {
	lastDot := strings.LastIndex(numberStr, ".")
	lastComma := strings.LastIndex(numberStr, ",")

	if lastDot < 0 || lastComma < 0 {
		return ""
	}

	if lastComma > lastDot {
		return ","
	}

	return "."
}

func normalizeNumberWithMark(s, decimalMark string) string {
	var dm byte
	if decimalMark == "," {
		dm = ','
	} else {
		dm = '.'
	}

	var groupSep byte
	if dm == ',' {
		groupSep = '.'
	} else {
		groupSep = ','
	}

	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case groupSep:
			// skip group separator
		case dm:
			b.WriteByte('.')
		default:
			b.WriteByte(ch)
		}
	}

	return b.String()
}

func normalizeNumber(s, decimalMark string) string {
	if decimalMark != "" {
		return normalizeNumberWithMark(s, decimalMark)
	}

	var dotCount, commaCount int
	var lastDot, lastComma int

	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '.':
			dotCount++
			lastDot = i
		case ',':
			commaCount++
			lastComma = i
		}
	}

	if dotCount == 0 && commaCount == 0 {
		return s
	}

	if dotCount == 0 && commaCount == 1 {
		return s[:lastComma] + "." + s[lastComma+1:]
	}

	if dotCount == 1 && commaCount == 0 {
		return s
	}

	if commaCount > 1 && dotCount == 0 {
		return removeSeparator(s, ',')
	}

	if dotCount > 1 && commaCount == 0 {
		return removeSeparator(s, '.')
	}

	var decimalPos int

	if dotCount > 0 && commaCount == 1 && lastComma > lastDot {
		decimalPos = lastComma
	} else if commaCount > 0 && dotCount == 1 && lastDot > lastComma {
		decimalPos = lastDot
	} else if dotCount > 0 && commaCount == 1 {
		decimalPos = lastDot
	}

	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case '.', ',':
			if i == decimalPos {
				b.WriteByte('.')
			}
		default:
			b.WriteByte(ch)
		}
	}

	return b.String()
}

func removeSeparator(s string, sep byte) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != sep {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func isCommodityToken(t TokenType) bool {
	return t == TokenCommodity || t == TokenQuotedCommodity
}

func isValidCommodityText(value string) bool {
	if len(value) == 0 {
		return false
	}
	hasLetter := false
	for _, r := range value {
		if unicode.IsLetter(r) {
			hasLetter = true
		} else if !unicode.IsDigit(r) {
			return false
		}
	}
	return hasLetter
}
