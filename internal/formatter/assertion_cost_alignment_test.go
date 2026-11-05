package formatter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/testutil"
	"github.com/juev/hledger-lsp/internal/textutil"
)

// Balance assertions written after amounts whose `=` share a column line up on
// their decimal points within a transaction, the way the amounts before them do.
// An assignment keeps its `=` in the amount column, and each transaction lines up
// on its own.
func TestFormatDocument_LinesUpBalanceAssertions(t *testing.T) {
	input := strings.Join([]string{
		"commodity 1,000.00 USD",
		"commodity 1,000.000 VOO",
		"",
		"2026-06-19 * DAF Contribution",
		"    資産:daf:fidelity  2,060.96 USD = 2,108.30 USD  ; date:2026-06-23",
		"    equity:transfers  -3.000 VOO @@ 2,064.75 USD = -3.000 VOO",
		"    капитал:нереализованное  3.79 USD = -159.29 USD",
		"",
		"2026-08-11 * Fee",
		"    資産:daf:fidelity  -61.56 USD = 1,630.05 USD",
		"    expenses:fees  61.56 USD = 3,123.14 USD",
		"",
		"2026-09-30 * Statement",
		"    資産:daf:fidelity  = 1,107.40 USD",
		"    капитал:нереализованное  22.65 USD = -319.95 USD",
		"",
	}, "\n")
	want := strings.Join([]string{
		"commodity 1,000.00 USD",
		"commodity 1,000.000 VOO",
		"",
		"2026-06-19 * DAF Contribution",
		"    資産:daf:fidelity                      2,060.96 USD = 2,108.30 USD  ; date:2026-06-23",
		"    equity:transfers         -3.000 VOO @@ 2,064.75 USD =    -3.000 VOO",
		"    капитал:нереализованное                    3.79 USD =  -159.29 USD",
		"",
		"2026-08-11 * Fee",
		"    資産:daf:fidelity                        -61.56 USD = 1,630.05 USD",
		"    expenses:fees                             61.56 USD = 3,123.14 USD",
		"",
		"2026-09-30 * Statement",
		"    資産:daf:fidelity                         = 1,107.40 USD",
		"    капитал:нереализованное                   22.65 USD = -319.95 USD",
		"",
	}, "\n")

	assertFormatsTo(t, input, want)
}

// Assertions whose operators start in one column line up on their decimal
// points whichever operator they use, `=`, `==`, `=*` or `==*`; the padding
// after a wider operator is narrower.
func TestFormatDocument_LinesUpMixedAssertionOperators(t *testing.T) {
	input := strings.Join([]string{
		"2026-01-01 * Mixed",
		"    資産:a  1.00 USD = 1.00 USD",
		"    капитал:b  100.00 USD == 100.00 USD",
		"    c  10.00 USD =* 10.00 USD",
		"    d  -111.00 USD ==* -111.00 USD",
		"",
	}, "\n")
	want := strings.Join([]string{
		"2026-01-01 * Mixed",
		"    資産:a        1.00 USD =      1.00 USD",
		"    капитал:b   100.00 USD ==   100.00 USD",
		"    c            10.00 USD =*    10.00 USD",
		"    d          -111.00 USD ==* -111.00 USD",
		"",
	}, "\n")

	assertFormatsTo(t, input, want)
}

