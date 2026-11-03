package fmtcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	unformatted = "2024-01-15 grocery\n  expenses:food  $50.00\n  assets:bank:checking    $-50.00\n"
	formatted   = "2024-01-15 grocery\n    expenses:food         $50.00\n    assets:bank:checking  $-50.00\n"
)

func run(t *testing.T, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func writeJournal(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o640))
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func TestStdinToStdout(t *testing.T) {
	code, stdout, stderr := run(t, unformatted)
	assert.Equal(t, ExitOK, code, stderr)
	assert.Equal(t, formatted, stdout)
}

func TestDashReadsStdin(t *testing.T) {
	code, stdout, _ := run(t, unformatted, "-")
	assert.Equal(t, ExitOK, code)
	assert.Equal(t, formatted, stdout)
}

func TestFileToStdoutLeavesFileAlone(t *testing.T) {
	path := writeJournal(t, t.TempDir(), "a.journal", unformatted)

	code, stdout, _ := run(t, "", path)

	assert.Equal(t, ExitOK, code)
	assert.Equal(t, formatted, stdout)
	assert.Equal(t, unformatted, readFile(t, path))
}

func TestWriteIsIdempotentAndKeepsMode(t *testing.T) {
	dir := t.TempDir()
	path := writeJournal(t, dir, "a.journal", unformatted)
	require.NoError(t, os.Chmod(path, 0o640))

	code, stdout, stderr := run(t, "", "-w", path)
	require.Equal(t, ExitOK, code, stderr)
	assert.Empty(t, stdout)
	assert.Equal(t, formatted, readFile(t, path))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())

	before := info.ModTime()
	code, _, _ = run(t, "", "-w", path)
	require.Equal(t, ExitOK, code)
	info, err = os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, before, info.ModTime(), "an already formatted file must not be rewritten")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file may be left behind")
}

func TestWriteFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := writeJournal(t, dir, "real.journal", unformatted)
	link := filepath.Join(dir, "link.journal")
	require.NoError(t, os.Symlink(target, link))

	code, _, stderr := run(t, "", "-w", link)
	require.Equal(t, ExitOK, code, stderr)

	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "the symlink must survive")
	assert.Equal(t, formatted, readFile(t, target))
}

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	dirty := writeJournal(t, dir, "dirty.journal", unformatted)
	clean := writeJournal(t, dir, "clean.journal", formatted)

	code, stdout, _ := run(t, "", "-c", dirty, clean)

	assert.Equal(t, ExitUnformatted, code)
	assert.Equal(t, dirty+"\n", stdout)
	assert.Equal(t, unformatted, readFile(t, dirty))

	code, stdout, _ = run(t, "", "--check", clean)
	assert.Equal(t, ExitOK, code)
	assert.Empty(t, stdout)
}

func TestParseErrorRefusesToFormat(t *testing.T) {
	broken := "2024-13-45 bad date\n  expenses:food  $1\n  assets:cash\n"
	path := writeJournal(t, t.TempDir(), "a.journal", broken)

	code, stdout, stderr := run(t, "", "-w", path)

	assert.Equal(t, ExitError, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, path+":1:")
	assert.Equal(t, broken, readFile(t, path))
}

// hledger rejects these journals, so the parser must too: -w would otherwise
// rewrite a journal hledger cannot read.
func TestJournalHledgerRejectsIsLeftAlone(t *testing.T) {
	for _, journal := range []string{
		"2024-01-01 x\n    e:x  $1\n    a:y  ((\n",
		"2024-01-01 касса\n    расходы:еда  ₽1\n    現金  ((\n",
	} {
		path := writeJournal(t, t.TempDir(), "a.journal", journal)

		code, stdout, stderr := run(t, "", "-w", path)

		assert.Equal(t, ExitError, code)
		assert.Empty(t, stdout)
		assert.Contains(t, stderr, path+":3:")
		assert.Equal(t, journal, readFile(t, path))
	}
}

func TestByteOrderMarkIsKept(t *testing.T) {
	const bom = "\xef\xbb\xbf"
	for _, newline := range []string{"\n", "\r\n"} {
		crlf := func(s string) string { return strings.ReplaceAll(s, "\n", newline) }
		path := writeJournal(t, t.TempDir(), "a.journal", bom+crlf(unformatted))

		code, _, stderr := run(t, "", "-w", path)

		require.Equal(t, ExitOK, code, stderr)
		assert.Equal(t, bom+crlf(formatted), readFile(t, path))
		code, stdout, _ := run(t, "", "-c", path)
		assert.Equal(t, ExitOK, code, stdout)
	}
}

