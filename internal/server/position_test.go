package server

import "testing"

func TestUTF16Len(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "", 0},
		{"ascii", "hello", 5},
		{"cyrillic", "Активы", 6},
		{"cyrillic with colon", "Активы:Банк", 11},
		{"chinese", "资产:银行", 5},
		{"emoji surrogate", "😀", 2},
		{"mixed ascii and cyrillic", "assets:Активы", 13},
		{"emoji in text", "hello😀world", 12},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := utf16Len(tt.input)
			if got != tt.want {
				t.Errorf("utf16Len(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestUTF16OffsetToByteOffset(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		utf16Off    int
		wantByteOff int
	}{
		{"empty at 0", "", 0, 0},
		{"ascii at 0", "hello", 0, 0},
		{"ascii at 3", "hello", 3, 3},
		{"ascii at end", "hello", 5, 5},
		{"cyrillic at 0", "Активы", 0, 0},
		{"cyrillic at 3", "Активы", 3, 6},
		{"cyrillic at end", "Активы", 6, 12},
		{"cyrillic with colon at colon", "Активы:Банк", 6, 12},
		{"emoji at 0", "😀", 0, 0},
		{"emoji after", "😀", 2, 4},
		{"mixed after emoji", "a😀b", 3, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := utf16OffsetToByteOffset(tt.input, tt.utf16Off)
			if got != tt.wantByteOff {
				t.Errorf("utf16OffsetToByteOffset(%q, %d) = %d, want %d", tt.input, tt.utf16Off, got, tt.wantByteOff)
			}
		})
	}
}
