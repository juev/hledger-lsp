package document

import (
	"strings"
	"testing"

	"go.lsp.dev/protocol"
)

// lineCases covers the awkward shapes the byte-offset arithmetic has to agree
// with: multi-byte characters whose byte and rune counts differ, a document
// with no final newline, an empty document, and an interior blank line.
var lineCases = map[string]string{
	"plain":               "2024-01-15 store\n    expenses:food  $20\n    assets:cash\n",
	"no trailing newline": "2024-01-15 store\n    expenses:food  $20\n    assets:cash",
	"empty":               "",
	"blank line":          "a\n\nc\n",
	"cjk":                 "2024-01-15 食料品店\n    expenses:食料品  ¥1980\n    assets:cash\n",
	"cyrillic":            "2024-01-15 Пятёрочка\n    expenses:продукты  500 RUB\n    assets:cash\n",
	"emoji":               "2024-01-15 🛒 groceries\n    expenses:food  $20\n    assets:cash\n",
	"crlf":                "2024-01-15 store\r\n    expenses:food  $20\r\n    assets:cash\r\n",
}

func TestText_ByteOffsetOfMatchesSplit(t *testing.T) {
	for name, content := range lineCases {
		t.Run(name, func(t *testing.T) {
			nt := NewText(content)
			normalized := nt.String()
			lines := strings.Split(normalized, "\n")

			if got, want := nt.LineCount(), len(lines); got != want {
				t.Fatalf("LineCount() = %d, want %d", got, want)
			}

			offset := 0
			for i, line := range lines {
				if got := nt.ByteOffsetOf(i); got != offset {
					t.Errorf("ByteOffsetOf(%d) = %d, want %d", i, got, offset)
				}
				offset += len(line) + 1
			}
			// Past the last line is end of document, not one byte further.
			if got, want := nt.ByteOffsetOf(len(lines)), len(normalized); got != want {
				t.Errorf("ByteOffsetOf(LineCount()) = %d, want %d (end of document)", got, want)
			}
		})
	}
}

func TestText_LineAtByteRoundTrips(t *testing.T) {
	for name, content := range lineCases {
		t.Run(name, func(t *testing.T) {
			nt := NewText(content)
			normalized := nt.String()
			lines := strings.Split(normalized, "\n")

			for i, want := range lines {
				start := nt.ByteOffsetOf(i)
				// Every byte of the line must resolve back to it, including the
				// offset one past the last character.
				for b := 0; b <= len(want); b++ {
					line, byteInLine := nt.LineAtByte(start + b)
					if line != want || byteInLine != b {
						t.Fatalf("LineAtByte(%d) = (%q, %d), want (%q, %d) [line %d]",
							start+b, line, byteInLine, want, b, i)
					}
				}
			}

			// Past the end clamps to the last line rather than panicking.
			line, byteInLine := nt.LineAtByte(len(normalized) + 100)
			last := lines[len(lines)-1]
			if line != last || byteInLine != len(last) {
				t.Errorf("LineAtByte(past end) = (%q, %d), want (%q, %d)", line, byteInLine, last, len(last))
			}
		})
	}
}

func TestText_EachLineMatchesSplit(t *testing.T) {
	for name, content := range lineCases {
		t.Run(name, func(t *testing.T) {
			nt := NewText(content)
			want := strings.Split(nt.String(), "\n")

			var got []string
			nt.EachLine(func(_ int, line string) bool {
				got = append(got, line)
				return true
			})

			if len(got) != len(want) {
				t.Fatalf("EachLine yielded %d lines, want %d", len(got), len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("line %d = %q, want %q", i, got[i], want[i])
				}
			}
		})
	}
}

func TestText_EachLineStopsOnFalse(t *testing.T) {
	nt := NewText("a\nb\nc\nd\n")

	var seen []string
	nt.EachLine(func(i int, line string) bool {
		seen = append(seen, line)
		return i < 1
	})

	if want := []string{"a", "b"}; len(seen) != len(want) || seen[0] != want[0] || seen[1] != want[1] {
		t.Errorf("EachLine(stop after 2) visited %q, want %q", seen, want)
	}
}

// The byte arithmetic has to survive edits, not just hold for a freshly built
// document: every edit changes the length of the middle line and therefore the
// start offset of everything after it.
func TestText_ByteOffsetsFollowEdits(t *testing.T) {
	nt := NewText("2024-01-15 Пятёрочка\n    expenses:продукты  500 RUB\n    assets:cash\n")

	edits := []struct {
		name string
		rng  protocol.Range
		text string
	}{
		{"insert CJK", protocol.Range{Start: protocol.Position{Line: 1, Character: 4}, End: protocol.Position{Line: 1, Character: 4}}, "食"},
		{"insert emoji", protocol.Range{Start: protocol.Position{Line: 0, Character: 11}, End: protocol.Position{Line: 0, Character: 11}}, "🛒"},
		{"delete a line", protocol.Range{Start: protocol.Position{Line: 1, Character: 0}, End: protocol.Position{Line: 2, Character: 0}}, ""},
		{"split a line", protocol.Range{Start: protocol.Position{Line: 0, Character: 9}, End: protocol.Position{Line: 0, Character: 9}}, "\n; comment"},
	}

	for _, edit := range edits {
		t.Run(edit.name, func(t *testing.T) {
			nt.ApplyChange(edit.rng, edit.text)

			normalized := nt.String()
			lines := strings.Split(normalized, "\n")
			if got := nt.LineCount(); got != len(lines) {
				t.Fatalf("LineCount() = %d, want %d", got, len(lines))
			}

			offset := 0
			for i, line := range lines {
				if got := nt.ByteOffsetOf(i); got != offset {
					t.Fatalf("ByteOffsetOf(%d) = %d, want %d after edit", i, got, offset)
				}
				for b := 0; b <= len(line); b++ {
					gotLine, byteInLine := nt.LineAtByte(offset + b)
					if gotLine != line || byteInLine != b {
						t.Fatalf("LineAtByte(%d) = (%q, %d), want (%q, %d)",
							offset+b, gotLine, byteInLine, line, b)
					}
				}
				offset += len(line) + 1
			}
		})
	}
}

func TestText_ByteOffsetsEmptyDocument(t *testing.T) {
	nt := NewText("")

	if got := nt.LineCount(); got != 1 {
		t.Fatalf("LineCount() = %d, want 1", got)
	}
	if got := nt.ByteOffsetOf(0); got != 0 {
		t.Errorf("ByteOffsetOf(0) = %d, want 0", got)
	}
	if line, byteInLine := nt.LineAtByte(0); line != "" || byteInLine != 0 {
		t.Errorf("LineAtByte(0) = (%q, %d), want empty", line, byteInLine)
	}
}
