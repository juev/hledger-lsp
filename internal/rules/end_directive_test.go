package rules

import (
	"testing"
)

func TestLexer_EndWithValue_ProducesTokenText(t *testing.T) {
	// "end value" should not be treated as a directive since "end" takes no value.
	// The lexer should fall through to TokenText for "end value".
	input := "end value"
	l := NewLexer(input)
	tokens := collectTokens(l)
	types := tokenTypes(tokens)

	if len(types) < 1 {
		t.Fatal("expected at least one token")
	}
	if types[0] == TokenDirective {
		t.Errorf("token[0]: got TokenDirective, want TokenText for 'end value'")
	}
	if types[0] != TokenText {
		t.Errorf("token[0]: got %v, want TokenText for 'end value'", types[0])
	}
}

func TestLexer_EndAlone_ProducesEndKeyword(t *testing.T) {
	// "end" alone should still produce TokenEndKeyword.
	input := "end"
	l := NewLexer(input)
	tokens := collectTokens(l)
	types := tokenTypes(tokens)

	if len(types) < 1 {
		t.Fatal("expected at least one token")
	}
	if types[0] != TokenEndKeyword {
		t.Errorf("token[0]: got %v, want TokenEndKeyword for 'end'", types[0])
	}
}

func TestComplete_EndInDirectiveCompletions(t *testing.T) {
	// "end" should appear in directive completions even after being removed from KnownDirectives.
	items := Complete("", 0, 0, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, "end") {
		t.Error("expected 'end' in directive completions")
	}
}
