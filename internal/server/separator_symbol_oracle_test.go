package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/testutil"
)

// oracleRoundTripLayouts place the directives under test in the journal itself
// or in a file it includes before the transaction.
var oracleRoundTripLayouts = []struct {
	name  string
	files func(directives, tx string) map[string]string
}{
	{"same file", func(directives, tx string) map[string]string {
		return map[string]string{"main.journal": directives + tx}
	}},
	{"included before", func(directives, tx string) map[string]string {
		return map[string]string{"main.journal": "include c.journal\n" + tx, "c.journal": directives}
	}},
}

// checkRoundTrip compares, for the amount of transaction x, the value hledger
// reads with the value the parser reads, the value hledger reads from the hover
// text written back in place of the amount, and the value hledger reads from
// the formatted journal.
func checkRoundTrip(t *testing.T, hledgerPath string, files map[string]string, amount string) {
	t.Helper()
	dir := t.TempDir()
	writeJournals(t, dir, files)
	path := filepath.Join(dir, "main.journal")
	want := testutil.HledgerReadFile(t, hledgerPath, path)
	require.True(t, want.Accepted, "hledger rejects %q", files)

	text := files["main.journal"]
	ts, docURI := formatServer(t, dir, path, text)
	journal, errs := ts.cachedJournal(docURI, text)
	require.Empty(t, errs)
	got := journal.Transactions[len(journal.Transactions)-1].Postings[0].Amount
	require.NotNil(t, got)
	assert.Equal(t, want, testutil.HledgerAmount{Accepted: true, Quantity: got.Quantity.String(), Commodity: got.Commodity.Symbol}, "parser")

	line := uint32(strings.Count(text[:strings.Index(text, "2024-01-01 x")], "\n") + 1)
	hoverText := strings.TrimPrefix(firstHoverLine(hoverAt(t, ts, docURI, line, 8)), "**Amount:** ")
	reread := files["main.journal"]
	reread = strings.Replace(reread, "    a  "+amount+"\n", "    a  "+hoverText+"\n", 1)
	require.NoError(t, os.WriteFile(path, []byte(reread), 0o644))
	assert.Equal(t, want, testutil.HledgerReadFile(t, hledgerPath, path), "hover text %q", hoverText)

	once := formatted(t, ts, docURI)
	require.NoError(t, os.WriteFile(path, []byte(once), 0o644))
	assert.Equal(t, want, testutil.HledgerReadFile(t, hledgerPath, path), "formatted journal:\n%s", once)
}

// TestOracle_SeparatorSymbolFormatsRoundTripThroughHledger covers a commodity
// whose symbol is a separator itself, written before or after the number, with
// or without a space, including numbers that end in the same separator.
func TestOracle_SeparatorSymbolFormatsRoundTripThroughHledger(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	formats := []string{
		", 1.000,", ",1.000,", ", 1000,", ",1000,", ", 1.000,00", ",1.000,00",
		", 1,000.", ",1,000.", ", 1,000.00", "1.000, ,", "1,000. ,", "1,000.00 ,", "1.000,00 ,",
	}
	spellings := []string{"15,000 ,", ", 15,000", "1.500 ,", "1,5 ,"}
	for _, layout := range oracleRoundTripLayouts {
		for _, format := range formats {
			for _, spelling := range spellings {
				t.Run(fmt.Sprintf("%s %q reads %q", layout.name, format, spelling), func(t *testing.T) {
					tx := "2024-01-01 x\n    a  " + spelling + "\n    b\n"
					checkRoundTrip(t, hledgerPath, layout.files("commodity ,\n    format "+format+"\n", tx), spelling)
				})
			}
		}
	}
}

// TestOracle_MixedDefaultCommodityStylesHoverRoundTripsThroughHledger covers bare
// amounts whose D commodity declares a different mark than the D directive,
// with the directives in the journal or in an included file.
func TestOracle_MixedDefaultCommodityStylesHoverRoundTripsThroughHledger(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	prefixes := []string{
		"D 1000. X\ncommodity 1.000,00 X\n",
		"commodity 1.000,00 X\nD 1000. X\n",
		"commodity 1.000,00 X\nD 1000. X\nD 1000,\n",
		"D 1000, X\ncommodity 1,000.00 X\n",
		"commodity 1,000.00 X\nD 1000, X\n",
		"D 1.000,00 RUB\nD 1000.\n",
		"D 1000. руб\ncommodity 1.000,00 руб\n",
	}
	for _, layout := range oracleRoundTripLayouts {
		for _, prefix := range prefixes {
			for _, bare := range []string{"1.500", "1,500", "1,5", "1.5", "15"} {
				t.Run(fmt.Sprintf("%s %q reads %q", layout.name, prefix, bare), func(t *testing.T) {
					tx := "2024-01-01 x\n    a  " + bare + "\n    b\n"
					checkRoundTrip(t, hledgerPath, layout.files(prefix, tx), bare)
				})
			}
		}
	}
}

// TestOracle_DefaultCommodityAndIncludedStyleHoverRoundTripsThroughHledger keeps
// the D directive in the journal and the commodity style in a file it includes,
// so the bare amount inherits D's commodity and the hover text has to be written
// with the style that crossed the include.
func TestOracle_DefaultCommodityAndIncludedStyleHoverRoundTripsThroughHledger(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	pairs := [][2]string{
		{"D 1000. X\n", "commodity 1.000,00 X\n"},
		{"D 1000, X\n", "commodity 1,000.00 X\n"},
		{"D 1000. руб\n", "commodity 1.000,00 руб\n"},
	}
	for _, pair := range pairs {
		for _, bare := range []string{"1.500", "1,500", "1,5", "1.5", "15"} {
			t.Run(fmt.Sprintf("%q %q reads %q", pair[0], pair[1], bare), func(t *testing.T) {
				files := map[string]string{
					"main.journal": pair[0] + "include c.journal\n2024-01-01 x\n    a  " + bare + "\n    b\n",
					"c.journal":    pair[1],
				}
				checkRoundTrip(t, hledgerPath, files, bare)
			})
		}
	}
}
