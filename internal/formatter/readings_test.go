package formatter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/parser"
)

// readingUnder parses body behind a decimal-mark directive, so the same text is
// read once per mark. Both marks have the same length, which keeps every offset
// of the two parses identical.
func readingUnder(t *testing.T, mark, body string, formats map[string]CommodityFormat) (Reading, string) {
	t.Helper()
	content := "decimal-mark " + mark + "\n" + body
	journal, errs := parser.Parse(content)
	require.Empty(t, errs)
	return Reading{Journal: journal, Formats: formats}, content
}

func eurFormats() map[string]CommodityFormat {
	return map[string]CommodityFormat{"EUR": ParseCommodityFormat("1.000,00 EUR", "EUR")}
}

func TestFormatDocumentInReadings_SingleReadingIsPlainFormatting(t *testing.T) {
	reading, content := readingUnder(t, ",", "2024-01-01 x\n  a   10 EUR\n  b\n", eurFormats())

	assert.Equal(t,
		FormatDocumentWithOptions(reading.Journal, content, reading.Formats, DefaultOptions()),
		FormatDocumentInReadings([]Reading{reading}, content, DefaultOptions()))
	assert.Empty(t, FormatDocumentInReadings(nil, content, DefaultOptions()))
}

// Readings that render every line the same way format like a single reading.
func TestFormatDocumentInReadings_AgreeingReadingsFormatAsOne(t *testing.T) {
	body := "2024-01-01 x\n  a   10 EUR\n  b\n"
	first, content := readingUnder(t, ",", body, eurFormats())
	second, _ := readingUnder(t, ",", body, eurFormats())

	assert.Equal(t,
		FormatDocumentWithOptions(first.Journal, content, first.Formats, DefaultOptions()),
		FormatDocumentInReadings([]Reading{first, second}, content, DefaultOptions()))
}

// `10 EUR` rewritten as `10,00 EUR` is a thousand and ten under a dot mark.
func TestFormatDocumentInReadings_DisagreeingReadingsKeepTheNumbers(t *testing.T) {
	bodies := map[string]string{
		"ascii":    "2024-01-01 x\n  expenses:food   10 EUR\n  assets:cash\n",
		"cyrillic": "2024-01-01 x\n  расходы:еда   10 EUR\n  активы:касса\n",
		"cjk":      "2024-01-01 x\n  費用:食費   10 EUR\n  資産:現金\n",
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			comma, content := readingUnder(t, ",", body, eurFormats())
			dot, _ := readingUnder(t, ".", body, eurFormats())

			for _, readings := range [][]Reading{{comma, dot}, {dot, comma}, {comma, comma, dot}} {
				formatted := applyEdits(content, FormatDocumentInReadings(readings, content, DefaultOptions()))
				assert.Contains(t, formatted, "  10 EUR\n")
				assert.NotContains(t, formatted, "10,00")
				assert.NotContains(t, formatted, "10.00")
				assert.Contains(t, formatted, "\n    ", "indent is still normalised")
			}
		})
	}
}

// The layout of the kept numbers follows the text that is written, so a second
// pass changes nothing.
func TestFormatDocumentInReadings_DisagreementIsIdempotent(t *testing.T) {
	body := "2024-01-01 x\n  expenses:food   10 EUR\n  assets:cash\n\n2024-01-02 y\n  expenses:food  5,5 EUR\n  assets:cash  -5,5 EUR\n"
	comma, content := readingUnder(t, ",", body, eurFormats())
	dot, _ := readingUnder(t, ".", body, eurFormats())

	once := applyEdits(content, FormatDocumentInReadings([]Reading{comma, dot}, content, DefaultOptions()))

	again, _ := readingUnder(t, ",", strings.TrimPrefix(once, "decimal-mark ,\n"), eurFormats())
	againDot, _ := readingUnder(t, ".", strings.TrimPrefix(once, "decimal-mark ,\n"), eurFormats())
	twice := applyEdits(once, FormatDocumentInReadings([]Reading{again, againDot}, once, DefaultOptions()))
	assert.Equal(t, once, twice)
}

// A reading without formats falls back to the directives of its own journal, as
// FormatDocumentWithOptions does for a nil map.
func TestFormatDocumentInReadings_NilFormatsFallBackToTheJournalsDirectives(t *testing.T) {
	body := "commodity 1,000.00 EUR\n2024-01-01 x\n  a   1500 EUR\n  b\n"
	first, content := readingUnder(t, ".", body, nil)
	second, _ := readingUnder(t, ".", body, nil)

	formatted := applyEdits(content, FormatDocumentInReadings([]Reading{first, second}, content, DefaultOptions()))
	assert.Contains(t, formatted, "1,500.00 EUR")
}
