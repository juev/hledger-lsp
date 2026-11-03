package ast

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseYear(t *testing.T) {
	for digits, want := range map[string]struct {
		year   int
		digits string
		ok     bool
	}{
		"2024":                    {2024, "", true},
		"0099":                    {99, "", true},
		"9223372036854775807":     {math.MaxInt, "", true},
		"9223372036854775808":     {math.MaxInt, "9223372036854775808", true},
		"00099999999999999999999": {math.MaxInt, "99999999999999999999", true},
		"":                        {0, "", false},
		"+5":                      {0, "", false},
		"12a":                     {0, "", false},
	} {
		year, yearDigits, ok := ParseYear(digits)
		assert.Equal(t, want.ok, ok, digits)
		assert.Equal(t, want.year, year, digits)
		assert.Equal(t, want.digits, yearDigits, digits)
	}
}

func TestDate_CalendarYearKeepsLeapYears(t *testing.T) {
	isLeap := func(year int) bool { return year%4 == 0 && (year%100 != 0 || year%400 == 0) }
	for digits, leap := range map[string]bool{
		"99999999999999999996":  true,
		"99999999999999999999":  false,
		"100000000000000000000": true,  // divisible by 400
		"100000000000000000100": false, // divisible by 100, not by 400
		"400000000000000000000": true,  // divisible by 400
	} {
		year := Date{Year: math.MaxInt, YearDigits: digits}.CalendarYear()
		assert.GreaterOrEqual(t, year, 1)
		assert.LessOrEqual(t, year, 400)
		assert.Equal(t, leap, isLeap(year), digits)
	}
	assert.Equal(t, 2024, Date{Year: 2024}.CalendarYear())
}

func TestDate_YearText(t *testing.T) {
	assert.Equal(t, "0099", Date{Year: 99}.YearText())
	assert.Equal(t, "2024", Date{Year: 2024}.YearText())
	assert.Equal(t, "99999999999999999999", Date{Year: math.MaxInt, YearDigits: "99999999999999999999"}.YearText())
}
