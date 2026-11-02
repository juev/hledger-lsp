package testutil

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// NumberDirectives are the directive styles that can decide how an ambiguous
// number is read. Each is placed before the transaction under test.
var NumberDirectives = []string{
	"",
	"commodity 1000.000 X\n",
	"commodity 1,000.000 X\n",
	"commodity 0,000.000 X\n",
	"commodity 1000. X\n",
	"commodity 1000, X\n",
	"commodity 1,000 X\n",
	"commodity 1.000 X\n",
	"commodity 1,000. X\n",
	"commodity 1.000, X\n",
	"commodity 1,000,000 X\n",
	"commodity 1000,00 X\n",
	"commodity 1.000,00 X\n",
	"commodity 1 000.00 X\n",
	"commodity 1 000,00 X\n",
	"commodity 1000 X\n",
	"commodity 1 000 X\n",
	"commodity X 1000.00\n",
	"commodity X 1000,00\n",
	"commodity X\n",
	"commodity 1000.00\n",
	"commodity 1000,00\n",
	"commodity 1000\n",
	"commodity X\n  format 1000.000 X\n",
	"commodity X\n  format 1000,00 X\n",
	"commodity X\n  format 1,000.00 X\n",
	"commodity X\n  format 1000 X\n",
	"decimal-mark .\n",
	"decimal-mark ,\n",
	"decimal-mark ,\ncommodity 1000.00 X\n",
	"decimal-mark ,\ncommodity 1,000.00 X\n",
	"commodity 1000.00 X\ndecimal-mark ,\n",
	"commodity 1000.00 X\ncommodity 1000,00 X\n",
	"commodity 1000.00 X\ncommodity 1.000,00 X\n",
	"commodity 1000.00 X\ncommodity X\n",
	"D 1000.00 X\n",
	"D 1,000.00 X\n",
	"D 1000,00 X\n",
	"D 1.000,00 X\n",
	"D 1000. X\n",
	"D 1000, X\n",
	"D 1000 X\n",
	"D 1,000 X\n",
	"D 1.000 X\n",
	"D 1000.00 Y\n",
	"D 1000,00 Y\n",
	"D 1000.00\n",
	"D 1000,00\n",
	"D 1000.00 X\ncommodity 1000,00 X\n",
	"D 1000.00 Y\ncommodity 1000,00 X\n",
	"commodity 1000,00 X\nD 1000.00 X\n",
	"D 1000,00 Y\ndecimal-mark .\n",
	"decimal-mark ,\nD 1000.00 X\n",
	"2020-01-01 earlier\n    p  1,000.00 X\n    q\n\n",
	"2020-01-01 earlier\n    p  1.000,00 X\n    q\n\n",
}

// NumberSpellings are the ways a number can be written in a posting.
var NumberSpellings = []string{
	"15,000 X",
	"1.500 X",
	"15000 X",
	"1,5 X",
	"1.5 X",
	"15, X",
	"15. X",
	"1,234.50 X",
	"1.234,50 X",
	"1,234,567 X",
	"1.234.567 X",
	"1,234,567.89 X",
	"1 500 X",
	"1 500,50 X",
	"X 15,000",
	"X 1.500",
	"15,000",
	"1.500",
	"1,5",

	// hledger refuses an exponent next to a digit group separator
	"1E3 X",
	"1,5E3 X",
	"1.5E3 X",
	"1.5e3 X",
	"1,500E3 X",
	"1.500E3 X",
	"1,234,567E3 X",
	"1.5E3",
	"1,5E3",
	"1.5E-3 X",
	"1.E3 X",
}

// AmountSyntaxDirectives are the styles that matter for how a sign or a leading
// decimal mark is read. The list is short because AmountSyntaxSpellings is long.
var AmountSyntaxDirectives = []string{
	"",
	"decimal-mark ,\n",
	"commodity 1.000,00 X\n",
	"commodity 1,000.00 X\n",
	"commodity 1000.00 X\n",
	"D 1.000,00 X\n",
}

