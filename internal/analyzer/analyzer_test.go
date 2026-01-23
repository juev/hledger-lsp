package analyzer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/parser"
)

func TestAnalyzer_EmptyJournal(t *testing.T) {
	journal, _ := parser.Parse("")

	a := New()
	result := a.Analyze(journal)

	assert.NotNil(t, result)
	assert.NotNil(t, result.Accounts)
	assert.Empty(t, result.Accounts.All)
	assert.Empty(t, result.Payees)
	assert.Empty(t, result.Commodities)
	assert.Empty(t, result.Diagnostics)
}

func TestAnalyzer_CollectsAllData(t *testing.T) {
	input := `2024-01-15 Grocery Store
    expenses:food  $50
    assets:cash

2024-01-16 Coffee Shop
    expenses:food  EUR 5
    assets:bank`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	assert.Contains(t, result.Accounts.All, "expenses:food")
	assert.Contains(t, result.Accounts.All, "assets:cash")
	assert.Contains(t, result.Accounts.All, "assets:bank")

	assert.Contains(t, result.Payees, "Grocery Store")
	assert.Contains(t, result.Payees, "Coffee Shop")

	assert.Contains(t, result.Commodities, "$")
	assert.Contains(t, result.Commodities, "EUR")
}

func TestAnalyzer_DiagnosticsForUnbalanced(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $50
    assets:cash  $-40`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	require.Len(t, result.Diagnostics, 1)
	assert.Equal(t, "UNBALANCED", result.Diagnostics[0].Code)
	assert.Equal(t, SeverityError, result.Diagnostics[0].Severity)
	assert.Contains(t, result.Diagnostics[0].Message, "$")
	assert.Contains(t, result.Diagnostics[0].Message, "10")
}

func TestAnalyzer_DiagnosticsForMultipleInferred(t *testing.T) {
	input := `2024-01-15 test
    expenses:food
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	require.Len(t, result.Diagnostics, 1)
	assert.Equal(t, "MULTIPLE_INFERRED", result.Diagnostics[0].Code)
	assert.Equal(t, SeverityError, result.Diagnostics[0].Severity, "MULTIPLE_INFERRED should have Error severity")
}

func TestAnalyzer_NoDiagnosticsForBalanced(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $50
    assets:cash  $-50`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	assert.Empty(t, result.Diagnostics)
}

func TestAnalyzer_NoDiagnosticsForInferred(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	assert.Empty(t, result.Diagnostics)
}

func TestAnalyzer_MultipleTransactions(t *testing.T) {
	input := `2024-01-15 balanced
    expenses:food  $50
    assets:cash  $-50

2024-01-16 unbalanced
    expenses:food  $30
    assets:cash  $-20`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	require.Len(t, result.Diagnostics, 1)
	assert.Equal(t, 5, result.Diagnostics[0].Range.Start.Line)
	assert.Equal(t, SeverityError, result.Diagnostics[0].Severity, "UNBALANCED should have Error severity")
}

func TestAnalyzer_AccountsByPrefix(t *testing.T) {
	input := `2024-01-15 test
    expenses:food:groceries  $30
    expenses:food:restaurant  $20
    assets:cash  $-50`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	assert.Len(t, result.Accounts.ByPrefix["expenses:"], 2)
	assert.Contains(t, result.Accounts.ByPrefix["expenses:food:"], "expenses:food:groceries")
	assert.Contains(t, result.Accounts.ByPrefix["expenses:food:"], "expenses:food:restaurant")
}

func TestAnalyzer_UndeclaredAccount(t *testing.T) {
	input := `account expenses:food

2024-01-15 test
    expenses:food  $50
    custom:wallet  $-50`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	var foundUndeclared bool
	for _, d := range result.Diagnostics {
		if d.Code == "UNDECLARED_ACCOUNT" {
			foundUndeclared = true
			assert.Contains(t, d.Message, "custom:wallet")
			assert.Equal(t, SeverityWarning, d.Severity, "UNDECLARED_ACCOUNT should have Warning severity")
		}
	}
	assert.True(t, foundUndeclared, "expected UNDECLARED_ACCOUNT diagnostic")
}

func TestAnalyzer_DeclaredAccount_NoDiagnostic(t *testing.T) {
	input := `account expenses:food
account assets:cash

2024-01-15 test
    expenses:food  $50
    assets:cash  $-50`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	for _, d := range result.Diagnostics {
		assert.NotEqual(t, "UNDECLARED_ACCOUNT", d.Code)
	}
}

func TestAnalyzer_NoAccountDirectives_NoDiagnostic(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $50
    assets:cash  $-50`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	for _, d := range result.Diagnostics {
		assert.NotEqual(t, "UNDECLARED_ACCOUNT", d.Code)
	}
}

