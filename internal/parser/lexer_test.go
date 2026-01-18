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
		{Type: TokenSign, Value: "-"},
		{Type: TokenNumber, Value: "50.00"},
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

func TestLexer_LowercaseCommodity(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		tokens []Token
	}{
		{
			name:  "mixed case FFf returns Text (parser handles as commodity)",
			input: "    expenses:food  3.000 FFf",
			tokens: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "expenses:food"},
				{Type: TokenNumber, Value: "3.000"},
				{Type: TokenText, Value: "FFf"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "lowercase Rub returns Text (parser handles as commodity)",
			input: "    expenses:food  100 Rub",
			tokens: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "expenses:food"},
				{Type: TokenNumber, Value: "100"},
				{Type: TokenText, Value: "Rub"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "all lowercase hours returns Text (parser handles as commodity)",
			input: "    work:project  8 hours",
			tokens: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "work:project"},
				{Type: TokenNumber, Value: "8"},
				{Type: TokenText, Value: "hours"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "uppercase only USD2024 returns Commodity",
			input: "    assets:stocks  10 USD2024",
			tokens: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "assets:stocks"},
				{Type: TokenNumber, Value: "10"},
				{Type: TokenCommodity, Value: "USD2024"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "cyrillic Руб returns Text (parser handles as commodity)",
			input: "    expenses:food  100 Руб",
			tokens: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "expenses:food"},
				{Type: TokenNumber, Value: "100"},
				{Type: TokenText, Value: "Руб"},
				{Type: TokenEOF},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens := collectTokens(lexer)
			assertTokenTypesAndValues(t, tt.tokens, tokens)
		})
	}
}

func TestLexer_AmbiguousCases(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		tokens []Token
	}{
		{
			name:  "word after amount becomes Text (parser decides if commodity)",
			input: "    expenses:food  100 note",
			tokens: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "expenses:food"},
				{Type: TokenNumber, Value: "100"},
				{Type: TokenText, Value: "note"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "multiple words after amount",
			input: "    expenses:food  100 note some text",
			tokens: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "expenses:food"},
				{Type: TokenNumber, Value: "100"},
				{Type: TokenText, Value: "note some text"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "comment terminates text",
			input: "    expenses:food  100 note ; comment",
			tokens: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "expenses:food"},
				{Type: TokenNumber, Value: "100"},
				{Type: TokenText, Value: "note"},
				{Type: TokenComment, Value: " comment"},
				{Type: TokenEOF},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens := collectTokens(lexer)
			assertTokenTypesAndValues(t, tt.tokens, tokens)
		})
	}
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