// The postings of a transaction that carry a cost line their `@` and `@@` up in
// one column, with the amounts before it lined up on their decimal points and
// the costs after it where the alignment puts them. A wide amount with a narrow
// cost beside a narrow amount with a wide one needs more room than either line
// alone, so the transaction's signs and costs move right together; a
// transaction with one cost posting is laid out as before.
func TestFormatDocument_LinesUpCostSigns(t *testing.T) {
	input := strings.Join([]string{
		"commodity 1,000.00 USD",
		"commodity 1,000.000 VOO",
		"",
		"2026-06-19 * Rebalance",
		"    資産:brokerage  -3.000 VOO @@ 2,064.75 USD",
		"    資産:brokerage  12.5 AAPL @ 150.00 USD",
		"    капитал:перевод  1,000 VOO @@ 3.00 USD",
		"    assets:cash",
		"",
		"2026-06-20 * Single",
		"    資産:brokerage  -3.000 VOO @@ 2,064.75 USD",
		"    assets:cash",
		"",
	}, "\n")
	want := strings.Join([]string{
		"commodity 1,000.00 USD",
		"commodity 1,000.000 VOO",
		"",
		"2026-06-19 * Rebalance",
		"    資産:brokerage      -3.000 VOO @@ 2,064.75 USD",
		"    資産:brokerage      12.5 AAPL  @    150.00 USD",
		"    капитал:перевод  1,000.000 VOO @@     3.00 USD",
		"    assets:cash",
		"",
		"2026-06-20 * Single",
		"    資産:brokerage   -3.000 VOO @@ 2,064.75 USD",
		"    assets:cash",
		"",
	}, "\n")

	assertFormatsTo(t, input, want)
}

// Cost postings that also assert a balance line up their signs and costs, and
// the assertions after them line up on their decimal points.
func TestFormatDocument_LinesUpCostsAndTheirAssertions(t *testing.T) {
	input := strings.Join([]string{
		"commodity 1,000.00 USD",
		"commodity 1,000.000 VOO",
		"",
		"2026-06-19 * Rebalance",
		"    資産:brokerage  -3.000 VOO @@ 2,064.75 USD = 10.000 VOO",
		"    капитал:акции  12.5 AAPL @ 150.00 USD == 112.5 AAPL",
		"    assets:cash",
		"",
	}, "\n")
	want := strings.Join([]string{
		"commodity 1,000.00 USD",
		"commodity 1,000.000 VOO",
		"",
		"2026-06-19 * Rebalance",
		"    資産:brokerage  -3.000 VOO @@ 2,064.75 USD =   10.000 VOO",
		"    капитал:акции   12.5 AAPL  @    150.00 USD == 112.5 AAPL",
		"    assets:cash",
		"",
	}, "\n")

	assertFormatsTo(t, input, want)
}

// A lot price beside plain costs starts its own column after the amounts, and
// the signs move past it.
func TestFormatDocument_LinesUpLotPricesBesideCosts(t *testing.T) {
	input := strings.Join([]string{
		"2026-06-19 * Lot price beside plain costs",
		"    資産:brokerage  -3.000 VOO @@ 2,064.75 USD",
		"    資産:brokerage  12.5 AAPL @ 150.00 USD",
		"    капитал:лот  2 VOO {1.00 USD} @ 2.00 USD",
		"    assets:cash",
		"",
	}, "\n")
	want := strings.Join([]string{
		"2026-06-19 * Lot price beside plain costs",
		"    資産:brokerage  -3.000 VOO            @@ 2,064.75 USD",
		"    資産:brokerage  12.5 AAPL             @    150.00 USD",
		"    капитал:лот      2 VOO     {1.00 USD} @      2.00 USD",
		"    assets:cash",
		"",
	}, "\n")

	assertFormatsTo(t, input, want)
}

// Unit, total and fixed lot prices line up on the decimal points of their
// numbers, with the `{{` and `{=` reaching left of the `{`.
func TestFormatDocument_LinesUpUnitTotalAndFixedLotPrices(t *testing.T) {
	input := strings.Join([]string{
		"2026-06-20 * Unit, total and fixed lot prices",
		"    資産:lots  10 VOO {100.00 USD} @ 120.00 USD",
		"    資産:lots  5 VOO {{550.00 USD}} @@ 600.00 USD",
		"    капитал:лот  -2 VOO {=200.00 USD} @ 130.00 USD",
		"    資産:lots  3 VOO {1.00 USD} @ 2.00 USD",
		"    income:gains",
		"",
	}, "\n")
	want := strings.Join([]string{
		"2026-06-20 * Unit, total and fixed lot prices",
		"    資産:lots    10 VOO  {100.00 USD}  @  120.00 USD",
		"    資産:lots     5 VOO {{550.00 USD}} @@ 600.00 USD",
		"    капитал:лот  -2 VOO {=200.00 USD}  @  130.00 USD",
		"    資産:lots     3 VOO    {1.00 USD}  @    2.00 USD",
		"    income:gains",
		"",
	}, "\n")

	assertFormatsTo(t, input, want)
}