func TestErrorInOneFileStillFormatsTheRest(t *testing.T) {
	dir := t.TempDir()
	broken := writeJournal(t, dir, "broken.journal", "2024-13-45 bad\n")
	good := writeJournal(t, dir, "good.journal", unformatted)

	code, _, _ := run(t, "", "-w", broken, good)

	assert.Equal(t, ExitError, code)
	assert.Equal(t, formatted, readFile(t, good))
}

func TestCheckErrorWinsOverUnformatted(t *testing.T) {
	dir := t.TempDir()
	dirty := writeJournal(t, dir, "dirty.journal", unformatted)
	broken := writeJournal(t, dir, "broken.journal", "2024-13-45 bad\n")

	for _, files := range [][]string{{dirty, broken}, {broken, dirty}} {
		code, _, _ := run(t, "", append([]string{"-c"}, files...)...)
		assert.Equal(t, ExitError, code)
	}
}

func TestRulesFileRefused(t *testing.T) {
	content := "skip 1\n"
	path := writeJournal(t, t.TempDir(), "bank.csv.rules", content)

	code, _, stderr := run(t, "", "-w", path)

	assert.Equal(t, ExitError, code)
	assert.Contains(t, stderr, "rules")
	assert.Equal(t, content, readFile(t, path))
}

func TestMissingFile(t *testing.T) {
	code, _, stderr := run(t, "", filepath.Join(t.TempDir(), "missing.journal"))
	assert.Equal(t, ExitError, code)
	assert.Contains(t, stderr, "missing.journal")
}

func TestCRLFIsPreserved(t *testing.T) {
	crlf := func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }

	t.Run("reformatted", func(t *testing.T) {
		code, stdout, _ := run(t, crlf(unformatted))
		assert.Equal(t, ExitOK, code)
		assert.Equal(t, crlf(formatted), stdout)
	})

	t.Run("already formatted is unchanged", func(t *testing.T) {
		code, stdout, _ := run(t, crlf(formatted), "-c")
		assert.Equal(t, ExitOK, code)
		assert.Empty(t, stdout)
	})

	t.Run("idempotent", func(t *testing.T) {
		_, once, _ := run(t, crlf(unformatted))
		_, twice, _ := run(t, once)
		assert.Equal(t, once, twice)
	})
}

func TestNonASCIIAlignmentAndIdempotency(t *testing.T) {
	tests := []struct {
		name, input, want string
	}{
		{
			name:  "CJK uses display width",
			input: "2024-01-15 買い物\n  支出:食費  ¥500\n  assets:cash  ¥-500\n",
			want:  "2024-01-15 買い物\n    支出:食費    ¥500\n    assets:cash  ¥-500\n",
		},
		{
			name:  "Cyrillic",
			input: "2024-01-15 магазин\n  расходы:еда  100 RUB\n  активы:наличные  -100 RUB\n",
			want:  "2024-01-15 магазин\n    расходы:еда       100 RUB\n    активы:наличные  -100 RUB\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, once, stderr := run(t, tt.input)
			require.Equal(t, ExitOK, code, stderr)
			assert.Equal(t, tt.want, once)

			_, twice, _ := run(t, once)
			assert.Equal(t, once, twice)
		})
	}
}

func TestOptions(t *testing.T) {
	code, stdout, _ := run(t, unformatted, "--indent-size", "2", "--no-align")
	require.Equal(t, ExitOK, code)
	assert.Contains(t, stdout, "\n  expenses:food  $50.00\n")

	code, stdout, _ = run(t, unformatted, "--amount-alignment-mode", "left", "--amount-alignment-column", "40")
	require.Equal(t, ExitOK, code)
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n")[1:] {
		assert.Equal(t, 40, strings.Index(line, "$"), line)
	}
}

