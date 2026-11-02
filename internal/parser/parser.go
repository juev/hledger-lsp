package parser

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"github.com/juev/hledger-lsp/internal/ast"
)

// Parse error codes travel to the client on published diagnostics so editors and
// tooling can filter or group them without matching message text.
const (
	CodeParseError        = "PARSE_ERROR"
	CodeParseUnexpected   = "PARSE_UNEXPECTED"
	CodeInvalidDate       = "INVALID_DATE"
	CodeExpectedAmount    = "EXPECTED_AMOUNT"
	CodeExpectedCommodity = "EXPECTED_COMMODITY"
	CodeExpectedAccount   = "EXPECTED_ACCOUNT"
	CodeUnknownDirective  = "UNKNOWN_DIRECTIVE"
	CodeMalformedInclude  = "MALFORMED_INCLUDE"

	missingDecimalMarkMessage = "commodity format needs a decimal mark, such as 1000.00 or 1.000,00 (1000. and 1000, also work)"
)

// defaultYearForPartialDates supplies the year hledger assumes when a date is
// written without one (for example "01-15"). It is a variable so tests can pin it.
var defaultYearForPartialDates = func() int { return time.Now().Year() }

type ParseError struct {
	Message string
	Code    string
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
	defaultCommodityPosition    ast.CommodityPosition
	commodityDecimalMarks       map[string]string
	accountPrefixes             []string
	basicAliases                []basicAlias
}