func TestAnalyzer_PredefinedAccounts_NoDiagnostic(t *testing.T) {
	input := `account custom:category

2024-01-15 test
    expenses:food  $50
    assets:cash  $-50
    liabilities:credit  $10
    equity:opening  $5
    revenues:salary  $100`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	for _, d := range result.Diagnostics {
		if d.Code == "UNDECLARED_ACCOUNT" {
			assert.NotContains(t, d.Message, "expenses:", "predefined 'expenses' should not trigger warning")
			assert.NotContains(t, d.Message, "assets:", "predefined 'assets' should not trigger warning")
			assert.NotContains(t, d.Message, "liabilities:", "predefined 'liabilities' should not trigger warning")
			assert.NotContains(t, d.Message, "equity:", "predefined 'equity' should not trigger warning")
			assert.NotContains(t, d.Message, "revenues:", "predefined 'revenues' should not trigger warning")
		}
	}
}

func TestAnalyzer_PredefinedAccounts_CaseInsensitive(t *testing.T) {
	input := `account custom:category

2024-01-15 test
    Expenses:Food  $50
    Assets:Cash  $-50
    LIABILITIES:Credit  $10
    Equity:Opening  $5
    Revenues:Salary  $100`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	for _, d := range result.Diagnostics {
		if d.Code == "UNDECLARED_ACCOUNT" {
			t.Errorf("predefined accounts should be case-insensitive, got warning: %s", d.Message)
		}
	}
}

func TestAnalyzer_CustomAccount_StillRequiresDeclaration(t *testing.T) {
	input := `account expenses:food

2024-01-15 test
    expenses:food  $50
    custom:account  $-50`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	var foundCustom bool
	for _, d := range result.Diagnostics {
		if d.Code == "UNDECLARED_ACCOUNT" && d.Message == "account 'custom:account' is not declared" {
			foundCustom = true
		}
	}
	assert.True(t, foundCustom, "custom:account should still require declaration")
}

func TestAnalyzer_PredefinedAccounts_Income(t *testing.T) {
	input := `account custom:category

2024-01-15 test
    income:salary  $1000
    assets:bank`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	for _, d := range result.Diagnostics {
		if d.Code == "UNDECLARED_ACCOUNT" {
			assert.NotContains(t, d.Message, "income:", "predefined 'income' should not trigger warning")
		}
	}
}

func TestAnalyzer_UndeclaredCommodity_Amount(t *testing.T) {
	input := `commodity USD

2024-01-15 test
    expenses:food  EUR 50
    assets:cash  USD -50`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	var foundUndeclared bool
	for _, d := range result.Diagnostics {
		if d.Code == "UNDECLARED_COMMODITY" && d.Message == "commodity 'EUR' has no directive" {
			foundUndeclared = true
			assert.Equal(t, SeverityWarning, d.Severity, "UNDECLARED_COMMODITY should have Warning severity")
		}
	}
	assert.True(t, foundUndeclared, "expected UNDECLARED_COMMODITY diagnostic for EUR")
}

func TestAnalyzer_UndeclaredCommodity_Cost(t *testing.T) {
	input := `commodity BTC

2024-01-15 buy bitcoin
    assets:crypto  1 BTC @ EUR 45000
    assets:bank`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	var foundUndeclared bool
	for _, d := range result.Diagnostics {
		if d.Code == "UNDECLARED_COMMODITY" && d.Message == "commodity 'EUR' has no directive" {
			foundUndeclared = true
			assert.Equal(t, SeverityWarning, d.Severity, "UNDECLARED_COMMODITY should have Warning severity")
		}
	}
	assert.True(t, foundUndeclared, "expected UNDECLARED_COMMODITY diagnostic for cost commodity EUR")
}

