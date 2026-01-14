package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/juev/hledger-lsp/internal/ast"
)

type ParseError struct {
	Message string
	Pos     Position
}

func (e ParseError) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.Pos.Line, e.Pos.Column, e.Message)
}

type Parser struct {
	lexer       *Lexer
	current     Token
	errors      []ParseError
	defaultYear int
}

func Parse(input string) (*ast.Journal, []ParseError) {
	p := &Parser{
		lexer: NewLexer(input),
	}
	p.advance()
	return p.parseJournal(), p.errors
}

func (p *Parser) parseJournal() *ast.Journal {
	journal := &ast.Journal{}

	for p.current.Type != TokenEOF {
		switch p.current.Type {
		case TokenNewline:
			p.advance()
		case TokenComment:
			journal.Comments = append(journal.Comments, p.parseComment())
		case TokenDate:
			tx := p.parseTransaction()
			if tx != nil {
				journal.Transactions = append(journal.Transactions, *tx)
			}
		case TokenDirective:
			dir := p.parseDirective()
			if dir != nil {
				if inc, ok := dir.(ast.Include); ok {
					journal.Includes = append(journal.Includes, inc)
				} else {
					journal.Directives = append(journal.Directives, dir)
				}
			}
		default:
			p.error("unexpected token: %s", p.current.Type)
			p.skipToNextLine()
		}
	}

	return journal
}

func (p *Parser) parseTransaction() *ast.Transaction {
	tx := &ast.Transaction{}
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
		p.advance()

		if p.current.Type == TokenPipe {
			tx.Payee = strings.TrimSpace(desc)
			p.advance()
			if p.current.Type == TokenText {
				tx.Note = strings.TrimSpace(p.current.Value)
				p.advance()
			}
			tx.Description = tx.Payee
			if tx.Note != "" {
				tx.Description = tx.Payee + " | " + tx.Note
			}
		} else {
			tx.Description = desc
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

func (p *Parser) parseDate() *ast.Date {
	if p.current.Type != TokenDate {
		p.error("expected date")
		return nil
	}

	value := p.current.Value
	pos := p.current.Pos
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
			p.errorAt(pos, "partial date requires Y directive: %s", value)
			return nil
		}
		month, err := strconv.Atoi(parts[0])
		if err != nil {
			p.errorAt(pos, "invalid month: %s", parts[0])
			return nil
		}
		day, err := strconv.Atoi(parts[1])
		if err != nil {
			p.errorAt(pos, "invalid day: %s", parts[1])
			return nil
		}
		return &ast.Date{
			Year:  p.defaultYear,
			Month: month,
			Day:   day,
			Range: ast.Range{Start: toASTPosition(pos)},
		}
	case 3:
		year, err := strconv.Atoi(parts[0])
		if err != nil {
			p.errorAt(pos, "invalid year: %s", parts[0])
			return nil
		}
		month, err := strconv.Atoi(parts[1])
		if err != nil {
			p.errorAt(pos, "invalid month: %s", parts[1])
			return nil
		}
		day, err := strconv.Atoi(parts[2])
		if err != nil {
			p.errorAt(pos, "invalid day: %s", parts[2])
			return nil
		}
		return &ast.Date{
			Year:  year,
			Month: month,
			Day:   day,
			Range: ast.Range{Start: toASTPosition(pos)},
		}
	default:
		p.errorAt(pos, "invalid date format: %s", value)
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

	posting.Account = ast.Account{
		Name:  p.current.Value,
		Parts: strings.Split(p.current.Value, ":"),
		Range: ast.Range{Start: toASTPosition(p.current.Pos)},
	}
	p.advance()

	if closingToken != 0 && p.current.Type == closingToken {
		p.advance()
	}

	if p.current.Type == TokenCommodity || p.current.Type == TokenNumber {
		amount := p.parseAmount()
		if amount != nil {
			posting.Amount = amount
		}
	}

	if p.current.Type == TokenAt || p.current.Type == TokenAtAt {
		posting.Cost = p.parseCost()
	}

	if p.current.Type == TokenEquals || p.current.Type == TokenDoubleEquals {
		posting.BalanceAssertion = p.parseBalanceAssertion()
	}

	if p.current.Type == TokenComment {
		posting.Comment = p.current.Value
		posting.Tags = parseTags(p.current.Value)
		p.advance()
	}

	posting.Range.End = toASTPosition(p.current.Pos)
	return posting
}

func (p *Parser) parseAmount() *ast.Amount {
	amount := &ast.Amount{}
	amount.Range.Start = toASTPosition(p.current.Pos)

	if p.current.Type == TokenCommodity {
		amount.Commodity = ast.Commodity{
			Symbol:   p.current.Value,
			Position: ast.CommodityLeft,
			Range:    ast.Range{Start: toASTPosition(p.current.Pos)},
		}
		p.advance()
	}

	if p.current.Type != TokenNumber {
		p.error("expected number")
		return nil
	}

	qty, err := decimal.NewFromString(p.current.Value)
	if err != nil {
		p.error("invalid number: %s", p.current.Value)
		return nil
	}
	amount.Quantity = qty
	p.advance()

	if p.current.Type == TokenCommodity && amount.Commodity.Symbol == "" {
		amount.Commodity = ast.Commodity{
			Symbol:   p.current.Value,
			Position: ast.CommodityRight,
			Range:    ast.Range{Start: toASTPosition(p.current.Pos)},
		}
		p.advance()
	}

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

func (p *Parser) parseBalanceAssertion() *ast.BalanceAssertion {
	ba := &ast.BalanceAssertion{}
	ba.Range.Start = toASTPosition(p.current.Pos)

	if p.current.Type == TokenDoubleEquals {
		ba.IsStrict = true
	}
	p.advance()

	amount := p.parseAmount()
	if amount == nil {
		return nil
	}
	ba.Amount = *amount
	ba.Range.End = toASTPosition(p.current.Pos)
	return ba
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
			Name:  accountName,
			Parts: strings.Split(accountName, ":"),
			Range: ast.Range{Start: toASTPosition(accountPos)},
		},
		Range: ast.Range{Start: toASTPosition(startPos)},
	}

	if p.current.Type == TokenComment {
		dir.Comment = p.current.Value
		dir.Tags = parseTags(p.current.Value)
		p.advance()
	}

	for p.current.Type != TokenNewline && p.current.Type != TokenEOF {
		p.advance()
	}

	dir.Subdirs = p.parseSubdirectives()

	return dir
}

