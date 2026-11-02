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
	"commodity $\n    format -$1,000.00\n",
	"commodity $\n    format $-1,000.00\n",
	"commodity $\n    format +$1,000.00\n",
	"commodity $\n    format - $1,000.00\n",
	"commodity $\n    format + $1,000.00\n",
	"commodity $\n    format $ -1,000.00\n",
	"commodity $\n    format $ - 1,000.00\n",
	"commodity $\n    format $.50\n",
	"commodity $\n    format $ .50\n",
	"commodity $\n    format $1.\n",
	"commodity $\n    format -$.50\n",
	"commodity $\n    format $-.50\n",
	"commodity EUR\n    format -EUR 1000.00\n",
	"commodity EUR\n    format - EUR 1000.00\n",
	"commodity EUR\n    format -EUR1000\n",
	"commodity EUR\n    format EUR -1000.00\n",
	"commodity EUR\n    format EUR - 1000.00\n",
	"commodity EUR\n    format +1000.00 EUR\n",
	"commodity EUR\n    format .50 EUR\n",
	"commodity EUR\n    format EUR .50\n",
	"commodity EUR\n    format EUR.50\n",
	"commodity EUR\n    format 1000. EUR\n",
	"commodity EUR\n    format 1E3 EUR\n",
	"commodity EUR\n    format EUR 1E3\n",
	"commodity EUR\n    format 1.5E3 EUR\n",
	"commodity EUR\n    format 1000.00 EUR ; c\n",
	"commodity EUR\n    format 1000.00EUR\n",
	"commodity EUR\n    format EUR1000.00\n",
	"commodity EUR\n    format -.50 EUR\n",
	"commodity €\n    format -€1.000,00\n",
	"commodity €\n    format - €1.000,00\n",
	"commodity €\n    format €-1.000,00\n",
	"commodity €\n    format € -1.000,00\n",
	"commodity €\n    format -€ 1.000,00\n",
	"commodity €\n    format €.50\n",
	"commodity €\n    format -1.000,00 €\n",
	"commodity €\n    format - 1.000,00 €\n",
	"commodity €\n    format 1.000,00€\n",
	"commodity €\n    format -1.000,00€\n",
	"commodity \"10 Year Bond\"\n    format \"10 Year Bond\" 1000.00\n",
	"commodity \"10 Year Bond\"\n    format 1000.00 \"10 Year Bond\"\n",
	"commodity \"10 Year Bond\"\n    format -\"10 Year Bond\" 1000.00\n",
	"commodity \"10 Year Bond\"\n    format - 1000.00 \"10 Year Bond\"\n",
	"commodity \"AB1\"\n    format \"AB1\"1000.00\n",
	"commodity \"AB1\"\n    format -1000.00 \"AB1\"\n",
	"commodity \"A B\"\n    format \"A B\" -1000.00\n",
	"commodity \"A B\"\n    format -\"A B\"1000.00\n",
	"commodity Fr.\n    format Fr. 1'000.00\n",
	"commodity Fr.\n    format Fr.1000.00\n",
	"commodity Fr.\n    format 1000.00 Fr.\n",
	"commodity Fr\n    format Fr. 1000.00\n",
	"commodity kr\n    format 1 000,00 kr\n",
	"commodity kr\n    format kr 1000,00\n",
	"commodity kr\n    format -1000,00 kr\n",
	"commodity zł\n    format 1 000,00 zł\n",
	"commodity zł\n    format -1 000,00 zł\n",
	"commodity ₹\n    format ₹1,00,000.00\n",
	"commodity ₹\n    format -₹1,00,000.00\n",
	"commodity ₹\n    format ₹ -1,00,000.00\n",
	"commodity US$\n    format -US$1,000.00\n",
	"commodity US$\n    format US$-1,000.00\n",
	"commodity US$\n    format - US$1,000.00\n",
	"commodity US$\n    format US$ -1,000.00\n",
	"commodity US$\n    format -1,000.00 US$\n",
	"commodity US$\n    format US$.50\n",
	"commodity Rp\n    format -Rp1.000,00\n",
	"commodity Rp\n    format Rp-1.000,00\n",
	"commodity Rp\n    format Rp.50\n",
	"commodity €X\n    format -€X1000.00\n",
	"commodity €X\n    format €X-1000.00\n",
	"commodity EUR\n    format 1000.00 EUR EUR\n",
	"commodity EUR\n    format EUR 1000.00 EUR\n",
	"commodity EUR\n    format 1000.00 1000.00 EUR\n",
	"commodity EUR\n    format EUR 1000.00 1000.00\n",
	"commodity EUR\n    format 1000.00 EUR ; x\n",
	"commodity EUR\n    format 1000.00 EUR\t; x\n",
	"commodity X\n    format 1000.00 X  ; c\n",
	"commodity X\n    format X 1000.00 ; c\n",
	"commodity EUR\n    format 1 000,00 EUR\n",
	"commodity EUR\n    format EUR 1000,00\n",
	"commodity EUR\n    format 1000,00 EUR\n",
	"commodity EUR\n    format 1 000,00 EUR\n",
	"commodity EUR\n    format EUR 1000,00\n",
	"commodity EUR\n    format 1000,00 EUR\n",
	"commodity EUR\n    format 1 000,00 EUR\n",
	"commodity EUR\n    format EUR 1000,00\n",
	"commodity EUR\n    format 1000,00 EUR\n",
	"commodity EUR\n    format 1 000,00 EUR\n",
	"commodity EUR\n    format EUR 1000,00\n",
	"commodity EUR\n    format 1000,00 EUR\n",
	"commodity EUR\n    format 1 000,00 EUR\n",
	"commodity EUR\n    format EUR 1000,00\n",
	"commodity EUR\n    format 1000,00 EUR\n",
	"commodity EUR\n    format 1 000,00 EUR\n",
	"commodity EUR\n    format EUR 1000,00\n",
	"commodity EUR\n    format 1000,00 EUR\n",
	"commodity EUR\n    format 1 000,00 EUR\n",
	"commodity EUR\n    format EUR 1000,00\n",
	"commodity EUR\n    format 1000,00 EUR\n",
	"commodity EUR\n    format 1 000,00 EUR\n",
	"commodity EUR\n    format EUR 1000,00\n",
	"commodity EUR\n    format 1000,00 EUR\n",
	"commodity EUR\n    format 1 000,00 EUR\n",
	"commodity EUR\n    format EUR 1000,00\n",
	"commodity EUR\n    format 1000,00 EUR\n",
	"commodity EUR\n    format 1　000,00 EUR\n",
	"commodity EUR\n    format EUR　1000,00\n",
	"commodity EUR\n    format 1000,00　EUR\n",
	"commodity EUR\n    format 1 000,00 EUR\n",
	"commodity EUR\n    format EUR 1000,00\n",
	"commodity EUR\n    format 1000,00 EUR\n",
	"commodity EUR\n    format 1 000,00 EUR\n",
	"commodity EUR\n    format EUR 1000,00\n",
	"commodity EUR\n    format 1000,00 EUR\n",
	"commodity \"\"\n    format -1000.00\n",
	"commodity \"\"\n    format \"\" 1000.00\n",

	"commodity US$\n    format US$1,000.00\n",
	"commodity HK$\n    format HK$1,000.00\n",
	"commodity NZ$\n    format NZ$1,000.00\n",
	"commodity A$\n    format A$1,000.00\n",
	"commodity S$\n    format S$1,000.00\n",
	"commodity C$\n    format C$1,000.00\n",
	"commodity R$\n    format R$1,000.00\n",
	"commodity USD$\n    format USD$1,000.00\n",
	"commodity GB£\n    format GB£1,000.00\n",
	"commodity €X\n    format €X1,000.00\n",
	"commodity X€\n    format X€1,000.00\n",
	"commodity $X\n    format $X1,000.00\n",
	"commodity US$\n    format US$ 1,000.00\n",
	"commodity US$\n    format 1,000.00 US$\n",
	"commodity R$\n    format R$ 1.000,00\n",
	"commodity US$\n    format US 1,000.00\n",
	"commodity US$\n    format $1,000.00\n",
	"commodity US\n    format US$1,000.00\n",
	"commodity \"US$\"\n    format US$1,000.00\n",
	"commodity US$\n    format \"US$\"1,000.00\n",
	"commodity EUR\n    format 1 000,00 EUR\n",
	"commodity EUR\n    format 1\u202f000,00 EUR\n",
	"commodity EUR\n    format 1\u00a0000,00 EUR\n",
	"commodity EUR\n    format 1\u3000000,00 EUR\n",
	"commodity лв\n    format 1 000,00 лв\n",
	"commodity EUR\n    format - 1000.00 EUR\n",
	"commodity EUR\n    format -1000.00 EUR\n",
	"commodity EUR\n    format 1000.00 EUR @ 2 USD\n",
	"commodity \"\"\n    format 1000.00 X\n",
	"commodity \"\"\n    format 1000.00 \"\"\n",
	"commodity \"\"\n    format 1000.00\n",
	"commodity X\n    format 1000.00 \"\"\n",
	"commodity ¢\n    format 1000.00¢\n",
	"commodity Rp\n    format Rp1.000,00\n",
	"commodity Fr\n    format Fr 1'000.00\n",
	"commodity ¥en\n    format ¥en1000\n",

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

