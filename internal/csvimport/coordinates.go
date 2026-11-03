package csvimport

import (
	"strings"
	"unicode/utf16"

	"github.com/juev/hledger-lsp/internal/textutil"
)

// CoordinateText returns the text that positions in a data file are counted in,
// and whether an editor shows the file with the same lines.
//
// hledger reports a record by its line numbers in the decoded text. UTF-8 and
// the single-byte encodings an `encoding` rule can name keep every line break a
// lone \n byte, so the bytes split into the same lines the editor shows. UTF-16
// does not: a \n byte is also half of many characters (U+010A is 0A 01), so a
// file with a byte order mark is decoded first. UTF-32, or UTF-16 without a
// byte order mark, cannot be split reliably; ok is false.
func CoordinateText(raw string) (text string, ok bool) {
	switch {
	case strings.HasPrefix(raw, "\xff\xfe\x00\x00"), strings.HasPrefix(raw, "\x00\x00\xfe\xff"):
		return "", false
	case strings.HasPrefix(raw, "\xff\xfe"):
		return decodeUTF16(raw[2:], false)
	case strings.HasPrefix(raw, "\xfe\xff"):
		return decodeUTF16(raw[2:], true)
	case strings.IndexByte(raw, 0) >= 0:
		return "", false
	}
	return textutil.NormalizeLineEndings(raw), true
}

func decodeUTF16(raw string, bigEndian bool) (string, bool) {
	if len(raw)%2 != 0 {
		return "", false
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		hi, lo := raw[2*i+1], raw[2*i]
		if bigEndian {
			hi, lo = lo, hi
		}
		units[i] = uint16(hi)<<8 | uint16(lo)
	}
	return textutil.NormalizeLineEndings(string(utf16.Decode(units))), true
}
