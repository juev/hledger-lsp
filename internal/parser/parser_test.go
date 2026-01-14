package parser

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/ast"
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

func TestParser_Date2(t *testing.T) {
	input := `2024-01-15=2024-01-20 transaction with date2
    expenses:food  $50
    assets:cash`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	tx := journal.Transactions[0]
	assert.Equal(t, 2024, tx.Date.Year)
	assert.Equal(t, 1, tx.Date.Month)
	assert.Equal(t, 15, tx.Date.Day)

	require.NotNil(t, tx.Date2)
	assert.Equal(t, 2024, tx.Date2.Year)
	assert.Equal(t, 1, tx.Date2.Month)
	assert.Equal(t, 20, tx.Date2.Day)

	assert.Equal(t, "transaction with date2", tx.Description)
}

func TestParser_Date2Formats(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		year2  int
		month2 int
		day2   int
	}{
		{
			name: "dashes",
			input: `2024-01-15=2024-01-20 test
    e:f  $1
    a:c`,
			year2: 2024, month2: 1, day2: 20,
		},
		{
			name: "slashes",
			input: `2024/01/15=2024/01/20 test
    e:f  $1
    a:c`,
			year2: 2024, month2: 1, day2: 20,
		},
		{
			name: "mixed separators",
			input: `2024-01-15=2024/01/20 test
    e:f  $1
    a:c`,
			year2: 2024, month2: 1, day2: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			journal, errs := Parse(tt.input)
			require.Empty(t, errs)
			require.Len(t, journal.Transactions, 1)
			require.NotNil(t, journal.Transactions[0].Date2)
			assert.Equal(t, tt.year2, journal.Transactions[0].Date2.Year)
			assert.Equal(t, tt.month2, journal.Transactions[0].Date2.Month)
			assert.Equal(t, tt.day2, journal.Transactions[0].Date2.Day)
		})
	}
}

func TestParser_PriceDirective(t *testing.T) {
	input := `P 2024-01-15 EUR $1.08`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Directives, 1)

	dir, ok := journal.Directives[0].(ast.PriceDirective)
	require.True(t, ok)
	assert.Equal(t, 2024, dir.Date.Year)
	assert.Equal(t, 1, dir.Date.Month)
	assert.Equal(t, 15, dir.Date.Day)
	assert.Equal(t, "EUR", dir.Commodity.Symbol)
	assert.Equal(t, "$", dir.Price.Commodity.Symbol)
	assert.True(t, dir.Price.Quantity.Equal(decimal.NewFromFloat(1.08)))
}

func TestParser_PriceDirectiveVariants(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		commodity string
		priceSym  string
		priceQty  float64
	}{
		{
			name:      "stock price",
			input:     `P 2024-01-15 AAPL $185.50`,
			commodity: "AAPL",
			priceSym:  "$",
			priceQty:  185.50,
		},
		{
			name:      "crypto price",
			input:     `P 2024-01-15 BTC $42000.00`,
			commodity: "BTC",
			priceSym:  "$",
			priceQty:  42000.00,
		},
		{
			name:      "currency with right commodity",
			input:     `P 2024-01-15 USD 0.92 EUR`,
			commodity: "USD",
			priceSym:  "EUR",
			priceQty:  0.92,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			journal, errs := Parse(tt.input)
			require.Empty(t, errs)
			require.Len(t, journal.Directives, 1)

			dir, ok := journal.Directives[0].(ast.PriceDirective)
			require.True(t, ok)
			assert.Equal(t, tt.commodity, dir.Commodity.Symbol)
			assert.Equal(t, tt.priceSym, dir.Price.Commodity.Symbol)
			assert.True(t, dir.Price.Quantity.Equal(decimal.NewFromFloat(tt.priceQty)))
		})
	}
}

func TestParser_VirtualPostings(t *testing.T) {
	input := `2024-01-15 transaction with virtual postings
    expenses:food           $50
    assets:cash            $-50
    [budget:food]          $-50
    [budget:available]      $50
    (tracking:note)`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	tx := journal.Transactions[0]
	require.Len(t, tx.Postings, 5)

	assert.Equal(t, ast.VirtualNone, tx.Postings[0].Virtual)
	assert.Equal(t, "expenses:food", tx.Postings[0].Account.Name)

	assert.Equal(t, ast.VirtualNone, tx.Postings[1].Virtual)
	assert.Equal(t, "assets:cash", tx.Postings[1].Account.Name)

	assert.Equal(t, ast.VirtualBalanced, tx.Postings[2].Virtual)
	assert.Equal(t, "budget:food", tx.Postings[2].Account.Name)

	assert.Equal(t, ast.VirtualBalanced, tx.Postings[3].Virtual)
	assert.Equal(t, "budget:available", tx.Postings[3].Account.Name)

	assert.Equal(t, ast.VirtualUnbalanced, tx.Postings[4].Virtual)
	assert.Equal(t, "tracking:note", tx.Postings[4].Account.Name)
}