// AmountSyntaxSpellings are amounts that differ from NumberSpellings in the way
// they are punctuated rather than in the digit groups they use.
var AmountSyntaxSpellings = []string{
	// a sign before a prefix commodity
	"-X 1,500",
	"-X 1.500",
	"- X 5",
	"-X5",
	"+X 5",
	"-X -5",
	"X - 5",
	"-$5",
	"- $5",
	"-$ 1,5",
	"$-5",
	`-"A B" 5`,
	`-"A B"5`,
	"-X 1 500",
	"-X 1,5E3",
	"-X 1.5E3",
	"--X 5",
	"-X +5",
	"+X -5",
	"- -5",
	"-руб 5",
	"-元 5",
	`-"元 B" 5`,
	"-€ .5",

	// a sign, a prefix commodity and a second sign, spaces allowed between them
	"-X - 5",
	"-X - 1,500",
	"-X - 1.500",
	"- X - 5",
	"-X  -  5",
	"-X -  5",
	"+X - 5",
	"-X + 5",
	"+X + 5",
	"-X - .5",
	"-X - 1 500",
	"-X - 1,5E3",
	"-X-5",
	"-X+5",
	"-X --5",
	"-X - - 5",
	"X - - 5",
	"X - 5",
	"X+ 5",
	"$ - 5",
	"-$ - 5",
	"-$ - 1,5",
	"- $ - 5",
	`-"A B" - 5`,
	`-"A B"-5`,
	"-€ - .5",
	"-руб - 5",
	"-元 - 5",
	"- 5 X",
	"- - 5 X",

	// a number that starts with the decimal mark
	".5 X",
	"-.5 X",
	"+.5 X",
	"$.5",
	"$ .5",
	"X .5",
	"X -.5",
	"-X .5",
	".5",
	"-.5",
	".5E2 X",
	".500 X",
	".5 руб",
	".5 元",
	".5.5 X",
	".5,5 X",
	",5 X",
	". X",
	".E2 X",
	"1.5 .5",

	// commodity symbols that hold punctuation: hledger reads every character
	// except digits, spaces and -+.@*;"{}= as part of an unquoted symbol
	",5",
	",,5",
	", 5",
	"$,5",
	"$, 5",
	"€,5",
	"EUR,5",
	"EUR, 5",
	"a,b 5",
	"AU$5",
	"AU$ 5",
	"!5",
	"! 5",
	"%5",
	"#5",
	"~5",
	"?5",
	"/5",
	"(5",
	")5",
	"[5",
	"]5",
	"|5",
	"<5",
	"x:y 5",
	"-!5",
	"- ! 5",
	"-,5",
	"-, 5",
	"-,, 5",
	"+,5",
	"$ -,5",
	"-$ ,5",
	"$ - ,5",
	"$ ,5",
	"$ ,500",
	"$ ,5E2",
	"EUR ,5",
	"-EUR ,5",
	`"A B" ,5`,
	", ,5",
	"$ ,5,5",
	"$ ,5.5",
	"$ , 5",
	"$ ,",
	",5 EUR",
	"5 EUR ,5",
	"5 ,",
	"5 ,,",
	"5 ,x",
	"5 x,",
	"5 EUR,",
	"1,5 ,",
	"1,500 ,",
	"1.500 ,",
	"1 500 ,",
	"5, ,",
	"5 !",
	"5 %",
	"5 #",
	"5 ~",
	"5 ?",
	"5 /",
	"5 )",
	"5 ]",
	"5 |",
	"5 <",
	"5 >",
	"5 (x)",
	"5 [x]",
	"5 x|y",
	"5 x:y",
	"5 a,b",
	"5 EUR!",
	"5 AU$",
	"5 $,",
	"5 ,$",
	"5 $%",
	"5 x*",
	"5 x=",
	"-5 ,",
	"- 5 ,",
	"1E3 ,",
	"-! 5",
	"-%5",
	"- % 5",
	"5 ×",
	"5 °",
	"5 ‰",
	"5 «»",
	"5 😀",
	"5 🇷🇺",
	"5 руб,",
	"5 元,",
	"5 €,",
	"5 ₽!",
	"×5",
	"°5",
	"‰ 5",
	"«» 5",
	"😀5",
	"😀 5",
	"руб,5",
	"元,5",
	"€,, 5",
	"₽! 5",
	"5 ٣",
	"٣5",
	"5 ５",
	"５5",
}

// NumberCase is one journal prefix and one amount written after it.
type NumberCase struct {
	Directive string
	Spelling  string
}

// NumberCases lists every case the oracle suites compare with hledger.
func NumberCases() []NumberCase {
	var cases []NumberCase
	for _, directive := range NumberDirectives {
		for _, spelling := range NumberSpellings {
			cases = append(cases, NumberCase{directive, spelling})
		}
	}
	for _, directive := range AmountSyntaxDirectives {
		for _, spelling := range AmountSyntaxSpellings {
			cases = append(cases, NumberCase{directive, spelling})
		}
	}
	return cases
}

// HledgerAmount is what a journal's first posting of its last transaction reads
// as. The zero value means hledger rejected the journal.
type HledgerAmount struct {
	Accepted  bool
	Quantity  string
	Commodity string
}

// RequireHledger returns the installed hledger binary, skipping the test unless
// the oracle is enabled:
//
//	nix shell nixpkgs#hledger -c env HLEDGER_ORACLE=1 go test ./... -run Oracle
func RequireHledger(t *testing.T) string {
	t.Helper()
	if os.Getenv("HLEDGER_ORACLE") == "" {
		t.Skip("set HLEDGER_ORACLE=1 to compare against the installed hledger binary")
	}
	path, err := exec.LookPath("hledger")
	if err != nil {
		t.Skip("hledger is not installed")
	}
	return path
}