func TestInvalidArguments(t *testing.T) {
	dir := t.TempDir()
	a := writeJournal(t, dir, "a.journal", unformatted)
	b := writeJournal(t, dir, "b.journal", unformatted)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"write and check", []string{"-w", "-c", a}, "cannot be used together"},
		{"write stdin", []string{"-w"}, "standard input"},
		{"write dash", []string{"-w", "-"}, "standard input"},
		{"several files to stdout", []string{a, b}, "require -w or -c"},
		{"bad mode", []string{"--amount-alignment-mode", "center"}, "--amount-alignment-mode"},
		{"bad target", []string{"--amount-alignment-target", "x"}, "--amount-alignment-target"},
		{"bad indent", []string{"--indent-size", "0"}, "--indent-size"},
		{"negative column", []string{"--amount-alignment-column", "-1"}, "--amount-alignment-column"},
		{"root with stdin", []string{"--root", a}, "--root cannot be used with standard input"},
		{"unknown flag", []string{"--nope"}, "--nope"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := run(t, unformatted, tt.args...)
			assert.Equal(t, ExitError, code)
			assert.Contains(t, stderr, strings.TrimLeft(tt.want, "-"))
		})
	}
	assert.Equal(t, unformatted, readFile(t, a))
}

func TestHelp(t *testing.T) {
	code, _, stderr := run(t, "", "-h")
	assert.Equal(t, ExitOK, code)
	assert.Contains(t, stderr, "Usage: hledger-fmt")
}

// A lone comma is a thousands separator here only because of a directive in
// another file. Read without that context it is a decimal mark, and the number
// comes back out as -1.234.
const separatorJournal = "2026-10-01 redemption\n    assets:cash      42.67 USD\n    assets:fund     -1,234 FDRXX  @@  42.67 USD  =  -1,234 FDRXX\n    equity\n"

// amountValueCases are numbers with a single separator that the directive in
// force decides the meaning of. Each want is how the formatter writes the value
// hledger reads, in the directive's style. wantIncluded replaces it when the
// directive is in another file, for a directive whose effect ends with its file.
var amountValueCases = []struct {
	name         string
	directive    string
	journal      string
	want         []string
	wantIncluded []string
}{
	{
		name:      "thousands separator and assertion",
		directive: "commodity 0,000.000 FDRXX\n",
		journal:   separatorJournal,
		want:      []string{"-1,234.000 FDRXX", "= -1,234.000 FDRXX"},
	},
	{
		name:      "commodity without thousands separator",
		directive: "commodity 1000.00 USD\n",
		journal:   "2024-01-01 a\n    e:x  1,000 USD\n    a:y\n",
		want:      []string{"1000.00 USD"},
	},
	{
		name:      "default commodity with a bare number",
		directive: "D $1000.00\n",
		journal:   "2024-01-01 a\n    e:x  1,000\n    a:y\n",
		want:      []string{"$1000.00"},
		// D only applies to the rest of its own file, so elsewhere the number
		// has no commodity and keeps its written form.
		wantIncluded: []string{"e:x  1,000\n"},
	},
	{
		name:      "comma decimal mark",
		directive: "commodity 1000,00 EUR\n",
		journal:   "2024-01-01 a\n    e:x  1.000 EUR\n    a:y\n",
		want:      []string{"1000,00 EUR"},
	},
	{
		name:      "Cyrillic and CJK accounts",
		directive: "commodity 1000.00 ₽\n",
		journal:   "2024-01-01 касса\n    расходы:еда  1,000 ₽\n    現金\n",
		want:      []string{"1000.00 ₽"},
	},
}

// amountValueLayouts place a case's directive where the formatted file reads it
// from, and return the arguments that format that file.
var amountValueLayouts = []struct {
	name     string
	sameFile bool
	layout   func(t *testing.T, dir, directive, journal string) (path string, args []string)
}{
	{
		name:     "same file",
		sameFile: true,
		layout: func(t *testing.T, dir, directive, journal string) (string, []string) {
			path := writeJournal(t, dir, "main.journal", directive+"\n"+journal)
			return path, nil
		},
	},
	{
		name: "included file",
		layout: func(t *testing.T, dir, directive, journal string) (string, []string) {
			writeJournal(t, dir, "commodities.journal", directive)
			path := writeJournal(t, dir, "main.journal", "include commodities.journal\n\n"+journal)
			return path, nil
		},
	},
	{
		name: "sibling of root",
		layout: func(t *testing.T, dir, directive, journal string) (string, []string) {
			writeJournal(t, dir, "commodities.journal", directive)
			path := writeJournal(t, dir, "2024.journal", journal)
			root := writeJournal(t, dir, "main.journal", "include commodities.journal\n\ninclude 2024.journal\n")
			return path, []string{"--root", root}
		},
	},
}

