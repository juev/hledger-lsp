package lsputil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func TestUTF16Len(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"empty", "", 0},
		{"ascii", "hello", 5},
		{"cyrillic", "Привет", 6},
		{"mixed_ascii_cyrillic", "hello Мир", 9},
		{"with_colon", "Активы:Кошелек", 14},
		{"surrogate_pair", "a\U00010400b", 4}, // 𐐀 requires surrogate pair (2 UTF-16 units)
		{"emoji", "a😀b", 4},                   // emoji requires surrogate pair
		{"chinese", "hello世界", 7},
		{"multiple_emoji", "a😀😀😀b", 8}, // 1 + 2*3 + 1 = 8 UTF-16 units
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UTF16Len(tt.input)
			assert.Equal(t, tt.expected, got, "UTF16Len(%q)", tt.input)
		})
	}
}

func TestUTF16OffsetToByteOffset(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		utf16Offset int
		expected    int
	}{
		{"empty", "", 0, 0},
		{"ascii_start", "hello", 0, 0},
		{"ascii_middle", "hello", 2, 2},
		{"ascii_end", "hello", 5, 5},
		{"cyrillic_start", "Привет", 0, 0},
		{"cyrillic_middle", "Привет", 3, 6}, // each cyrillic char is 2 bytes
		{"cyrillic_end", "Привет", 6, 12},
		{"mixed", "aПb", 1, 1},                    // after 'a'
		{"mixed_after_cyr", "aПb", 2, 3},          // after 'П' (2 bytes)
		{"account_name", "Активы:Кошелек", 7, 13}, // after "Активы:" = 12 + 1 = 13 bytes
		{"surrogate_start", "a\U00010400b", 1, 1}, // after 'a'
		{"surrogate_after", "a\U00010400b", 3, 5}, // after surrogate (1 + 4 bytes)
		{"multiple_emoji_middle", "😀😀😀", 2, 4},    // after first emoji (4 bytes)
		{"multiple_emoji_end", "😀😀😀", 6, 12},      // after all emoji (12 bytes)
		{"out_of_bounds", "hello", 10, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UTF16OffsetToByteOffset(tt.input, tt.utf16Offset)
			assert.Equal(t, tt.expected, got, "UTF16OffsetToByteOffset(%q, %d)", tt.input, tt.utf16Offset)
		})
	}
}

func TestByteOffsetToUTF16(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		byteOffset int
		expected   int
	}{
		{"empty", "", 0, 0},
		{"ascii_start", "hello", 0, 0},
		{"ascii_middle", "hello", 2, 2},
		{"ascii_end", "hello", 5, 5},
		{"cyrillic_start", "Привет", 0, 0},
		{"cyrillic_middle", "Привет", 6, 3}, // 6 bytes = 3 cyrillic chars
		{"cyrillic_end", "Привет", 12, 6},
		{"account_name", "Активы:Кошелек", 13, 7}, // 13 bytes = "Активы:" (7 chars = 6*2 + 1)
		{"surrogate", "a\U00010400b", 5, 3},       // after surrogate
		{"multiple_emoji", "😀😀😀", 8, 4},           // 8 bytes = 2 emoji = 4 UTF-16 units
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ByteOffsetToUTF16(tt.input, tt.byteOffset)
			assert.Equal(t, tt.expected, got, "ByteOffsetToUTF16(%q, %d)", tt.input, tt.byteOffset)
		})
	}
}

func TestNewPositionMapper(t *testing.T) {
	content := "line1\nline2\nline3"
	mapper := NewPositionMapper(content)

	require.NotNil(t, mapper)
	assert.Equal(t, 3, len(mapper.lines))
	assert.Equal(t, "line1", mapper.lines[0])
	assert.Equal(t, "line2", mapper.lines[1])
	assert.Equal(t, "line3", mapper.lines[2])
}

func TestPositionMapper_LineUTF16Len(t *testing.T) {
	content := "hello\nПривет\na\U00010400b"
	mapper := NewPositionMapper(content)

	assert.Equal(t, 5, mapper.LineUTF16Len(0))  // "hello"
	assert.Equal(t, 6, mapper.LineUTF16Len(1))  // "Привет"
	assert.Equal(t, 4, mapper.LineUTF16Len(2))  // "a𐐀b" (surrogate pair)
	assert.Equal(t, 0, mapper.LineUTF16Len(-1)) // out of bounds
	assert.Equal(t, 0, mapper.LineUTF16Len(10)) // out of bounds
}

