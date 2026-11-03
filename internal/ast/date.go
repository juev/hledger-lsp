package ast

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParseYear reads the digits of a year the way hledger does: any number of
// them. A year too large for an int is returned as math.MaxInt with its digits,
// without leading zeros, for Date.YearDigits; yearDigits is empty otherwise.
func ParseYear(digits string) (year int, yearDigits string, ok bool) {
	if digits == "" || strings.Trim(digits, "0123456789") != "" {
		return 0, "", false
	}
	year, err := strconv.Atoi(digits)
	switch {
	case err == nil:
		return year, "", true
	case errors.Is(err, strconv.ErrRange):
		return math.MaxInt, strings.TrimLeft(digits, "0"), true
	default:
		return 0, "", false
	}
}

// CalendarYear returns a year with the same leap years as the date's year: the
// year itself, or for a year too large for an int its place in the 400-year
// Gregorian cycle, from 1 to 400.
func (d Date) CalendarYear() int {
	if d.YearDigits == "" {
		return d.Year
	}
	year := 0
	for _, digit := range d.YearDigits {
		year = (year*10 + int(digit-'0')) % 400
	}
	if year == 0 {
		return 400
	}
	return year
}

// YearText returns the year as hledger writes it: at least four digits.
func (d Date) YearText() string {
	if d.YearDigits != "" {
		return d.YearDigits
	}
	return fmt.Sprintf("%04d", d.Year)
}

// Compare orders two dates, treating an unset date as the earliest. Years too
// large for an int are compared by their digits.
func (d Date) Compare(other Date) int {
	if c := compareYears(d, other); c != 0 {
		return c
	}
	if c := cmp.Compare(d.Month, other.Month); c != 0 {
		return c
	}
	return cmp.Compare(d.Day, other.Day)
}

func compareYears(a, b Date) int {
	if a.YearDigits == "" && b.YearDigits == "" {
		return cmp.Compare(a.Year, b.Year)
	}
	if a.YearDigits == "" {
		return -1
	}
	if b.YearDigits == "" {
		return 1
	}
	if c := cmp.Compare(len(a.YearDigits), len(b.YearDigits)); c != 0 {
		return c
	}
	return strings.Compare(a.YearDigits, b.YearDigits)
}
