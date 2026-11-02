package formatter

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/juev/hledger-lsp/internal/testutil"
	"github.com/juev/hledger-lsp/internal/textutil"
)

// costSymbolAmounts write a left-side symbol directly against the digits of a
// cost or lot price after an amount, which is where the symbol and the number
// used to be read as one commodity.
var costSymbolAmounts = []string{
	`EUR -675.64 {円9571.57}`,
	`EUR10 @ EUR4581.56`,
	`EUR10 @@ EUR4581.56`,
	`EUR10 {EUR4581.56}`,
	`EUR10 {{руб4581.56}}`,
	`EUR10 {=円4581.56}`,
	`EUR10 @ 円9571.57`,
	`EUR10 @ EUR-4581.56`,
	`EUR10 @ -EUR4581.56`,
	`EUR10 @ EUR4,581.56`,
	`EUR10 @ EUR4581`,
	`10 EUR @ EUR4581.56`,
	`10 EUR {EUR4581.56} @ 円9571.57`,
	`руб10 @ руб4581.56`,
	`漢字10 {漢字4581.56}`,
	`$10 @ $4581.56`,
	`EUR 10 @ EUR 4581.56`,
}

var costSymbolDirectives = []string{
	"",
	"commodity 1,000.00 EUR\n",
	"commodity 円1.00\n",
	"commodity EUR1.00\n",
	"D 1,000.00 EUR\n",
	"commodity 1,000.00 EUR\ncommodity 円1.00\nD 1,000.00 EUR\n",
}

type hledgerCostReading struct {
	Accepted bool
	Postings string
}

func readCosts(t *testing.T, hledgerPath, input string) hledgerCostReading {
	t.Helper()
	cmd := exec.Command(hledgerPath, "-f", "-", "print", "-O", "json")
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		return hledgerCostReading{}
	}
	var transactions []struct {
		Postings []struct {
			Amount []map[string]any `json:"pamount"`
		} `json:"tpostings"`
	}
	require.NoError(t, json.Unmarshal(out, &transactions))
	var kept []any
	for _, transaction := range transactions {
		for _, posting := range transaction.Postings {
			kept = append(kept, stripDisplayStyles(posting.Amount))
		}
	}
	encoded, err := json.Marshal(kept)
	require.NoError(t, err)
	return hledgerCostReading{Accepted: true, Postings: string(encoded)}
}

func stripDisplayStyles(v any) any {
	switch value := v.(type) {
	case map[string]any:
		if mantissa, ok := value["decimalMantissa"]; ok {
			places, _ := value["decimalPlaces"].(float64)
			m, _ := mantissa.(float64)
			return decimal.NewFromFloat(m).Shift(-int32(places)).String()
		}
		cleaned := make(map[string]any, len(value))
		for k, inner := range value {
			if k == "astyle" || k == "floatingPoint" {
				continue
			}
			cleaned[k] = stripDisplayStyles(inner)
		}
		return cleaned
	case []map[string]any:
		cleaned := make([]any, len(value))
		for i, inner := range value {
			cleaned[i] = stripDisplayStyles(inner)
		}
		return cleaned
	case []any:
		cleaned := make([]any, len(value))
		for i, inner := range value {
			cleaned[i] = stripDisplayStyles(inner)
		}
		return cleaned
	}
	return v
}

func costSymbolJournal(directive, amount string) string {
	return directive + "2024-01-01 x\n    a  " + amount + "\n    b\n"
}

func TestParser_CostSymbolIsNotGluedToItsDigits(t *testing.T) {
	cases := map[string]struct {
		symbol   string
		quantity string
	}{
		`EUR10 @ EUR4581.56`:      {"EUR", "4581.56"},
		`EUR -675.64 {円9571.57}`:  {"円", "9571.57"},
		`EUR10 {{руб4581.56}}`:    {"руб", "4581.56"},
		`EUR10 @@ 漢字4581.56`:      {"漢字", "4581.56"},
		`EUR10 {EUR4581.56} @ 円9`: {"EUR", "4581.56"},
	}
	for amount, want := range cases {
		t.Run(amount, func(t *testing.T) {
			journal, errs := parser.Parse(costSymbolJournal("", amount))
			require.Empty(t, errs)
			posting := journal.Transactions[0].Postings[0]
			cost := posting.LotPrice
			if cost != nil && cost.Cost != nil {
				assert.Equal(t, want.symbol, cost.Cost.Commodity.Symbol)
				assert.Equal(t, want.quantity, cost.Cost.Quantity.String())
				return
			}
			require.NotNil(t, posting.Cost)
			assert.Equal(t, want.symbol, posting.Cost.Amount.Commodity.Symbol)
			assert.Equal(t, want.quantity, posting.Cost.Amount.Quantity.String())
		})
	}
}

func TestFormatDocument_CostWithGluedSymbolKeepsItsValue(t *testing.T) {
	for _, directive := range costSymbolDirectives {
		for _, amount := range costSymbolAmounts {
			input := costSymbolJournal(directive, amount)
			once := formatForOracle(t, input)
			assert.Equal(t, once, formatForOracle(t, once), "input %q", input)
			assert.NotContains(t, once, " .", "a symbol must not be split from its digits: %q", once)

			crlf := strings.ReplaceAll(input, "\n", "\r\n")
			assert.Equal(t, once, formatForOracle(t, textutil.NormalizeLineEndings(crlf)))
		}
	}
}

func TestOracle_CostWithGluedSymbolKeepsItsValue(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	for _, directive := range costSymbolDirectives {
		for _, amount := range costSymbolAmounts {
			input := costSymbolJournal(directive, amount)
			before := readCosts(t, hledgerPath, input)
			if !before.Accepted {
				continue
			}
			once := formatForOracle(t, input)
			twice := formatForOracle(t, once)
			assert.Equal(t, before, readCosts(t, hledgerPath, once), "after one pass: %q -> %q", input, once)
			assert.Equal(t, before, readCosts(t, hledgerPath, twice), "after two passes: %q -> %q", input, twice)
		}
	}
}
