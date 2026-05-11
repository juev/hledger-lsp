package formatter

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/juev/hledger-lsp/internal/ast"
)

func TestParseNumberFormat(t *testing.T) {
	tests := []struct {
		name     string
		format   string
		expected NumberFormat
	}{
		{
			name:   "European format with space thousands separator",
			format: "1 000,00 RUB",
			expected: NumberFormat{
				DecimalMark:   ',',
				ThousandsSep:  " ",
				DecimalPlaces: 2,
				HasDecimal:    true,
			},
		},
		{
			name:   "European format with dot thousands separator",
			format: "1.000,00 EUR",
			expected: NumberFormat{
				DecimalMark:   ',',
				ThousandsSep:  ".",
				DecimalPlaces: 2,
				HasDecimal:    true,
			},
		},
		{
			name:   "US format with comma thousands separator",
			format: "$1,000.00",
			expected: NumberFormat{
				DecimalMark:   '.',
				ThousandsSep:  ",",
				DecimalPlaces: 2,
				HasDecimal:    true,
			},
		},
		{
			name:   "Simple format no thousands separator",
			format: "1000.00 USD",
			expected: NumberFormat{
				DecimalMark:   '.',
				ThousandsSep:  "",
				DecimalPlaces: 2,
				HasDecimal:    true,
			},
		},
		{
			name:   "Format with 3 decimal places",
			format: "1.000,000 BTC",
			expected: NumberFormat{
				DecimalMark:   ',',
				ThousandsSep:  ".",
				DecimalPlaces: 3,
				HasDecimal:    true,
			},
		},
		{
			name:   "Integer format no decimal",
			format: "1000 USD",
			expected: NumberFormat{
				DecimalMark:   '.',
				ThousandsSep:  "",
				DecimalPlaces: 0,
				HasDecimal:    false,
			},
		},
		{
			name:   "Format with space thousands no decimal",
			format: "1 000 RUB",
			expected: NumberFormat{
				DecimalMark:   '.',
				ThousandsSep:  " ",
				DecimalPlaces: 0,
				HasDecimal:    false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseNumberFormat(tt.format)
			assert.Equal(t, tt.expected.DecimalMark, result.DecimalMark, "DecimalMark")
			assert.Equal(t, tt.expected.ThousandsSep, result.ThousandsSep, "ThousandsSep")
			assert.Equal(t, tt.expected.DecimalPlaces, result.DecimalPlaces, "DecimalPlaces")
			assert.Equal(t, tt.expected.HasDecimal, result.HasDecimal, "HasDecimal")
		})
	}
}