// A lot price on a posting without a cost takes the lot column but does not
// move the signs of the postings with a cost, and leaves no empty cost column.
func TestFormatDocument_LotPriceWithoutCostLeavesTheSignsAlone(t *testing.T) {
	input := strings.Join([]string{
		"2026-06-21 * Lot price without a cost",
		"    資産:brokerage  -3.000 VOO @@ 2,064.75 USD",
		"    капитал:лот  2 VOO {1.00 USD}",
		"    資産:brokerage  12.5 AAPL @ 150.00 USD",
		"    assets:cash",
		"",
	}, "\n")
	want := strings.Join([]string{
		"2026-06-21 * Lot price without a cost",
		"    資産:brokerage  -3.000 VOO @@ 2,064.75 USD",
		"    капитал:лот      2 VOO     {1.00 USD}",
		"    資産:brokerage  12.5 AAPL  @    150.00 USD",
		"    assets:cash",
		"",
	}, "\n")

	assertFormatsTo(t, input, want)
}

// A lot annotation's date and label are part of it, so a long one moves the
// signs and costs of its transaction right.
func TestFormatDocument_LinesUpLongLotAnnotations(t *testing.T) {
	input := strings.Join([]string{
		"2026-06-22 * Long annotation",
		"    資産:brokerage  -3.000 VOO @@ 2,064.75 USD",
		"    資産:brokerage  12.5 AAPL @ 150.00 USD",
		"    капитал:лот  2 VOO {1.00 USD} [2026-01-15] (транш A) @ 2.00 USD",
		"    assets:cash",
		"",
	}, "\n")
	want := strings.Join([]string{
		"2026-06-22 * Long annotation",
		"    資産:brokerage  -3.000 VOO                                   @@ 2,064.75 USD",
		"    資産:brokerage  12.5 AAPL                                    @    150.00 USD",
		"    капитал:лот      2 VOO     {1.00 USD} [2026-01-15] (транш A) @      2.00 USD",
		"    assets:cash",
		"",
	}, "\n")

	assertFormatsTo(t, input, want)
}

// A lot annotation without a number, an empty `{}` or only a date, starts where
// the lot column does.
func TestFormatDocument_LotAnnotationsWithoutANumber(t *testing.T) {
	input := strings.Join([]string{
		"2026-06-23 * Annotations without a number",
		"    資産:lots  2 VOO {} @ 2.00 USD",
		"    資産:lots  3 VOO [2026-01-15] @ 3.00 USD",
		"    капитал:лот  4 VOO {10.00 USD} @ 4.00 USD",
		"    assets:cash",
		"",
	}, "\n")
	want := strings.Join([]string{
		"2026-06-23 * Annotations without a number",
		"    資産:lots    2 VOO {}           @ 2.00 USD",
		"    資産:lots    3 VOO [2026-01-15] @ 3.00 USD",
		"    капитал:лот  4 VOO {10.00 USD}  @ 4.00 USD",
		"    assets:cash",
		"",
	}, "\n")

	assertFormatsTo(t, input, want)
}