func TestAnalyzer_UndeclaredCommodity_BalanceAssertion(t *testing.T) {
	input := `commodity USD

2024-01-15 test
    expenses:food  USD 50
    assets:cash  USD -50 = EUR 100`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	var foundUndeclared bool
	for _, d := range result.Diagnostics {
		if d.Code == "UNDECLARED_COMMODITY" && d.Message == "commodity 'EUR' has no directive" {
			foundUndeclared = true
			assert.Equal(t, SeverityWarning, d.Severity, "UNDECLARED_COMMODITY should have Warning severity")
		}
	}
	assert.True(t, foundUndeclared, "expected UNDECLARED_COMMODITY diagnostic for balance assertion commodity EUR")
}

func TestAnalyzer_NoCommodityDirectives_NoDiagnostic(t *testing.T) {
	input := `2024-01-15 test
    expenses:food  $50
    assets:cash  $-50`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	for _, d := range result.Diagnostics {
		assert.NotEqual(t, "UNDECLARED_COMMODITY", d.Code)
	}
}

func TestAnalyzer_CommodityDeclaredInlineFormat(t *testing.T) {
	input := `commodity 1.000,00 USD
commodity 1.000,00 EUR

2024-01-15 test
    expenses:food  100.00 USD
    assets:cash  100.00 EUR`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	for _, d := range result.Diagnostics {
		if d.Code == "UNDECLARED_COMMODITY" {
			t.Errorf("unexpected UNDECLARED_COMMODITY diagnostic: %s", d.Message)
		}
	}
}

func TestAnalyzer_AnalyzeWithExternalDeclarations_SuppressesCommodityWarning(t *testing.T) {
	input := `commodity USD

2024-01-15 test
    expenses:food  EUR 50
    assets:cash  USD -50`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	external := ExternalDeclarations{
		Commodities: map[string]bool{"EUR": true},
	}

	a := New()
	result := a.AnalyzeWithExternalDeclarations(journal, external)

	for _, d := range result.Diagnostics {
		if d.Code == "UNDECLARED_COMMODITY" {
			assert.NotContains(t, d.Message, "EUR", "EUR should not trigger warning when in external declarations")
		}
	}
}

func TestAnalyzer_AnalyzeWithExternalDeclarations_SuppressesAccountWarning(t *testing.T) {
	input := `account expenses:food

2024-01-15 test
    expenses:food  $50
    assets:cash  $-50`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	external := ExternalDeclarations{
		Accounts: map[string]bool{"assets:cash": true},
	}

	a := New()
	result := a.AnalyzeWithExternalDeclarations(journal, external)

	for _, d := range result.Diagnostics {
		if d.Code == "UNDECLARED_ACCOUNT" {
			assert.NotContains(t, d.Message, "assets:cash", "assets:cash should not trigger warning when in external declarations")
		}
	}
}

func TestAnalyzer_AnalyzeWithExternalDeclarations_NilMaps(t *testing.T) {
	input := `commodity USD

2024-01-15 test
    expenses:food  EUR 50
    assets:cash  USD -50`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	external := ExternalDeclarations{
		Accounts:    nil,
		Commodities: nil,
	}

	a := New()
	result := a.AnalyzeWithExternalDeclarations(journal, external)

	var foundEUR bool
	for _, d := range result.Diagnostics {
		if d.Code == "UNDECLARED_COMMODITY" && d.Message == "commodity 'EUR' has no directive" {
			foundEUR = true
		}
	}
	assert.True(t, foundEUR, "EUR should trigger warning when external declarations are nil")
}

func TestAnalyzer_AnalyzeWithExternalDeclarations_OverlappingDeclarations(t *testing.T) {
	input := `commodity USD
commodity EUR

2024-01-15 test
    expenses:food  USD 50
    expenses:travel  EUR 30
    assets:bank  RUB 1000
    assets:cash  GBP -20`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	external := ExternalDeclarations{
		Commodities: map[string]bool{
			"EUR": true,
			"RUB": true,
		},
	}

	a := New()
	result := a.AnalyzeWithExternalDeclarations(journal, external)

	var warnings []string
	for _, d := range result.Diagnostics {
		if d.Code == "UNDECLARED_COMMODITY" {
			warnings = append(warnings, d.Message)
		}
	}

	assert.Len(t, warnings, 1, "Expected exactly 1 undeclared commodity warning")
	if len(warnings) == 1 {
		assert.Contains(t, warnings[0], "GBP", "Only GBP should trigger warning")
	}
}

