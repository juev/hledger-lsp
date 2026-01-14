package analyzer

import (
	"fmt"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/parser"
)

func mustDecimal(s string) decimal.Decimal {
	d, _ := decimal.NewFromString(s)
	return d
}

func generateJournal(numTransactions int) string {
	var sb strings.Builder

	accounts := []string{
		"expenses:food:groceries",
		"expenses:food:restaurants",
		"expenses:transport:fuel",
		"expenses:utilities:electricity",
		"expenses:utilities:water",
		"assets:bank:checking",
		"assets:bank:savings",
		"assets:cash",
		"liabilities:credit:visa",
		"income:salary",
	}

	commodities := []string{"$", "EUR", "RUB"}

	for i := range numTransactions {
		year := 2020 + (i / 365)
		month := (i/30)%12 + 1
		day := i%28 + 1

		fromAcc := accounts[i%len(accounts)]
		toAcc := accounts[(i+1)%len(accounts)]
		commodity := commodities[i%len(commodities)]
		amount := (i%1000 + 1) * 10

		fmt.Fprintf(&sb, "%04d-%02d-%02d * Payee %d | Transaction note\n", year, month, day, i)
		fmt.Fprintf(&sb, "    %s  %s%d.%02d\n", fromAcc, commodity, amount/100, amount%100)

		if i%5 == 0 {
			fmt.Fprintf(&sb, "    %s  %s%d.%02d @ $1.10\n", toAcc, commodity, amount/100, amount%100)
			sb.WriteString("    assets:cash\n")
		} else {
			fmt.Fprintf(&sb, "    %s\n", toAcc)
		}

		if i%10 == 0 {
			fmt.Fprintf(&sb, "    ; tag:value%d\n", i)
		}

		sb.WriteString("\n")
	}

	return sb.String()
}

var (
	smallJournal, _  = parser.Parse(generateJournal(10))
	mediumJournal, _ = parser.Parse(generateJournal(100))
	largeJournal, _  = parser.Parse(generateJournal(1000))
)

func BenchmarkAnalyze_Small(b *testing.B) {
	analyzer := New()
	for b.Loop() {
		analyzer.Analyze(smallJournal)
	}
}

func BenchmarkAnalyze_Medium(b *testing.B) {
	analyzer := New()
	for b.Loop() {
		analyzer.Analyze(mediumJournal)
	}
}

func BenchmarkAnalyze_Large(b *testing.B) {
	analyzer := New()
	for b.Loop() {
		analyzer.Analyze(largeJournal)
	}
}

func BenchmarkCheckBalance(b *testing.B) {
	tx := &ast.Transaction{
		Postings: []ast.Posting{
			{
				Account: ast.Account{Name: "expenses:food"},
				Amount:  &ast.Amount{Quantity: mustDecimal("50.00"), Commodity: ast.Commodity{Symbol: "$"}},
			},
			{
				Account: ast.Account{Name: "assets:bank"},
				Amount:  &ast.Amount{Quantity: mustDecimal("-50.00"), Commodity: ast.Commodity{Symbol: "$"}},
			},
		},
	}

	for b.Loop() {
		CheckBalance(tx)
	}
}

func BenchmarkCheckBalance_MultiCommodity(b *testing.B) {
	tx := &ast.Transaction{
		Postings: []ast.Posting{
			{
				Account: ast.Account{Name: "assets:stocks"},
				Amount:  &ast.Amount{Quantity: mustDecimal("10"), Commodity: ast.Commodity{Symbol: "AAPL"}},
				Cost:    &ast.Cost{Amount: ast.Amount{Quantity: mustDecimal("150"), Commodity: ast.Commodity{Symbol: "$"}}},
			},
			{
				Account: ast.Account{Name: "assets:bank"},
				Amount:  &ast.Amount{Quantity: mustDecimal("-1500"), Commodity: ast.Commodity{Symbol: "$"}},
			},
		},
	}

	for b.Loop() {
		CheckBalance(tx)
	}
}

func BenchmarkCollectAccounts(b *testing.B) {
	for b.Loop() {
		CollectAccounts(largeJournal)
	}
}

func BenchmarkCollectPayees(b *testing.B) {
	for b.Loop() {
		CollectPayees(largeJournal)
	}
}

func BenchmarkCollectCommodities(b *testing.B) {
	for b.Loop() {
		CollectCommodities(largeJournal)
	}
}

func BenchmarkCollectTags(b *testing.B) {
	for b.Loop() {
		CollectTags(largeJournal)
	}
}
