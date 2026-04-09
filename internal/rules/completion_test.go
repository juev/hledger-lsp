package rules

import (
	"strings"
	"testing"
)

func TestComplete_LineStart(t *testing.T) {
	// At start of empty line -> should offer all known directives
	items := Complete("", 0, 0, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, "skip") {
		t.Error("expected 'skip' directive completion")
	}
	if !contains(labels, "fields") {
		t.Error("expected 'fields' directive completion")
	}
	if !contains(labels, "if") {
		t.Error("expected 'if' keyword completion")
	}
}

func TestComplete_FieldsValue(t *testing.T) {
	// After "fields " -> built-in field names
	items := Complete("fields ", 0, 7, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, "date") {
		t.Error("expected 'date' field completion")
	}
	if !contains(labels, "amount") {
		t.Error("expected 'amount' field completion")
	}
}

func TestComplete_SeparatorValue(t *testing.T) {
	items := Complete("separator ", 0, 10, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, ",") {
		t.Error("expected ',' separator completion")
	}
	if !contains(labels, "TAB") {
		t.Error("expected 'TAB' separator completion")
	}
}

func TestComplete_DateFormatValue(t *testing.T) {
	items := Complete("date-format ", 0, 12, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, "%Y-%m-%d") {
		t.Error("expected date format completion to include year-month-day format")
	}
}

func TestComplete_DecimalMarkValue(t *testing.T) {
	items := Complete("decimal-mark ", 0, 13, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, ".") {
		t.Error("expected '.' decimal mark completion")
	}
	if !contains(labels, ",") {
		t.Error("expected ',' decimal mark completion")
	}
}

func TestComplete_BalanceTypeValue(t *testing.T) {
	items := Complete("balance-type ", 0, 13, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, "=") {
		t.Error("expected '=' balance type completion")
	}
	if !contains(labels, "==") {
		t.Error("expected '==' balance type completion")
	}
}

func TestComplete_AccountValue(t *testing.T) {
	// In indented line after account1 -> workspace accounts
	accounts := []string{"expenses:food", "assets:bank"}
	items := Complete("  account1 ", 0, 11, accounts, nil)
	labels := itemLabels(items)
	if !contains(labels, "expenses:food") {
		t.Error("expected 'expenses:food' account completion")
	}
}

func TestComplete_NoCompletionInComment(t *testing.T) {
	// In comment line -> no completions
	items := Complete("# this is a comment", 0, 5, nil, nil)
	if len(items) != 0 {
		t.Errorf("expected no completions in comment, got: %d", len(items))
	}
}

func TestComplete_ColBeyondLineLength(t *testing.T) {
	// col > len(line) falls back to prefix="" -> directive completions
	items := Complete("fields", 0, 100, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, "skip") {
		t.Error("expected directive completions when col > len(line), missing 'skip'")
	}
	if !contains(labels, "if") {
		t.Error("expected directive completions when col > len(line), missing 'if'")
	}
}

// --- isInsideIfBlock tests ---

func TestIsInsideIfBlock_AfterIfKeyword(t *testing.T) {
	// Cursor immediately after "if" line
	lines := strings.Split("if\n", "\n")
	if !isInsideIfBlock(lines, 1) {
		t.Error("expected cursor on line 1 (after 'if') to be inside if block")
	}
}

func TestIsInsideIfBlock_AfterPatternLine(t *testing.T) {
	// Cursor on a new line after a pattern inside the block
	lines := strings.Split("if\n%date 2024\n", "\n")
	if !isInsideIfBlock(lines, 2) {
		t.Error("expected cursor on line 2 (after percent-date pattern) to be inside if block")
	}
}

func TestIsInsideIfBlock_OnPatternLine(t *testing.T) {
	// Cursor on the pattern line itself
	lines := strings.Split("if\n%date 2024\n", "\n")
	if !isInsideIfBlock(lines, 1) {
		t.Error("expected cursor on line 1 (the pattern line) to be inside if block")
	}
}

func TestIsInsideIfBlock_InsideIndentedAssignment(t *testing.T) {
	// Indented assignment line is still inside the block
	lines := strings.Split("if\n%date 2024\n  account1 foo\n", "\n")
	if !isInsideIfBlock(lines, 3) {
		t.Error("expected cursor on line 3 (after indented assignment) to be inside if block")
	}
}

