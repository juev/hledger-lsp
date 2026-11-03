package analyzer

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/textutil"
)

// orderDependentJournal makes an inclusive assignment take off two subaccounts
// that hold one cost key, written 1.00010 on first and 1.0001 on second. hledger
// 1.52.4 sums the subaccounts in the order of an internal hash table and keeps
// the literal of the one it meets first, so the 0.00002 EUR left over is within
// the tolerance of 1.0001 and outside that of 1.00010 depending on the names.
func orderDependentJournal(first, second string) string {
	return "2024-01-01 p\n    " + first + "  0.1 USD @ 1.00010 EUR\n    " + second + "  0.1 USD @ 1.0001 EUR\n    y  = 0 GBP\n    e\n\n" +
		"2024-01-02 q\n    x  =* 0 USD\n    w  0.2 USD @ 1.0000 EUR\n"
}

func TestCheckJournalBalance_OrderDependentToleranceIsAWarning(t *testing.T) {
	for _, names := range [][2]string{{"x:a", "x:b"}, {"x:b", "x:a"}, {"x:касса", "x:現金"}} {
		for _, newline := range []string{"\n", "\r\n"} {
			input := strings.ReplaceAll(orderDependentJournal(names[0], names[1]), "\n", newline)
			resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: textutil.NormalizeLineEndings(input)})

			diagnostics := CheckJournalBalance(resolved, New().balanceTolerance())

			require.Len(t, diagnostics, 1, "%v", diagnostics)
			assert.Equal(t, CodeOrderDependentBalance, diagnostics[0].Code)
			assert.Equal(t, SeverityWarning, diagnostics[0].Severity)
			assert.Equal(t, 7, diagnostics[0].Range.Start.Line, "the transaction q")
		}
	}
}

// The same subaccounts at one precision decide the verdict whatever the order,
// so it stays a definite pass or error.
func TestCheckJournalBalance_OrderIndependentToleranceStaysDefinite(t *testing.T) {
	for literal, want := range map[string]int{"1.0001": 0, "1.00010": 1} {
		input := "2024-01-01 p\n    x:a  0.1 USD @ " + literal + " EUR\n    x:b  0.1 USD @ " + literal + " EUR\n    y  = 0 GBP\n    e\n\n" +
			"2024-01-02 q\n    x  =* 0 USD\n    w  0.2 USD @ 1.0000 EUR\n"
		resolved := resolvedFromSources(t, journalSource{path: "/main.journal", content: input})
		diagnostics := CheckJournalBalance(resolved, New().balanceTolerance())
		require.Len(t, diagnostics, want, "%s: %v", literal, diagnostics)
		if want > 0 {
			assert.Equal(t, CodeUnbalanced, diagnostics[0].Code)
		}
	}
}

// The warning stands for a verdict hledger itself decides by the subaccount
// names: renaming them flips it.
func TestOracle_OrderDependentToleranceFlipsInHledger(t *testing.T) {
	if os.Getenv("HLEDGER_ORACLE") == "" {
		t.Skip("set HLEDGER_ORACLE=1 to compare against the installed hledger binary")
	}
	hledgerPath, err := exec.LookPath("hledger")
	if err != nil {
		t.Skip("hledger is not installed")
	}
	verdicts := map[bool]bool{}
	for _, names := range [][2]string{{"x:a", "x:b"}, {"x:b", "x:a"}, {"x:aa", "x:zz"}, {"x:zz", "x:aa"}} {
		verdicts[hledgerAccepts(t, hledgerPath, orderDependentJournal(names[0], names[1]))] = true
	}
	assert.Len(t, verdicts, 2, "hledger should accept some namings and reject others")
}