func findToken(tokens []Token, tokenType TokenType) *Token {
	for i := range tokens {
		if tokens[i].Type == tokenType {
			return &tokens[i]
		}
	}
	return nil
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

func TestLexer_UnicodeAccountNames(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Token
	}{
		{
			name:  "cyrillic account in posting",
			input: "    Активы:Банк  100 RUB",
			want: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "Активы:Банк"},
				{Type: TokenNumber, Value: "100"},
				{Type: TokenCommodity, Value: "RUB"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "cyrillic account directive",
			input: "account Активы:Банк",
			want: []Token{
				{Type: TokenDirective, Value: "account"},
				{Type: TokenAccount, Value: "Активы:Банк"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "chinese account name",
			input: "    资产:银行  100 CNY",
			want: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "资产:银行"},
				{Type: TokenNumber, Value: "100"},
				{Type: TokenCommodity, Value: "CNY"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "mixed unicode and latin",
			input: "    Расходы:Food  50 USD",
			want: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "Расходы:Food"},
				{Type: TokenNumber, Value: "50"},
				{Type: TokenCommodity, Value: "USD"},
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

func TestLexer_SpecialCharactersInAccountNames(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Token
	}{
		{
			name:  "account with slash",
			input: "    equity:opening/closing balances  100 RUB",
			want: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "equity:opening/closing balances"},
				{Type: TokenNumber, Value: "100"},
				{Type: TokenCommodity, Value: "RUB"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "account with dot",
			input: "    assets:bank.main  100 USD",
			want: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "assets:bank.main"},
				{Type: TokenNumber, Value: "100"},
				{Type: TokenCommodity, Value: "USD"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "account with ampersand",
			input: "    expenses:food&drink  50 EUR",
			want: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "expenses:food&drink"},
				{Type: TokenNumber, Value: "50"},
				{Type: TokenCommodity, Value: "EUR"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "account with apostrophe",
			input: "    liabilities:john's card  200 USD",
			want: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "liabilities:john's card"},
				{Type: TokenNumber, Value: "200"},
				{Type: TokenCommodity, Value: "USD"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "account with hash inside",
			input: "    expenses:item#123  10 USD",
			want: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "expenses:item#123"},
				{Type: TokenNumber, Value: "10"},
				{Type: TokenCommodity, Value: "USD"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "account with plus",
			input: "    assets:c++fund  100 USD",
			want: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "assets:c++fund"},
				{Type: TokenNumber, Value: "100"},
				{Type: TokenCommodity, Value: "USD"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "account stops at semicolon",
			input: "    assets:bank  100 USD  ; comment",
			want: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "assets:bank"},
				{Type: TokenNumber, Value: "100"},
				{Type: TokenCommodity, Value: "USD"},
				{Type: TokenComment, Value: " comment"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "account stops at at-sign for cost",
			input: "    assets:bank  100 USD @ 90 EUR",
			want: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "assets:bank"},
				{Type: TokenNumber, Value: "100"},
				{Type: TokenCommodity, Value: "USD"},
				{Type: TokenAt, Value: "@"},
				{Type: TokenNumber, Value: "90"},
				{Type: TokenCommodity, Value: "EUR"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "account stops at equals for balance assertion",
			input: "    assets:bank  100 USD = 500 USD",
			want: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "assets:bank"},
				{Type: TokenNumber, Value: "100"},
				{Type: TokenCommodity, Value: "USD"},
				{Type: TokenEquals, Value: "="},
				{Type: TokenNumber, Value: "500"},
				{Type: TokenCommodity, Value: "USD"},
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

func TestLexer_YearDirective(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Token
	}{
		{
			name:  "Y directive with year",
			input: "Y2026",
			want: []Token{
				{Type: TokenDirective, Value: "Y"},
				{Type: TokenNumber, Value: "2026"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "Y directive with space",
			input: "Y 2026",
			want: []Token{
				{Type: TokenDirective, Value: "Y"},
				{Type: TokenNumber, Value: "2026"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "year directive full",
			input: "year 2025",
			want: []Token{
				{Type: TokenDirective, Value: "year"},
				{Type: TokenNumber, Value: "2025"},
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

func TestLexer_PartialDate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Token
	}{
		{
			name:  "partial date MM-DD",
			input: "01-02 description",
			want: []Token{
				{Type: TokenDate, Value: "01-02"},
				{Type: TokenText, Value: "description"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "partial date MM/DD",
			input: "01/02 description",
			want: []Token{
				{Type: TokenDate, Value: "01/02"},
				{Type: TokenText, Value: "description"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "partial date with status",
			input: "01-02 * cleared tx",
			want: []Token{
				{Type: TokenDate, Value: "01-02"},
				{Type: TokenStatus, Value: "*"},
				{Type: TokenText, Value: "cleared tx"},
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

func TestLexer_Date2(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Token
	}{
		{
			name:  "date with date2",
			input: "2024-01-15=2024-01-20 description",
			want: []Token{
				{Type: TokenDate, Value: "2024-01-15"},
				{Type: TokenEquals, Value: "="},
				{Type: TokenDate, Value: "2024-01-20"},
				{Type: TokenText, Value: "description"},
				{Type: TokenEOF, Value: ""},
			},
		},
		{
			name:  "date2 with slashes",
			input: "2024/01/15=2024/01/20 test",
			want: []Token{
				{Type: TokenDate, Value: "2024/01/15"},
				{Type: TokenEquals, Value: "="},
				{Type: TokenDate, Value: "2024/01/20"},
				{Type: TokenText, Value: "test"},
				{Type: TokenEOF, Value: ""},
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

func TestLexer_SignBeforeCommodity(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		tokens []TokenType
	}{
		{
			name:   "negative dollar",
			input:  "-$100",
			tokens: []TokenType{TokenSign, TokenCommodity, TokenNumber, TokenEOF},
		},
		{
			name:   "positive dollar",
			input:  "+$100",
			tokens: []TokenType{TokenSign, TokenCommodity, TokenNumber, TokenEOF},
		},
		{
			name:   "negative euro",
			input:  "-€100",
			tokens: []TokenType{TokenSign, TokenCommodity, TokenNumber, TokenEOF},
		},
		{
			name:   "negative ruble",
			input:  "-₽100",
			tokens: []TokenType{TokenSign, TokenCommodity, TokenNumber, TokenEOF},
		},
		{
			name:   "negative in posting",
			input:  "    assets:cash  -$50.00",
			tokens: []TokenType{TokenIndent, TokenAccount, TokenSign, TokenCommodity, TokenNumber, TokenEOF},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens := collectTokens(lexer)
			require.Len(t, tokens, len(tt.tokens), "token count mismatch")
			for i, expectedType := range tt.tokens {
				assert.Equal(t, expectedType, tokens[i].Type, "token %d type mismatch", i)
			}
		})
	}
}

func TestLexer_NumberFormatsExtended(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantNumber  string
		numberIndex int
		wantTokens  int
	}{
		{"space grouping", "    a:b  1 000.00", "1 000.00", 2, 4},
		{"space grouping euro", "    a:b  1 000,50", "1 000,50", 2, 4},
		{"large space grouped", "    a:b  3 037 850,96", "3 037 850,96", 2, 4},
		{"scientific lower", "    a:b  1e-6", "1e-6", 2, 4},
		{"scientific upper", "    a:b  1E3", "1E3", 2, 4},
		{"scientific with plus", "    a:b  1E+3", "1E+3", 2, 4},
		{"scientific with minus", "    a:b  1E-10", "1E-10", 2, 4},
		{"explicit plus", "    a:b  +100", "100", 3, 5},
		{"explicit plus decimal", "    a:b  +100.50", "100.50", 3, 5},
		{"space grouping with commodity", "    a:b  1 000.00 USD", "1 000.00", 2, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens := collectTokens(lexer)
			require.GreaterOrEqual(t, len(tokens), tt.wantTokens, "not enough tokens")
			assert.Equal(t, TokenNumber, tokens[tt.numberIndex].Type, "expected Number token")
			assert.Equal(t, tt.wantNumber, tokens[tt.numberIndex].Value, "number value mismatch")
		})
	}
}

func TestLexer_InvalidScientificNotation(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantNumber string
	}{
		{
			name:       "E without digits",
			input:      "    a:b  1E",
			wantNumber: "1",
		},
		{
			name:       "E+ without digits",
			input:      "    a:b  1E+",
			wantNumber: "1",
		},
		{
			name:       "E- without digits",
			input:      "    a:b  1E-",
			wantNumber: "1",
		},
		{
			name:       "E followed by non-digit",
			input:      "    a:b  1Ex",
			wantNumber: "1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens := collectTokens(lexer)

			tok := findToken(tokens, TokenNumber)
			require.NotNil(t, tok, "expected a Number token")
			assert.Equal(t, tt.wantNumber, tok.Value, "E without digits should not be included in number")
		})
	}
}

func TestLexer_ScientificNotationConsumesExponent(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantNumber string
	}{
		{
			name:       "1E3 followed by text",
			input:      "    a:b  1E3x",
			wantNumber: "1E3",
		},
		{
			name:       "1E+3 followed by text",
			input:      "    a:b  1E+3x",
			wantNumber: "1E+3",
		},
		{
			name:       "1E-3 followed by text",
			input:      "    a:b  1E-3x",
			wantNumber: "1E-3",
		},
		{
			name:       "1E10 multi-digit exponent",
			input:      "    a:b  1E10",
			wantNumber: "1E10",
		},
		{
			name:       "1E+10 multi-digit with sign",
			input:      "    a:b  1E+10",
			wantNumber: "1E+10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens := collectTokens(lexer)

			tok := findToken(tokens, TokenNumber)
			require.NotNil(t, tok, "expected a Number token")
			assert.Equal(t, tt.wantNumber, tok.Value, "scientific notation should include full exponent")
		})
	}
}

func TestLexer_UnterminatedTokens(t *testing.T) {
	t.Run("unterminated quoted commodity", func(t *testing.T) {
		input := `    a:b  3 "USD`
		lexer := NewLexer(input)
		tokens := collectTokens(lexer)

		tok := findToken(tokens, TokenCommodity)
		require.NotNil(t, tok)
		assert.Equal(t, "USD", tok.Value)
	})

	t.Run("unterminated quoted commodity at newline", func(t *testing.T) {
		input := "    a:b  3 \"USD\n    c:d"
		lexer := NewLexer(input)
		tokens := collectTokens(lexer)

		tok := findToken(tokens, TokenCommodity)
		require.NotNil(t, tok)
		assert.Equal(t, "USD", tok.Value)
	})

	t.Run("unterminated code parenthesis", func(t *testing.T) {
		input := "2024-01-15 * (123"
		lexer := NewLexer(input)
		tokens := collectTokens(lexer)

		tok := findToken(tokens, TokenCode)
		require.NotNil(t, tok)
		assert.Equal(t, "123", tok.Value)
	})

	t.Run("unterminated code at newline", func(t *testing.T) {
		input := "2024-01-15 * (123\n    a:b"
		lexer := NewLexer(input)
		tokens := collectTokens(lexer)

		tok := findToken(tokens, TokenCode)
		require.NotNil(t, tok)
		assert.Equal(t, "123", tok.Value)
	})

	t.Run("incomplete scientific notation E+ stops at number", func(t *testing.T) {
		input := "    a:b  1E+"
		lexer := NewLexer(input)
		tokens := collectTokens(lexer)

		tok := findToken(tokens, TokenNumber)
		require.NotNil(t, tok)
		assert.Equal(t, "1", tok.Value)
	})

	t.Run("incomplete scientific notation E only stops at number", func(t *testing.T) {
		input := "    a:b  1E"
		lexer := NewLexer(input)
		tokens := collectTokens(lexer)

		tok := findToken(tokens, TokenNumber)
		require.NotNil(t, tok)
		assert.Equal(t, "1", tok.Value)
	})

	t.Run("partial date at EOF", func(t *testing.T) {
		input := "2024-01"
		lexer := NewLexer(input)
		tokens := collectTokens(lexer)

		tok := findToken(tokens, TokenDate)
		require.NotNil(t, tok)
		assert.Equal(t, "2024-01", tok.Value)
	})
}

func TestLexer_AmountFormatVariations(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		tokens []Token
	}{
		{
			name:  "-USD222 should split into sign, commodity, number",
			input: "-USD222",
			tokens: []Token{
				{Type: TokenSign, Value: "-"},
				{Type: TokenCommodity, Value: "USD"},
				{Type: TokenNumber, Value: "222"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "USD222 should split into commodity, number (in posting context)",
			input: "    a:b  USD222",
			tokens: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "a:b"},
				{Type: TokenCommodity, Value: "USD"},
				{Type: TokenNumber, Value: "222"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "USD-222 should split into commodity, sign, number (in posting context)",
			input: "    a:b  USD-222",
			tokens: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "a:b"},
				{Type: TokenCommodity, Value: "USD"},
				{Type: TokenSign, Value: "-"},
				{Type: TokenNumber, Value: "222"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "$-100 should split into commodity, sign, number",
			input: "$-100",
			tokens: []Token{
				{Type: TokenCommodity, Value: "$"},
				{Type: TokenSign, Value: "-"},
				{Type: TokenNumber, Value: "100"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "-100 USD should split into sign, number, commodity",
			input: "-100 USD",
			tokens: []Token{
				{Type: TokenSign, Value: "-"},
				{Type: TokenNumber, Value: "100"},
				{Type: TokenCommodity, Value: "USD"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "+USD100 should split into sign, commodity, number",
			input: "+USD100",
			tokens: []Token{
				{Type: TokenSign, Value: "+"},
				{Type: TokenCommodity, Value: "USD"},
				{Type: TokenNumber, Value: "100"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "EUR+50 should split into commodity, sign, number (in posting context)",
			input: "    a:b  EUR+50",
			tokens: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "a:b"},
				{Type: TokenCommodity, Value: "EUR"},
				{Type: TokenSign, Value: "+"},
				{Type: TokenNumber, Value: "50"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "posting with -USD222",
			input: "    expenses:food  -USD222",
			tokens: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "expenses:food"},
				{Type: TokenSign, Value: "-"},
				{Type: TokenCommodity, Value: "USD"},
				{Type: TokenNumber, Value: "222"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "posting with USD-222",
			input: "    expenses:food  USD-222",
			tokens: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "expenses:food"},
				{Type: TokenCommodity, Value: "USD"},
				{Type: TokenSign, Value: "-"},
				{Type: TokenNumber, Value: "222"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "10 USD2024 should keep commodity intact (letters+digits after space)",
			input: "    a:b  10 USD2024",
			tokens: []Token{
				{Type: TokenIndent, Value: "    "},
				{Type: TokenAccount, Value: "a:b"},
				{Type: TokenNumber, Value: "10"},
				{Type: TokenCommodity, Value: "USD2024"},
				{Type: TokenEOF},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens := collectTokens(lexer)
			assertTokenTypesAndValues(t, tt.tokens, tokens)
		})
	}
}

func TestLexer_IncludeWithTransaction(t *testing.T) {
	input := `include level1.journal

2024-01-15 * main transaction
    expenses:main  $10.00
    assets:cash
`
	lexer := NewLexer(input)
	tokens := collectTokens(lexer)

	expected := []Token{
		{Type: TokenDirective, Value: "include"},
		{Type: TokenText, Value: "level1.journal"},
		{Type: TokenNewline},
		{Type: TokenNewline},
		{Type: TokenDate, Value: "2024-01-15"},
		{Type: TokenStatus, Value: "*"},
		{Type: TokenText, Value: "main transaction"},
		{Type: TokenNewline},
		{Type: TokenIndent},
		{Type: TokenAccount, Value: "expenses:main"},
		{Type: TokenCommodity, Value: "$"},
		{Type: TokenNumber, Value: "10.00"},
		{Type: TokenNewline},
		{Type: TokenIndent},
		{Type: TokenAccount, Value: "assets:cash"},
		{Type: TokenNewline},
		{Type: TokenEOF},
	}

	assertTokenTypesAndValues(t, expected, tokens)
}

func TestParser_AmountFormatVariations(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantQty  string
		wantComm string
	}{
		{
			name: "-USD222",
			input: `2024-01-15 test
    expenses:food  -USD222
    assets:cash
`,
			wantQty:  "-222",
			wantComm: "USD",
		},
		{
			name: "USD222",
			input: `2024-01-15 test
    expenses:food  USD222
    assets:cash
`,
			wantQty:  "222",
			wantComm: "USD",
		},
		{
			name: "USD-222",
			input: `2024-01-15 test
    expenses:food  USD-222
    assets:cash
`,
			wantQty:  "-222",
			wantComm: "USD",
		},
		{
			name: "$-100",
			input: `2024-01-15 test
    expenses:food  $-100
    assets:cash
`,
			wantQty:  "-100",
			wantComm: "$",
		},
		{
			name: "-100 USD",
			input: `2024-01-15 test
    expenses:food  -100 USD
    assets:cash
`,
			wantQty:  "-100",
			wantComm: "USD",
		},
		{
			name: "-$100",
			input: `2024-01-15 test
    expenses:food  -$100
    assets:cash
`,
			wantQty:  "-100",
			wantComm: "$",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			journal, errs := Parse(tt.input)
			require.Empty(t, errs, "unexpected parse errors: %v", errs)
			require.Len(t, journal.Transactions, 1)
			require.Len(t, journal.Transactions[0].Postings, 2)

			posting := journal.Transactions[0].Postings[0]
			require.NotNil(t, posting.Amount, "amount should not be nil")

			assert.Equal(t, tt.wantQty, posting.Amount.Quantity.String(), "quantity mismatch")
			assert.Equal(t, tt.wantComm, posting.Amount.Commodity.Symbol, "commodity mismatch")
		})
	}
}

func TestLexer_DateBoundaryConditions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Valid dates
		{name: "standard date", input: "2024-01-15", expected: "2024-01-15"},
		{name: "date with slashes", input: "2024/01/15", expected: "2024/01/15"},
		{name: "date with dots", input: "2024.01.15", expected: "2024.01.15"},

		// Partial dates (month/day only)
		{name: "partial date month-day", input: "2024-01", expected: "2024-01"},

		// Invalid month/day values still recognized as date tokens
		{name: "month 99", input: "2024-99-01", expected: "2024-99-01"},
		{name: "day 99", input: "2024-01-99", expected: "2024-01-99"},
		{name: "all 99s", input: "9999-99-99", expected: "9999-99-99"},

		// Large years
		{name: "year 10000", input: "10000-01-15", expected: "10000-01-15"},

		// Boundary at exactly 8 chars (minimum for date detection)
		{name: "exactly 8 chars", input: "2024-0-1", expected: "2024-0-1"},

		// Partial years/dates still tokenized as dates (tolerant lexer)
		{name: "only year", input: "2024", expected: "2024"},
		{name: "year and sep", input: "2024-", expected: "2024-"},

		// These are still recognized as date tokens (tolerant lexer behavior)
		{name: "text starts like date", input: "2024abc", expected: "2024"},
		{name: "compact date no separator", input: "20240115", expected: "20240115"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens := collectTokens(lexer)

			tok := findToken(tokens, TokenDate)
			require.NotNil(t, tok, "expected a Date token for input: %s", tt.input)
			assert.Equal(t, tt.expected, tok.Value)
		})
	}
}