func TestIsInsideIfBlock_AfterTopLevelDirective(t *testing.T) {
	// Top-level directive closes the block
	lines := strings.Split("if\n%date 2024\nskip 1\n", "\n")
	if isInsideIfBlock(lines, 3) {
		t.Error("expected cursor on line 3 (after 'skip 1' top-level directive) NOT to be inside if block")
	}
}

func TestIsInsideIfBlock_NoIfAbove(t *testing.T) {
	lines := strings.Split("skip 1\nfields date\n", "\n")
	if isInsideIfBlock(lines, 2) {
		t.Error("expected cursor on line 2 with no 'if' above NOT to be inside if block")
	}
}

func TestIsInsideIfBlock_EmptyDocument(t *testing.T) {
	lines := strings.Split("", "\n")
	if isInsideIfBlock(lines, 0) {
		t.Error("expected empty document NOT to be inside if block")
	}
}

func TestIsInsideIfBlock_CommentsBetween(t *testing.T) {
	// Comments don't close the block
	lines := strings.Split("if\n# comment\n", "\n")
	if !isInsideIfBlock(lines, 2) {
		t.Error("expected cursor on line 2 (comment between 'if' and cursor) to still be inside if block")
	}
}

func TestIsInsideIfBlock_EmptyLineInBlock(t *testing.T) {
	// Blank line doesn't close the block
	lines := strings.Split("if\n%date 2024\n\n", "\n")
	if !isInsideIfBlock(lines, 3) {
		t.Error("expected cursor on line 3 (blank line between pattern and cursor) to still be inside if block")
	}
}

func TestIsInsideIfBlock_AmpAmpContinuation(t *testing.T) {
	// `&&` continuation line is recognised the same way as `&`
	lines := strings.Split("if\n%payee X\n&& %a", "\n")
	if !isInsideIfBlock(lines, 2) {
		t.Error("expected cursor on '&& %a' continuation line to be inside if block")
	}
}

func TestIsInsideIfBlock_NegationContinuation(t *testing.T) {
	// `!` negation line is a pattern, not a top-level closer
	lines := strings.Split("if\n! %date 2024", "\n")
	if !isInsideIfBlock(lines, 1) {
		t.Error("expected cursor on negation line ('! %' date 2024) to be inside if block")
	}
}

func TestIsInsideIfBlock_AmpAmpNegationContinuation(t *testing.T) {
	// `&& !` AND-NOT continuation
	lines := strings.Split("if\n%payee X\n&& ! %date 2024", "\n")
	if !isInsideIfBlock(lines, 2) {
		t.Error("expected cursor on AND-NOT continuation ('&& ! %' date 2024) to be inside if block")
	}
}

func TestIsInsideIfBlock_RawRegexFirstPattern(t *testing.T) {
	// Raw regex (no `%` prefix) on first pattern line, cursor on next
	// pattern line is still inside the block.
	lines := strings.Split("if\nsomething\n%date 2024", "\n")
	if !isInsideIfBlock(lines, 2) {
		t.Error("expected cursor on field-pattern line below raw regex to be inside if block")
	}
}

func TestIsInsideIfBlock_RawRegexOnPatternLine(t *testing.T) {
	// Cursor sits on the raw-regex pattern line itself.
	lines := strings.Split("if\nsomething", "\n")
	if !isInsideIfBlock(lines, 1) {
		t.Error("expected cursor on raw-regex pattern line itself to be inside if block")
	}
}

func TestIsInsideIfBlock_KnownDirectiveStillCloses(t *testing.T) {
	// Regression: known directive must still close the block.
	lines := strings.Split("if\n%date 2024\nskip 1", "\n")
	if isInsideIfBlock(lines, 2) {
		t.Error("expected cursor on 'skip 1' line to NOT be inside if block (skip is a known directive)")
	}
}

func TestIsInsideIfBlock_BuiltinFieldStillCloses(t *testing.T) {
	// Regression: top-level field assignment must close the block.
	lines := strings.Split("if\n%date 2024\naccount1 assets:bank", "\n")
	if isInsideIfBlock(lines, 2) {
		t.Error("expected cursor on top-level 'account1' assignment to NOT be inside if block")
	}
}

// --- if-block field reference completion tests ---