// HledgerRead returns how hledger reads the journal on stdin.
func HledgerRead(t *testing.T, hledgerPath, input string) HledgerAmount {
	t.Helper()
	cmd := exec.Command(hledgerPath, "-f", "-", "print", "-O", "json")
	cmd.Stdin = strings.NewReader(input)
	return decodeHledgerAmount(t, cmd)
}

// HledgerReadFile returns how hledger reads the journal at path, with its includes.
func HledgerReadFile(t *testing.T, hledgerPath, path string) HledgerAmount {
	t.Helper()
	return decodeHledgerAmount(t, exec.Command(hledgerPath, "-f", path, "print", "-O", "json"))
}

func decodeHledgerAmount(t *testing.T, cmd *exec.Cmd) HledgerAmount {
	t.Helper()
	var stdout strings.Builder
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return HledgerAmount{}
	}

	var transactions []struct {
		Postings []struct {
			Amount []struct {
				Commodity string `json:"acommodity"`
				Quantity  struct {
					Mantissa json.Number `json:"decimalMantissa"`
					Places   int32       `json:"decimalPlaces"`
				} `json:"aquantity"`
			} `json:"pamount"`
		} `json:"tpostings"`
	}
	decoder := json.NewDecoder(strings.NewReader(stdout.String()))
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&transactions))
	require.NotEmpty(t, transactions)

	amounts := transactions[len(transactions)-1].Postings[0].Amount
	require.Len(t, amounts, 1)
	mantissa, err := decimal.NewFromString(amounts[0].Quantity.Mantissa.String())
	require.NoError(t, err)
	return HledgerAmount{
		Accepted:  true,
		Quantity:  mantissa.Shift(-amounts[0].Quantity.Places).String(),
		Commodity: amounts[0].Commodity,
	}
}

// HledgerPostingAmounts returns every posting amount of the journal at path as
// "<quantity> <commodity>", in the order hledger prints them. Unlike
// HledgerReadFile it covers all transactions, so a file included several times
// is compared at every occurrence. ok is false when hledger rejects the journal.
func HledgerPostingAmounts(t *testing.T, hledgerPath, path string) (amounts []string, ok bool) {
	t.Helper()
	cmd := exec.Command(hledgerPath, "-f", path, "print", "-O", "json")
	var stdout strings.Builder
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil, false
	}

	var transactions []struct {
		Postings []struct {
			Amount []struct {
				Commodity string `json:"acommodity"`
				Quantity  struct {
					Mantissa json.Number `json:"decimalMantissa"`
					Places   int32       `json:"decimalPlaces"`
				} `json:"aquantity"`
			} `json:"pamount"`
		} `json:"tpostings"`
	}
	decoder := json.NewDecoder(strings.NewReader(stdout.String()))
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&transactions))

	for _, transaction := range transactions {
		for _, posting := range transaction.Postings {
			for _, amount := range posting.Amount {
				mantissa, err := decimal.NewFromString(amount.Quantity.Mantissa.String())
				require.NoError(t, err)
				amounts = append(amounts, mantissa.Shift(-amount.Quantity.Places).String()+" "+amount.Commodity)
			}
		}
	}
	return amounts, true
}

// HledgerPostingDetails returns how hledger reads every posting of the journal
// on stdin: amounts with their costs and lot annotations, and balance
// assertions, with display styles removed. Two journals that give equal details
// mean the same to hledger. ok is false when hledger rejects the journal.
func HledgerPostingDetails(t *testing.T, hledgerPath, input string) (details string, ok bool) {
	t.Helper()
	cmd := exec.Command(hledgerPath, "-f", "-", "print", "-O", "json")
	cmd.Stdin = strings.NewReader(input)
	var stdout strings.Builder
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", false
	}

	var transactions []struct {
		Postings []map[string]any `json:"tpostings"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout.String()), &transactions))

	var kept []map[string]any
	for _, transaction := range transactions {
		for _, posting := range transaction.Postings {
			kept = append(kept, map[string]any{
				"amount":    stripStyles(posting["pamount"]),
				"assertion": stripStyles(posting["pbalanceassertion"]),
			})
		}
	}
	encoded, err := json.Marshal(kept)
	require.NoError(t, err)
	return string(encoded), true
}

func stripStyles(v any) any {
	switch value := v.(type) {
	case map[string]any:
		cleaned := make(map[string]any, len(value))
		for k, inner := range value {
			if k == "astyle" {
				continue
			}
			cleaned[k] = stripStyles(inner)
		}
		return cleaned
	case []any:
		cleaned := make([]any, len(value))
		for i, inner := range value {
			cleaned[i] = stripStyles(inner)
		}
		return cleaned
	}
	return v
}