// Equal reports whether a journal parsed under either context would parse the
// same way.
func (c Context) Equal(other Context) bool {
	return c.defaultYear == other.defaultYear &&
		c.decimalMark == other.decimalMark &&
		c.decimalMarkExplicit == other.decimalMarkExplicit &&
		c.defaultCommodityDecimalMark == other.defaultCommodityDecimalMark &&
		c.defaultCommoditySymbol == other.defaultCommoditySymbol &&
		c.defaultCommodityPosition == other.defaultCommodityPosition &&
		maps.Equal(c.commodityDecimalMarks, other.commodityDecimalMarks) &&
		slices.Equal(c.accountPrefixes, other.accountPrefixes) &&
		slices.Equal(c.basicAliases, other.basicAliases)
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
	defaultCommodityPosition     ast.CommodityPosition
	inputLen                     int
	accountPrefixes              []string // stack for nested apply account directives
	basicAliases                 []basicAlias
	items                        []LocalItem
	includeSites                 []IncludeSite
	checkpoints                  []ContextCheckpoint
	resolveInclude               IncludeResolver
	initialCommodityDecimalMarks map[string]string
	// postingComments collects indented comment lines that belong to the
	// transaction being parsed: hledger applies their tags to the transaction and
	// to all of its postings.
	postingComments []ast.Comment
	// prevEnd is where the token before the current one ends, prevPos where it starts.
	prevEnd Position
	prevPos Position
	// glueSensitiveEnd is the end offset of the last amount whose text hledger
	// extends over a `[` written right against it: a bare number or an unquoted
	// commodity on the right. -1 when the last amount is not one.
	glueSensitiveEnd int
	// faulted is set while a posting is parsed when its rest cannot be read:
	// abandonPostingOnFault then drops the remainder of the line.
	faulted bool
	// inAutoPostingRule is set while parsing the postings of an auto posting
	// rule, the only place hledger accepts a `*` multiplier amount.
	inAutoPostingRule bool
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
		defaultCommodityPosition:     context.defaultCommodityPosition,
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
				p.errorCode(CodeParseUnexpected, "unexpected content: %s", p.current.Value)
				p.skipToNextLine()
			}
		default:
			if p.current.Value != "" {
				p.errorCode(CodeParseUnexpected, "unexpected content: %s", p.current.Value)
			} else {
				p.errorCode(CodeParseUnexpected, "unexpected content")
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
		// A header the parser cannot read takes its whole block with it: report
		// the header once instead of one "unexpected content" per posting.
		p.skipToNextLine()
		p.skipIndentedBlock()
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
		p.postingComments = nil
		posting := p.parsePosting()
		if posting != nil {
			tx.Postings = append(tx.Postings, *posting)
		}
		// An indented comment line inside a transaction is a transaction
		// comment: hledger applies its tags to the transaction and to every
		// posting, so it must not be discarded.
		if len(p.postingComments) > 0 {
			tx.Comments = append(tx.Comments, p.postingComments...)
			p.postingComments = nil
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
		p.postingComments = nil
		posting := p.parsePosting()
		if posting != nil {
			ptx.Postings = append(ptx.Postings, *posting)
		}
		// Indented comment lines stay with the periodic transaction, as they do
		// with an ordinary one: a ; line under a posting is its comment.
		if len(p.postingComments) > 0 {
			ptx.Comments = append(ptx.Comments, p.postingComments...)
			p.postingComments = nil
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
	p.inAutoPostingRule = true
	defer func() { p.inAutoPostingRule = false }()
	for p.current.Type == TokenIndent {
		p.postingComments = nil
		posting := p.parsePosting()
		if posting != nil {
			rule.Postings = append(rule.Postings, *posting)
		}
		// Indented comment lines stay with the rule: a ; line under a posting
		// is its comment.
		if len(p.postingComments) > 0 {
			rule.Comments = append(rule.Comments, p.postingComments...)
			p.postingComments = nil
		}
		if p.current.Type == TokenNewline {
			p.advance()
		}
	}

	rule.Range.End = toASTPosition(p.current.Pos)
	return rule
}

// isMultiplier reports whether the current `*` token is an auto posting rule
// multiplier. Like hledger, the amount must follow the `*` directly; a `*`
// followed only by spaces and an optional comment is a multiplier without an
// amount, while `* -1` is not a multiplier at all.
func (p *Parser) isMultiplier() bool {
	line, _, _ := strings.Cut(p.lexer.input[p.current.End.Offset:], "\n")
	if line != "" && line[0] != ' ' && line[0] != '\t' {
		return true
	}
	trimmed := strings.TrimLeft(line, " \t")
	return trimmed == "" || trimmed[0] == ';'
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
		// hledger accepts a date without a year (a "smart date") and fills in
		// the current year; it does not require a Y directive.
		year := p.defaultYear
		if year == 0 {
			year = defaultYearForPartialDates()
		}
		month, err := strconv.Atoi(parts[0])
		if err != nil {
			p.errorCodeAt(CodeInvalidDate, pos, end, "invalid month: %s", parts[0])
			return nil
		}
		day, err := strconv.Atoi(parts[1])
		if err != nil {
			p.errorCodeAt(CodeInvalidDate, pos, end, "invalid day: %s", parts[1])
			return nil
		}
		if !IsValidCalendarDate(year, month, day) {
			p.errorCodeAt(CodeInvalidDate, pos, end, "invalid date: %s", value)
			return nil
		}
		return &ast.Date{
			Year:  year,
			Month: month,
			Day:   day,
			Range: ast.Range{Start: toASTPosition(pos), End: toASTPosition(end)},
		}
	case 3:
		year, err := strconv.Atoi(parts[0])
		if err != nil {
			p.errorCodeAt(CodeInvalidDate, pos, end, "invalid year: %s", parts[0])
			return nil
		}
		month, err := strconv.Atoi(parts[1])
		if err != nil {
			p.errorCodeAt(CodeInvalidDate, pos, end, "invalid month: %s", parts[1])
			return nil
		}
		day, err := strconv.Atoi(parts[2])
		if err != nil {
			p.errorCodeAt(CodeInvalidDate, pos, end, "invalid day: %s", parts[2])
			return nil
		}
		if !IsValidCalendarDate(year, month, day) {
			p.errorCodeAt(CodeInvalidDate, pos, end, "invalid date: %s", value)
			return nil
		}
		return &ast.Date{
			Year:  year,
			Month: month,
			Day:   day,
			Range: ast.Range{Start: toASTPosition(pos), End: toASTPosition(end)},
		}
	default:
		p.errorCodeAt(CodeInvalidDate, pos, end, "invalid date format: %s", value)
		return nil
	}
}

// IsValidCalendarDate reports whether a year/month/day combination exists in the
// proleptic Gregorian calendar. hledger refuses to load a journal containing an
// impossible date such as 2024-02-31, so the parser and the analyzer's date-tag
// validation share this rule. A zero year means "no year given", which passes.
func IsValidCalendarDate(year, month, day int) bool {
	if month < 1 || month > 12 || day < 1 {
		return false
	}
	if year == 0 {
		return true
	}
	return day <= daysInMonth(year, month)
}

func daysInMonth(year, month int) int {
	switch month {
	case 1, 3, 5, 7, 8, 10, 12:
		return 31
	case 4, 6, 9, 11:
		return 30
	default:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
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

// parsePosting reads one posting line. A posting whose line produced an error
// is recorded as unparsed, so the formatter leaves it as written: rebuilding it
// from a partial reading could change what hledger reads.
func (p *Parser) parsePosting() *ast.Posting {
	errorsBefore := len(p.errors)
	posting := p.parsePostingLine()
	if posting != nil && len(p.errors) > errorsBefore && posting.UnparsedTail.End.Offset <= posting.UnparsedTail.Start.Offset {
		posting.UnparsedTail = ast.Range{Start: posting.Range.Start, End: p.lineEnd(posting.Range.Start)}
	}
	return posting
}

// lineEnd returns the end of the line that holds pos, before its line break.
func (p *Parser) lineEnd(pos ast.Position) ast.Position {
	input := p.lexer.input
	start := min(pos.Offset, len(input))
	end := len(input)
	if idx := strings.IndexByte(input[start:], '\n'); idx >= 0 {
		end = start + idx
	}
	return ast.Position{
		Line:   pos.Line,
		Column: pos.Column + utf8.RuneCountInString(input[start:end]),
		Offset: end,
	}
}

func (p *Parser) parsePostingLine() *ast.Posting {
	if p.current.Type != TokenIndent {
		return nil
	}
	p.advance()

	if p.current.Type == TokenComment {
		p.postingComments = append(p.postingComments, p.parseComment())
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
		p.errorCode(CodeExpectedAccount, "expected account name")
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

	if p.inAutoPostingRule && p.current.Type == TokenStatus && p.current.Value == "*" && p.isMultiplier() {
		posting.Multiplier = true
		p.advance()
	}

	if isCommodityToken(p.current.Type) || p.current.Type == TokenNumber || p.current.Type == TokenSign {
		amount := p.parseAmount()
		if amount == nil {
			p.faulted = true
		} else {
			posting.Amount = amount
		}
		if p.abandonPostingOnFault(posting) {
			return posting
		}
	}

	// Lot annotations and costs belong to an amount; without one hledger reads
	// them as unexpected content.
	if posting.Amount != nil {
		p.parseLotAnnotations(posting)
		if p.abandonPostingOnFault(posting) {
			return posting
		}
	}

	if p.current.Type == TokenEquals || p.current.Type == TokenDoubleEquals ||
		p.current.Type == TokenEqualsStar || p.current.Type == TokenDoubleEqualsStar {
		posting.BalanceAssertion = p.parseBalanceAssertion()
		if p.abandonPostingOnFault(posting) {
			return posting
		}
	}

	if p.current.Type == TokenComment {
		posting.Comment = p.current.Value
		posting.CommentRange = ast.Range{Start: toASTPosition(p.current.Pos), End: toASTPosition(p.current.End)}
		posting.Tags = parseTags(p.current.Value, p.current.Pos)
		p.advance()
	}

	// Text the posting grammar does not cover (for example the `:=` balance
	// assignment, which hledger 1.52 rejects) is reported and recorded so the
	// formatter can leave the line alone instead of rewriting it and losing the
	// text. Skipping to the next line keeps the rest of the transaction intact.
	if p.current.Type != TokenNewline && p.current.Type != TokenEOF && p.current.Type != TokenIndent {
		posting.UnparsedTail = ast.Range{
			Start: toASTPosition(p.current.Pos),
			End:   toASTPosition(p.current.End),
		}
		p.errorCode(CodeParseUnexpected, "unexpected content: %s", p.current.Value)
		p.skipToNextLine()
	}

	posting.Range.End = toASTPosition(p.current.Pos)
	return posting
}

func (p *Parser) parseAmount() *ast.Amount {
	amount := &ast.Amount{}
	amount.Range.Start = toASTPosition(p.current.Pos)

	// Two signs cancel out: hledger reads "-EUR -5" as EUR 5.
	negative := false
	signBeforeCommodity := false
	signs := 0
	if p.current.Type == TokenSign {
		negative = p.current.Value == "-"
		signBeforeCommodity = true
		signs++
		p.advance()
	}

	if isCommodityToken(p.current.Type) {
		if p.unterminatedQuote() {
			return p.failUnterminatedQuote()
		}
		amount.Commodity = ast.Commodity{
			Symbol:   p.current.Value,
			Quoted:   p.current.Type == TokenQuotedCommodity,
			Position: ast.CommodityLeft,
			Range: ast.Range{
				Start: toASTPosition(p.current.Pos),
				End:   toASTPosition(p.current.End),
			},
		}
		if signBeforeCommodity {
			amount.SignBeforeCommodity = true
		}
		p.advance()
	} else if signs > 0 && p.current.Type == TokenSign {
		p.error("expected number")
		p.skipToNextLine()
		return nil
	}

	if p.current.Type == TokenSign {
		negative = negative != (p.current.Value == "-")
		p.advance()
	}

	if p.current.Type != TokenNumber {
		p.error("expected number")
		p.skipToNextLine()
		return nil
	}

	rawNumberStr := p.current.Value
	if negative {
		rawNumberStr = "-" + rawNumberStr
	}
	amount.RawQuantity = rawNumberStr
	rawTokenValue := p.current.Value
	numberPos, numberEnd := p.current.Pos, p.current.End
	p.advance()

	if amount.Commodity.Symbol == "" {
		isCommodity := isCommodityToken(p.current.Type) ||
			(p.current.Type == TokenText && isValidCommodityText(p.current.Value))
		if isCommodity {
			if p.unterminatedQuote() {
				return p.failUnterminatedQuote()
			}
			amount.Commodity = ast.Commodity{
				Symbol:   p.current.Value,
				Quoted:   p.current.Type == TokenQuotedCommodity,
				Position: ast.CommodityRight,
				Range: ast.Range{
					Start: toASTPosition(p.current.Pos),
					End:   toASTPosition(p.current.End),
				},
			}
			quoted := amount.Commodity.Quoted
			end := p.current.End
			p.advance()
			// hledger reads a `(...)` written right against an unquoted commodity
			// as part of its symbol, so `Y(n)` is one commodity, not `Y` and a note.
			for !quoted && p.current.Type == TokenCode && p.current.Pos.Offset == end.Offset {
				if !isSymbolText(p.current.Value) || p.unterminatedCode() {
					p.error("a note must be separated from the commodity by a space")
					p.skipToNextLine()
					return nil
				}
				amount.Commodity.Symbol += p.lexer.input[p.current.Pos.Offset:p.current.End.Offset]
				amount.Commodity.Range.End = toASTPosition(p.current.End)
				end = p.current.End
				p.advance()
			}
		} else if p.defaultCommoditySymbol != "" {
			// hledger reads a bare number as the default commodity set by the
			// D directive. Keep Inferred so the amount can still be rendered
			// the way it was written.
			amount.Commodity = ast.Commodity{
				Symbol:   p.defaultCommoditySymbol,
				Inferred: true,
				Position: p.defaultCommodityPosition,
			}
		}
	}

	numberStr := strings.ReplaceAll(rawNumberStr, " ", "")
	// A bare number is read with the D directive's number format, not with the
	// style of the commodity it inherits: `D 1.000,00 RUB` makes `1.234` mean
	// 1234 even when `commodity 1,000.00 RUB` declares a different style for
	// explicitly written RUB amounts (hledger 1.52.4).
	//
	// A declared decimal mark only settles a number that could be read either
	// way. `1.234,50` and `1 500,50` say which mark they use, and hledger takes
	// them at their word whatever the commodity declares.
	amount.DecimalMark = p.resolveDecimalMark(amount.Commodity.WrittenSymbol())
	mark := ""
	if isAmbiguousNumber(rawTokenValue) {
		mark = amount.DecimalMark
	}
	if strings.HasPrefix(rawTokenValue, ".") || strings.HasPrefix(rawTokenValue, ",") {
		// A number without an integer part takes the mark it starts with, even
		// under a decimal-mark directive that names the other one.
		if !isLeadingDecimalNumber(rawTokenValue) {
			p.errorAt(numberPos, numberEnd, "invalid number: %s", rawTokenValue)
			return nil
		}
		mark = rawTokenValue[:1]
	}
	numberStr = normalizeNumber(numberStr, mark)

	if hasExponent(rawTokenValue) && hasDigitGroupSeparator(rawTokenValue, numberStr) {
		p.errorAt(numberPos, numberEnd, "invalid number: digit separators and exponents may not be used together")
		return nil
	}

	qty, err := decimal.NewFromString(numberStr)
	if err != nil {
		p.errorAt(numberPos, numberEnd, "invalid number: %s", rawTokenValue)
		return nil
	}
	amount.Quantity = qty

	amount.Range.End = toASTPosition(p.current.Pos)
	p.glueSensitiveEnd = -1
	if amount.Commodity.Inferred || amount.Commodity.Symbol == "" ||
		(amount.Commodity.Position == ast.CommodityRight && !amount.Commodity.Quoted) {
		p.glueSensitiveEnd = p.prevEnd.Offset
	}
	return amount
}

// isSymbolText reports whether text can be part of an unquoted commodity symbol,
// which hledger ends at digits, signs, `.`, `@`, `*`, `;`, `=`, braces, quotes
// and whitespace.
func isSymbolText(text string) bool {
	return !strings.ContainsAny(text, "0123456789-+.@*;= \t\"{}")
}

// unterminatedCode reports whether the current `(` note lacks its `)`.
func (p *Parser) unterminatedCode() bool {
	end := p.current.End.Offset
	return end-p.current.Pos.Offset < 2 || p.lexer.input[end-1] != ')'
}

// unterminatedQuote reports whether the current quoted commodity lacks its
// closing quote, which happens at a line break or at a `;`.
func (p *Parser) unterminatedQuote() bool {
	if p.current.Type != TokenQuotedCommodity {
		return false
	}
	end := p.current.End.Offset
	return end-p.current.Pos.Offset < 2 || p.lexer.input[end-1] != '"'
}

func (p *Parser) failUnterminatedQuote() *ast.Amount {
	p.error("expected a closing quote after the commodity")
	p.skipToNextLine()
	return nil
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
		p.faulted = true
		return nil
	}
	cost.Amount = *amount
	cost.Range.End = toASTPosition(p.current.Pos)
	return cost
}

// parseLotDate reads `[DATE]` and reports whether it was a closed date.
func (p *Parser) parseLotDate(lot *ast.LotPrice) bool {
	open := p.current
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
	default:
		p.annotationFault(open, "expected a date after [")
		return false
	}

	if p.current.Type != TokenRBracket {
		p.annotationFault(open, "expected ] to close the lot date")
		return false
	}
	lot.Range.End = toASTPosition(p.current.End)
	p.advance()
	return true
}

// parseLotPriceInto reads `{...}` or `{{...}}` and reports whether it was closed
// by the matching brace. The braces hold one amount, in hledger's own syntax:
// a quoted text is the commodity of that amount, not a label.
func (p *Parser) parseLotPriceInto(lot *ast.LotPrice) bool {
	open := p.current
	lot.Braces = true
	if p.current.Type == TokenDoubleLBrace {
		lot.IsTotal = true
	}
	p.advance()

	closingToken := TokenRBrace
	closing := "}"
	if lot.IsTotal {
		closingToken = TokenDoubleRBrace
		closing = "}}"
	}

	// hledger allows a fixed lot cost, written as {=PRICE} or {{=PRICE}}: the
	// explicit `=` means the amount states the whole lot cost rather than a
	// per-unit price.
	if p.current.Type == TokenEquals {
		lot.Fixed = true
		p.advance()
	}

	if p.current.Type != closingToken &&
		(isCommodityToken(p.current.Type) || p.current.Type == TokenNumber || p.current.Type == TokenSign) {
		amount := p.parseAmount()
		if amount == nil {
			p.faulted = true
			return false
		}
		lot.Cost = amount
	}

	if p.current.Type != closingToken {
		p.annotationFault(open, "expected %s to close the lot cost", closing)
		return false
	}
	lot.Range.End = toASTPosition(p.current.End)
	p.advance()
	return true
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
		p.faulted = true
		return nil
	}
	ba.Amount = *amount
	// hledger allows only a cost after an asserted amount; lot annotations
	// there are left to the caller to report as unexpected content.
	if p.current.Type == TokenAt || p.current.Type == TokenAtAt {
		ba.Cost = p.parseCost()
	}
	ba.Range.End = toASTPosition(p.current.Pos)
	return ba
}

// gluedToCommodity reports whether the current `[` follows, without a space, an
// amount that hledger reads on into the bracket: a bare number or a commodity on
// the right that is not quoted. After a commodity on the left, a closing quote or
// a closing `}` `)` `]` the bracket starts a lot date.
func (p *Parser) gluedToCommodity() bool {
	end := p.prevEnd.Offset
	return end == p.current.Pos.Offset && end == p.glueSensitiveEnd
}

// parseLotAnnotations reads the cost and lot annotations that follow a posting
// amount, in any order. hledger takes each of them at most once, so a repeat
// ends the loop with an annotation fault instead of being merged into the first.
func (p *Parser) parseLotAnnotations(posting *ast.Posting) {
	var seenLotCost, seenDate, seenNote, seenCost bool
	ensureLot := func() *ast.LotPrice {
		if posting.LotPrice == nil {
			posting.LotPrice = &ast.LotPrice{}
			posting.LotPrice.Range.Start = toASTPosition(p.current.Pos)
		}
		return posting.LotPrice
	}
	for !p.faulted {
		start := p.current
		switch p.current.Type {
		case TokenLBrace, TokenDoubleLBrace:
			if seenLotCost {
				p.annotationFault(start, "only one lot cost ({...}) is allowed per amount")
				return
			}
			seenLotCost = true
			if !p.parseLotPriceInto(ensureLot()) {
				return
			}
		case TokenLBracket:
			if seenDate {
				p.annotationFault(start, "only one lot date ([...]) is allowed per amount")
				return
			}
			seenDate = true
			if p.gluedToCommodity() {
				p.annotationFault(start, "expected a space before the lot date")
				return
			}
			if !p.parseLotDate(ensureLot()) {
				return
			}
		case TokenCode:
			if seenNote {
				p.annotationFault(start, "only one lot note ((...)) is allowed per amount")
				return
			}
			seenNote = true
			if p.unterminatedCode() {
				p.annotationFault(start, "expected ) to close the lot note")
				return
			}
			lot := ensureLot()
			lot.Label = p.current.Value
			lot.Noted = true
			lot.Range.End = toASTPosition(p.current.End)
			p.advance()
		case TokenAt, TokenAtAt:
			if seenCost {
				p.annotationFault(start, "only one cost (@ or @@) is allowed per amount")
				return
			}
			seenCost = true
			posting.Cost = p.parseCost()
		default:
			return
		}
	}
}

// annotationFault reports text in the annotation area that hledger rejects and
// marks the posting as unreadable, so it is left as written instead of being
// rebuilt without the text. The error spans from the start of the annotation to
// the offending token, and never past the end of the line.
func (p *Parser) annotationFault(from Token, format string, args ...any) {
	end := p.current.End
	if p.current.Type == TokenNewline || p.current.Type == TokenEOF {
		end = p.current.Pos
	}
	p.errorCodeAt(CodeParseUnexpected, from.Pos, end, format, args...)
	p.faulted = true
}

// abandonPostingOnFault finishes a posting that could not be read to its end:
// what is left of the line is skipped, unless an earlier error already skipped
// it. It reports whether the posting was abandoned.
func (p *Parser) abandonPostingOnFault(posting *ast.Posting) bool {
	if !p.faulted {
		return false
	}
	p.faulted = false
	if p.current.Pos.Line > posting.Range.Start.Line {
		posting.Range.End = p.lineEnd(posting.Range.Start)
		return true
	}
	posting.Range.End = toASTPosition(p.current.Pos)
	p.skipToNextLine()
	return true
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
		p.errorCode(CodeExpectedAccount, "expected account name")
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

	// The written number decides the decimal mark. A `format` subdirective
	// replaces it below, and has no token of its own to report against.
	var number string
	formatPos, formatEnd := startPos, startPos

	spec := parseCommodityText(p.directiveLineText(), p.current.Pos)
	if spec.hasSymbol {
		dir.Commodity = ast.Commodity{
			Symbol: spec.symbol,
			Quoted: spec.quoted,
			Range: ast.Range{
				Start: toASTPosition(spec.symbolPos),
				End:   toASTPosition(spec.symbolEnd),
			},
		}
	}
	if spec.hasNumber {
		number, formatPos, formatEnd = spec.number, spec.numberPos, spec.numberEnd
		dir.Format = spec.format()
	}

	for p.current.Type != TokenNewline && p.current.Type != TokenEOF && p.current.Type != TokenComment {
		p.advance()
	}
	if p.current.Type == TokenComment {
		p.advance()
	}

	hasInlineFormat := number != ""
	lines := p.parseSubdirectiveLines()
	dir.Subdirs = make(map[string]string, len(lines))
	for _, line := range lines {
		dir.Subdirs[line.name] = line.value
	}
	p.checkCommodityFormats(dir.Commodity.Symbol, spec.hasSymbol, hasInlineFormat, lines)

	if format, ok := dir.Subdirs["format"]; ok {
		dir.Format = format
		number = numberInFormat(format, dir.Commodity.Symbol)
		formatEnd = p.current.Pos
	}
	if note, ok := dir.Subdirs["note"]; ok {
		dir.Note = note
	}

	if number != "" {
		mark := numberDecimalMark(number, p.resolveDecimalMark(dir.Commodity.Symbol))
		if mark == "" {
			p.errorAt(formatPos, formatEnd, missingDecimalMarkMessage)
		} else {
			p.commodityDecimalMarks[dir.Commodity.Symbol] = mark
		}
	} else if dir.Commodity.Symbol != "" {
		// A declaration without a format resets the style. The empty mark, rather
		// than a deleted entry, is what lets the reset leave an included file.
		p.commodityDecimalMarks[dir.Commodity.Symbol] = ""
	}

	dir.Range.End = toASTPosition(p.current.Pos)
	return dir
}

// commoditySpec is a commodity symbol and number as written after the
// commodity keyword or in a format subdirective.
type commoditySpec struct {
	symbol               string
	quoted               bool
	hasSymbol            bool
	symbolPos, symbolEnd Position
	symbolFirst          bool
	number               string
	hasNumber            bool
	numberPos, numberEnd Position
	// trailing is set when something other than a price, an assertion or a
	// comment follows the symbol and number.
	trailing bool
}

// format renders the spec the way the directive's Format field spells it: the
// symbol keeps the distance from the number it was written with.
func (c commoditySpec) format() string {
	switch {
	case !c.hasSymbol:
		return c.number
	case c.symbolFirst && c.numberPos == c.symbolEnd:
		return c.symbol + c.number
	case c.symbolFirst:
		return c.symbol + " " + c.number
	}
	return c.number + " " + c.symbol
}

// directiveLineText returns the text of the current directive line from the
// current token, up to a comment or the end of the line.
func (p *Parser) directiveLineText() string {
	if p.current.Type == TokenNewline || p.current.Type == TokenEOF || p.current.Type == TokenComment {
		return ""
	}
	rest := p.lexer.input[p.current.Pos.Offset:]
	if end := strings.IndexAny(rest, ";\n"); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// isNumberStart reports whether a number begins at text[i]: a digit, or a
// decimal point followed by a digit.
func isNumberStart(text string, i int) bool {
	if i >= len(text) {
		return false
	}
	if text[i] >= '0' && text[i] <= '9' {
		return true
	}
	return text[i] == '.' && i+1 < len(text) && text[i+1] >= '0' && text[i+1] <= '9'
}

// isSymbolRune reports whether r can be part of an unquoted commodity symbol.
// Only ASCII whitespace ends a symbol: hledger reads a symbol such as "EUR" and a
// non-breaking space after it as one symbol.
func isSymbolRune(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '-', '+', '.', '@', '*', ';', '"', '{', '}', '=':
		return false
	}
	return r < '0' || r > '9'
}

// digitGroupSpace reports whether r separates digit groups inside a number. The
// set is the one hledger accepts, found by probing it: ASCII space, no-break
// space, en space, em space, punctuation space, thin space, narrow no-break
// space and medium mathematical space.
func digitGroupSpace(r rune) bool {
	switch r {
	case ' ', '\u00a0', '\u2002', '\u2003', '\u2008', '\u2009', '\u202f', '\u205f':
		return true
	}
	return false
}

// parseCommodityText reads the symbol and number of a commodity directive line
// or of a format subdirective: the symbol before or after the number, an
// optional sign, a quoted or unquoted symbol and a number that may omit its
// integer part. Anything after the symbol and number is left unread. start is
// the position of text[0].
func parseCommodityText(text string, start Position) commoditySpec {
	var spec commoditySpec
	at := func(i int) Position { return advancePosition(start, text[:i]) }
	skipSpaces := func(i int) int {
		for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
			i++
		}
		return i
	}
	// hledger allows any Unicode space around a number but only ASCII space
	// next to a symbol written before it. A tab counts as a space.
	skipGap := func(i int) int {
		for i < len(text) {
			r, size := utf8.DecodeRuneInString(text[i:])
			if r != ' ' && r != '\t' && !unicode.Is(unicode.Zs, r) {
				break
			}
			i += size
		}
		return i
	}
	skipSign := func(i int) int {
		if i < len(text) && (text[i] == '-' || text[i] == '+') {
			return skipSpaces(i + 1)
		}
		return i
	}
	readNumber := func(i int) int {
		for i < len(text) {
			r, size := utf8.DecodeRuneInString(text[i:])
			next, _ := utf8.DecodeRuneInString(text[min(i+size, len(text)):])
			switch {
			case r >= '0' && r <= '9', r == '.', r == ',':
			case digitGroupSpace(r) && next >= '0' && next <= '9':
			case (r == 'e' || r == 'E') && i > 0 && (next >= '0' && next <= '9' || ((next == '-' || next == '+') && i+size+1 < len(text) && text[i+size+1] >= '0' && text[i+size+1] <= '9')):
				i += size
				if next == '-' || next == '+' {
					i++
				}
				continue
			default:
				return i
			}
			i += size
		}
		return i
	}
	readSymbol := func(i int) int {
		begin := i
		if i < len(text) && text[i] == '"' {
			end := strings.IndexByte(text[i+1:], '"')
			if end < 0 {
				return begin
			}
			spec.hasSymbol, spec.quoted = true, true
			spec.symbol = text[i+1 : i+1+end]
			spec.symbolPos, spec.symbolEnd = at(i), at(i+end+2)
			return i + end + 2
		}
		for i < len(text) {
			r, size := utf8.DecodeRuneInString(text[i:])
			if !isSymbolRune(r) {
				break
			}
			i += size
		}
		if i > begin {
			spec.hasSymbol = true
			spec.symbol = text[begin:i]
			spec.symbolPos, spec.symbolEnd = at(begin), at(i)
		}
		return i
	}

	i := skipSign(skipGap(0))
	if isNumberStart(text, i) {
		end := readNumber(i)
		spec.number, spec.numberPos, spec.numberEnd, spec.hasNumber = text[i:end], at(i), at(end), true
		i = readSymbol(skipGap(end))
	} else {
		spec.symbolFirst = true
		i = readSymbol(i)
		if spec.hasSymbol {
			i = skipSign(skipSpaces(i))
			if isNumberStart(text, i) {
				end := readNumber(i)
				spec.number, spec.numberPos, spec.numberEnd, spec.hasNumber = text[i:end], at(i), at(end), true
				i = end
			}
		}
	}

	rest := strings.TrimLeft(text[i:], " \t")
	spec.trailing = rest != "" && !strings.ContainsRune("@={([", rune(rest[0]))
	if spec.hasNumber && strings.HasPrefix(spec.number, ".") && !isLeadingDecimalNumber(spec.number) {
		spec.trailing = true
	}
	return spec
}

