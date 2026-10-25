package rules

import (
	"fmt"
	"strings"

	"github.com/juev/hledger-lsp/internal/ast"
)

// Parse parses a rules file and returns its AST and any parse-time diagnostics.
func Parse(input string) (*RulesFile, []Diagnostic) {
	p := &parser{lexer: NewLexer(input)}
	p.advance()
	return p.parseFile()
}

type parser struct {
	lexer   *Lexer
	current Token
	rf      *RulesFile
	diags   []Diagnostic
}

func (p *parser) advance() {
	p.current = p.lexer.Next()
}

func (p *parser) parseFile() (*RulesFile, []Diagnostic) {
	rf := &RulesFile{}
	p.rf = rf

	for p.current.Type != TokenEOF {
		switch p.current.Type {
		case TokenNewline:
			p.advance()

		case TokenComment:
			rf.Comments = append(rf.Comments, p.parseComment())

		case TokenIfKeyword:
			ifTok := p.current
			p.advance() // consume "if"
			if p.current.Type == TokenSeparator {
				p.parseIfTable(rf, ifTok)
			} else {
				rf.IfBlocks = append(rf.IfBlocks, p.parseIfBlock(ifTok))
			}

		case TokenEndKeyword:
			p.advance()

		case TokenDirective:
			p.parseDirective(rf)

		case TokenIndent:
			// Top-level indented line — field assignment outside if block
			p.parseIndentedAssignment(rf)

		case TokenFieldName:
			// Unindented field assignment
			a := p.parseFieldAssignmentFromCurrent()
			rf.Assignments = append(rf.Assignments, a)

		default:
			if p.current.Type == TokenText && p.current.Pos.Column == 1 {
				tok := p.current
				p.diags = append(p.diags, Diagnostic{
					Range:    tokenRange(tok),
					Message:  "unknown directive: " + firstWord(tok.Value),
					Code:     "UNKNOWN_DIRECTIVE",
					Severity: SeverityWarning,
				})
			}
			p.advance()
		}
	}

	return rf, p.diags
}

func (p *parser) parseComment() RulesComment {
	tok := p.current
	p.advance()
	return RulesComment{
		Text:  tok.Value,
		Range: tokenRange(tok),
	}
}

func (p *parser) parseDirective(rf *RulesFile) {
	dirTok := p.current
	p.advance()

	var valueTok *Token
	if p.current.Type == TokenText {
		t := p.current
		valueTok = &t
		p.advance()
	}

	name := dirTok.Value
	value := ""
	if valueTok != nil {
		value = valueTok.Value
	}

	rng := tokenRange(dirTok)
	if valueTok != nil {
		rng.End = ast.Position{Line: valueTok.End.Line, Column: valueTok.End.Column, Offset: valueTok.End.Offset}
	}

	switch name {
	case "fields":
		fd := FieldsDirective{Range: rng}
		for f := range strings.SplitSeq(value, ",") {
			fn := strings.TrimSpace(f)
			if fn != "" {
				fd.Names = append(fd.Names, fn)
			}
		}
		rf.FieldsDefs = append(rf.FieldsDefs, fd)

	case "include":
		rf.Includes = append(rf.Includes, IncludeDirective{
			Path:  value,
			Range: rng,
		})

	case "source":
		rf.Sources = append(rf.Sources, SourceDirective{
			Path:  value,
			Range: rng,
		})

	default:
		rf.Directives = append(rf.Directives, SimpleDirective{
			Name:  name,
			Value: value,
			Range: rng,
		})
	}
}

