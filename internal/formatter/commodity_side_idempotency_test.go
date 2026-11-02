package formatter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/juev/hledger-lsp/internal/testutil"
)

// sideSymbols are commodity symbols of different widths and scripts. Display
// width differs from rune count for the CJK ones.
var sideSymbols = []string{"$", "X", "руб", "円", "漢字", `"A B"`}

// sideStyles declare how a symbol is rendered: on the left or the right, with
// or without a space, with different number styles.
func sideStyles(symbol string) []string {
	return []string{
		"",
		"commodity 1.00 " + symbol + "\n",
		"commodity " + symbol + "1.00\n",
		"commodity " + symbol + " 1.00\n",
		"commodity 1,000.00 " + symbol + "\n",
		"commodity 1.000,00 " + symbol + "\n",
		"commodity " + symbol + " 1.000,00\n",
	}
}

// sideAmounts are posting amounts written in every way that can sit on either
// side of the declared style: the symbol on the left or the right, bare numbers
// and another commodity.
func sideAmounts(symbol string) []string {
	return []string{
		symbol + "5", symbol + " 5", "5" + symbol, "5 " + symbol,
		"-" + symbol + "5", symbol + "-5", "1234.5 " + symbol,
		"2", "-2", "1,000.5",
		"3 EUR", "EUR 3",
		"5 " + symbol + " @ 3 EUR",
	}
}

type sideCase struct {
	name  string
	input string
}

// sideCases is the product of symbol, declared style and the amounts of the
// first two postings; a third posting has no amount.
func sideCases() []sideCase {
	var cases []sideCase
	for _, symbol := range sideSymbols {
		amounts := sideAmounts(symbol)
		for _, style := range sideStyles(symbol) {
			for _, first := range amounts {
				for _, second := range amounts {
					input := style + "2024-01-02\n    a  " + first + "\n    b  " + second + "\n    c\n"
					cases = append(cases, sideCase{name: fmt.Sprintf("%q", input), input: input})
				}
			}
		}
	}
	return cases
}

// sideModes are the alignment settings the formatter can run under: the three
// modes, each with and without a fixed column or a minimum column, and with the
// cost annotation as the anchor or trailing.
func sideModes() []Options {
	var all []Options
	for _, mode := range []string{"", "left", "decimal"} {
		for _, column := range []int{0, 30} {
			for _, minColumn := range []int{0, 20} {
				for _, target := range []string{"", AlignTargetPosting} {
					opts := DefaultOptions()
					opts.AmountAlignmentMode = mode
					opts.AmountAlignmentColumn = column
					opts.MinAlignmentColumn = minColumn
					opts.AmountAlignmentTarget = target
					all = append(all, opts)
				}
			}
		}
	}
	return all
}

func formatSideCase(t *testing.T, input string, opts Options) string {
	t.Helper()
	journal, errs := parser.Parse(input)
	require.Empty(t, errs, "input %q", input)
	return applyEdits(input, FormatDocumentWithOptions(journal, input, ExtractCommodityFormats(journal.Directives), opts))
}

// Moving a commodity to the other side must not change how the next pass lays
// the postings out: format(format(x)) == format(x) in every alignment mode.
func TestFormatDocument_CommoditySideChangeIsIdempotent(t *testing.T) {
	var failures []string
	for _, c := range sideCases() {
		for _, opts := range sideModes() {
			once := formatSideCase(t, c.input, opts)
			twice := formatSideCase(t, once, opts)
			if once != twice {
				failures = append(failures, fmt.Sprintf("options %+v input %q\n once  %q\n twice %q", opts, c.input, once, twice))
			}
		}
	}
	if len(failures) > 0 {
		shown := failures
		if len(shown) > 10 {
			shown = shown[:10]
		}
		t.Fatalf("%d of %d journal/mode pairs are not idempotent, first:\n%s", len(failures), len(sideCases())*len(sideModes()), strings.Join(shown, "\n"))
	}
}

// The layout from the report: `$5` rewritten as `5.00 $` beside a bare `2`,
// also with CJK and Cyrillic symbols and with CRLF line endings.
func TestFormatDocument_CommoditySideChangeNextToBareAmount(t *testing.T) {
	for _, symbol := range []string{"$", "円", "руб"} {
		t.Run(symbol, func(t *testing.T) {
			input := "commodity 1.00 " + symbol + "\n2024-01-02\n    a  " + symbol + "5\n    b  2\n    c\n"
			once := formatForOracle(t, input)
			assert.Equal(t, once, formatForOracle(t, once))

			crlf := strings.ReplaceAll(strings.ReplaceAll(input, "\n", "\r\n"), "\r\n", "\n")
			assert.Equal(t, once, formatForOracle(t, crlf))
		})
	}
}

// Formatting changes how an amount is written but never what hledger reads.
func TestOracle_CommoditySideChangeKeepsValues(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)
	dir := t.TempDir()

	read := func(input string) ([]string, bool) {
		path := filepath.Join(dir, "j.journal")
		require.NoError(t, os.WriteFile(path, []byte(input), 0o600))
		return testutil.HledgerPostingAmounts(t, hledgerPath, path)
	}

	for _, c := range sideCases() {
		before, ok := read(c.input)
		if !ok {
			continue
		}
		once := formatSideCase(t, c.input, DefaultOptions())
		after, ok := read(once)
		require.True(t, ok, "formatted journal rejected:\n%s", once)
		require.Equal(t, before, after, "input %q formatted to %q", c.input, once)
	}
}
