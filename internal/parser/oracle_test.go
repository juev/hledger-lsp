package parser

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/juev/hledger-lsp/internal/testutil"
)

func ourRead(input string) testutil.HledgerAmount {
	journal, errs := Parse(input)
	if len(errs) > 0 || journal == nil || len(journal.Transactions) == 0 {
		return testutil.HledgerAmount{}
	}
	last := journal.Transactions[len(journal.Transactions)-1]
	amount := last.Postings[0].Amount
	if amount == nil {
		return testutil.HledgerAmount{}
	}
	return testutil.HledgerAmount{Accepted: true, Quantity: amount.Quantity.String(), Commodity: amount.Commodity.Symbol}
}

// TestOracle_NumberReadingMatchesHledger compares how hledger and the parser read
// every number spelling under every directive style.
func TestOracle_NumberReadingMatchesHledger(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	for _, c := range testutil.NumberCases() {
		t.Run(fmt.Sprintf("%q reads %q", c.Directive, c.Spelling), func(t *testing.T) {
			input := c.Directive + "2024-01-01 x\n    a  " + c.Spelling + "\n    b\n"
			assert.Equal(t, testutil.HledgerRead(t, hledgerPath, input), ourRead(input), "journal:\n%s", input)
		})
	}
}

// commodityDirectiveJournals are commodity directives whose acceptance depends
// on the subdirectives below them.
var commodityDirectiveJournals = []string{
	"commodity X\n    format 1000.00 Y\n",
	"commodity X\n    format 1000.00 X\n",
	"commodity X\n    format X 1000.00\n",
	"commodity X\n    format 1000.00\n",
	"commodity X\n    format 1000.00 \"X\"\n",
	"commodity X\n    format 1000.00 X Y\n",
	"commodity X\n    format\n",
	"commodity X\n    format 1000.00 X\n    format 1,000.00 X\n",
	"commodity X\n    format 1000.00 X\n    format 1000.00 Y\n",
	"commodity X\n    format 1000.00 Y\n    format 1000.00 X\n",
	"commodity X\n    note a\n    format 1000.00 Y\n",
	"commodity X\n    note a\n    format 1000.00 X\n",
	"commodity X\n    format 1000.00 X\n    note a\n",
	"commodity X\n    default\n    format 1000.00 Y\n",
	"commodity X\n    ; comment\n    format 1000.00 Y\n",
	"commodity X\n    format 1000.00 Y ; comment\n",
	"commodity X ; comment\n    format 1000.00 Y\n",
	"commodity X\n    format 1,000.00 Y\n",
	"commodity X\n    format 1 000.00 Y\n",
	"commodity X\n    format Y 1000.00\n",
	"commodity X\n    format $1000.00\n",
	"commodity $\n    format $1000.00\n",
	"commodity $\n    format 1000.00 EUR\n",
	"commodity $\n    format -$1000.00\n",
	"commodity \"A B\"\n    format 1000.00 \"A B\"\n",
	"commodity \"A B\"\n    format 1000.00 \"A C\"\n",
	"commodity \"A B\"\n    format 1000.00 A\n",
	"commodity \"X Y\"\n    format \"X Y\" 1000.00\n",
	"commodity X\n    format \"X Y\" 1000.00\n",
	"commodity 1000.00 X\n    format 1000.00 X\n",
	"commodity 1000.00 X\n    format 1000.00 Y\n",
	"commodity 1000.00 X\n    note hello\n",
	"commodity 1000.00 X\n    default\n",
	"commodity 1000.00 X\n    alias x\n",
	"commodity 1000.00 X\n    type\n",
	"commodity 1000.00 X\n    format\n",
	"commodity 1000.00 X\n    note a\n    note b\n",
	"commodity 1000.00 X\n    format 1000.00 Y\n    format 1000.00 X\n",
	"commodity 1000.00 X\n  format 1000.00 X\n",
	"commodity 1000.00 X ; comment\n    format 1000.00 X\n",
	"commodity 1000.00 X\n    ; comment\n",
	"commodity 1000.00 X\n\n    note hi\n",
	"commodity X\n\n    format 1000.00 Y\n",
	"commodity 1000.00\n    note hi\n",
	"commodity 1000.00\n    format 1000.00 X\n",
	"commodity X 1000.00\n    format 1000.00 X\n",
	"commodity X 1000.00\n    format 1000.00 Y\n",
	"commodity $1000.00\n    format 1000.00 $\n",
	"commodity 1000.00 X\ncommodity Y\n    format 1000.00 Y\n",
	"commodity 1000.00 X\ncommodity 1000.00 Y\n    note a\n",
	"commodity Y\n    format 1000.00 Y\ncommodity X\n    format 1000.00 Y\n",
	"commodity руб\n    format 1000.00 руб\n",
	"commodity руб\n    format 1000.00 рубл\n",
	"commodity 元\n    format 1000.00 元\n",
	"commodity 元\n    format 1000.00 圆\n",
	"commodity 1000.00 元\n    note 备注\n",
}

// TestOracle_CommodityDirectiveSubdirectivesMatchHledger compares which commodity
// directives hledger accepts with the errors the parser reports for them.
func TestOracle_CommodityDirectiveSubdirectivesMatchHledger(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	for _, directive := range commodityDirectiveJournals {
		for _, newline := range []string{"\n", "\r\n"} {
			t.Run(fmt.Sprintf("%q with %q", directive, newline), func(t *testing.T) {
				input := directive + "2024-01-01 x\n    a  1 X\n    b\n"
				hledgerInput := strings.ReplaceAll(input, "\n", newline)
				// The server normalises line endings before the parser sees a document.
				ours := ourRead(strings.ReplaceAll(hledgerInput, "\r\n", "\n"))
				assert.Equal(t, testutil.HledgerRead(t, hledgerPath, hledgerInput), ours, "journal:\n%s", input)
			})
		}
	}
}
