package analyzer

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/parser"
)

// TestOracle_JournalVerdictsMatchHledger compares the server's verdict with the
// installed hledger binary for the journals the table tests encode. It is opt-in
// because CI images may not ship hledger:
//
//	HLEDGER_ORACLE=1 go test ./internal/analyzer -run Oracle
func TestOracle_JournalVerdictsMatchHledger(t *testing.T) {
	if os.Getenv("HLEDGER_ORACLE") == "" {
		t.Skip("set HLEDGER_ORACLE=1 to compare against the installed hledger binary")
	}

	hledgerPath, err := exec.LookPath("hledger")
	if err != nil {
		t.Skip("hledger is not installed")
	}

	for _, tc := range hledgerVerifiedCases() {
		t.Run(tc.name, func(t *testing.T) {
			accepted := hledgerAccepts(t, hledgerPath, tc.input)

			journal, errs := parser.Parse(tc.input)
			require.Empty(t, errs)
			require.Len(t, journal.Transactions, 1)

			result := CheckBalance(&journal.Transactions[0], decimal.Zero)

			assert.Equal(t, accepted, result.Balanced,
				"our verdict must match hledger for:\n%s", tc.input)
		})
	}
}

func TestOracle_AssertionVerdictsMatchHledger(t *testing.T) {
	if os.Getenv("HLEDGER_ORACLE") == "" {
		t.Skip("set HLEDGER_ORACLE=1 to compare against the installed hledger binary")
	}

	hledgerPath, err := exec.LookPath("hledger")
	if err != nil {
		t.Skip("hledger is not installed")
	}

	cases := map[string]string{
		"failing assertion": `2024-01-01 x
    assets:cash  $-100 = $50
    expenses:food  $100
`,
		"passing assertion": `2024-01-01 x
    assets:cash  $100 = $100
    equity
`,
		"assertion-only posting": `2024-01-01 first
    b:bb  $50
    a:aa

2024-01-02 second
    b:bb  = $50
    a:aa  $0
`,
	}

	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			accepted := hledgerAccepts(t, hledgerPath, input)

			journal, errs := parser.Parse(input)
			require.Empty(t, errs)

			resolved := &include.ResolvedJournal{Primary: journal}
			diagnostics := CheckJournalBalance(resolved, decimal.Zero)

			assert.Equal(t, accepted, len(diagnostics) == 0,
				"our assertion verdict must match hledger for:\n%s\n got %v", input, diagnostics)
		})
	}
}

// hledgerAccepts reports whether the installed hledger loads the journal.
func hledgerAccepts(t *testing.T, hledgerPath, journal string) bool {
	t.Helper()

	cmd := exec.Command(hledgerPath, "-f", "-", "print")
	cmd.Stdin = strings.NewReader(journal)
	cmd.Stdout = &strings.Builder{}
	cmd.Stderr = &strings.Builder{}

	return cmd.Run() == nil
}
