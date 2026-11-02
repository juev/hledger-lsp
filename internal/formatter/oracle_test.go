package formatter

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/juev/hledger-lsp/internal/testutil"
)

// TestOracle_FormattingKeepsHledgerValues formats every number spelling under
// every directive style, and asks hledger to read the journal before and after.
// Whatever the formatter does to a number's style, hledger must still see the
// same quantity, and formatting a second time must change nothing.
func TestOracle_FormattingKeepsHledgerValues(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	for _, c := range testutil.NumberCases() {
		t.Run(fmt.Sprintf("%q formats %q", c.Directive, c.Spelling), func(t *testing.T) {
			input := c.Directive + "2024-01-01 x\n    a  " + c.Spelling + "\n    b\n"
			before := testutil.HledgerRead(t, hledgerPath, input)
			if !before.Accepted {
				t.Skip("hledger rejects this journal")
			}

			once := formatForOracle(t, input)
			assert.Equal(t, before, testutil.HledgerRead(t, hledgerPath, once), "formatted journal:\n%s", once)
			assert.Equal(t, once, formatForOracle(t, once), "format(format(x)) must equal format(x)")
		})
	}
}

// A style declared after an amount does not change how that amount is read, but
// the formatter still writes the amount in that style.
func TestOracle_FormattingKeepsValuesAboveTheDirective(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	for _, c := range testutil.NumberCases() {
		t.Run(fmt.Sprintf("%q after %q", c.Directive, c.Spelling), func(t *testing.T) {
			input := "2024-01-01 x\n    a  " + c.Spelling + "\n    b\n\n" + c.Directive
			before := testutil.HledgerRead(t, hledgerPath, input)
			if !before.Accepted {
				t.Skip("hledger rejects this journal")
			}

			journal, errs := parser.Parse(input)
			require.Empty(t, errs)
			if len(journal.Transactions) != 1 {
				t.Skip("the c.Directive adds its own transaction")
			}
			once := applyEdits(input, FormatDocument(journal, input))
			assert.Equal(t, before, testutil.HledgerRead(t, hledgerPath, once), "formatted journal:\n%s", once)
		})
	}
}

func formatForOracle(t *testing.T, input string) string {
	t.Helper()
	journal, errs := parser.Parse(input)
	require.Empty(t, errs)
	return applyEdits(input, FormatDocument(journal, input))
}