func TestAnalyzer_SubaccountOfDeclaredParent_NoDiagnostic(t *testing.T) {
	input := `account expenses

2024-01-15 test
    expenses:food  $50
    assets:cash  $-50`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	for _, d := range result.Diagnostics {
		if d.Code == "UNDECLARED_ACCOUNT" {
			assert.NotContains(t, d.Message, "expenses:food",
				"subaccount of declared parent should not trigger warning")
		}
	}
}

func TestAnalyzer_DeepSubaccountOfDeclaredParent_NoDiagnostic(t *testing.T) {
	input := `account Расходы

2024-01-15 test
    Расходы:Продукты:Магазин  100 RUB
    Активы:Банк  -100 RUB`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	for _, d := range result.Diagnostics {
		if d.Code == "UNDECLARED_ACCOUNT" {
			assert.NotContains(t, d.Message, "Расходы:Продукты",
				"deep subaccount of declared parent should not trigger warning")
		}
	}
}

func TestAnalyzer_SimilarNameNotSubaccount_Diagnostic(t *testing.T) {
	input := `account expenses

2024-01-15 test
    expenses2:food  $50
    assets:cash  $-50`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	var foundExpenses2 bool
	for _, d := range result.Diagnostics {
		if d.Code == "UNDECLARED_ACCOUNT" && strings.Contains(d.Message, "expenses2:food") {
			foundExpenses2 = true
		}
	}
	assert.True(t, foundExpenses2, "expenses2:food should trigger warning (not a subaccount of expenses)")
}

func TestAnalyzeResolved_PayeeTemplates_Deterministic(t *testing.T) {
	// This test verifies that payee templates from resolved journals
	// are deterministic - using FileOrder instead of map iteration.
	// The last file in FileOrder should win for duplicate payees.

	file1Content := `2024-01-15 Grocery
    expenses:food:groceries  $100
    assets:cash`

	file2Content := `2024-01-16 Grocery
    expenses:food:supermarket  $200
    assets:bank`

	journal1, _ := parser.Parse(file1Content)
	journal2, _ := parser.Parse(file2Content)
	primary, _ := parser.Parse("")

	resolved := &include.ResolvedJournal{
		Primary: primary,
		Files: map[string]*ast.Journal{
			"/path/to/file1.journal": journal1,
			"/path/to/file2.journal": journal2,
		},
		FileOrder: []string{"/path/to/file1.journal", "/path/to/file2.journal"},
	}

	a := New()

	// Run analysis multiple times to verify determinism
	for i := 0; i < 10; i++ {
		result := a.AnalyzeResolved(resolved)

		templates, ok := result.PayeeTemplates["Grocery"]
		require.True(t, ok, "iteration %d: Grocery templates should exist", i)
		require.Len(t, templates, 2, "iteration %d: should have 2 postings", i)

		// file2 is last in FileOrder, so its template should win
		assert.Equal(t, "expenses:food:supermarket", templates[0].Account,
			"iteration %d: first posting should be from file2 (last in FileOrder)", i)
		assert.Equal(t, "assets:bank", templates[1].Account,
			"iteration %d: second posting should be from file2 (last in FileOrder)", i)
	}
}

func TestAnalyzeResolved_PayeeTemplates_PrimaryOverridesIncludes(t *testing.T) {
	// Primary file should always override included files for duplicate payees

	includeContent := `2024-01-15 Grocery
    expenses:food:included  $100
    assets:included`

	primaryContent := `2024-01-16 Grocery
    expenses:food:primary  $200
    assets:primary`

	includeJournal, _ := parser.Parse(includeContent)
	primaryJournal, _ := parser.Parse(primaryContent)

	resolved := &include.ResolvedJournal{
		Primary: primaryJournal,
		Files: map[string]*ast.Journal{
			"/path/to/include.journal": includeJournal,
		},
		FileOrder: []string{"/path/to/include.journal"},
	}

	a := New()
	result := a.AnalyzeResolved(resolved)

	templates, ok := result.PayeeTemplates["Grocery"]
	require.True(t, ok, "Grocery templates should exist")
	require.Len(t, templates, 2, "should have 2 postings")

	// Primary should override included file
	assert.Equal(t, "expenses:food:primary", templates[0].Account,
		"first posting should be from primary file")
	assert.Equal(t, "assets:primary", templates[1].Account,
		"second posting should be from primary file")
}
