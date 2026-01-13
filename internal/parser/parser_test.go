package parser

import (
	"testing"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParser_SimpleTransaction(t *testing.T) {
	input := `2024-01-15 grocery store
    expenses:food  $50.00
    assets:cash`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	tx := journal.Transactions[0]
	assert.Equal(t, 2024, tx.Date.Year)
	assert.Equal(t, 1, tx.Date.Month)
	assert.Equal(t, 15, tx.Date.Day)
	assert.Equal(t, "grocery store", tx.Description)
	assert.Equal(t, ast.StatusNone, tx.Status)
	require.Len(t, tx.Postings, 2)

	p1 := tx.Postings[0]
	assert.Equal(t, "expenses:food", p1.Account.Name)
	require.NotNil(t, p1.Amount)
	assert.Equal(t, "$", p1.Amount.Commodity.Symbol)
	assert.True(t, p1.Amount.Quantity.Equal(decimal.NewFromFloat(50.00)))

	p2 := tx.Postings[1]
	assert.Equal(t, "assets:cash", p2.Account.Name)
	assert.Nil(t, p2.Amount)
}

func TestParser_TransactionWithStatus(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		status ast.Status
	}{
		{
			name: "cleared",
			input: `2024-01-15 * grocery store
    expenses:food  $50
    assets:cash`,
			status: ast.StatusCleared,
		},
		{
			name: "pending",
			input: `2024-01-15 ! grocery store
    expenses:food  $50
    assets:cash`,
			status: ast.StatusPending,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			journal, errs := Parse(tt.input)
			require.Empty(t, errs)
			require.Len(t, journal.Transactions, 1)
			assert.Equal(t, tt.status, journal.Transactions[0].Status)
		})
	}
}

func TestParser_TransactionWithCode(t *testing.T) {
	input := `2024-01-15 * (12345) grocery store
    expenses:food  $50
    assets:cash`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)
	assert.Equal(t, "12345", journal.Transactions[0].Code)
}

func TestParser_TransactionWithPayeeAndNote(t *testing.T) {
	input := `2024-01-15 Grocery Store | weekly shopping
    expenses:food  $50
    assets:cash`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)
	assert.Equal(t, "Grocery Store", journal.Transactions[0].Payee)
	assert.Equal(t, "weekly shopping", journal.Transactions[0].Note)
}

func TestParser_PostingWithCost(t *testing.T) {
	input := `2024-01-15 buy stocks
    assets:stocks  10 AAPL @ $150
    assets:cash`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	p := journal.Transactions[0].Postings[0]
	assert.Equal(t, "assets:stocks", p.Account.Name)
	require.NotNil(t, p.Amount)
	assert.Equal(t, "AAPL", p.Amount.Commodity.Symbol)
	assert.True(t, p.Amount.Quantity.Equal(decimal.NewFromInt(10)))

	require.NotNil(t, p.Cost)
	assert.False(t, p.Cost.IsTotal)
	assert.Equal(t, "$", p.Cost.Amount.Commodity.Symbol)
	assert.True(t, p.Cost.Amount.Quantity.Equal(decimal.NewFromInt(150)))
}

func TestParser_PostingWithTotalCost(t *testing.T) {
	input := `2024-01-15 buy stocks
    assets:stocks  10 AAPL @@ $1500
    assets:cash`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	p := journal.Transactions[0].Postings[0]
	require.NotNil(t, p.Cost)
	assert.True(t, p.Cost.IsTotal)
	assert.True(t, p.Cost.Amount.Quantity.Equal(decimal.NewFromInt(1500)))
}

func TestParser_BalanceAssertion(t *testing.T) {
	input := `2024-01-15 check balance
    assets:checking  $100 = $1000
    income:salary`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	p := journal.Transactions[0].Postings[0]
	require.NotNil(t, p.BalanceAssertion)
	assert.False(t, p.BalanceAssertion.IsStrict)
	assert.True(t, p.BalanceAssertion.Amount.Quantity.Equal(decimal.NewFromInt(1000)))
}