// parseIfBlock parses an if block; startTok is the already-consumed "if" keyword.
func (p *parser) parseIfBlock(startTok Token) IfBlock {
	block := IfBlock{}

	// Collect inline pattern from TokenRegex on the "if" line
	if p.current.Type == TokenRegex || p.current.Type == TokenText {
		block.Patterns = append(block.Patterns, p.current.Value)
		p.advance()
	}

	// Skip newline after "if PATTERN"
	if p.current.Type == TokenNewline {
		p.advance()
	}

	// Collect continuation patterns (&) and indented assignments
	var lastPatternEnd ast.Position
loop:
	for p.current.Type != TokenEOF {
		switch p.current.Type {
		case TokenIndent:
			a := p.parseIndentedAssignmentDirect()
			if a.Field != "" {
				block.Assignments = append(block.Assignments, a)
			}
		case TokenText:
			if strings.HasPrefix(p.current.Value, "& ") || p.current.Value == "&" {
				tok := p.current
				pat := strings.TrimPrefix(strings.TrimPrefix(p.current.Value, "& "), "&")
				pat = strings.TrimSpace(pat)
				if pat != "" {
					block.Patterns = append(block.Patterns, pat)
				} else {
					p.diags = append(p.diags, Diagnostic{
						Range:    tokenRange(tok),
						Message:  "empty continuation pattern",
						Code:     "EMPTY_PATTERN",
						Severity: SeverityWarning,
					})
				}
				lastPatternEnd = ast.Position{Line: tok.End.Line, Column: tok.End.Column, Offset: tok.End.Offset}
				p.advance()
				if p.current.Type == TokenNewline {
					p.advance()
				}
			} else if len(block.Assignments) == 0 && p.current.Pos.Column == 1 {
				tok := p.current
				block.Patterns = append(block.Patterns, tok.Value)
				lastPatternEnd = ast.Position{
					Line: tok.End.Line, Column: tok.End.Column, Offset: tok.End.Offset,
				}
				p.advance()
				if p.current.Type == TokenNewline {
					p.advance()
				}
			} else {
				break loop
			}
		case TokenNewline:
			p.advance()
		case TokenComment:
			if len(block.Assignments) == 0 {
				tok := p.current
				block.Patterns = append(block.Patterns, tok.Value)
				lastPatternEnd = ast.Position{
					Line: tok.End.Line, Column: tok.End.Column, Offset: tok.End.Offset,
				}
				p.advance()
				if p.current.Type == TokenNewline {
					p.advance()
				}
			} else {
				break loop
			}
		case TokenEndKeyword, TokenDirective, TokenIfKeyword:
			break loop
		default:
			break loop
		}
	}

	blockEnd := ast.Position{Line: startTok.Pos.Line, Column: startTok.End.Column, Offset: startTok.End.Offset}
	if len(block.Assignments) > 0 {
		blockEnd = block.Assignments[len(block.Assignments)-1].Range.End
	} else if lastPatternEnd.Line > 0 {
		blockEnd = lastPatternEnd
	}
	block.Range = ast.Range{
		Start: posFromToken(startTok),
		End:   blockEnd,
	}
	return block
}

// parseIfTable parses an if table; ifTok is the already-consumed "if" keyword and
// the current token is the header's first separator. Each row becomes an if block.
func (p *parser) parseIfTable(rf *RulesFile, ifTok Token) {
	table := IfTable{}
	headerEnd := ifTok.End

	// Every separator in the header opens a field-name slot.
	slotOpen := false
	var lastSep Token
	closeEmptySlot := func() {
		if slotOpen {
			table.Fields = append(table.Fields, "")
			p.diags = append(p.diags, Diagnostic{
				Range:    tokenRange(lastSep),
				Message:  "missing field name in if table header",
				Code:     "EMPTY_FIELD_NAME",
				Severity: SeverityError,
			})
		}
	}
	for p.current.Type != TokenNewline && p.current.Type != TokenEOF {
		tok := p.current
		switch tok.Type {
		case TokenSeparator:
			closeEmptySlot()
			slotOpen = true
			lastSep = tok
		default:
			if tok.Type != TokenFieldName {
				p.diags = append(p.diags, Diagnostic{
					Range:    tokenRange(tok),
					Message:  "unknown field name: " + tok.Value,
					Code:     "UNKNOWN_FIELD",
					Severity: SeverityWarning,
				})
			}
			table.Fields = append(table.Fields, tok.Value)
			slotOpen = false
		}
		headerEnd = tok.End
		p.advance()
	}
	closeEmptySlot()

	// The lexer ends a table at the first blank line, so table rows are exactly
	// the lines that start with a matcher or a separator.
rows:
	for {
		switch {
		case p.current.Type == TokenNewline:
			p.advance()
		case p.current.Type == TokenComment:
			rf.Comments = append(rf.Comments, p.parseComment())
		case (p.current.Type == TokenRegex || p.current.Type == TokenSeparator) && p.current.Pos.Column == 1:
			table.Rows = append(table.Rows, p.parseIfTableRow(table.Fields))
		default:
			break rows
		}
	}

	headerRange := ast.Range{Start: posFromToken(ifTok), End: astPos(headerEnd)}
	table.Range = headerRange
	if len(table.Rows) == 0 {
		p.diags = append(p.diags, Diagnostic{
			Range:    headerRange,
			Message:  "if table has no rows",
			Code:     "IF_TABLE_NO_ROWS",
			Severity: SeverityError,
		})
	} else {
		table.Range.End = table.Rows[len(table.Rows)-1].Range.End
	}

	rf.IfBlocks = append(rf.IfBlocks, table.Rows...)
	rf.IfTables = append(rf.IfTables, table)
}