func TestComplete_InsideIfBlock_WithDeclaredFields(t *testing.T) {
	// fields declared, cursor inside an if block typing a partial %-ref
	doc := "fields date, description, amount\nif\n%d"
	items := Complete(doc, 2, 2, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, "%date") {
		t.Errorf("expected %%date in completions, got %v", labels)
	}
	if !contains(labels, "%description") {
		t.Errorf("expected %%description in completions, got %v", labels)
	}
	if !contains(labels, "%amount") {
		t.Errorf("expected %%amount in completions, got %v", labels)
	}
	if contains(labels, "date-format") {
		t.Errorf("did NOT expect top-level directive date-format inside if block, got %v", labels)
	}
	if contains(labels, "skip") {
		t.Errorf("did NOT expect top-level directive skip inside if block, got %v", labels)
	}
}

func TestComplete_InsideIfBlock_NoDeclaredFields(t *testing.T) {
	// No fields directive — fallback to builtins, still with % prefix
	doc := "if\n%d"
	items := Complete(doc, 1, 2, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, "%date") {
		t.Errorf("expected builtin %%date fallback, got %v", labels)
	}
	if !contains(labels, "%description") {
		t.Errorf("expected builtin %%description fallback, got %v", labels)
	}
	if contains(labels, "date-format") {
		t.Errorf("did NOT expect date-format inside if block, got %v", labels)
	}
}

func TestComplete_InsideIfBlock_ContinuationAmpersand(t *testing.T) {
	// Continuation with '& ' — still inside the block
	doc := "fields payee\nif\n%payee FOO\n& %p"
	items := Complete(doc, 3, 5, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, "%payee") {
		t.Errorf("expected %%payee completion on '& %%p' continuation line, got %v", labels)
	}
}

func TestComplete_InsideIfBlock_ContinuationAmpAmp(t *testing.T) {
	// Continuation with '&&' — also inside the block
	doc := "fields payee, date\nif\n%payee FOO\n&& %d"
	items := Complete(doc, 3, 5, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, "%date") {
		t.Errorf("expected %%date completion on '&& %%d' continuation line, got %v", labels)
	}
	if contains(labels, "date-format") {
		t.Errorf("did NOT expect top-level directive date-format on '&& %%d' line, got %v", labels)
	}
}

func TestComplete_InsideIfBlock_NegationLine(t *testing.T) {
	// Negation with '!' — line is a pattern
	doc := "fields date\nif\n! %d"
	items := Complete(doc, 2, 4, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, "%date") {
		t.Errorf("expected %%date completion on '! %%d' negation line, got %v", labels)
	}
	if contains(labels, "date-format") {
		t.Errorf("did NOT expect top-level directive date-format on '! %%d' line, got %v", labels)
	}
}

func TestComplete_InsideIfBlock_AfterRawRegexPattern(t *testing.T) {
	// Raw regex pattern (no `%`) on first pattern line — cursor on next
	// pattern line still gets field reference completions.
	doc := "fields date, payee\nif\nsomething\n%d"
	items := Complete(doc, 3, 2, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, "%date") {
		t.Errorf("expected %%date completion on field-pattern line below raw regex, got %v", labels)
	}
	if contains(labels, "date-format") {
		t.Errorf("did NOT expect top-level directive date-format below raw regex pattern, got %v", labels)
	}
}

func TestComplete_InsideIfBlock_IndentedAssignment(t *testing.T) {
	// Indented line inside if block: value completion for account1 (accounts)
	// NOT %field completions
	doc := "fields date\nif\n%date 2024\n  account1 "
	accounts := []string{"expenses:food", "assets:bank"}
	items := Complete(doc, 3, 11, accounts, nil)
	labels := itemLabels(items)
	if !contains(labels, "expenses:food") {
		t.Errorf("expected account completions for indented assignment in if block, got %v", labels)
	}
	if contains(labels, "%date") {
		t.Errorf("did NOT expect %%date on indented assignment line, got %v", labels)
	}
}

func TestComplete_NotInsideIfBlock_AfterTopLevelCloses(t *testing.T) {
	// Top-level directive after the if block closes it — skip value completion, not %fields
	doc := "if\n%date 2024\nskip "
	items := Complete(doc, 2, 5, nil, nil)
	labels := itemLabels(items)
	// "skip " is a directive-value context but "skip" has no value completions
	// defined. What matters is we are NOT inside the if block anymore, so we
	// should NOT see %date.
	if contains(labels, "%date") {
		t.Errorf("did NOT expect %%date on line after if-block closed by 'skip', got %v", labels)
	}
}

func TestComplete_InsideIfBlock_EmptyLineInBlock(t *testing.T) {
	// Blank line between pattern and cursor is still inside the block
	doc := "fields date\nif\n\n"
	items := Complete(doc, 2, 0, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, "%date") {
		t.Errorf("expected %%date on blank line inside if block, got %v", labels)
	}
}