// checkCommodityFormats reports the subdirectives hledger refuses below a
// commodity directive. A directive that already carries a format on its own
// line takes no subdirectives at all, and the symbol of a format subdirective
// must be the directive's symbol.
func (p *Parser) checkCommodityFormats(symbol string, hasSymbol, hasInlineFormat bool, lines []subdirectiveLine) {
	if hasInlineFormat {
		if len(lines) > 0 {
			first := lines[0]
			p.errorAt(first.namePos, first.nameEnd,
				"subdirective %q is not allowed after a commodity directive that already has a format; write the format as a \"format\" subdirective instead",
				first.name)
		}
		return
	}
	if !hasSymbol {
		return
	}
	for _, line := range lines {
		if line.name != "format" {
			continue
		}
		spec := parseCommodityText(line.value, line.valuePos)
		if spec.trailing {
			p.errorAt(line.valuePos, line.valueEnd,
				"invalid format %q: expected a number and one commodity symbol", line.value)
			continue
		}
		if spec.symbol == symbol {
			continue
		}
		if !spec.hasSymbol {
			p.errorAt(line.valuePos, line.valueEnd,
				"format for commodity %q has no commodity symbol; it must use %q", symbol, symbol)
			continue
		}
		p.errorAt(line.valuePos, line.valueEnd,
			"commodity directive symbol %q and format directive symbol %q should be the same", symbol, spec.symbol)
	}
}

