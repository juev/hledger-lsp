package formatter

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
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
			},
		},
		{
			name:   "European format with dot thousands separator",
			format: "1.000,00 EUR",
			expected: NumberFormat{
				DecimalMark:   ',',
				ThousandsSep:  ".",
				DecimalPlaces: 2,
			},
		},
		{
			name:   "US format with comma thousands separator",
			format: "$1,000.00",
			expected: NumberFormat{
				DecimalMark:   '.',
				ThousandsSep:  ",",
				DecimalPlaces: 2,
			},
		},
		{
			name:   "Simple format no thousands separator",
			format: "1000.00 USD",
			expected: NumberFormat{
				DecimalMark:   '.',
				ThousandsSep:  "",
				DecimalPlaces: 2,
			},
		},
		{
			name:   "Format with 3 decimal places",
			format: "1.000,000 BTC",
			expected: NumberFormat{
				DecimalMark:   ',',
				ThousandsSep:  ".",
				DecimalPlaces: 3,
			},
		},
		{
			name:   "Integer format",
			format: "1000 USD",
			expected: NumberFormat{
				DecimalMark:   '.',
				ThousandsSep:  "",
				DecimalPlaces: 2,
			},
		},
		{
			name:   "Format with space thousands no decimal",
			format: "1 000 RUB",
			expected: NumberFormat{
				DecimalMark:   '.',
				ThousandsSep:  " ",
				DecimalPlaces: 2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseNumberFormat(tt.format)
			assert.Equal(t, tt.expected.DecimalMark, result.DecimalMark, "DecimalMark")
			assert.Equal(t, tt.expected.ThousandsSep, result.ThousandsSep, "ThousandsSep")
			assert.Equal(t, tt.expected.DecimalPlaces, result.DecimalPlaces, "DecimalPlaces")
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
			},
			expected: "100,00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatNumber(tt.qty, tt.format)
			assert.Equal(t, tt.expected, result)
		})
	}
}