func TestComplete_InsideIfBlock_OnIfLineItself(t *testing.T) {
	// Cursor on the 'if' line itself — this is not "inside" a block yet,
	// it IS the if line. Expect top-level directive completions (or at least
	// NOT field references). This matches the user typing 'if' fresh.
	doc := "if"
	items := Complete(doc, 0, 2, nil, nil)
	labels := itemLabels(items)
	if contains(labels, "%date") {
		t.Errorf("did NOT expect %%date when cursor is on the 'if' line itself, got %v", labels)
	}
}

func TestComplete_InsideIfBlock_SameLineAfterIfSpace(t *testing.T) {
	// `if %d` on a single line — cursor after `if ` should offer field refs.
	doc := "fields date, payee\nif %d"
	items := Complete(doc, 1, 5, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, "%date") {
		t.Errorf("expected %%date completion on 'if %%d' inline pattern, got %v", labels)
	}
	if contains(labels, "date-format") {
		t.Errorf("did NOT expect top-level directive date-format on 'if %%d' inline pattern, got %v", labels)
	}
}

func TestComplete_InsideIfBlock_SameLineMultiPattern(t *testing.T) {
	// `if %payee X && %a` — cursor inside the second pattern still gets refs.
	doc := "fields payee, amount\nif %payee X && %a"
	items := Complete(doc, 1, 17, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, "%amount") {
		t.Errorf("expected %%amount completion on 'if %%payee X && %%a' multi-pattern, got %v", labels)
	}
}

func TestComplete_InsideIfBlock_SameLineRawRegexThenField(t *testing.T) {
	// `if something && %d` — cursor inside the named-field part gets refs,
	// even though the first part is a raw regex.
	doc := "fields date\nif something && %d"
	items := Complete(doc, 1, 18, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, "%date") {
		t.Errorf("expected %%date completion on 'if something && %%d', got %v", labels)
	}
}

func TestComplete_OnBareIfKeywordWithSpace(t *testing.T) {
	// `if ` with trailing space — cursor right after the space is in pattern
	// position, should already offer field refs.
	doc := "fields date\nif "
	items := Complete(doc, 1, 3, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, "%date") {
		t.Errorf("expected %%date right after 'if ' space, got %v", labels)
	}
}

// --- field reference interpolation in assignment values ---

func TestComplete_AssignmentValue_FieldRef(t *testing.T) {
	// `description %p` — cursor in value position typing %-prefixed token,
	// should offer field references (not nil, not directives).
	doc := "fields date, payee, description\n  description %p"
	items := Complete(doc, 1, 16, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, "%payee") {
		t.Errorf("expected %%payee in description value completion, got %v", labels)
	}
	if !contains(labels, "%date") {
		t.Errorf("expected %%date in description value completion, got %v", labels)
	}
}

func TestComplete_AssignmentValue_AccountValueWithFieldRef(t *testing.T) {
	// `account1 %p` — cursor on %-token in account1 value should offer
	// field references, NOT workspace accounts.
	doc := "fields date, payee\n  account1 %p"
	accounts := []string{"expenses:food", "assets:bank"}
	items := Complete(doc, 1, 13, accounts, nil)
	labels := itemLabels(items)
	if !contains(labels, "%payee") {
		t.Errorf("expected %%payee in account1 value completion, got %v", labels)
	}
	if contains(labels, "expenses:food") {
		t.Errorf("did NOT expect account expenses:food when cursor word starts with %%, got %v", labels)
	}
}

func TestComplete_AssignmentValue_AccountValueWithoutPercent(t *testing.T) {
	// Regression: `account1 ex` — cursor on plain word should offer accounts.
	doc := "fields date, payee\n  account1 ex"
	accounts := []string{"expenses:food", "assets:bank"}
	items := Complete(doc, 1, 13, accounts, nil)
	labels := itemLabels(items)
	if !contains(labels, "expenses:food") {
		t.Errorf("expected expenses:food account completion, got %v", labels)
	}
	if contains(labels, "%payee") {
		t.Errorf("did NOT expect %%payee in plain account value completion, got %v", labels)
	}
}