// commodityStyleJournals declare a commodity style and then read an amount whose
// meaning depends on it.
var commodityStyleJournals = []string{
	"commodity $1.000,00\ncommodity $.50\n2024-01-02 t\n    a  $1.234\n    c\n",
	"commodity $.50\n2024-01-02 t\n    a  $1.234\n    c\n",
	"commodity $.50\n2024-01-02 t\n    a  $1,234\n    c\n",
	"commodity EUR.50\n2024-01-02 t\n    a  EUR1,234\n    c\n",
	"commodity EUR.50\n2024-01-02 t\n    a  EUR1.234\n    c\n",
	"commodity 1.000,00 EUR\ncommodity EUR.50\n2024-01-02 t\n    a  EUR1.234\n    c\n",
	"commodity -EUR 1.000,00\n2024-01-02 t\n    a  EUR1.234\n    c\n",
	"commodity -EUR1.000,00\n2024-01-02 t\n    a  EUR1.234\n    c\n",
	"commodity -US$1.000,00\n2024-01-02 t\n    a  US$1.234\n    c\n",
	"commodity US$-1.000,00\n2024-01-02 t\n    a  US$1.234\n    c\n",
	"commodity Rp-1.000,00\n2024-01-02 t\n    a  Rp1.234\n    c\n",
	"commodity -Rp1.000,00\n2024-01-02 t\n    a  Rp1.234\n    c\n",
	"commodity €X1.000,00\n2024-01-02 t\n    a  €1.234\n    c\n",
	"commodity 1.000,00 US$\n2024-01-02 t\n    a  1.234 US\n    c\n",
	"commodity US$ 1.000,00\n2024-01-02 t\n    a  1.234 US\n    c\n",
	"commodity 1.000,00 EUR\ncommodity EUR1,000.00\n2024-01-02 t\n    a  EUR1.234\n    c\n",
	"commodity 1,000.00 EUR\ncommodity EUR1.000,00\n2024-01-02 t\n    a  EUR1.234\n    c\n",
	"commodity 1.000,00 US$\ncommodity US$\n2024-01-02 t\n    a  US$1.234\n    c\n",
	"commodity US$1.000,00\ncommodity US$\n2024-01-02 t\n    a  US$1.234\n    c\n",
	"commodity US$1.000,00\ncommodity US\n2024-01-02 t\n    a  US$1.234\n    c\n",
	"commodity US1.000,00\n2024-01-02 t\n    a  US$1.234\n    c\n",
	"commodity US 1.000,00\n2024-01-02 t\n    a  US$1.234\n    c\n",
	"commodity $1.000,00\ncommodity US$\n2024-01-02 t\n    a  $1.234\n    c\n",
	"commodity $1.000,00\ncommodity US$1,000.00\n2024-01-02 t\n    a  $1.234\n    c\n",
	"commodity 1.000,00 $\ncommodity €$1,000.00\n2024-01-02 t\n    a  $1.234\n    c\n",
	"commodity EUR\n    format EUR1.000,00\n2024-01-02 t\n    a  EUR1.234\n    c\n",
	"commodity EUR\n    format 1.000,00EUR\n2024-01-02 t\n    a  EUR1.234\n    c\n",
	"commodity Rp\n    format Rp1.000,00\n2024-01-02 t\n    a  Rp1.234\n    c\n",
	"commodity Rp\n    format Rp 1.000,00\n2024-01-02 t\n    a  Rp1.234\n    c\n",
	"commodity \"A B\"1.000,00\n2024-01-02 t\n    a  \"A B\"1.234\n    c\n",
	"commodity \"A B\" 1.000,00\n2024-01-02 t\n    a  \"A B\"1.234\n    c\n",
	"commodity 1.000,00 \"A B\"\n2024-01-02 t\n    a  \"A B\"1.234\n    c\n",
	"commodity €1.000,00 ; c\n2024-01-02 t\n    a  €1.234\n    c\n",
	"commodity € 1.000,00\n2024-01-02 t\n    a  €1.234\n    c\n",
	"commodity 1.000,00€\n2024-01-02 t\n    a  €1.234\n    c\n",
}

// TestOracle_CommodityStyleDeclarationsReadLikeHledger compares how hledger and
// the parser read an amount after a commodity directive in its different spellings.
func TestOracle_CommodityStyleDeclarationsReadLikeHledger(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	for _, input := range commodityStyleJournals {
		t.Run(fmt.Sprintf("%q", input), func(t *testing.T) {
			assert.Equal(t, testutil.HledgerRead(t, hledgerPath, input), ourRead(input), "journal:\n%s", input)
		})
	}
}
