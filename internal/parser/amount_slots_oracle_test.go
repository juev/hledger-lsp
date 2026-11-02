package parser

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/testutil"
)

type hledgerSlotAmount struct {
	Commodity string `json:"acommodity"`
	Quantity  struct {
		Mantissa json.Number `json:"decimalMantissa"`
		Places   int32       `json:"decimalPlaces"`
	} `json:"aquantity"`
}

func (a hledgerSlotAmount) read(t *testing.T) testutil.HledgerAmount {
	t.Helper()
	mantissa, err := decimal.NewFromString(a.Quantity.Mantissa.String())
	require.NoError(t, err)
	return testutil.HledgerAmount{Accepted: true, Quantity: mantissa.Shift(-a.Quantity.Places).String(), Commodity: a.Commodity}
}

// hledgerSlotRead returns the amount hledger reads in the slot a context puts
// it in. The zero value means hledger rejects the journal.
func hledgerSlotRead(t *testing.T, hledgerPath, context, input string) testutil.HledgerAmount {
	t.Helper()
	if context == "price directive" {
		return hledgerPriceRead(t, hledgerPath, input)
	}

	cmd := exec.Command(hledgerPath, "-f", "-", "print", "-O", "json")
	cmd.Stdin = strings.NewReader(input)
	var stdout strings.Builder
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return testutil.HledgerAmount{}
	}

	var transactions []struct {
		Postings []struct {
			Amount []struct {
				hledgerSlotAmount
				Cost *struct {
					Contents hledgerSlotAmount `json:"contents"`
				} `json:"acost"`
			} `json:"pamount"`
			Assertion *struct {
				Amount hledgerSlotAmount `json:"baamount"`
			} `json:"pbalanceassertion"`
		} `json:"tpostings"`
	}
	decoder := json.NewDecoder(strings.NewReader(stdout.String()))
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&transactions))

	posting := transactions[0].Postings[0]
	switch context {
	case "posting":
		return posting.Amount[0].read(t)
	case "unit cost", "total cost":
		require.NotNil(t, posting.Amount[0].Cost)
		return posting.Amount[0].Cost.Contents.read(t)
	default:
		require.NotNil(t, posting.Assertion)
		return posting.Assertion.Amount.read(t)
	}
}

// hledgerPriceRead reads the single P directive of the journal through
// `hledger prices`, which prints it back as "P date commodity amount".
func hledgerPriceRead(t *testing.T, hledgerPath, input string) testutil.HledgerAmount {
	t.Helper()
	cmd := exec.Command(hledgerPath, "-f", "-", "prices")
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		return testutil.HledgerAmount{}
	}
	fields := strings.Fields(string(out))
	require.GreaterOrEqual(t, len(fields), 4, "prices output: %q", out)

	price := strings.Join(fields[3:], " ")
	number := priceNumber.FindString(price)
	require.NotEmpty(t, number, "prices output: %q", out)
	commodity := strings.Trim(strings.TrimSpace(strings.Replace(price, number, "", 1)), `"`)
	quantity, err := decimal.NewFromString(strings.ReplaceAll(number, " ", ""))
	require.NoError(t, err, "prices output: %q", out)
	return testutil.HledgerAmount{Accepted: true, Quantity: quantity.String(), Commodity: commodity}
}

var priceNumber = regexp.MustCompile(`-?(?:[0-9]+(?: [0-9]+)*(?:\.[0-9]+)?|\.[0-9]+)`)

// TestOracle_SignedAmountSlotsMatchHledger compares how hledger and the parser
// read a sign, a prefix commodity and a second sign in every slot that holds an
// amount.
func TestOracle_SignedAmountSlotsMatchHledger(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	amounts := []string{
		"-EUR - 1.5", "-X - 5", "- EUR - 5", "-EUR -5", "-EUR  -  5", "+EUR - 5",
		"-EUR + 5", "+EUR + 5", "EUR - 5", "EUR -5", "-EUR 5", "- EUR 5", "EUR 5",
		"EUR5", "-EUR - .5", "-EUR - 1 500", `-"A B" - 5`, `-"A B" 5`, "-$ - 5",
		"$ - 5", "- $ - 5", "- $ 5", "-руб - 5", "руб -5", "руб 5", "-元 - 5",
		"-EUR --5", "-EUR - - 5", "EUR - - 5", "- - 5", "-EUR - EUR 5", "-EUR -",
		"-5 EUR", "- 5 EUR", "5 EUR", "-.5 EUR", "- .5 EUR", "-X-5", "-X+5",
	}
	for context, template := range signedAmountContexts {
		for _, amount := range amounts {
			t.Run(context+"/"+amount, func(t *testing.T) {
				input := fmt.Sprintf(template, amount)
				want := hledgerSlotRead(t, hledgerPath, context, input)

				got := testutil.HledgerAmount{}
				if journal, errs := Parse(input); len(errs) == 0 {
					got = oursInSlot(t, context, journal)
				}
				assert.Equal(t, want, got, "journal:\n%s", input)
			})
		}
	}
}

