package formatter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

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

	once := formatForOracle(t, input)
	assert.Equal(t, want, once)
	assert.Equal(t, once, formatForOracle(t, once), "formatting must be idempotent")

	crlf := strings.ReplaceAll(input, "\n", "\r\n")
	assert.Equal(t, once, formatForOracle(t, textutil.NormalizeLineEndings(crlf)))
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

	once := formatForOracle(t, input)
	assert.Equal(t, want, once)
	assert.Equal(t, once, formatForOracle(t, once), "formatting must be idempotent")

	crlf := strings.ReplaceAll(input, "\n", "\r\n")
	assert.Equal(t, once, formatForOracle(t, textutil.NormalizeLineEndings(crlf)))
}
