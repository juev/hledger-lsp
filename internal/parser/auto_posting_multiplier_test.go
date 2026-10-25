package parser

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Auto posting rules accept a `*` multiplier directly before the amount, as in
// hledger 1.52.4 (`postingphelper`, `multiplierp`). Issue #86.
func TestParser_AutoPostingRuleMultiplier(t *testing.T) {
	tests := []struct {
		amount    string
		quantity  string
		commodity string
	}{
		{amount: "*2", quantity: "2"},
		{amount: "*0.25", quantity: "0.25"},
		{amount: "*-1", quantity: "-1"},
		{amount: "*-0.25", quantity: "-0.25"},
		{amount: "*+1", quantity: "1"},
		{amount: "*$2", quantity: "2", commodity: "$"},
		{amount: "*-$2", quantity: "-2", commodity: "$"},
		{amount: "*2 EUR", quantity: "2", commodity: "EUR"},
		{amount: "*2 руб", quantity: "2", commodity: "руб"},
	}
	for _, tt := range tests {
		t.Run(tt.amount, func(t *testing.T) {
			input := "= expenses:groceries\n    (budget:groceries)    " + tt.amount + "\n"
			journal, errs := Parse(input)
			require.Empty(t, errs)
			require.Len(t, journal.AutoPostingRules, 1)
			require.Len(t, journal.AutoPostingRules[0].Postings, 1)

			posting := journal.AutoPostingRules[0].Postings[0]
			assert.True(t, posting.Multiplier)
			require.NotNil(t, posting.Amount)
			assert.Equal(t, tt.quantity, posting.Amount.Quantity.String())
			assert.Equal(t, tt.commodity, posting.Amount.Commodity.Symbol)
		})
	}
}

func TestParser_AutoPostingRuleMultiplierWithCommentAndCyrillicAccount(t *testing.T) {
	input := "= расходы:еда\r\n    (бюджет:еда)    *0.25  ; заметка\r\n"
	journal, errs := Parse(strings.ReplaceAll(input, "\r\n", "\n"))
	require.Empty(t, errs)
	posting := journal.AutoPostingRules[0].Postings[0]
	assert.Equal(t, "бюджет:еда", posting.Account.Name)
	assert.True(t, posting.Multiplier)
	assert.Equal(t, "0.25", posting.Amount.Quantity.String())
	assert.Equal(t, " заметка", posting.Comment)
}

// hledger accepts a bare `*` in an auto posting rule (the generated amount is 0).
func TestParser_AutoPostingRuleBareMultiplier(t *testing.T) {
	journal, errs := Parse("= expenses\n    (budget)    *\n")
	require.Empty(t, errs)
	posting := journal.AutoPostingRules[0].Postings[0]
	assert.True(t, posting.Multiplier)
	assert.Nil(t, posting.Amount)
}

func TestParser_MultiplierRejectedOutsideAutoPostingRules(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "space after star", input: "= expenses\n    (budget)    * -1\n"},
		{name: "regular transaction", input: "2026-10-01 shop\n    expenses  *2\n    assets\n"},
		{name: "periodic transaction", input: "~ monthly\n    (budget:x)    *2\n    assets:cash\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, errs := Parse(tt.input)
			require.NotEmpty(t, errs)
			assert.Contains(t, errs[0].Message, "unexpected content: *")
		})
	}
}
