package server

import (
	"testing"
)

func TestRulesTextEditRange_UTF16(t *testing.T) {
	tests := []struct {
		name          string
		line          string
		lineNum       int
		col           int // UTF-16 cursor position
		wantStartChar uint32
		wantEndChar   uint32
	}{
		{
			name:          "ascii only",
			line:          "account1 expenses:food",
			lineNum:       0,
			col:           22,
			wantStartChar: 9,
			wantEndChar:   22,
		},
		{
			name:          "cyrillic before word",
			line:          "Привет skip",
			lineNum:       0,
			col:           11, // 6 Cyrillic (1 UTF-16 each) + 1 space + 4 ascii = 11
			wantStartChar: 7,
			wantEndChar:   11,
		},
		{
			name:          "emoji (2 UTF-16 units) before word",
			line:          "😀 skip",
			lineNum:       0,
			col:           7, // 2 UTF-16 for emoji + 1 space + 4 ascii = 7
			wantStartChar: 3,
			wantEndChar:   7,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rulesTextEditRange(tt.line, tt.lineNum, tt.col)
			if got == nil {
				t.Fatal("expected non-nil range")
			}
			if got.Start.Character != tt.wantStartChar {
				t.Errorf("Start.Character: got %d, want %d", got.Start.Character, tt.wantStartChar)
			}
			if got.End.Character != tt.wantEndChar {
				t.Errorf("End.Character: got %d, want %d", got.End.Character, tt.wantEndChar)
			}
		})
	}
}
