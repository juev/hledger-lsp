package textutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeLineEndings(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "LF unchanged", input: "line1\nline2\nline3", expected: "line1\nline2\nline3"},
		{name: "CRLF to LF", input: "line1\r\nline2\r\nline3", expected: "line1\nline2\nline3"},
		{name: "bare CR to LF", input: "line1\rline2\rline3", expected: "line1\nline2\nline3"},
		{name: "mixed line endings", input: "line1\r\nline2\nline3\rline4", expected: "line1\nline2\nline3\nline4"},
		{name: "trailing CR", input: "line1\r", expected: "line1\n"},
		{name: "empty string", input: "", expected: ""},
		{name: "no line endings", input: "hello", expected: "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, NormalizeLineEndings(tt.input))
		})
	}
}