// parseIfTableRow parses one "matcher<sep>value<sep>value…" row of an if table.
func (p *parser) parseIfTableRow(fields []string) IfBlock {
	startTok := p.current
	block := IfBlock{}
	end := startTok.End

	indented := false
	if p.current.Type == TokenRegex {
		matcher := p.current.Value
		if pattern := strings.TrimSpace(matcher); pattern != "" {
			block.Patterns = append(block.Patterns, pattern)
		}
		// hledger requires a row's matcher to start at the beginning of the line.
		if strings.TrimLeft(matcher, " \t") != matcher {
			indented = true
			p.diags = append(p.diags, Diagnostic{
				Range:    tokenRange(p.current),
				Message:  "if table rows must not be indented",
				Code:     "INDENTED_IF_TABLE_ROW",
				Severity: SeverityError,
			})
		}
		p.advance()
	}
	// hledger accepts a row that starts with the separator, but such a row never matches.
	if len(block.Patterns) == 0 && !indented {
		p.diags = append(p.diags, Diagnostic{
			Range:    tokenRange(startTok),
			Message:  "empty if table pattern: this row never matches",
			Code:     "EMPTY_PATTERN",
			Severity: SeverityWarning,
		})
	}

	// Each separator starts a value; a text token after it fills the value in.
	var values []FieldAssignment
	for p.current.Type == TokenSeparator || p.current.Type == TokenText {
		tok := p.current
		if tok.Type == TokenSeparator {
			pos := astPos(tok.End)
			values = append(values, FieldAssignment{Range: ast.Range{Start: pos, End: pos}})
		} else if len(values) > 0 {
			values[len(values)-1].Value = tok.Value
			values[len(values)-1].Range = tokenRange(tok)
		}
		end = tok.End
		p.advance()
	}
	if p.current.Type == TokenNewline {
		p.advance()
	}

	block.Range = ast.Range{Start: posFromToken(startTok), End: astPos(end)}

	if len(values) != len(fields) {
		p.diags = append(p.diags, Diagnostic{
			Range:    block.Range,
			Message:  fmt.Sprintf("if table row should have %s, but has %d", pluralize(len(fields), "value"), len(values)),
			Code:     "IF_TABLE_VALUE_COUNT",
			Severity: SeverityError,
		})
	}

	for i, v := range values {
		if i >= len(fields) {
			break
		}
		if fields[i] == "" {
			continue
		}
		v.Field = fields[i]
		block.Assignments = append(block.Assignments, v)
	}
	return block
}

func pluralize(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// parseIndentedAssignment handles a top-level indented line as a field assignment
// outside of any if block (adds to rf.Assignments).
func (p *parser) parseIndentedAssignment(rf *RulesFile) {
	a := p.parseIndentedAssignmentDirect()
	if a.Field != "" {
		rf.Assignments = append(rf.Assignments, a)
	}
}

// parseIndentedAssignmentDirect parses an indented field assignment and returns it.
func (p *parser) parseIndentedAssignmentDirect() FieldAssignment {
	p.advance() // consume TokenIndent

	if p.current.Type == TokenFieldName {
		return p.parseFieldAssignmentFromCurrent()
	}

	// skip non-field indented line; multi-word text signals an unknown field name
	if p.current.Type == TokenText {
		tok := p.current
		if firstWord(tok.Value) != tok.Value {
			p.diags = append(p.diags, Diagnostic{
				Range:    tokenRange(tok),
				Message:  "unknown field name: " + firstWord(tok.Value),
				Code:     "UNKNOWN_FIELD",
				Severity: SeverityWarning,
			})
		}
		p.advance()
	}
	if p.current.Type == TokenNewline {
		p.advance()
	}
	return FieldAssignment{}
}

func (p *parser) parseFieldAssignmentFromCurrent() FieldAssignment {
	fieldTok := p.current
	p.advance()

	a := FieldAssignment{
		Field: fieldTok.Value,
		Range: tokenRange(fieldTok),
	}

	if p.current.Type == TokenText {
		a.Value = p.current.Value
		a.Range.End = ast.Position{Line: p.current.End.Line, Column: p.current.End.Column, Offset: p.current.End.Offset}
		p.advance()
	}

	if p.current.Type == TokenNewline {
		p.advance()
	}

	return a
}

func tokenRange(tok Token) ast.Range {
	return ast.Range{
		Start: posFromToken(tok),
		End:   ast.Position{Line: tok.End.Line, Column: tok.End.Column, Offset: tok.End.Offset},
	}
}

func posFromToken(tok Token) ast.Position {
	return ast.Position{Line: tok.Pos.Line, Column: tok.Pos.Column, Offset: tok.Pos.Offset}
}

func astPos(pos Position) ast.Position {
	return ast.Position{Line: pos.Line, Column: pos.Column, Offset: pos.Offset}
}