func oursInSlot(t *testing.T, context string, journal *ast.Journal) testutil.HledgerAmount {
	t.Helper()
	var amount ast.Amount
	switch context {
	case "posting":
		amount = *journal.Transactions[0].Postings[0].Amount
	case "unit cost", "total cost":
		amount = journal.Transactions[0].Postings[0].Cost.Amount
	case "balance assertion":
		amount = journal.Transactions[0].Postings[0].BalanceAssertion.Amount
	default:
		amount = journal.Directives[0].(ast.PriceDirective).Price
	}
	return testutil.HledgerAmount{Accepted: true, Quantity: amount.Quantity.String(), Commodity: amount.Commodity.Symbol}
}

// leadingCommaNumber matches an amount whose number starts with a decimal comma
// after a prefix commodity.
var leadingCommaNumber = regexp.MustCompile(`^-?(?:\$|€|EUR|"[^"]*"|, ?) ?-? ?,[0-9]`)

// TestOracle_CommoditySymbolSlotsMatchHledger compares how hledger and the
// parser read symbols that hold punctuation in every slot that holds an amount.
func TestOracle_CommoditySymbolSlotsMatchHledger(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	var amounts []string
	for _, tt := range punctuatedSymbolAmounts {
		amounts = append(amounts, tt.amount)
	}
	amounts = append(amounts,
		"$ -,5", "-$ ,5", "-EUR ,5", "-EUR -\t5", "-EUR\t- 5", "-EUR\t5", "EUR -\t5",
		"-AU$ - 5", "-AU$ 5", "-AU$5", "$,", "5 $,x", "$ , 5", "$ ,5,5", "5 ,5",
		"-$,5", "- $,5", "-€,5", "€ - ,5",
	)
	for context, template := range signedAmountContexts {
		for _, amount := range amounts {
			if context == "price directive" && leadingCommaNumber.MatchString(amount) {
				// hledger prints a leading-comma price back with a comma mark, so
				// its text cannot be read the way the other slots are.
				continue
			}
			t.Run(context+"/"+amount, func(t *testing.T) {
				input := fmt.Sprintf(template, amount)
				want := hledgerSlotRead(t, hledgerPath, context, input)

				got := testutil.HledgerAmount{}
				if journal, errs := Parse(input); len(errs) == 0 {
					got = oursInSlot(t, context, journal)
				}
				assert.Equal(t, want, got, "journal:\n%s", input)
			})
		}
	}
}

// TestOracle_PricedCommodityMatchesHledger compares the commodity a P directive
// prices, which hledger may refuse or read as a longer symbol than a letter run.
func TestOracle_PricedCommodityMatchesHledger(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	lines := []string{
		"P 2024-01-01 , 5 USD", "P 2024-01-01 %% 5 USD", "P 2024-01-01 A! 5 USD", "P 2024-01-01 A:B 5 USD",
		"P 2024-01-01 A :5", "P 2024-01-01 € 5 USD", "P 2024-01-01 руб, 5 USD", "P 2024-01-01 元! 5 USD",
		"P 2024-01-01 $AB 5", "P 2024-01-01 €EUR 1.5", "P 2024-01-01 $X 1.5", "P 2024-01-01 AU$ 5",
		"P 2024-01-01 руб€ 5", "P 2024-01-01 09:30 $AB 5", `P 2024-01-01 "A B" 5`,
		"P 2024-01-01 A1 EUR 5", "P 2024-01-01 A.B EUR 5", "P 2024-01-01 A-B EUR 5", "P 2024-01-01 A+ $-5",
		"P 2024-01-01 A+ $ 5", "P 2024-01-01 A5 5", `P 2024-01-01 "A B"5`, "P 2024-01-01 A@ 5", "P 2024-01-01 A= 5",
		"P 2024-01-01 A\t5", "P 2024-01-01\tA 5", "P 2024-01-01 A 5 EUR junk",
	}
	for _, line := range lines {
		t.Run(line, func(t *testing.T) {
			input := line + "\n"
			want := hledgerPriceRead(t, hledgerPath, input)
			got := testutil.HledgerAmount{}
			if journal, errs := Parse(input); len(errs) == 0 {
				price := journal.Directives[0].(ast.PriceDirective)
				got = testutil.HledgerAmount{Accepted: true, Quantity: price.Price.Quantity.String(), Commodity: price.Price.Commodity.Symbol}
			}
			assert.Equal(t, want.Accepted, got.Accepted, "journal:\n%s", input)
		})
	}
}
