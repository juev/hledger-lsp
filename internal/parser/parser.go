package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/shopspring/decimal"
)

type ParseError struct {
	Message string
	Pos     Position
}

func (e ParseError) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.Pos.Line, e.Pos.Column, e.Message)
}

type Parser struct {
	lexer   *Lexer
	current Token
	errors  []ParseError
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

	p.skipSpaceTokens()

	if p.current.Type == TokenStatus {
		tx.Status = p.parseStatus()
		p.skipSpaceTokens()
	}

	if p.current.Type == TokenCode {
		tx.Code = p.current.Value
		p.advance()
		p.skipSpaceTokens()
	}

	if p.current.Type == TokenText {
		desc := p.current.Value
		p.advance()

		if p.current.Type == TokenPipe {
			tx.Payee = strings.TrimSpace(desc)
			p.advance()
			p.skipSpaceTokens()
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
	if len(parts) != 3 {
		p.errorAt(pos, "invalid date format: %s", value)
		return nil
	}

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

	p.skipSpaceTokens()

	if p.current.Type == TokenCommodity || p.current.Type == TokenNumber {
		amount := p.parseAmount()
		if amount != nil {
			posting.Amount = amount
		}
	}

	p.skipSpaceTokens()

	if p.current.Type == TokenAt || p.current.Type == TokenAtAt {
		posting.Cost = p.parseCost()
	}

	p.skipSpaceTokens()

	if p.current.Type == TokenEquals || p.current.Type == TokenDoubleEquals {
		posting.BalanceAssertion = p.parseBalanceAssertion()
	}

	p.skipSpaceTokens()

	if p.current.Type == TokenComment {
		posting.Comment = p.current.Value
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
		p.skipSpaceTokens()
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

	p.skipSpaceTokens()

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
	p.skipSpaceTokens()

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
	p.skipSpaceTokens()

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
	p.skipSpaceTokens()

	switch directive {
	case "account":
		return p.parseAccountDirective(pos)
	case "commodity":
		return p.parseCommodityDirective(pos)
	case "include":
		return p.parseIncludeDirective(pos)
	default:
		p.skipToNextLine()
		return nil
	}
}

func (p *Parser) parseAccountDirective(startPos Position) ast.Directive {
	if p.current.Type != TokenAccount {
		p.error("expected account name")
		p.skipToNextLine()
		return nil
	}

	dir := ast.AccountDirective{
		Account: ast.Account{
			Name:  p.current.Value,
			Parts: strings.Split(p.current.Value, ":"),
			Range: ast.Range{Start: toASTPosition(p.current.Pos)},
		},
		Range: ast.Range{Start: toASTPosition(startPos)},
	}
	p.advance()
	p.skipToNextLine()
	return dir
}

func (p *Parser) parseCommodityDirective(startPos Position) ast.Directive {
	dir := ast.CommodityDirective{
		Range: ast.Range{Start: toASTPosition(startPos)},
	}

	if p.current.Type == TokenCommodity {
		dir.Commodity = ast.Commodity{
			Symbol: p.current.Value,
			Range:  ast.Range{Start: toASTPosition(p.current.Pos)},
		}
		p.advance()
	}

	p.skipToNextLine()
	return dir
}

func (p *Parser) parseIncludeDirective(startPos Position) ast.Directive {
	if p.current.Type != TokenText {
		p.error("expected file path")
		p.skipToNextLine()
		return nil
	}

	inc := ast.Include{
		Path:  p.current.Value,
		Range: ast.Range{Start: toASTPosition(startPos)},
	}
	p.advance()
	p.skipToNextLine()
	return inc
}

func (p *Parser) parseComment() ast.Comment {
	comment := ast.Comment{
		Text:  p.current.Value,
		Range: ast.Range{Start: toASTPosition(p.current.Pos)},
	}
	p.advance()
	return comment
}

func (p *Parser) advance() {
	p.current = p.lexer.Next()
}

func (p *Parser) skipSpaceTokens() {
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
