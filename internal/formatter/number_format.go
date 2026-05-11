package formatter

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"github.com/juev/hledger-lsp/internal/ast"
)

type CommodityFormat struct {
	NumberFormat
	Position     ast.CommodityPosition
	SpaceBetween bool
}

func ParseCommodityFormat(formatStr string, symbol string) CommodityFormat {
	cf := CommodityFormat{
		NumberFormat: ParseNumberFormat(formatStr),
		Position:     ast.CommodityRight,
		SpaceBetween: true,
	}

	if symbol == "" {
		return cf
	}

	symbolIdx := strings.Index(formatStr, symbol)
	if symbolIdx < 0 {
		return cf
	}

	numberStart := -1
	for i, r := range formatStr {
		if unicode.IsDigit(r) {
			numberStart = i
			break
		}
	}
	if numberStart < 0 {
		return cf
	}

	if symbolIdx < numberStart {
		cf.Position = ast.CommodityLeft
		symbolEnd := symbolIdx + len(symbol)
		cf.SpaceBetween = symbolEnd < len(formatStr) && formatStr[symbolEnd] == ' '
	} else {
		cf.Position = ast.CommodityRight
		cf.SpaceBetween = symbolIdx > 0 && formatStr[symbolIdx-1] == ' '
	}

	return cf
}

type NumberFormat struct {
	DecimalMark   rune
	ThousandsSep  string
	DecimalPlaces int
	HasDecimal    bool
}

func ParseNumberFormat(formatStr string) NumberFormat {
	nf := NumberFormat{
		DecimalMark:   '.',
		ThousandsSep:  "",
		DecimalPlaces: 0,
		HasDecimal:    false,
	}

	numberPart := extractNumberPart(formatStr)
	if numberPart == "" {
		return nf
	}

	lastDot := strings.LastIndex(numberPart, ".")
	lastComma := strings.LastIndex(numberPart, ",")

	if lastDot > lastComma {
		nf.DecimalMark = '.'
		nf.HasDecimal = true
		if lastComma >= 0 {
			nf.ThousandsSep = ","
		} else if strings.Contains(numberPart[:lastDot], " ") {
			nf.ThousandsSep = " "
		}
		nf.DecimalPlaces = len(numberPart) - lastDot - 1
	} else if lastComma > lastDot {
		nf.DecimalMark = ','
		nf.HasDecimal = true
		if lastDot >= 0 {
			nf.ThousandsSep = "."
		} else if strings.Contains(numberPart[:lastComma], " ") {
			nf.ThousandsSep = " "
		}
		nf.DecimalPlaces = len(numberPart) - lastComma - 1
	} else {
		if strings.Contains(numberPart, " ") {
			nf.ThousandsSep = " "
		}
	}

	return nf
}

func extractNumberPart(formatStr string) string {
	var start, end int
	inNumber := false
	lastDigitPos := -1

	for i, r := range formatStr {
		isNumberChar := unicode.IsDigit(r) || r == '.' || r == ',' || r == ' '
		if isNumberChar {
			if !inNumber {
				start = i
				inNumber = true
			}
			if unicode.IsDigit(r) {
				lastDigitPos = i
			}
			end = i + utf8.RuneLen(r)
		} else if inNumber {
			break
		}
	}

	if !inNumber || lastDigitPos < 0 {
		return ""
	}

	result := formatStr[start:end]
	return strings.TrimSpace(result)
}

func FormatNumber(qty decimal.Decimal, format NumberFormat) string {
	var str string
	if format.HasDecimal {
		// Pad to format.DecimalPlaces, but preserve any additional
		// significant digits so quantities like `-0.01234 BYN` are not
		// silently rounded to `-0.01` when the commodity directive
		// declares only 2 decimal places (hledger-vscode issue #151).
		natural := qty.String()
		naturalPlaces := 0
		if dot := strings.IndexByte(natural, '.'); dot >= 0 {
			decPart := strings.TrimRight(natural[dot+1:], "0")
			naturalPlaces = len(decPart)
		}
		places := format.DecimalPlaces
		if naturalPlaces > places {
			places = naturalPlaces
		}
		if places > 0 {
			str = qty.StringFixed(int32(places))
		} else {
			str = natural
			if strings.ContainsRune(str, '.') {
				str = strings.TrimRight(str, "0")
				str = strings.TrimSuffix(str, ".")
			}
		}
	} else {
		str = qty.Round(0).String()
	}

	parts := strings.Split(str, ".")
	intPart := parts[0]
	decPart := ""
	if len(parts) > 1 {
		decPart = parts[1]
	}

	negative := false
	if strings.HasPrefix(intPart, "-") {
		negative = true
		intPart = intPart[1:]
	}

	if format.ThousandsSep != "" && len(intPart) > 3 {
		var groups []string
		for len(intPart) > 3 {
			groups = append([]string{intPart[len(intPart)-3:]}, groups...)
			intPart = intPart[:len(intPart)-3]
		}
		if len(intPart) > 0 {
			groups = append([]string{intPart}, groups...)
		}
		intPart = strings.Join(groups, format.ThousandsSep)
	}

	var result strings.Builder
	if negative {
		result.WriteString("-")
	}
	result.WriteString(intPart)

	if format.HasDecimal && (format.DecimalPlaces > 0 || len(decPart) > 0) {
		result.WriteRune(format.DecimalMark)
		result.WriteString(decPart)
	}

	return result.String()
}
