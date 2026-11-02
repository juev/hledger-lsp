package include

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/testutil"
)

// TestOracle_IncludedDirectivesMatchHledger compares which directives cross an
// include, in both directions, and across sibling includes.
func TestOracle_IncludedDirectivesMatchHledger(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	tx := func(spelling string) string { return "2024-01-01 x\n    a  " + spelling + "\n    b\n" }
	layouts := []struct {
		name  string
		files func(directive, spelling string) map[string]string
	}{
		{"sibling declares", func(directive, spelling string) map[string]string {
			return map[string]string{
				"main.journal": "include d.journal\ninclude t.journal\n",
				"d.journal":    directive,
				"t.journal":    tx(spelling),
			}
		}},
		{"parent declares", func(directive, spelling string) map[string]string {
			return map[string]string{
				"main.journal": directive + "include t.journal\n",
				"t.journal":    tx(spelling),
			}
		}},
		{"child declares for its parent", func(directive, spelling string) map[string]string {
			return map[string]string{
				"main.journal": "include d.journal\n" + tx(spelling),
				"d.journal":    directive,
			}
		}},
		{"grandchild declares", func(directive, spelling string) map[string]string {
			return map[string]string{
				"main.journal": "include c.journal\n" + tx(spelling),
				"c.journal":    "include d.journal\n",
				"d.journal":    directive,
			}
		}},
		{"sibling declares after", func(directive, spelling string) map[string]string {
			return map[string]string{
				"main.journal": "include t.journal\ninclude d.journal\n",
				"d.journal":    directive,
				"t.journal":    tx(spelling),
			}
		}},
		{"child resets the parent style", func(directive, spelling string) map[string]string {
			return map[string]string{
				"main.journal": directive + "include d.journal\n" + tx(spelling),
				"d.journal":    "commodity X\n",
			}
		}},
	}

	for _, layout := range layouts {
		for _, directive := range testutil.NumberDirectives {
			for _, spelling := range testutil.NumberSpellings {
				t.Run(fmt.Sprintf("%s %q reads %q", layout.name, directive, spelling), func(t *testing.T) {
					dir := t.TempDir()
					for name, content := range layout.files(directive, spelling) {
						require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
					}
					root := filepath.Join(dir, "main.journal")

					want := testutil.HledgerReadFile(t, hledgerPath, root)
					assert.Equal(t, want, ourReadFile(root), "layout %s", layout.name)
				})
			}
		}
	}
}

func ourReadFile(root string) testutil.HledgerAmount {
	resolved, errs := NewLoader().Load(root)
	if len(errs) > 0 {
		return testutil.HledgerAmount{}
	}
	// hledger prints by date, so the transaction under test is found by name: a
	// directive style can add an earlier one after it in file order.
	for _, transaction := range resolved.AllTransactions() {
		if transaction.Description != "x" {
			continue
		}
		amount := transaction.Postings[0].Amount
		if amount == nil {
			break
		}
		return testutil.HledgerAmount{Accepted: true, Quantity: amount.Quantity.String(), Commodity: amount.Commodity.Symbol}
	}
	return testutil.HledgerAmount{}
}

// TestOracle_CommodityAndDefaultCommodityInSeparateFilesMatchHledger compares a
// commodity style and a D directive that declare different decimal marks when
// they sit in different files, in either order, for bare and for symbol-written
// numbers.
func TestOracle_CommodityAndDefaultCommodityInSeparateFilesMatchHledger(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	pairs := [][2]string{
		{"commodity 1.000,00 X\n", "D 1000. X\n"},
		{"commodity 1,000.00 X\n", "D 1000, X\n"},
		{"commodity 1.000,00 X\n", "D 1000.00 Y\n"},
		{"commodity 1.000,00 Y\n", "D 1000. X\n"},
		{"commodity 1.000,00\n", "D 1000. X\n"},
		{"commodity 1.000,00\n", "D 1000.00 X\n"},
		{"commodity 1000, X\n", "D 1000. X\n"},
		{"decimal-mark ,\n", "D 1000. X\n"},
	}
	spellings := []string{"15,000", "1.500", "1,5", "15,000 X", "1.500 X", "15,000 Y"}
	tx := func(spelling string) string { return "2024-01-01 x\n    a  " + spelling + "\n    b\n" }
	layouts := []struct {
		name  string
		files func(commodity, defaultCommodity, spelling string) map[string]string
	}{
		{"commodity file then D file", func(c, d, s string) map[string]string {
			return map[string]string{"main.journal": "include c.journal\ninclude d.journal\n" + tx(s), "c.journal": c, "d.journal": d}
		}},
		{"D file then commodity file", func(c, d, s string) map[string]string {
			return map[string]string{"main.journal": "include d.journal\ninclude c.journal\n" + tx(s), "c.journal": c, "d.journal": d}
		}},
		{"D in the parent, commodity in the child", func(c, d, s string) map[string]string {
			return map[string]string{"main.journal": d + "include c.journal\n" + tx(s), "c.journal": c}
		}},
		{"commodity in the parent, D in the child", func(c, d, s string) map[string]string {
			return map[string]string{"main.journal": c + "include d.journal\n" + tx(s), "d.journal": d}
		}},
		{"transaction in a sibling after both", func(c, d, s string) map[string]string {
			return map[string]string{"main.journal": "include c.journal\ninclude d.journal\ninclude t.journal\n", "c.journal": c, "d.journal": d, "t.journal": tx(s)}
		}},
	}

	for _, layout := range layouts {
		for _, pair := range pairs {
			for _, spelling := range spellings {
				t.Run(fmt.Sprintf("%s %q %q reads %q", layout.name, pair[0], pair[1], spelling), func(t *testing.T) {
					dir := t.TempDir()
					for name, content := range layout.files(pair[0], pair[1], spelling) {
						require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
					}
					root := filepath.Join(dir, "main.journal")

					want := testutil.HledgerReadFile(t, hledgerPath, root)
					assert.Equal(t, want, ourReadFile(root), "layout %s", layout.name)
				})
			}
		}
	}
}
