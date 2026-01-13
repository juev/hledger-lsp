package analyzer

import (
	"testing"

	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
    assets:cash  $-50`

	journal, errs := parser.Parse(input)
	require.Empty(t, errs)

	a := New()
	result := a.Analyze(journal)

	var foundUndeclared bool
	for _, d := range result.Diagnostics {
		if d.Code == "UNDECLARED_ACCOUNT" {
			foundUndeclared = true
			assert.Contains(t, d.Message, "assets:cash")
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
