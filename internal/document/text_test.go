package document

import (
	"math/rand"
	"strings"
	"testing"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/lsputil"
)

// oracleApply applies an LSP change using the reference PositionMapper.
func oracleApply(content string, r protocol.Range, text string) string {
	return lsputil.NewPositionMapper(content).ApplyChange(r, text)
}

func TestText_StringRoundTrip(t *testing.T) {
	for _, content := range []string{
		"",
		"a",
		"a\nb\nc",
		"a\nb\nc\n",
		"\n\n",
		"2024-01-01 * store\n    expenses:food  $50\n    assets:cash\n",
	} {
		nt := NewText(content)
		if got := nt.String(); got != content {
			t.Fatalf("round-trip mismatch for %q: got %q", content, got)
		}
	}
}

func TestText_CRLFNormalized(t *testing.T) {
	nt := NewText("a\r\nb\r\nc")
	if got := nt.String(); got != "a\nb\nc" {
		t.Fatalf("CRLF not normalized: %q", got)
	}
	nt2 := NewText("a\rb\rc")
	if got := nt2.String(); got != "a\nb\nc" {
		t.Fatalf("lone CR not normalized: %q", got)
	}
}

func TestText_LineAndCount(t *testing.T) {
	nt := NewText("a\nbb\nccc")
	if nt.LineCount() != 3 {
		t.Fatalf("LineCount = %d, want 3", nt.LineCount())
	}
	if nt.Line(0) != "a" || nt.Line(1) != "bb" || nt.Line(2) != "ccc" {
		t.Fatalf("Line mismatch: %q %q %q", nt.Line(0), nt.Line(1), nt.Line(2))
	}
	if nt.Line(3) != "" || nt.Line(-1) != "" {
		t.Fatalf("out-of-range Line should be empty, got %q / %q", nt.Line(3), nt.Line(-1))
	}
}

func TestText_ApplyChange_InsertSingleChar(t *testing.T) {
	nt := NewText("hello world")
	nt.ApplyChange(protocol.Range{
		Start: protocol.Position{Line: 0, Character: 5},
		End:   protocol.Position{Line: 0, Character: 5},
	}, ",")
	if got := nt.String(); got != "hello, world" {
		t.Fatalf("got %q, want %q", got, "hello, world")
	}
}

func TestText_ApplyChange_MultiLineInsert(t *testing.T) {
	nt := NewText("ab")
	nt.ApplyChange(protocol.Range{
		Start: protocol.Position{Line: 0, Character: 1},
		End:   protocol.Position{Line: 0, Character: 1},
	}, "X\nY\nZ")
	if got := nt.String(); got != "aX\nY\nZb" {
		t.Fatalf("got %q, want %q", got, "aX\nY\nZb")
	}
	if nt.LineCount() != 3 {
		t.Fatalf("LineCount = %d, want 3", nt.LineCount())
	}
}

func TestText_ApplyChange_DeleteAcrossLines(t *testing.T) {
	nt := NewText("line1\nline2\nline3")
	// Delete from line0 char2 to line2 char2 → "li" + "ne3" = "line3".
	nt.ApplyChange(protocol.Range{
		Start: protocol.Position{Line: 0, Character: 2},
		End:   protocol.Position{Line: 2, Character: 2},
	}, "")
	if got := nt.String(); got != "line3" {
		t.Fatalf("got %q, want %q", got, "line3")
	}
}

func TestText_ApplyChange_ReplaceWholeDocument(t *testing.T) {
	nt := NewText("old\ncontent")
	last := nt.LineCount() - 1
	lastLen := lsputil.UTF16Len(nt.Line(last))
	nt.ApplyChange(protocol.Range{
		Start: protocol.Position{Line: 0, Character: 0},
		End:   protocol.Position{Line: uint32(last), Character: uint32(lastLen)},
	}, "brand\nnew\ntext")
	if got := nt.String(); got != "brand\nnew\ntext" {
		t.Fatalf("got %q, want %q", got, "brand\nnew\ntext")
	}
}

func TestText_ApplyChange_UTF16EmojiAndCJK(t *testing.T) {
	// "💶" is a surrogate pair (2 UTF-16 units, 4 bytes); "日" is 1 unit, 3 bytes.
	nt := NewText("a💶日b")
	// Insert after the emoji: UTF-16 offset 3 (a=1, 💶=2).
	nt.ApplyChange(protocol.Range{
		Start: protocol.Position{Line: 0, Character: 3},
		End:   protocol.Position{Line: 0, Character: 3},
	}, "X")
	if got := nt.String(); got != "a💶X日b" {
		t.Fatalf("got %q, want %q", got, "a💶X日b")
	}
}

func TestText_ApplyChange_InvalidatesCachedString(t *testing.T) {
	nt := NewText("abc")
	before := nt.String()
	nt.ApplyChange(protocol.Range{
		Start: protocol.Position{Line: 0, Character: 3},
		End:   protocol.Position{Line: 0, Character: 3},
	}, "d")
	after := nt.String()
	if before != "abc" || after != "abcd" {
		t.Fatalf("cache not invalidated: before=%q after=%q", before, after)
	}
}

func TestText_ApplyChange_OutOfRangeNoPanic(t *testing.T) {
	nt := NewText("abc\ndef")
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on out-of-range change: %v", r)
		}
	}()
	nt.ApplyChange(protocol.Range{
		Start: protocol.Position{Line: 99, Character: 99},
		End:   protocol.Position{Line: 100, Character: 0},
	}, "x")
	_ = nt.String()
}