// Lining up assertions, costs and lot prices changes only the layout: hledger
// reads the same amounts, costs, lot annotations and assertions before and after
// formatting.
func TestOracle_AssertionCostAndLotAlignmentKeepHledgerMeaning(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	journals := map[string]string{
		"mixed operators": strings.Join([]string{
			"2026-01-01 * Open",
			"    資産:a  1.00 USD",
			"    капитал:b  100.00 USD",
			"    c  10.00 USD",
			"    d  -111.00 USD",
			"",
			"2026-01-02 * Check",
			"    資産:a  0 USD = 1.00 USD",
			"    капитал:b  0 USD == 100.00 USD",
			"    c  0 USD =* 10.00 USD",
			"    d  0 USD ==* -111.00 USD",
			"",
		}, "\n"),
		"costs with assertions": strings.Join([]string{
			"2026-06-19 * Rebalance",
			"    資産:brokerage  -3.000 VOO @@ 2,064.75 USD = -3.000 VOO",
			"    капитал:акции  12.5 AAPL @ 150.00 USD == 12.5 AAPL",
			"    assets:cash",
			"",
		}, "\n"),
		"lot prices": strings.Join([]string{
			"2026-06-19 * Lot price beside plain costs",
			"    資産:brokerage  -3.000 VOO @@ 2,064.75 USD",
			"    資産:brokerage  12.5 AAPL @ 150.00 USD",
			"    капитал:лот  2 VOO {1.00 USD} @ 2.00 USD",
			"    assets:cash",
			"",
			"2026-06-20 * Unit, total and fixed lot prices",
			"    資産:lots  10 VOO {100.00 USD} @ 120.00 USD",
			"    資産:lots  5 VOO {{550.00 USD}} @@ 600.00 USD",
			"    капитал:лот  -2 VOO {=200.00 USD} @ 130.00 USD",
			"    資産:lots  3 VOO {1.00 USD} @ 2.00 USD",
			"    income:gains",
			"",
			"2026-06-21 * Lot price without a cost",
			"    資産:brokerage  -3.000 VOO @@ 2,064.75 USD",
			"    капитал:лот  2 VOO {1.00 USD}",
			"    資産:brokerage  12.5 AAPL @ 150.00 USD",
			"    assets:cash",
			"",
			"2026-06-22 * Long annotation",
			"    資産:brokerage  -3.000 VOO @@ 2,064.75 USD",
			"    資産:brokerage  12.5 AAPL @ 150.00 USD",
			"    капитал:лот  2 VOO {1.00 USD} [2026-01-15] (транш A) @ 2.00 USD",
			"    assets:cash",
			"",
			"2026-06-23 * Annotations without a number",
			"    資産:lots  2 VOO {} @ 2.00 USD",
			"    資産:lots  3 VOO [2026-01-15] @ 3.00 USD",
			"    капитал:лот  4 VOO {10.00 USD} @ 4.00 USD",
			"    assets:cash",
			"",
		}, "\n"),
	}
	for name, input := range journals {
		t.Run(name, func(t *testing.T) {
			before, accepted := testutil.HledgerMeaning(t, hledgerPath, input)
			require.True(t, accepted, "hledger must accept the journal:\n%s", input)

			once := formatForOracle(t, input)
			after, accepted := testutil.HledgerMeaning(t, hledgerPath, once)
			require.True(t, accepted, "hledger must accept the formatted journal:\n%s", once)
			assert.Equal(t, before, after, "formatted journal:\n%s", once)
			assert.Equal(t, once, formatForOracle(t, once), "format(format(x)) must equal format(x)")
		})
	}
}

// assertFormatsTo checks that input formats to want, that formatting it again
// changes nothing, and that its CRLF spelling formats the same.
func assertFormatsTo(t *testing.T, input, want string) {
	t.Helper()
	once := formatForOracle(t, input)
	assert.Equal(t, want, once)
	assert.Equal(t, once, formatForOracle(t, once), "formatting must be idempotent")

	crlf := strings.ReplaceAll(input, "\n", "\r\n")
	assert.Equal(t, once, formatForOracle(t, textutil.NormalizeLineEndings(crlf)))
}
