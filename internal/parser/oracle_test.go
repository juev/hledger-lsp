package parser

import (
	"fmt"
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
