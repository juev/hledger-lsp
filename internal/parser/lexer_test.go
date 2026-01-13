package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLexer_Date(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Token
	}{
		{
			name:  "date with dashes",
			input: "2024-01-15",
			want: []Token{
				{Type: TokenDate, Value: "2024-01-15", Pos: Position{Line: 1, Column: 1, Offset: 0}},
				{Type: TokenEOF, Value: "", Pos: Position{Line: 1, Column: 11, Offset: 10}},
			},
		},
		{
			name:  "date with slashes",
			input: "2024/01/15",
			want: []Token{
				{Type: TokenDate, Value: "2024/01/15", Pos: Position{Line: 1, Column: 1, Offset: 0}},
				{Type: TokenEOF, Value: "", Pos: Position{Line: 1, Column: 11, Offset: 10}},
			},
		},
		{
			name:  "date with dots",
			input: "2024.01.15",
			want: []Token{
				{Type: TokenDate, Value: "2024.01.15", Pos: Position{Line: 1, Column: 1, Offset: 0}},
				{Type: TokenEOF, Value: "", Pos: Position{Line: 1, Column: 11, Offset: 10}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens := collectTokens(lexer)
			assertTokensEqual(t, tt.want, tokens)
		})
	}
}

func TestLexer_Status(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Token
	}{
		{
			name:  "cleared status",
			input: "2024-01-15 *",
			want: []Token{
				{Type: TokenDate, Value: "2024-01-15"},
				{Type: TokenStatus, Value: "*"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "pending status",
			input: "2024-01-15 !",
			want: []Token{
				{Type: TokenDate, Value: "2024-01-15"},
				{Type: TokenStatus, Value: "!"},
				{Type: TokenEOF},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens := collectTokens(lexer)
			assertTokenTypesAndValues(t, tt.want, tokens)
		})
	}
}

func TestLexer_Code(t *testing.T) {
	input := "2024-01-15 * (12345)"
	lexer := NewLexer(input)
	tokens := collectTokens(lexer)

	expected := []Token{
		{Type: TokenDate, Value: "2024-01-15"},
		{Type: TokenStatus, Value: "*"},
		{Type: TokenCode, Value: "12345"},
		{Type: TokenEOF},
	}
	assertTokenTypesAndValues(t, expected, tokens)
}

func TestLexer_Description(t *testing.T) {
	input := "2024-01-15 grocery store"
	lexer := NewLexer(input)
	tokens := collectTokens(lexer)

	expected := []Token{
		{Type: TokenDate, Value: "2024-01-15"},
		{Type: TokenText, Value: "grocery store"},
		{Type: TokenEOF},
	}
	assertTokenTypesAndValues(t, expected, tokens)
}

func TestLexer_DescriptionWithPipe(t *testing.T) {
	input := "2024-01-15 Payee Name | description note"
	lexer := NewLexer(input)
	tokens := collectTokens(lexer)

	expected := []Token{
		{Type: TokenDate, Value: "2024-01-15"},
		{Type: TokenText, Value: "Payee Name"},
		{Type: TokenPipe, Value: "|"},
		{Type: TokenText, Value: "description note"},
		{Type: TokenEOF},
	}
	assertTokenTypesAndValues(t, expected, tokens)
}

func TestLexer_Comment(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Token
	}{
		{
			name:  "line comment",
			input: "; this is a comment",
			want: []Token{
				{Type: TokenComment, Value: " this is a comment"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "inline comment",
			input: "2024-01-15 test ; inline",
			want: []Token{
				{Type: TokenDate, Value: "2024-01-15"},
				{Type: TokenText, Value: "test"},
				{Type: TokenComment, Value: " inline"},
				{Type: TokenEOF},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens := collectTokens(lexer)
			assertTokenTypesAndValues(t, tt.want, tokens)
		})
	}
}

func TestLexer_Posting(t *testing.T) {
	input := "    expenses:food  $50.00"
	lexer := NewLexer(input)
	tokens := collectTokens(lexer)

	expected := []Token{
		{Type: TokenIndent, Value: "    "},
		{Type: TokenAccount, Value: "expenses:food"},
		{Type: TokenCommodity, Value: "$"},
		{Type: TokenNumber, Value: "50.00"},
		{Type: TokenEOF},
	}
	assertTokenTypesAndValues(t, expected, tokens)
}

func TestLexer_PostingWithCost(t *testing.T) {
	input := "    assets:stocks  10 AAPL @ $150.00"
	lexer := NewLexer(input)
	tokens := collectTokens(lexer)

	expected := []Token{
		{Type: TokenIndent, Value: "    "},
		{Type: TokenAccount, Value: "assets:stocks"},
		{Type: TokenNumber, Value: "10"},
		{Type: TokenCommodity, Value: "AAPL"},
		{Type: TokenAt, Value: "@"},
		{Type: TokenCommodity, Value: "$"},
		{Type: TokenNumber, Value: "150.00"},
		{Type: TokenEOF},
	}
	assertTokenTypesAndValues(t, expected, tokens)
}

func TestLexer_PostingWithTotalCost(t *testing.T) {
	input := "    assets:stocks  10 AAPL @@ $1500.00"
	lexer := NewLexer(input)
	tokens := collectTokens(lexer)

	expected := []Token{
		{Type: TokenIndent, Value: "    "},
		{Type: TokenAccount, Value: "assets:stocks"},
		{Type: TokenNumber, Value: "10"},
		{Type: TokenCommodity, Value: "AAPL"},
		{Type: TokenAtAt, Value: "@@"},
		{Type: TokenCommodity, Value: "$"},
		{Type: TokenNumber, Value: "1500.00"},
		{Type: TokenEOF},
	}
	assertTokenTypesAndValues(t, expected, tokens)
}

func TestLexer_BalanceAssertion(t *testing.T) {
	input := "    assets:checking  $100 = $1000"
	lexer := NewLexer(input)
	tokens := collectTokens(lexer)

	expected := []Token{
		{Type: TokenIndent, Value: "    "},
		{Type: TokenAccount, Value: "assets:checking"},
		{Type: TokenCommodity, Value: "$"},
		{Type: TokenNumber, Value: "100"},
		{Type: TokenEquals, Value: "="},
		{Type: TokenCommodity, Value: "$"},
		{Type: TokenNumber, Value: "1000"},
		{Type: TokenEOF},
	}
	assertTokenTypesAndValues(t, expected, tokens)
}

func TestLexer_Directive(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Token
	}{
		{
			name:  "account directive",
			input: "account expenses:food",
			want: []Token{
				{Type: TokenDirective, Value: "account"},
				{Type: TokenAccount, Value: "expenses:food"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "include directive",
			input: "include accounts.journal",
			want: []Token{
				{Type: TokenDirective, Value: "include"},
				{Type: TokenText, Value: "accounts.journal"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "commodity directive",
			input: "commodity $1000.00",
			want: []Token{
				{Type: TokenDirective, Value: "commodity"},
				{Type: TokenCommodity, Value: "$"},
				{Type: TokenNumber, Value: "1000.00"},
				{Type: TokenEOF},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens := collectTokens(lexer)
			assertTokenTypesAndValues(t, tt.want, tokens)
		})
	}
}

func TestLexer_Tag(t *testing.T) {
	input := "    expenses:food  $50  ; trip:japan"
	lexer := NewLexer(input)
	tokens := collectTokens(lexer)

	expected := []Token{
		{Type: TokenIndent, Value: "    "},
		{Type: TokenAccount, Value: "expenses:food"},
		{Type: TokenCommodity, Value: "$"},
		{Type: TokenNumber, Value: "50"},
		{Type: TokenComment, Value: " trip:japan"},
		{Type: TokenEOF},
	}
	assertTokenTypesAndValues(t, expected, tokens)
}

func TestLexer_MultipleLines(t *testing.T) {
	input := `2024-01-15 grocery store
    expenses:food  $50.00
    assets:cash`

	lexer := NewLexer(input)
	tokens := collectTokens(lexer)

	expected := []Token{
		{Type: TokenDate, Value: "2024-01-15"},
		{Type: TokenText, Value: "grocery store"},
		{Type: TokenNewline, Value: "\n"},
		{Type: TokenIndent, Value: "    "},
		{Type: TokenAccount, Value: "expenses:food"},
		{Type: TokenCommodity, Value: "$"},
		{Type: TokenNumber, Value: "50.00"},
		{Type: TokenNewline, Value: "\n"},
		{Type: TokenIndent, Value: "    "},
		{Type: TokenAccount, Value: "assets:cash"},
		{Type: TokenEOF},
	}
	assertTokenTypesAndValues(t, expected, tokens)
}

func TestLexer_NegativeAmount(t *testing.T) {
	input := "    assets:cash  $-50.00"
	lexer := NewLexer(input)
	tokens := collectTokens(lexer)

	expected := []Token{
		{Type: TokenIndent, Value: "    "},
		{Type: TokenAccount, Value: "assets:cash"},
		{Type: TokenCommodity, Value: "$"},
		{Type: TokenNumber, Value: "-50.00"},
		{Type: TokenEOF},
	}
	assertTokenTypesAndValues(t, expected, tokens)
}

func TestLexer_QuotedCommodity(t *testing.T) {
	input := `    assets:items  3 "Chocolate Frogs"`
	lexer := NewLexer(input)
	tokens := collectTokens(lexer)

	expected := []Token{
		{Type: TokenIndent, Value: "    "},
		{Type: TokenAccount, Value: "assets:items"},
		{Type: TokenNumber, Value: "3"},
		{Type: TokenCommodity, Value: "Chocolate Frogs"},
		{Type: TokenEOF},
	}
	assertTokenTypesAndValues(t, expected, tokens)
}

func TestLexer_Position(t *testing.T) {
	input := "2024-01-15 test"
	lexer := NewLexer(input)
	tokens := collectTokens(lexer)

	require.Len(t, tokens, 3)
	assert.Equal(t, Position{Line: 1, Column: 1, Offset: 0}, tokens[0].Pos)
	assert.Equal(t, Position{Line: 1, Column: 12, Offset: 11}, tokens[1].Pos)
}

func collectTokens(l *Lexer) []Token {
	var tokens []Token
	for {
		tok := l.Next()
		tokens = append(tokens, tok)
		if tok.Type == TokenEOF {
			break
		}
	}
	return tokens
}

func assertTokensEqual(t *testing.T, expected, actual []Token) {
	t.Helper()
	require.Len(t, actual, len(expected), "token count mismatch")
	for i := range expected {
		assert.Equal(t, expected[i].Type, actual[i].Type, "token %d type", i)
		assert.Equal(t, expected[i].Value, actual[i].Value, "token %d value", i)
		assert.Equal(t, expected[i].Pos, actual[i].Pos, "token %d position", i)
	}
}

func assertTokenTypesAndValues(t *testing.T, expected, actual []Token) {
	t.Helper()
	require.Len(t, actual, len(expected), "token count mismatch")
	for i := range expected {
		assert.Equal(t, expected[i].Type, actual[i].Type, "token %d type mismatch: expected %s, got %s", i, expected[i].Type, actual[i].Type)
		if expected[i].Value != "" {
			assert.Equal(t, expected[i].Value, actual[i].Value, "token %d value", i)
		}
	}
}
