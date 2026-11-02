package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParser_DefaultCommodityAndCommodityStyleDisagree pins how hledger 1.52.4
// reads a number when a commodity directive and a D directive declare different
// decimal marks. A bare number takes the D directive's mark, and a symbol that
// has its own style takes that style whatever D says. A D number that ends in
// its mark is accepted whatever mark the commodity declares. A symbol-less
// commodity style declared before D wins for bare numbers; one declared after D
// changes nothing. The expected readings come from hledger.
func TestParser_DefaultCommodityAndCommodityStyleDisagree(t *testing.T) {
	tests := []struct {
		directives, amount string
		accepted           bool
		qty, commodity     string
	}{
		{"commodity 1.000,00 X\nD 1000. X\n", "15,000", true, "15000", "X"},
		{"commodity 1.000,00 X\nD 1000. X\n", "1.500", true, "1.5", "X"},
		{"commodity 1.000,00 X\nD 1000. X\n", "15,000 X", true, "15", "X"},
		{"commodity 1.000,00 X\nD 1000. X\n", "1.500 X", true, "1500", "X"},
		{"commodity 1.000,00 X\nD 1000. X\n", "15,000 Y", true, "15000", "Y"},
		{"commodity 1,000.00 X\nD 1000, X\n", "15,000", true, "15", "X"},
		{"commodity 1,000.00 X\nD 1000, X\n", "1.500", true, "1500", "X"},
		{"commodity 1,000.00 X\nD 1000, X\n", "15,000 X", true, "15000", "X"},
		{"commodity 1,000.00 X\nD 1000, X\n", "1.500 X", true, "1.5", "X"},
		{"D 1000. X\ncommodity 1.000,00 X\n", "15,000", true, "15000", "X"},
		{"D 1000. X\ncommodity 1.000,00 X\n", "1.500", true, "1.5", "X"},
		{"D 1000. X\ncommodity 1.000,00 X\n", "15,000 X", true, "15", "X"},
		{"D 1000. X\ncommodity 1.000,00 X\n", "1.500 X", true, "1500", "X"},
		{"D 1000, X\ncommodity 1,000.00 X\n", "15,000", true, "15", "X"},
		{"D 1000, X\ncommodity 1,000.00 X\n", "1.500", true, "1500", "X"},
		{"D 1000, X\ncommodity 1,000.00 X\n", "15,000 X", true, "15000", "X"},
		{"commodity 1.000,00 X\nD 1000. Y\n", "15,000", true, "15000", "Y"},
		{"commodity 1.000,00 X\nD 1000. Y\n", "15,000 X", true, "15", "X"},
		{"commodity 1.000,00 X\nD 1000. Y\n", "15,000 Y", true, "15000", "Y"},
		{"commodity 1.000,00 Y\nD 1000. X\n", "15,000", true, "15000", "X"},
		{"commodity 1.000,00 Y\nD 1000. X\n", "15,000 X", true, "15000", "X"},
		{"commodity 1.000,00 Y\nD 1000. X\n", "15,000 Y", true, "15", "Y"},
		{"D 1000. X\ncommodity 1.000,00\n", "15,000", true, "15000", "X"},
		{"D 1000. X\ncommodity 1.000,00\n", "15,000 X", true, "15000", "X"},
		{"commodity 1.000,00\nD 1000. X\n", "15,000", true, "15", "X"},
		{"commodity 1.000,00\nD 1000. X\n", "15,000 X", true, "15000", "X"},
		{"commodity 1000, X\nD 1000. X\n", "15,000", true, "15000", "X"},
		{"commodity 1000, X\nD 1000. X\n", "15,000 X", true, "15", "X"},
		{"decimal-mark ,\ncommodity 1.000,00 X\nD 1000. X\n", "15,000", true, "15", "X"},
		{"decimal-mark ,\ncommodity 1.000,00 X\nD 1000. X\n", "15,000 X", true, "15", "X"},
		{"decimal-mark .\ncommodity 1.000,00 X\nD 1000, X\n", "15,000", true, "15000", "X"},
		{"decimal-mark .\ncommodity 1.000,00 X\nD 1000, X\n", "15,000 X", true, "15000", "X"},
		{"commodity 1.000,00 X\nD 1000.00 X\n", "15,000", false, "", ""},
		{"commodity 1.000,00\nD 1000.00\n", "15,000", false, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.directives+tt.amount, func(t *testing.T) {
			qty, commodity, errs := firstAmount(t, tt.directives+"\n2024-01-01 x\n    a  "+tt.amount+"\n    b\n")
			if !tt.accepted {
				assert.NotEmpty(t, errs)
				return
			}
			require.Empty(t, errs)
			assert.Equal(t, tt.qty, qty)
			assert.Equal(t, tt.commodity, commodity)
		})
	}
}