func TestComplete_AssignmentValue_FieldRefInsideIfBlock(t *testing.T) {
	// Same case as TestComplete_AssignmentValue_FieldRef, but the assignment
	// lives inside an if block.
	doc := "fields date, payee, description\nif\n%payee Foo\n  description %p"
	items := Complete(doc, 3, 16, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, "%payee") {
		t.Errorf("expected %%payee in description value inside if block, got %v", labels)
	}
}

func TestComplete_AssignmentValue_FieldRefBuiltinFallback(t *testing.T) {
	// No `fields` directive declared — fall back to builtin field names.
	doc := "  description %p"
	items := Complete(doc, 0, 16, nil, nil)
	labels := itemLabels(items)
	if !contains(labels, "%payee") {
		t.Errorf("expected builtin %%payee fallback in description value, got %v", labels)
	}
}

// --- collectDeclaredFields tests ---

func TestCollectDeclaredFields_Single(t *testing.T) {
	rf, _ := Parse("fields date, description, amount")
	got := collectDeclaredFields(rf)
	want := []string{"date", "description", "amount"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectDeclaredFields_Multiple(t *testing.T) {
	// Two fields directives — union, preserving first-seen order, no dups
	rf, _ := Parse("fields date, description\nfields description, amount")
	got := collectDeclaredFields(rf)
	want := []string{"date", "description", "amount"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectDeclaredFields_None(t *testing.T) {
	rf, _ := Parse("skip 1\nseparator ,")
	got := collectDeclaredFields(rf)
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestCollectDeclaredFields_Malformed(t *testing.T) {
	// "fields" with no value — should not panic, returns empty
	rf, _ := Parse("fields\n")
	got := collectDeclaredFields(rf)
	if len(got) != 0 {
		t.Errorf("expected empty slice for fields with no value, got %v", got)
	}
}

func TestCollectDeclaredFields_NilInput(t *testing.T) {
	if got := collectDeclaredFields(nil); got != nil {
		t.Errorf("expected nil for nil input, got %v", got)
	}
}

// --- Complete() with ResolvedRules (transitive includes) ---
// These verify that field-reference completion inside an if block picks up
// field names declared in transitively-included .rules files, which is the
// user-visible fix for issue #24.

func TestComplete_IncludedFields_FieldReference(t *testing.T) {
	// main.rules: no local fields, only the if block. The included
	// common.rules declares `fields date, payee, amount`. Line 2 is a
	// non-indented pattern line inside the block, so completion must go
	// through the field-reference path (%-prefixed labels).
	mainDoc := "include common.rules\nif\n%pa"
	included, _ := Parse("fields date, payee, amount\n")

	mainRf, _ := Parse(mainDoc)
	resolved := NewResolvedRules(mainRf)
	resolved.Files["/tmp/common.rules"] = included
	resolved.FileOrder = []string{"/tmp/common.rules"}

	// Cursor at end of "%pa" on line 2.
	items := Complete(mainDoc, 2, 3, nil, resolved)
	labels := itemLabels(items)

	for _, want := range []string{"%date", "%payee", "%amount"} {
		if !contains(labels, want) {
			t.Errorf("expected %q in field-reference completions from included file, got %v", want, labels)
		}
	}
}

func TestComplete_IncludedFields_ValueInterpolation(t *testing.T) {
	// Inside an if block, value position of account1: `account1 %pa` should
	// offer %payee because payee is declared in the included file.
	mainDoc := "include common.rules\nif\n  account1 %"
	included, _ := Parse("fields date, payee, amount\n")

	mainRf, _ := Parse(mainDoc)
	resolved := NewResolvedRules(mainRf)
	resolved.Files["/tmp/common.rules"] = included
	resolved.FileOrder = []string{"/tmp/common.rules"}

	// Cursor at end of "  account1 %" (col 12 in UTF-16 code units).
	items := Complete(mainDoc, 2, 12, nil, resolved)
	labels := itemLabels(items)

	if !contains(labels, "%payee") {
		t.Errorf("expected %%payee from included fields in value position, got %v", labels)
	}
}

func TestComplete_NilResolved_FallbackToLocal(t *testing.T) {
	// When resolved is nil, Complete must behave as it did before #24:
	// only fields declared in doc itself are visible via the %-prefixed
	// field-reference path.
	doc := "fields date, payee\nif\n%"
	items := Complete(doc, 2, 1, nil, nil)
	labels := itemLabels(items)

	if !contains(labels, "%payee") {
		t.Errorf("expected local %%payee with nil resolved, got %v", labels)
	}
	// Sanity: cross-file field that does not exist locally must NOT appear.
	if contains(labels, "%foreign") {
		t.Errorf("unexpected %%foreign leaked in with nil resolved: %v", labels)
	}
}

// --- collectDeclaredFieldsResolved tests ---
// These guard the transitive-include case introduced for issue #24. When
// `resolved` is nil, behaviour must be identical to collectDeclaredFields(fallback)
// so that every existing caller keeps its current semantics.

func TestCollectDeclaredFieldsResolved_NilResolvedFallbackUsed(t *testing.T) {
	rf, _ := Parse("fields date, payee, amount\n")
	got := collectDeclaredFieldsResolved(nil, rf)
	want := []string{"date", "payee", "amount"}
	if !equalStrings(got, want) {
		t.Errorf("nil resolved + fallback: got %v, want %v", got, want)
	}
}

func TestCollectDeclaredFieldsResolved_BothNil(t *testing.T) {
	if got := collectDeclaredFieldsResolved(nil, nil); got != nil {
		t.Errorf("both nil: expected nil, got %v", got)
	}
}

func TestCollectDeclaredFieldsResolved_PrimaryOnly(t *testing.T) {
	primary, _ := Parse("fields date, amount\n")
	resolved := NewResolvedRules(primary)
	got := collectDeclaredFieldsResolved(resolved, primary)
	want := []string{"date", "amount"}
	if !equalStrings(got, want) {
		t.Errorf("primary only: got %v, want %v", got, want)
	}
}

func TestCollectDeclaredFieldsResolved_FieldsFromInclude(t *testing.T) {
	// Primary has no fields; the transitively-included file declares them.
	primary, _ := Parse("skip 1\ninclude common.rules\n")
	included, _ := Parse("fields date, payee, amount\n")

	resolved := NewResolvedRules(primary)
	resolved.Files["/tmp/common.rules"] = included
	resolved.FileOrder = []string{"/tmp/common.rules"}

	got := collectDeclaredFieldsResolved(resolved, primary)
	want := []string{"date", "payee", "amount"}
	if !equalStrings(got, want) {
		t.Errorf("fields from include: got %v, want %v", got, want)
	}
}

func TestCollectDeclaredFieldsResolved_PrimaryBeforeIncluded(t *testing.T) {
	// When both Primary and an included file declare fields, Primary wins
	// the first-seen slots, and the included file only contributes unseen
	// names. Order reflects traversal: Primary → FileOrder.
	primary, _ := Parse("fields date, payee\n")
	included, _ := Parse("fields payee, amount, description\n")

	resolved := NewResolvedRules(primary)
	resolved.Files["/tmp/common.rules"] = included
	resolved.FileOrder = []string{"/tmp/common.rules"}

	got := collectDeclaredFieldsResolved(resolved, primary)
	want := []string{"date", "payee", "amount", "description"}
	if !equalStrings(got, want) {
		t.Errorf("merged order/dedup: got %v, want %v", got, want)
	}
}

func TestCollectDeclaredFieldsResolved_MultipleIncludesOrdered(t *testing.T) {
	primary, _ := Parse("skip 1\n")
	first, _ := Parse("fields a, b\n")
	second, _ := Parse("fields c, d\n")

	resolved := NewResolvedRules(primary)
	resolved.Files["/tmp/first.rules"] = first
	resolved.Files["/tmp/second.rules"] = second
	resolved.FileOrder = []string{"/tmp/first.rules", "/tmp/second.rules"}

	got := collectDeclaredFieldsResolved(resolved, primary)
	want := []string{"a", "b", "c", "d"}
	if !equalStrings(got, want) {
		t.Errorf("multiple includes: got %v, want %v", got, want)
	}
}

func TestCollectDeclaredFieldsResolved_NilPrimaryFallbackToIncludes(t *testing.T) {
	// resolved is non-nil but Primary is nil (unlikely but defensible):
	// should still pull from Files.
	included, _ := Parse("fields date\n")
	resolved := &ResolvedRules{
		Files:     map[string]*RulesFile{"/tmp/common.rules": included},
		FileOrder: []string{"/tmp/common.rules"},
	}
	got := collectDeclaredFieldsResolved(resolved, nil)
	want := []string{"date"}
	if !equalStrings(got, want) {
		t.Errorf("nil primary: got %v, want %v", got, want)
	}
}

// --- helpers ---

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func itemLabels(items []CompletionItem) []string {
	labels := make([]string, len(items))
	for i, item := range items {
		labels[i] = item.Label
	}
	return labels
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
