package rules

import (
	"testing"
)

func TestComplete_UTF16Column(t *testing.T) {
	// "fields Ä" = 7 ASCII + "Ä" (1 UTF-16 unit, 2 bytes) = 9 bytes, 8 UTF-16 units.
	// With col=20 (UTF-16, past end):
	//   old code: 20 > 9 (byte len) → prefix="" → directiveCompletions, no "date"
	//   fixed code: UTF16OffsetToByteOffset("fields Ä", 20) = 9 (clamped) → prefix="fields Ä"
	//              → HasPrefix("fields Ä", "fields ") → fieldNameCompletions, has "date"
	line := "fields Ä"
	items := Complete(line, 0, 20, nil)
	labels := itemLabels(items)
	if !contains(labels, "date") {
		t.Errorf("expected field name completions when UTF-16 col is beyond line end on non-ASCII line, missing 'date'; got %v", labels)
	}

	// Sanity: comment line with non-ASCII should return nil
	commentLine := "# комментарий"
	items2 := Complete(commentLine, 0, 3, nil)
	if items2 != nil {
		t.Errorf("expected nil completions in comment line, got %d items", len(items2))
	}
}