func (p *Parser) parseCommodityDirective(startPos Position) ast.Directive {
	dir := ast.CommodityDirective{
		Range: ast.Range{Start: toASTPosition(startPos)},
	}

	if p.current.Type == TokenCommodity || p.current.Type == TokenText {
		dir.Commodity = ast.Commodity{
			Symbol: p.current.Value,
			Range:  ast.Range{Start: toASTPosition(p.current.Pos)},
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

	if p.current.Type == TokenCommodity || p.current.Type == TokenText {
		dir.Commodity = ast.Commodity{
			Symbol: p.current.Value,
			Range:  ast.Range{Start: toASTPosition(p.current.Pos)},
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
			if p.current.Type == TokenNumber || p.current.Type == TokenCommodity || p.current.Type == TokenText {
				value.WriteString(" ")
			}
			p.advance()
		}

		subdirs[name] = strings.TrimSpace(value.String())
	}

	return subdirs
}

func (p *Parser) parseYearDirective(startPos Position) ast.Directive {
	if p.current.Type != TokenNumber {
		p.error("expected year")
		p.skipToNextLine()
		return nil
	}

	year, err := strconv.Atoi(p.current.Value)
	if err != nil || year < 1900 || year > 2200 {
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
	p.skipToNextLine()
	return dir
}

func (p *Parser) parseComment() ast.Comment {
	comment := ast.Comment{
		Text:  p.current.Value,
		Range: ast.Range{Start: toASTPosition(p.current.Pos)},
		Tags:  parseTags(p.current.Value),
	}
	p.advance()
	return comment
}

func parseTags(text string) []ast.Tag {
	var tags []ast.Tag
	parts := strings.Split(text, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		colonIdx := strings.Index(part, ":")
		if colonIdx == -1 {
			continue
		}

		name := strings.TrimSpace(part[:colonIdx])
		if name == "" || !isValidTagName(name) {
			continue
		}

		value := ""
		if colonIdx+1 < len(part) {
			value = strings.TrimSpace(part[colonIdx+1:])
		}

		tags = append(tags, ast.Tag{
			Name:  name,
			Value: value,
		})
	}

	return tags
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

func (p *Parser) skipToNextLine() {
	for p.current.Type != TokenNewline && p.current.Type != TokenEOF {
		p.advance()
	}
	if p.current.Type == TokenNewline {
		p.advance()
	}
}

func (p *Parser) error(format string, args ...any) {
	p.errorAt(p.current.Pos, format, args...)
}

func (p *Parser) errorAt(pos Position, format string, args ...any) {
	p.errors = append(p.errors, ParseError{
		Message: fmt.Sprintf(format, args...),
		Pos:     pos,
	})
}

func toASTPosition(pos Position) ast.Position {
	return ast.Position{
		Line:   pos.Line,
		Column: pos.Column,
		Offset: pos.Offset,
	}
}