func TestParser_StrictBalanceAssertion(t *testing.T) {
	input := `2024-01-15 check balance
    assets:checking  $100 == $1000
    income:salary`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	p := journal.Transactions[0].Postings[0]
	require.NotNil(t, p.BalanceAssertion)
	assert.True(t, p.BalanceAssertion.IsStrict)
}

func TestParser_AccountDirective(t *testing.T) {
	input := `account expenses:food`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Directives, 1)

	dir, ok := journal.Directives[0].(ast.AccountDirective)
	require.True(t, ok)
	assert.Equal(t, "expenses:food", dir.Account.Name)
}

func TestParser_CommodityDirective(t *testing.T) {
	input := `commodity $1000.00`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Directives, 1)

	dir, ok := journal.Directives[0].(ast.CommodityDirective)
	require.True(t, ok)
	assert.Equal(t, "$", dir.Commodity.Symbol)
}

func TestParser_IncludeDirective(t *testing.T) {
	input := `include accounts.journal`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Includes, 1)

	inc := journal.Includes[0]
	assert.Equal(t, "accounts.journal", inc.Path)
}

func TestParser_Comment(t *testing.T) {
	input := `; This is a comment
2024-01-15 test
    expenses:misc  $10
    assets:cash`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Comments, 1)
	assert.Equal(t, " This is a comment", journal.Comments[0].Text)
	require.Len(t, journal.Transactions, 1)
}

func TestParser_NegativeAmount(t *testing.T) {
	input := `2024-01-15 withdrawal
    assets:cash  $-50
    assets:bank`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	p := journal.Transactions[0].Postings[0]
	assert.True(t, p.Amount.Quantity.Equal(decimal.NewFromInt(-50)))
}

func TestParser_MultipleTransactions(t *testing.T) {
	input := `2024-01-15 first
    expenses:food  $50
    assets:cash

2024-01-16 second
    expenses:transport  $20
    assets:cash`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 2)

	assert.Equal(t, "first", journal.Transactions[0].Description)
	assert.Equal(t, "second", journal.Transactions[1].Description)
}

func TestParser_CommodityRight(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  50 EUR
    assets:cash`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	p := journal.Transactions[0].Postings[0]
	assert.Equal(t, "EUR", p.Amount.Commodity.Symbol)
	assert.Equal(t, ast.CommodityRight, p.Amount.Commodity.Position)
	assert.True(t, p.Amount.Quantity.Equal(decimal.NewFromInt(50)))
}

func TestParser_DateFormats(t *testing.T) {
	tests := []struct {
		name  string
		input string
		year  int
		month int
		day   int
	}{
		{
			name: "dashes",
			input: `2024-01-15 test
    e:f  $1
    a:c`,
			year: 2024, month: 1, day: 15,
		},
		{
			name: "slashes",
			input: `2024/01/15 test
    e:f  $1
    a:c`,
			year: 2024, month: 1, day: 15,
		},
		{
			name: "dots",
			input: `2024.01.15 test
    e:f  $1
    a:c`,
			year: 2024, month: 1, day: 15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			journal, errs := Parse(tt.input)
			require.Empty(t, errs)
			require.Len(t, journal.Transactions, 1)
			assert.Equal(t, tt.year, journal.Transactions[0].Date.Year)
			assert.Equal(t, tt.month, journal.Transactions[0].Date.Month)
			assert.Equal(t, tt.day, journal.Transactions[0].Date.Day)
		})
	}
}

func TestParser_ErrorRecovery(t *testing.T) {
	input := `2024-01-15 valid transaction
    expenses:food  $50
    assets:cash

invalid line without date

2024-01-16 another valid
    expenses:misc  $10
    assets:cash`

	journal, errs := Parse(input)
	assert.NotEmpty(t, errs)
	assert.Len(t, journal.Transactions, 2)
}