func TestAmountValuesKept(t *testing.T) {
	for _, tc := range amountValueCases {
		for _, layout := range amountValueLayouts {
			for _, newline := range []string{"\n", "\r\n"} {
				name := tc.name + "/" + layout.name
				if newline == "\r\n" {
					name += "/CRLF"
				}
				t.Run(name, func(t *testing.T) {
					crlf := func(s string) string { return strings.ReplaceAll(s, "\n", newline) }
					path, args := layout.layout(t, t.TempDir(), crlf(tc.directive), crlf(tc.journal))

					code, _, stderr := run(t, "", append(args, "-w", path)...)
					require.Equal(t, ExitOK, code, stderr)
					once := readFile(t, path)
					want := tc.want
					if !layout.sameFile && tc.wantIncluded != nil {
						want = tc.wantIncluded
					}
					for _, want := range want {
						assert.Contains(t, once, crlf(want))
					}

					code, stdout, stderr := run(t, "", append(args, "-c", path)...)
					assert.Equal(t, ExitOK, code, "formatting must be idempotent: %s%s", stdout, stderr)
				})
			}
		}
	}
}

// A file included under two decimal marks means different things at each, so
// only its layout may change.
func TestAmountValuesKeptUnderEveryInclusion(t *testing.T) {
	dir := t.TempDir()
	path := writeJournal(t, dir, "tx.journal", "2024-01-01 a\n  e:x  1,000 EUR\n  a:y\n")
	root := writeJournal(t, dir, "main.journal", "decimal-mark .\ninclude tx.journal\ndecimal-mark ,\ninclude tx.journal\n")

	code, _, stderr := run(t, "", "--root", root, "-w", path)

	require.Equal(t, ExitOK, code, stderr)
	assert.Equal(t, "2024-01-01 a\n    e:x  1,000 EUR\n    a:y\n", readFile(t, path))
}

func TestAmountWithUnknownStyleKeepsItsWrittenForm(t *testing.T) {
	path := writeJournal(t, t.TempDir(), "a.journal", "include nope.journal\n\n"+separatorJournal)

	code, stdout, stderr := run(t, "", path)

	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "1,234 FDRXX")
	assert.Contains(t, stdout, "= -1,234 FDRXX")
	assert.Contains(t, stderr, "warning")
}

func TestParseErrorIsNotAlsoAWarning(t *testing.T) {
	path := writeJournal(t, t.TempDir(), "a.journal", "2024-13-45 bad\n")

	code, _, stderr := run(t, "", path)

	assert.Equal(t, ExitError, code)
	assert.NotContains(t, stderr, "warning")
}

const euroJournal = "2024-01-15 grocery\n  expenses:food  1000 EUR\n  assets:cash  -1000 EUR\n"

func TestCommodityFormatFromIncludedFile(t *testing.T) {
	dir := t.TempDir()
	writeJournal(t, dir, "commodities.journal", "commodity 1.000,00 EUR\n")
	path := writeJournal(t, dir, "main.journal", "include commodities.journal\n\n"+euroJournal)

	code, stdout, stderr := run(t, "", path)

	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "1.000,00 EUR")
}

func TestRootSuppliesCommodityFormatsToIncludedFile(t *testing.T) {
	dir := t.TempDir()
	child := writeJournal(t, dir, "2024.journal", euroJournal)
	root := writeJournal(t, dir, "main.journal", "commodity 1.000,00 EUR\n\ninclude 2024.journal\n")

	code, stdout, stderr := run(t, "", "--root", root, child)
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "1.000,00 EUR")
}

func TestFileOutsideRootIsRefused(t *testing.T) {
	dir := t.TempDir()
	other := writeJournal(t, dir, "other.journal", unformatted)
	root := writeJournal(t, dir, "main.journal", "commodity 1.000,00 EUR\n")

	code, _, stderr := run(t, "", "-w", "--root", root, other)

	assert.Equal(t, ExitError, code)
	assert.Contains(t, stderr, "not included by the root journal")
	assert.Equal(t, unformatted, readFile(t, other))
}

func TestMissingRootIsAnError(t *testing.T) {
	code, _, _ := run(t, unformatted, "--root", filepath.Join(t.TempDir(), "missing.journal"))
	assert.Equal(t, ExitError, code)
}

func TestMissingIncludeWarnsAndStillFormats(t *testing.T) {
	path := writeJournal(t, t.TempDir(), "a.journal", "include nope.journal\n\n"+unformatted)

	code, stdout, stderr := run(t, "", path)

	assert.Equal(t, ExitOK, code)
	assert.Contains(t, stdout, "expenses:food         $50.00")
	assert.Contains(t, stderr, "warning")
}
