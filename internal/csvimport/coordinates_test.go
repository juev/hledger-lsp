package csvimport

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const encodedCSV = "date,desc,amount\r\n2024-01-02,Кофе Ċ ☕,-3.50\r\n2024-01-03,salary,1000\r\n"

func utf16Bytes(text string, bigEndian bool) string {
	var b strings.Builder
	if bigEndian {
		b.WriteString("\xfe\xff")
	} else {
		b.WriteString("\xff\xfe")
	}
	for _, unit := range utf16.Encode([]rune(text)) {
		hi, lo := byte(unit>>8), byte(unit)
		if bigEndian {
			b.WriteByte(hi)
			b.WriteByte(lo)
		} else {
			b.WriteByte(lo)
			b.WriteByte(hi)
		}
	}
	return b.String()
}

func TestCoordinateText(t *testing.T) {
	want := strings.ReplaceAll(encodedCSV, "\r\n", "\n")
	for name, raw := range map[string]string{
		"UTF-8":        encodedCSV,
		"UTF-16LE BOM": utf16Bytes(encodedCSV, false),
		"UTF-16BE BOM": utf16Bytes(encodedCSV, true),
	} {
		text, ok := CoordinateText(raw)
		assert.True(t, ok, name)
		assert.Equal(t, want, text, name)
	}

	text, ok := CoordinateText("\xef\xbb\xbf" + encodedCSV)
	assert.True(t, ok)
	assert.Equal(t, "\xef\xbb\xbf"+want, text, "a UTF-8 byte order mark stays, as on disk")

	// U+010A is 0A 01 in UTF-16LE: its 0A byte is no line break.
	text, ok = CoordinateText(utf16Bytes("a\nĊ\nb", false))
	require.True(t, ok)
	assert.Equal(t, 3, strings.Count(text, "\n")+1)

	for name, raw := range map[string]string{
		"UTF-32LE BOM":    "\xff\xfe\x00\x00a\x00\x00\x00",
		"UTF-32BE BOM":    "\x00\x00\xfe\xff\x00\x00\x00a",
		"UTF-16 no BOM":   "a\x00\n\x00b\x00",
		"odd UTF-16 body": "\xff\xfea\x00\n",
	} {
		_, ok := CoordinateText(raw)
		assert.False(t, ok, name)
	}
}

// hledger reports records by the lines of the decoded file. Each supported
// encoding must place them on the same lines and offsets of CoordinateText, and
// one whose lines cannot be told is placed at the start with a warning.
func TestOracle_RecordsArePlacedOnTheirLinesInEveryEncoding(t *testing.T) {
	binary := previewHledger(t)
	const rules = "skip 1\nfields date, description, amount\ncurrency $\naccount1 a\naccount2 b\n"
	cases := []struct {
		name, raw, encoding string
		located             bool
	}{
		{name: "utf8", raw: encodedCSV, located: true},
		{name: "utf8bom", raw: "\xef\xbb\xbf" + encodedCSV, located: true},
		{name: "utf16le", raw: utf16Bytes(encodedCSV, false), encoding: "utf-16", located: true},
		{name: "utf16be", raw: utf16Bytes(encodedCSV, true), encoding: "utf-16", located: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			data := write(t, dir, tc.name+".csv", tc.raw)
			ruleText := rules
			if tc.encoding != "" {
				ruleText += "encoding " + tc.encoding + "\n"
			}
			write(t, dir, tc.name+".csv.rules", ruleText)

			result := New(Config{Enabled: true, Path: binary, Timeout: 30 * time.Second}).Import(context.Background(), Request{Reader: ReaderCSV, Path: data, MaxFileSize: 1 << 20})

			require.Empty(t, result.Err)
			require.Len(t, result.Transactions, 2)
			text, ok := CoordinateText(tc.raw)
			require.True(t, ok)
			for i, line := range []int{2, 3} {
				rng := result.Transactions[i].Range
				assert.Equal(t, line, rng.Start.Line)
				assert.Equal(t, line, rng.End.Line)
				record := text[rng.Start.Offset:rng.End.Offset]
				assert.Equal(t, strings.Split(text, "\n")[line-1], record)
			}
			assert.Empty(t, result.Warnings)
		})
	}

	t.Run("utf32", func(t *testing.T) {
		dir := t.TempDir()
		var raw strings.Builder
		raw.WriteString("\xff\xfe\x00\x00")
		for _, r := range encodedCSV {
			raw.WriteByte(byte(r))
			raw.WriteByte(byte(r >> 8))
			raw.WriteByte(byte(r >> 16))
			raw.WriteByte(byte(r >> 24))
		}
		data := write(t, dir, "utf32.csv", raw.String())
		write(t, dir, "utf32.csv.rules", rules+"encoding utf-32\n")

		result := New(Config{Enabled: true, Path: binary, Timeout: 30 * time.Second}).Import(context.Background(), Request{Reader: ReaderCSV, Path: data, MaxFileSize: 1 << 20})

		// Neither its lines nor its numbers can be checked, so it is refused
		// before hledger runs.
		assert.Empty(t, result.Transactions)
		assert.Contains(t, result.Err, "data file "+filepath.Base(data)+" is in an encoding the language server cannot read")
	})
}

// The guard against numbers hledger would expand into huge values reads the
// decoded text: in UTF-16 every digit is followed by a zero byte, so the raw
// bytes hide the digit run.
func TestBuildPlan_UTF16NumbersAreChecked(t *testing.T) {
	dir := t.TempDir()
	const rules = "skip 1\nfields date, desc, amount\ncurrency $\naccount1 a\naccount2 b\nencoding utf-16\n"
	for name, record := range map[string]string{"exponent": "1E999999999", "digits": strings.Repeat("9", 400)} {
		data := write(t, dir, name+".csv", utf16Bytes("date,desc,amount\n2024-01-02,a,"+record+"\n", false))
		write(t, dir, name+".csv.rules", rules)
		_, err := buildPlan(Request{Reader: ReaderCSV, Path: data, MaxFileSize: 1 << 20})
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), "data file "+name+".csv:", name)
	}
	data := write(t, dir, "fine.csv", utf16Bytes("date,desc,amount\n2024-01-02,a,1E3\n", false))
	write(t, dir, "fine.csv.rules", rules)
	_, err := buildPlan(Request{Reader: ReaderCSV, Path: data, MaxFileSize: 1 << 20})
	assert.NoError(t, err)
}