func TestPositionMapper_LSPToByte(t *testing.T) {
	content := "hello\nАктивы:Кошелек  100\nworld"
	mapper := NewPositionMapper(content)

	// Line byte calculations:
	// Line 0: "hello" = 5 bytes, starts at 0
	// Line 1: "Активы:Кошелек  100" = 12 + 1 + 14 + 5 = 32 bytes, starts at 6
	// Line 2: "world" = 5 bytes, starts at 6 + 32 + 1 = 39

	tests := []struct {
		name     string
		pos      protocol.Position
		expected int
	}{
		{"line0_start", protocol.Position{Line: 0, Character: 0}, 0},
		{"line0_middle", protocol.Position{Line: 0, Character: 3}, 3},
		{"line1_start", protocol.Position{Line: 1, Character: 0}, 6},
		{"line1_after_account", protocol.Position{Line: 1, Character: 14}, 33}, // "Активы:Кошелек" = 27 bytes + 6 = 33
		{"line2_start", protocol.Position{Line: 2, Character: 0}, 39},          // 6 + 32 + 1 = 39
		{"out_of_bounds_line", protocol.Position{Line: 10, Character: 0}, len(content)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapper.LSPToByte(tt.pos)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestPositionMapper_ByteToLSP(t *testing.T) {
	content := "hello\nАктивы:Кошелек  100\nworld"
	mapper := NewPositionMapper(content)

	// Line 1 starts at byte 6
	// "Активы:" = 13 bytes, so byte 19 (6+13) is at UTF-16 position 7
	// Line 2 starts at byte 39

	tests := []struct {
		name       string
		byteOffset int
		expected   protocol.Position
	}{
		{"line0_start", 0, protocol.Position{Line: 0, Character: 0}},
		{"line0_middle", 3, protocol.Position{Line: 0, Character: 3}},
		{"line1_start", 6, protocol.Position{Line: 1, Character: 0}},
		{"line1_after_colon", 19, protocol.Position{Line: 1, Character: 7}}, // 6 + 13 = after "Активы:"
		{"line2_start", 39, protocol.Position{Line: 2, Character: 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapper.ByteToLSP(tt.byteOffset)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestPositionMapper_ApplyChange(t *testing.T) {
	content := "Активы:Кошелек  100 RUB\nРасходы:Еда  50 RUB"
	mapper := NewPositionMapper(content)

	// Replace "Кошелек" with "Банк"
	r := protocol.Range{
		Start: protocol.Position{Line: 0, Character: 7},  // after "Активы:"
		End:   protocol.Position{Line: 0, Character: 14}, // end of "Кошелек"
	}

	result := mapper.ApplyChange(r, "Банк")

	expected := "Активы:Банк  100 RUB\nРасходы:Еда  50 RUB"
	assert.Equal(t, expected, result)
}

func TestPositionMapper_ApplyChange_MultiLine(t *testing.T) {
	content := "line1\nline2\nline3"
	mapper := NewPositionMapper(content)

	// Replace "line2" entirely
	r := protocol.Range{
		Start: protocol.Position{Line: 1, Character: 0},
		End:   protocol.Position{Line: 1, Character: 5},
	}

	result := mapper.ApplyChange(r, "REPLACED")
	expected := "line1\nREPLACED\nline3"
	assert.Equal(t, expected, result)
}

func TestPositionMapper_ApplyChange_MalformedRange(t *testing.T) {
	content := "hello world"
	mapper := NewPositionMapper(content)

	// Malformed range: start > end (should be handled gracefully)
	r := protocol.Range{
		Start: protocol.Position{Line: 0, Character: 6}, // "world"
		End:   protocol.Position{Line: 0, Character: 0}, // start of line
	}

	result := mapper.ApplyChange(r, "X")
	// Should swap and replace "hello " with "X"
	expected := "Xworld"
	assert.Equal(t, expected, result)
}

func TestRuneCount(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"hello", 5},
		{"Привет", 6},
		{"Активы:Кошелек", 14},
		{"a\U00010400b", 3}, // surrogate is 1 rune
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := RuneCount(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}