func TestParser_VirtualPostingWithAmount(t *testing.T) {
	input := `2024-01-15 test
    (opening:balance)  $1000
    assets:cash`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	p := journal.Transactions[0].Postings[0]
	assert.Equal(t, ast.VirtualUnbalanced, p.Virtual)
	assert.Equal(t, "opening:balance", p.Account.Name)
	require.NotNil(t, p.Amount)
	assert.True(t, p.Amount.Quantity.Equal(decimal.NewFromInt(1000)))
}

func TestParser_TagsInTransactionComment(t *testing.T) {
	input := `2024-01-15 Business dinner  ; client:acme, project:alpha
    expenses:meals  $50
    assets:cash`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	tx := journal.Transactions[0]
	require.Len(t, tx.Comments, 1)
	require.Len(t, tx.Comments[0].Tags, 2)

	assert.Equal(t, "client", tx.Comments[0].Tags[0].Name)
	assert.Equal(t, "acme", tx.Comments[0].Tags[0].Value)

	assert.Equal(t, "project", tx.Comments[0].Tags[1].Name)
	assert.Equal(t, "alpha", tx.Comments[0].Tags[1].Value)
}

func TestParser_TagWithoutValue(t *testing.T) {
	input := `2024-01-15 test  ; billable:
    expenses:meals  $50
    assets:cash`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	tx := journal.Transactions[0]
	require.Len(t, tx.Comments, 1)
	require.Len(t, tx.Comments[0].Tags, 1)

	assert.Equal(t, "billable", tx.Comments[0].Tags[0].Name)
	assert.Equal(t, "", tx.Comments[0].Tags[0].Value)
}

func TestParser_TagsInPostingComment(t *testing.T) {
	input := `2024-01-15 test
    expenses:meals  $50  ; date:2024-01-16, receipt:123
    assets:cash`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	p := journal.Transactions[0].Postings[0]
	require.Len(t, p.Tags, 2)

	assert.Equal(t, "date", p.Tags[0].Name)
	assert.Equal(t, "2024-01-16", p.Tags[0].Value)

	assert.Equal(t, "receipt", p.Tags[1].Name)
	assert.Equal(t, "123", p.Tags[1].Value)
}

func TestParser_YearDirective(t *testing.T) {
	tests := []struct {
		name  string
		input string
		year  int
	}{
		{
			name:  "Y directive",
			input: "Y2026",
			year:  2026,
		},
		{
			name:  "Y with space",
			input: "Y 2026",
			year:  2026,
		},
		{
			name:  "year directive",
			input: "year 2025",
			year:  2025,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			journal, errs := Parse(tt.input)
			require.Empty(t, errs)
			require.Len(t, journal.Directives, 1)

			dir, ok := journal.Directives[0].(ast.YearDirective)
			require.True(t, ok)
			assert.Equal(t, tt.year, dir.Year)
		})
	}
}

func TestParser_PartialDate(t *testing.T) {
	input := `Y2026
01-02 Магазин
    Расходы:Продукты  100 RUB
    Активы:Банк`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Directives, 1)
	require.Len(t, journal.Transactions, 1)

	tx := journal.Transactions[0]
	assert.Equal(t, 2026, tx.Date.Year)
	assert.Equal(t, 1, tx.Date.Month)
	assert.Equal(t, 2, tx.Date.Day)
	assert.Equal(t, "Магазин", tx.Description)
}

func TestParser_PartialDateWithoutYear(t *testing.T) {
	input := `01-02 test
    e:f  $1
    a:c`

	_, errs := Parse(input)
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Message, "partial date requires Y directive")
}

func TestParser_UnicodeAccountDirective(t *testing.T) {
	input := `account Активы:Банк`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Directives, 1)

	dir, ok := journal.Directives[0].(ast.AccountDirective)
	require.True(t, ok)
	assert.Equal(t, "Активы:Банк", dir.Account.Name)
}

func TestParser_UnicodeTransaction(t *testing.T) {
	input := `2024-01-15 Покупка продуктов
    Расходы:Продукты  100 RUB
    Активы:Наличные`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	tx := journal.Transactions[0]
	assert.Equal(t, "Покупка продуктов", tx.Description)
	assert.Equal(t, "Расходы:Продукты", tx.Postings[0].Account.Name)
	assert.Equal(t, "Активы:Наличные", tx.Postings[1].Account.Name)
}

func TestParser_CommodityDirectiveWithFormat(t *testing.T) {
	input := `commodity RUB
  format 1.000,00 RUB`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Directives, 1)

	dir, ok := journal.Directives[0].(ast.CommodityDirective)
	require.True(t, ok)
	assert.Equal(t, "RUB", dir.Commodity.Symbol)
	assert.Equal(t, "1.000,00 RUB", dir.Format)
}

func TestParser_CommodityDirectiveMultipleSubdirs(t *testing.T) {
	input := `commodity EUR
  format 1.000,00 EUR
  note European currency`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Directives, 1)

	dir, ok := journal.Directives[0].(ast.CommodityDirective)
	require.True(t, ok)
	assert.Equal(t, "EUR", dir.Commodity.Symbol)
	assert.Equal(t, "1.000,00 EUR", dir.Format)
	assert.Equal(t, "European currency", dir.Note)
}