func (p *Parser) parseIncludeDirective(startPos Position) ast.Directive {
	var path strings.Builder

	for p.current.Type != TokenNewline && p.current.Type != TokenEOF && p.current.Type != TokenComment {
		path.WriteString(p.current.Value)
		p.advance()
	}

	pathStr := strings.TrimSpace(path.String())
	if pathStr == "" {
		p.errorCode(CodeMalformedInclude, "expected file path")
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

	// hledger allows an optional clock time between the date and the commodity:
	// P 2024-01-15 09:30 EUR $1.08
	if p.current.Type == TokenTime {
		p.advance()
	}

	if isCommodityToken(p.current.Type) || p.current.Type == TokenText {
		commodityStart, commodityEnd := p.current.Pos, p.current.End
		dir.Commodity = ast.Commodity{
			Symbol: p.current.Value,
			Quoted: p.current.Type == TokenQuotedCommodity,
			Range: ast.Range{
				Start: toASTPosition(p.current.Pos),
				End:   toASTPosition(p.current.End),
			},
		}
		valid := p.current.Type == TokenQuotedCommodity || (p.current.Value != "" && isSymbolText(p.current.Value))
		p.advance()
		// hledger reads the price after at least one space, and a symbol holds
		// no digit, sign, decimal mark or other syntax character.
		if !valid || p.current.Pos.Offset == commodityEnd.Offset {
			p.errorAt(commodityStart, commodityEnd, "invalid commodity %q: a space must separate it from the price, and unless it is in double quotes it holds no digit or any of - + . @ * ; = { }", dir.Commodity.Symbol)
			p.skipToNextLine()
			return nil
		}
	} else {
		p.errorCode(CodeExpectedCommodity, "expected commodity")
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

// subdirectiveLine is one indented line below a directive, with the positions
// diagnostics are reported against.
type subdirectiveLine struct {
	name               string
	namePos, nameEnd   Position
	value              string
	valuePos, valueEnd Position
}

func (p *Parser) parseSubdirectives() map[string]string {
	subdirs := make(map[string]string)
	for _, line := range p.parseSubdirectiveLines() {
		subdirs[line.name] = line.value
	}
	return subdirs
}

func (p *Parser) parseSubdirectiveLines() []subdirectiveLine {
	var lines []subdirectiveLine

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
			pos := p.current.Pos
			p.advance()

			name, value, _ := strings.Cut(line, " ")
			lines = append(lines, subdirectiveLine{
				name:    name,
				namePos: pos,
				nameEnd: advancePosition(pos, name),
				value:   strings.TrimSpace(value),
			})
			continue
		}

		if p.current.Type != TokenDirective {
			p.skipToNextLine()
			continue
		}
		entry := subdirectiveLine{name: p.current.Value, namePos: p.current.Pos, nameEnd: p.current.End}
		p.advance()

		var value strings.Builder
		for p.current.Type != TokenNewline && p.current.Type != TokenEOF && p.current.Type != TokenComment {
			if value.Len() == 0 {
				entry.valuePos = p.current.Pos
				entry.valueEnd = advancePosition(p.current.Pos, strings.TrimSpace(p.current.Value))
			}
			value.WriteString(p.current.Value)
			if p.current.Type == TokenNumber || isCommodityToken(p.current.Type) || p.current.Type == TokenText {
				value.WriteString(" ")
			}
			p.advance()
		}

		entry.value = strings.TrimSpace(value.String())
		lines = append(lines, entry)
	}

	return lines
}

// advancePosition returns the position after text written at pos on one line.
func advancePosition(pos Position, text string) Position {
	return Position{Line: pos.Line, Column: pos.Column + utf8.RuneCountInString(text), Offset: pos.Offset + len(text)}
}

func (p *Parser) parseDefaultCommodityDirective(startPos Position) ast.Directive {
	dir := ast.DefaultCommodityDirective{
		Range: ast.Range{Start: toASTPosition(startPos)},
	}

	var numberStr string
	numberPos, numberEnd := startPos, startPos

	switch p.current.Type {
	case TokenCommodity, TokenQuotedCommodity:
		symbol := p.current.Value
		dir.Symbol = symbol
		p.advance()

		if p.current.Type == TokenNumber {
			numberStr, numberPos, numberEnd = p.current.Value, p.current.Pos, p.current.End
			dir.Format = symbol + numberStr
			p.advance()
		}
	case TokenText:
		// Word commodity before the number: `D RUB 1.000,00`, which hledger
		// reads as RUB with the directive's number format.
		symbol := p.current.Value
		p.advance()

		if p.current.Type == TokenNumber {
			numberStr, numberPos, numberEnd = p.current.Value, p.current.Pos, p.current.End
			dir.Symbol = symbol
			dir.Format = symbol + " " + numberStr
			p.advance()
		}
	case TokenNumber:
		numberStr, numberPos, numberEnd = p.current.Value, p.current.Pos, p.current.End
		p.advance()

		if isCommodityToken(p.current.Type) || p.current.Type == TokenText {
			dir.Symbol = p.current.Value
			dir.Format = numberStr + " " + p.current.Value
			p.advance()
		}
	}

	// The default commodity applies to every amount written without a symbol
	// from here on — including amounts in files included later, because
	// hledger's directive state flows into an include — so record it even when
	// the decimal mark is already known. A D directive with a number but no
	// symbol clears it again; a line hledger would reject (`D RUB`, a bare `D`)
	// leaves the state alone instead of silently dropping the commodity.
	if numberStr != "" {
		p.defaultCommoditySymbol = dir.Symbol
		p.defaultCommodityPosition = defaultCommodityPositionOf(dir.Format, dir.Symbol)

		if mark := numberDecimalMark(numberStr, p.resolveDecimalMark(dir.Symbol)); mark == "" {
			p.errorAt(numberPos, numberEnd, missingDecimalMarkMessage)
		} else {
			p.defaultCommodityDecimalMark = mark
		}
	}

	dir.Range.End = toASTPosition(p.current.Pos)
	p.skipToNextLine()
	return dir
}

// defaultCommodityPositionOf reports where a D directive writes its commodity
// symbol, so that bare amounts inherit the same layout: `D $1,000.00` is
// commodity-left, `D 1.000,00 RUB` is commodity-right.
func defaultCommodityPositionOf(format, symbol string) ast.CommodityPosition {
	if symbol == "" {
		return ast.CommodityLeft
	}
	symbolIdx := strings.Index(format, symbol)
	digitIdx := strings.IndexFunc(format, unicode.IsDigit)
	if symbolIdx >= 0 && digitIdx >= 0 && symbolIdx < digitIdx {
		return ast.CommodityLeft
	}
	return ast.CommodityRight
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
			p.errorCode(CodeUnknownDirective, "unknown 'end' directive type: %s", endType)
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
	if offset := p.current.Pos.Offset; offset >= 0 && offset < len(p.lexer.input) {
		comment.Marker = p.lexer.input[offset]
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
		if !IsValidTagName(name) {
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

// IsValidTagName reports whether name can be a tag name. Per hledger, any
// non-empty run of non-whitespace characters qualifies, in any script.
func IsValidTagName(name string) bool {
	return name != "" && !strings.ContainsFunc(name, unicode.IsSpace)
}

func (p *Parser) advance() {
	p.prevEnd = p.current.End
	p.prevPos = p.current.Pos
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
	return cloneContext(p.liveContext())
}

// liveContext returns the parser's state without copying it, so the result
// aliases maps and slices the parser goes on to mutate. It must not outlive the
// call that asked for it; context returns a snapshot that can.
func (p *Parser) liveContext() Context {
	return Context{
		defaultYear:                 p.defaultYear,
		decimalMark:                 p.decimalMark,
		decimalMarkExplicit:         p.decimalMarkExplicit,
		defaultCommodityDecimalMark: p.defaultCommodityDecimalMark,
		defaultCommoditySymbol:      p.defaultCommoditySymbol,
		defaultCommodityPosition:    p.defaultCommodityPosition,
		commodityDecimalMarks:       p.commodityDecimalMarks,
		accountPrefixes:             p.accountPrefixes,
		basicAliases:                p.basicAliases,
	}
}

// checkpoint records the context after a directive. Most directives leave it
// alone (a price history is thousands of them), so an unchanged context shares
// the previous snapshot rather than copying every commodity once per directive.
func (p *Parser) checkpoint() {
	var context Context
	if last := len(p.checkpoints) - 1; last >= 0 && p.checkpoints[last].Context.Equal(p.liveContext()) {
		context = p.checkpoints[last].Context
	} else {
		context = p.context()
	}
	p.checkpoints = append(p.checkpoints, ContextCheckpoint{
		Offset:  p.current.Pos.Offset,
		Context: context,
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
		defaultCommodityPosition:    context.defaultCommodityPosition,
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

// skipIndentedBlock consumes the posting-looking lines that follow a failed
// transaction header, so one broken header line produces a single diagnostic
// instead of one "unexpected content" error per posting.
func (p *Parser) skipIndentedBlock() {
	for {
		switch p.current.Type {
		case TokenNewline:
			p.advance()
		case TokenIndent:
			p.skipToNextLine()
		default:
			return
		}
	}
}

func (p *Parser) error(format string, args ...any) {
	// At a line break the token ends on the next line, so the error points at
	// the last token of the line instead.
	if (p.current.Type == TokenNewline || p.current.Type == TokenEOF) && p.prevPos.Line == p.current.Pos.Line && p.prevPos.Offset < p.current.Pos.Offset {
		p.errorCodeAt(CodeParseError, p.prevPos, p.current.Pos, format, args...)
		return
	}
	p.errorCodeAt(CodeParseError, p.current.Pos, p.current.End, format, args...)
}

func (p *Parser) errorAt(pos, end Position, format string, args ...any) {
	p.errorCodeAt(CodeParseError, pos, end, format, args...)
}

// errorCode records an error under an explicit code, using the current token as
// the range.
func (p *Parser) errorCode(code string, format string, args ...any) {
	p.errorCodeAt(code, p.current.Pos, p.current.End, format, args...)
}

// errorCodeAt records an error under an explicit code and range.
func (p *Parser) errorCodeAt(code string, pos, end Position, format string, args ...any) {
	p.errors = append(p.errors, ParseError{
		Message: fmt.Sprintf(format, args...),
		Code:    code,
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

// resolveDecimalMark returns the decimal mark in force for a commodity, the way
// hledger picks one: a decimal-mark directive wins, then the commodity's own
// style, then the style of the D directive, which is the fallback for every
// commodity and not only the one it names. "" means nothing is declared.
func (p *Parser) resolveDecimalMark(commodity string) string {
	if p.decimalMarkExplicit {
		return p.decimalMark
	}
	if mark := p.commodityDecimalMarks[commodity]; mark != "" {
		return mark
	}
	return p.defaultCommodityDecimalMark
}

// numberInFormat returns the number written in a commodity format string.
func numberInFormat(format, symbol string) string {
	format = strings.Replace(format, symbol, "", 1)
	isNumberRune := func(r rune) bool { return unicode.IsDigit(r) || r == '.' || r == ',' }
	start := strings.IndexFunc(format, isNumberRune)
	end := strings.LastIndexFunc(format, isNumberRune)
	if start < 0 {
		return ""
	}
	return format[start : end+1]
}

// isAmbiguousNumber reports whether a written number could be read with either
// decimal mark: it has one separator, and nothing else to tell its role.
func isAmbiguousNumber(number string) bool {
	return !strings.Contains(number, " ") && strings.Count(number, ".")+strings.Count(number, ",") == 1
}

var leadingDecimalNumber = regexp.MustCompile(`^[.,][0-9]+([eE][+-]?[0-9]+)?$`)

// isLeadingDecimalNumber reports whether number is a decimal mark followed by
// digits and an optional exponent, such as ".5", ",5" or ".5E2".
func isLeadingDecimalNumber(number string) bool {
	return leadingDecimalNumber.MatchString(number)
}

func hasExponent(number string) bool {
	return strings.ContainsAny(number, "eE")
}

// hasDigitGroupSeparator reports whether the mantissa of a number, written as
// raw and read as normalized, contains a digit group separator. A separator
// left over once the decimal mark is accounted for is a group separator. A
// separator with no digits after it is read as the decimal mark whatever mark is
// in force.
func hasDigitGroupSeparator(raw, normalized string) bool {
	mantissa, _, _ := strings.Cut(strings.ToUpper(raw), "E")
	normalizedMantissa, _, _ := strings.Cut(strings.ToUpper(normalized), "E")
	if strings.Contains(mantissa, " ") {
		return true
	}
	marks := strings.Count(normalizedMantissa, ".")
	if strings.HasSuffix(mantissa, ".") || strings.HasSuffix(mantissa, ",") {
		marks = 1
	}
	return strings.Count(mantissa, ".")+strings.Count(mantissa, ",") > marks
}

// numberDecimalMark returns the decimal mark of a number written in a commodity
// or D directive, or "" when it has none. hledger reads these numbers like any
// amount, so a lone separator that is not the mark already in force is a digit
// group separator and the number has no decimal mark.
func numberDecimalMark(number, inForce string) string {
	last := strings.LastIndexAny(number, ".,")
	if last < 0 {
		return ""
	}
	if strings.HasPrefix(number, ".") {
		// A number without an integer part always uses "." as its mark.
		return "."
	}
	mark := number[last : last+1]
	if isAmbiguousNumber(number) {
		if inForce != "" && inForce != mark {
			return ""
		}
		return mark
	}
	if strings.ContainsAny(number[:last], ".,") && !strings.ContainsAny(number[:last], otherDecimalMark(mark)) {
		// Only one separator kind, used repeatedly: digit groups.
		return ""
	}
	return mark
}

func otherDecimalMark(mark string) string {
	if mark == "." {
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