func TestParseCommodityFormat(t *testing.T) {
	tests := []struct {
		name         string
		format       string
		symbol       string
		wantPosition ast.CommodityPosition
		wantSpace    bool
	}{
		{
			name:         "RUB right with space",
			format:       "1.000,00 RUB",
			symbol:       "RUB",
			wantPosition: ast.CommodityRight,
			wantSpace:    true,
		},
		{
			name:         "$ left no space",
			format:       "$1,000.00",
			symbol:       "$",
			wantPosition: ast.CommodityLeft,
			wantSpace:    false,
		},
		{
			name:         "EUR right with space",
			format:       "1 000,00 EUR",
			symbol:       "EUR",
			wantPosition: ast.CommodityRight,
			wantSpace:    true,
		},
		{
			name:         "symbol left with space",
			format:       "$ 1,000.00",
			symbol:       "$",
			wantPosition: ast.CommodityLeft,
			wantSpace:    true,
		},
		{
			name:         "no symbol in format defaults to right",
			format:       "1 000,00",
			symbol:       "RUB",
			wantPosition: ast.CommodityRight,
			wantSpace:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseCommodityFormat(tt.format, tt.symbol)
			assert.Equal(t, tt.wantPosition, result.Position, "Position")
			assert.Equal(t, tt.wantSpace, result.SpaceBetween, "SpaceBetween")
		})
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		name     string
		qty      decimal.Decimal
		format   NumberFormat
		expected string
	}{
		{
			name: "European format with space separator",
			qty:  decimal.NewFromFloat(846661.89),
			format: NumberFormat{
				DecimalMark:   ',',
				ThousandsSep:  " ",
				DecimalPlaces: 2,
				HasDecimal:    true,
			},
			expected: "846 661,89",
		},
		{
			name: "European format with dot separator",
			qty:  decimal.NewFromFloat(1000.50),
			format: NumberFormat{
				DecimalMark:   ',',
				ThousandsSep:  ".",
				DecimalPlaces: 2,
				HasDecimal:    true,
			},
			expected: "1.000,50",
		},
		{
			name: "US format",
			qty:  decimal.NewFromFloat(1234567.89),
			format: NumberFormat{
				DecimalMark:   '.',
				ThousandsSep:  ",",
				DecimalPlaces: 2,
				HasDecimal:    true,
			},
			expected: "1,234,567.89",
		},
		{
			name: "No thousands separator",
			qty:  decimal.NewFromFloat(1000.00),
			format: NumberFormat{
				DecimalMark:   '.',
				ThousandsSep:  "",
				DecimalPlaces: 2,
				HasDecimal:    true,
			},
			expected: "1000.00",
		},
		{
			name: "Negative number",
			qty:  decimal.NewFromFloat(-5000.25),
			format: NumberFormat{
				DecimalMark:   ',',
				ThousandsSep:  " ",
				DecimalPlaces: 2,
				HasDecimal:    true,
			},
			expected: "-5 000,25",
		},
		{
			name: "Three decimal places",
			qty:  decimal.NewFromFloat(123.456),
			format: NumberFormat{
				DecimalMark:   '.',
				ThousandsSep:  ",",
				DecimalPlaces: 3,
				HasDecimal:    true,
			},
			expected: "123.456",
		},
		{
			name: "Small number no grouping",
			qty:  decimal.NewFromFloat(100.00),
			format: NumberFormat{
				DecimalMark:   ',',
				ThousandsSep:  " ",
				DecimalPlaces: 2,
				HasDecimal:    true,
			},
			expected: "100,00",
		},
		{
			name: "Integer format no decimals",
			qty:  decimal.NewFromFloat(1000.50),
			format: NumberFormat{
				DecimalMark:   '.',
				ThousandsSep:  " ",
				DecimalPlaces: 0,
				HasDecimal:    false,
			},
			expected: "1 001",
		},
		{
			name: "HasDecimal zero places preserves natural precision",
			qty:  decimal.RequireFromString("1000.5"),
			format: NumberFormat{
				DecimalMark:  ',',
				ThousandsSep: ".",
				HasDecimal:   true,
			},
			expected: "1.000,5",
		},
		{
			name: "HasDecimal zero places integer",
			qty:  decimal.RequireFromString("1000"),
			format: NumberFormat{
				DecimalMark:  ',',
				ThousandsSep: ".",
				HasDecimal:   true,
			},
			expected: "1.000",
		},
		{
			name: "HasDecimal zero places with 3 decimals",
			qty:  decimal.RequireFromString("42.123"),
			format: NumberFormat{
				DecimalMark: '.',
				HasDecimal:  true,
			},
			expected: "42.123",
		},
		{
			name: "HasDecimal zero places negative",
			qty:  decimal.RequireFromString("-1000.5"),
			format: NumberFormat{
				DecimalMark:  ',',
				ThousandsSep: ".",
				HasDecimal:   true,
			},
			expected: "-1.000,5",
		},
		{
			name: "Pad integer to commodity precision",
			qty:  decimal.RequireFromString("1"),
			format: NumberFormat{
				DecimalMark:   '.',
				DecimalPlaces: 2,
				HasDecimal:    true,
			},
			expected: "1.00",
		},
		{
			name: "Trim trailing zeros down to commodity precision",
			qty:  decimal.RequireFromString("1.010000000"),
			format: NumberFormat{
				DecimalMark:   '.',
				DecimalPlaces: 2,
				HasDecimal:    true,
			},
			expected: "1.01",
		},
		{
			name: "Preserve significant digits beyond commodity precision",
			qty:  decimal.RequireFromString("1.010000100"),
			format: NumberFormat{
				DecimalMark:   '.',
				DecimalPlaces: 2,
				HasDecimal:    true,
			},
			expected: "1.0100001",
		},
		{
			name: "Negative quantity preserves significant digits beyond commodity precision",
			qty:  decimal.RequireFromString("-0.01234"),
			format: NumberFormat{
				DecimalMark:   '.',
				DecimalPlaces: 2,
				HasDecimal:    true,
			},
			expected: "-0.01234",
		},
		{
			name: "Thousands separator with extra precision",
			qty:  decimal.RequireFromString("1234.56789"),
			format: NumberFormat{
				DecimalMark:   ',',
				ThousandsSep:  " ",
				DecimalPlaces: 2,
				HasDecimal:    true,
			},
			expected: "1 234,56789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatNumber(tt.qty, tt.format)
			assert.Equal(t, tt.expected, result)
		})
	}
}