func pos(line, char uint32) protocol.Position {
	return protocol.Position{Line: line, Character: char}
}

// TestText_ApplyChange_OutOfRangeMatchesOracle pins the regression where a line
// beyond the last was clamped INTO the last line (corrupting the buffer) instead
// of mapping to end-of-document like lsputil.PositionMapper.LSPToByte.
func TestText_ApplyChange_OutOfRangeMatchesOracle(t *testing.T) {
	cases := []struct {
		name    string
		content string
		r       protocol.Range
		text    string
	}{
		{"line-beyond-insert", "abc", protocol.Range{Start: pos(5, 0), End: pos(5, 0)}, "X"},
		{"line-beyond-char", "abc", protocol.Range{Start: pos(5, 2), End: pos(5, 2)}, "X"},
		{"end-line-beyond", "a\nb\nc", protocol.Range{Start: pos(1, 0), End: pos(99, 0)}, "X"},
		{"both-beyond", "abc\ndef", protocol.Range{Start: pos(99, 99), End: pos(100, 0)}, "x"},
	}
	for _, tc := range cases {
		want := oracleApply(tc.content, tc.r, tc.text)
		nt := NewText(tc.content)
		nt.ApplyChange(tc.r, tc.text)
		if got := nt.String(); got != want {
			t.Fatalf("%s: content=%q range=%+v text=%q want=%q got=%q", tc.name, tc.content, tc.r, tc.text, want, got)
		}
	}
}

func TestText_ApplyChange_OutOfRangeRandomMatchesOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	alphabet := []rune("ab \n日💶")
	for iter := 0; iter < 3000; iter++ {
		n := rng.Intn(30)
		var sb strings.Builder
		for i := 0; i < n; i++ {
			sb.WriteRune(alphabet[rng.Intn(len(alphabet))])
		}
		content := sb.String()
		lineCount := len(strings.Split(content, "\n"))

		m := rng.Intn(8)
		var isb strings.Builder
		for i := 0; i < m; i++ {
			isb.WriteRune(alphabet[rng.Intn(len(alphabet))])
		}
		insert := isb.String()

		// Positions may overshoot the document (line and char beyond the end).
		r := protocol.Range{
			Start: pos(uint32(rng.Intn(lineCount+5)), uint32(rng.Intn(20))),
			End:   pos(uint32(rng.Intn(lineCount+5)), uint32(rng.Intn(20))),
		}

		want := oracleApply(content, r, insert)
		nt := NewText(content)
		nt.ApplyChange(r, insert)
		if got := nt.String(); got != want {
			t.Fatalf("iter %d: content=%q range=%+v insert=%q want=%q got=%q", iter, content, r, insert, want, got)
		}
	}
}

// randomRange builds an LSP range with positions valid for content.
func randomRange(rng *rand.Rand, content string) protocol.Range {
	lines := strings.Split(content, "\n")
	lineCount := len(lines)
	sl := rng.Intn(lineCount)
	el := rng.Intn(lineCount)
	if sl > el {
		sl, el = el, sl
	}
	sc := rng.Intn(lsputil.UTF16Len(lines[sl]) + 1)
	ec := rng.Intn(lsputil.UTF16Len(lines[el]) + 1)
	return protocol.Range{
		Start: protocol.Position{Line: uint32(sl), Character: uint32(sc)},
		End:   protocol.Position{Line: uint32(el), Character: uint32(ec)},
	}
}

func TestText_ApplyChange_MatchesOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	// Alphabet spans ASCII, CJK, Cyrillic and a supplementary-plane emoji
	// (surrogate pair in UTF-16) to exercise UTF-16 column mapping.
	alphabet := []rune("ab$ \n\texpenses:cash日本語рубль💶")

	for iter := 0; iter < 3000; iter++ {
		n := rng.Intn(40)
		var sb strings.Builder
		for i := 0; i < n; i++ {
			sb.WriteRune(alphabet[rng.Intn(len(alphabet))])
		}
		content := sb.String()

		m := rng.Intn(10)
		var isb strings.Builder
		for i := 0; i < m; i++ {
			isb.WriteRune(alphabet[rng.Intn(len(alphabet))])
		}
		insert := isb.String()

		r := randomRange(rng, content)

		want := oracleApply(content, r, insert)

		nt := NewText(content)
		nt.ApplyChange(r, insert)
		got := nt.String()

		if got != want {
			t.Fatalf("iter %d: mismatch\ncontent=%q\nrange=%+v\ninsert=%q\nwant=%q\ngot=%q",
				iter, content, r, insert, want, got)
		}
	}
}

// TestText_ApplyChange_SequentialEdits applies a sequence of edits, re-deriving
// each range from the current content, and checks the rope stays consistent with
// the oracle after every step.
func TestText_ApplyChange_SequentialEdits(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	alphabet := []rune("xy \n日本語💶")

	content := "start\nmiddle\nend"
	nt := NewText(content)

	for step := 0; step < 500; step++ {
		m := rng.Intn(6)
		var isb strings.Builder
		for i := 0; i < m; i++ {
			isb.WriteRune(alphabet[rng.Intn(len(alphabet))])
		}
		insert := isb.String()
		r := randomRange(rng, content)

		content = oracleApply(content, r, insert)
		nt.ApplyChange(r, insert)

		if got := nt.String(); got != content {
			t.Fatalf("step %d: divergence\nwant=%q\ngot=%q", step, content, got)
		}
	}
}