func TestParser_AccountDirectiveWithComment(t *testing.T) {
	input := `account Активы  ; type:Asset`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Directives, 1)

	dir, ok := journal.Directives[0].(ast.AccountDirective)
	require.True(t, ok)
	assert.Equal(t, "Активы", dir.Account.Name)
	assert.Contains(t, dir.Comment, "type:Asset")
	require.Len(t, dir.Tags, 1)
	assert.Equal(t, "type", dir.Tags[0].Name)
	assert.Equal(t, "Asset", dir.Tags[0].Value)
}

func TestParser_AccountDirectiveWithSubdirs(t *testing.T) {
	input := `account expenses:food
  alias food
  note Food and groceries`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Directives, 1)

	dir, ok := journal.Directives[0].(ast.AccountDirective)
	require.True(t, ok)
	assert.Equal(t, "expenses:food", dir.Account.Name)
	assert.Equal(t, "food", dir.Subdirs["alias"])
	assert.Equal(t, "Food and groceries", dir.Subdirs["note"])
}

func TestParser_SignBeforeCommodity(t *testing.T) {
	input := `2024-01-15 test
    assets:cash  -$100
    expenses:food`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	p := journal.Transactions[0].Postings[0]
	require.NotNil(t, p.Amount)
	assert.True(t, p.Amount.Quantity.Equal(decimal.NewFromInt(-100)))
	assert.Equal(t, "$", p.Amount.Commodity.Symbol)
}

func TestParser_SpaceGroupedNumber(t *testing.T) {
	input := `2024-01-15 test
    assets:cash  3 037 850,96 RUB
    expenses:food`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	p := journal.Transactions[0].Postings[0]
	require.NotNil(t, p.Amount)
	expected, _ := decimal.NewFromString("3037850.96")
	assert.True(t, p.Amount.Quantity.Equal(expected), "got %s", p.Amount.Quantity.String())
	assert.Equal(t, "RUB", p.Amount.Commodity.Symbol)
}

func TestParser_ScientificNotation(t *testing.T) {
	input := `2024-01-15 test
    assets:cash  1E3 USD
    expenses:food`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	p := journal.Transactions[0].Postings[0]
	require.NotNil(t, p.Amount)
	expected := decimal.NewFromInt(1000)
	assert.True(t, p.Amount.Quantity.Equal(expected), "got %s", p.Amount.Quantity.String())
}

func TestParser_PositiveSignBeforeCommodity(t *testing.T) {
	input := `2024-01-15 test
    assets:cash  +$100
    expenses:food`

	journal, errs := Parse(input)
	require.Empty(t, errs)
	require.Len(t, journal.Transactions, 1)

	p := journal.Transactions[0].Postings[0]
	require.NotNil(t, p.Amount)
	assert.True(t, p.Amount.Quantity.Equal(decimal.NewFromInt(100)), "got %s", p.Amount.Quantity.String())
	assert.Equal(t, "$", p.Amount.Commodity.Symbol)
}

func TestParser_EuropeanNumberFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "european with dot grouping",
			input: `2024-01-15 test
    assets:cash  1.234.567,89 EUR
    expenses:food`,
			expected: "1234567.89",
		},
		{
			name: "us with comma grouping",
			input: `2024-01-15 test
    assets:cash  1,234,567.89 USD
    expenses:food`,
			expected: "1234567.89",
		},
		{
			name: "multiple dots as grouping",
			input: `2024-01-15 test
    assets:cash  1.234.567 EUR
    expenses:food`,
			expected: "1234567",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			journal, errs := Parse(tt.input)
			require.Empty(t, errs)
			require.Len(t, journal.Transactions, 1)

			p := journal.Transactions[0].Postings[0]
			require.NotNil(t, p.Amount)
			expected, _ := decimal.NewFromString(tt.expected)
			assert.True(t, p.Amount.Quantity.Equal(expected), "got %s, want %s", p.Amount.Quantity.String(), tt.expected)
		})
	}
}

func TestParser_HledgerNumberFormats(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "dots as grouping 1.2.3 equals 123",
			input: `2024-01-15 test
    assets:cash  1.2.3 EUR
    expenses:food`,
			expected: "123",
		},
		{
			name: "mixed format 1.2,3 equals 12.3",
			input: `2024-01-15 test
    assets:cash  1.2,3 EUR
    expenses:food`,
			expected: "12.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			journal, errs := Parse(tt.input)
			require.Empty(t, errs)
			require.Len(t, journal.Transactions, 1)

			p := journal.Transactions[0].Postings[0]
			require.NotNil(t, p.Amount)
			expected, _ := decimal.NewFromString(tt.expected)
			assert.True(t, p.Amount.Quantity.Equal(expected), "got %s, want %s", p.Amount.Quantity.String(), tt.expected)
		})
	}
}
