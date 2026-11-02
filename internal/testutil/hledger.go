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
