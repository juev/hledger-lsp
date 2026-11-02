package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pinDefaultYear makes a date without a year read in the given year for the rest
// of the test, as if the clock said so.
func pinDefaultYear(t *testing.T, year int) {
	t.Helper()
	previous := defaultYearForPartialDates
	defaultYearForPartialDates = func() int { return year }
	t.Cleanup(func() { defaultYearForPartialDates = previous })
}

// TestParser_PeriodicTransactionKeepsTheYearInForce checks the year a periodic
// transaction reads its dates without a year in: hledger 1.52.4 takes the Y
// directive in force where the transaction is written, else the current year.
func TestParser_PeriodicTransactionKeepsTheYearInForce(t *testing.T) {
	pinDefaultYear(t, 2025)

	for name, tc := range map[string]struct {
		input string
		want  int
	}{
		"Y 2023":                 {"Y 2023\n~ monthly\n    расход  5 USD  ; [02-29]\n    капитал\n", 2023},
		"Y 2024":                 {"Y 2024\n~ monthly\n    расход  5 USD  ; [02-29]\n    капитал\n", 2024},
		"no Y":                   {"~ monthly\n    a  5 USD\n    b\n", 2025},
		"Y written after it":     {"~ monthly\n    a  5 USD\n    b\n\nY 2024\n", 2025},
		"the latest Y before it": {"Y 2023\nY 2024\n~ monthly\n    a  5 USD\n    b\n", 2024},
	} {
		t.Run(name, func(t *testing.T) {
			journal, errs := Parse(tc.input)
			require.Empty(t, errs)
			require.Len(t, journal.PeriodicTransactions, 1)
			assert.Equal(t, tc.want, journal.PeriodicTransactions[0].Year)
		})
	}
}

func TestParser_PeriodicTransactionFollowsTheClockWithoutY(t *testing.T) {
	for _, year := range []int{2023, 2024} {
		pinDefaultYear(t, year)
		journal, errs := Parse("~ monthly\n    a  5 USD\n    b\n")
		require.Empty(t, errs)
		require.Len(t, journal.PeriodicTransactions, 1)
		assert.Equal(t, year, journal.PeriodicTransactions[0].Year)
	}
}

func TestParseWithContext_PeriodicTransactionInheritsTheIncludingYear(t *testing.T) {
	pinDefaultYear(t, 2025)

	for _, year := range []string{"2023", "2024"} {
		var site Context
		_, errs := ParseWithContext("Y "+year+"\ninclude child.journal\n", Context{}, func(s IncludeSite) ContextExports {
			site = s.Context
			return ContextExports{}
		})
		require.Empty(t, errs)

		child, childErrs := ParseWithContext("~ monthly\n    a  5 USD  ; [02-29]\n    b\n", site, nil)
		require.Empty(t, childErrs)
		require.Len(t, child.Journal.PeriodicTransactions, 1)
		want := 2023
		if year == "2024" {
			want = 2024
		}
		assert.Equal(t, want, child.Journal.PeriodicTransactions[0].Year)
	}
}
