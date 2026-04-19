package filetype_test

import (
	"testing"

	"github.com/juev/hledger-lsp/internal/filetype"
)

func TestDetect(t *testing.T) {
	tests := []struct {
		uri  string
		want filetype.FileType
	}{
		{"file:///home/user/main.journal", filetype.Journal},
		{"file:///home/user/main.hledger", filetype.Journal},
		{"file:///home/user/main.j", filetype.Journal},
		{"file:///home/user/main.ledger", filetype.Journal},
		{"file:///home/user/2026.prices", filetype.Journal},
		{"file:///home/user/2026.PRICES", filetype.Journal},
		{"file:///home/user/import.rules", filetype.Rules},
		{"file:///home/user/bank.rules", filetype.Rules},
		{"file:///home/user/README.md", filetype.Unknown},
		{"file:///home/user/data.csv", filetype.Unknown},
		{"", filetype.Unknown},
		{"file:///home/user/noext", filetype.Unknown},
	}

	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			got := filetype.Detect(tt.uri)
			if got != tt.want {
				t.Errorf("Detect(%q) = %v, want %v", tt.uri, got, tt.want)
			}
		})
	}
}

func TestIsJournal(t *testing.T) {
	if !filetype.IsJournal("file:///a.journal") {
		t.Error("expected IsJournal to be true for .journal")
	}
	if filetype.IsJournal("file:///a.rules") {
		t.Error("expected IsJournal to be false for .rules")
	}
}

func TestIsRules(t *testing.T) {
	if !filetype.IsRules("file:///a.rules") {
		t.Error("expected IsRules to be true for .rules")
	}
	if filetype.IsRules("file:///a.journal") {
		t.Error("expected IsRules to be false for .journal")
	}
}

func TestIsJournalPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/home/user/main.journal", true},
		{"/home/user/main.hledger", true},
		{"/home/user/main.j", true},
		{"/home/user/2026.prices", true},
		{"/home/user/2026.PRICES", true},
		{"/home/user/main.ledger", true},
		{"/home/user/main.JOURNAL", true},
		{"/home/user/main.Journal", true},
		{"/home/user/bank.rules", false},
		{"/home/user/data.csv", false},
		{"/home/user/notes.txt", false},
		{"/home/user/noext", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := filetype.IsJournalPath(tt.path)
			if got != tt.want {
				t.Errorf("IsJournalPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestFileType_String(t *testing.T) {
	tests := []struct {
		ft   filetype.FileType
		want string
	}{
		{filetype.Journal, "journal"},
		{filetype.Rules, "rules"},
		{filetype.Unknown, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.ft.String()
			if got != tt.want {
				t.Errorf("FileType(%d).String() = %q, want %q", tt.ft, got, tt.want)
			}
		})
	}
}
