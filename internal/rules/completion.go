package rules

import (
	"strings"

	"github.com/juev/hledger-lsp/internal/lsputil"
)

// CompletionItem is a simple completion suggestion for rules files.
type CompletionItem struct {
	Label  string
	Detail string
	Kind   CompletionItemKind
}

// CompletionItemKind mirrors LSP completion item kinds relevant to rules.
type CompletionItemKind int

const (
	KindKeyword  CompletionItemKind = 14
	KindField    CompletionItemKind = 5
	KindValue    CompletionItemKind = 12
	KindVariable CompletionItemKind = 6
)

// Complete returns completion items for the given cursor position in a rules file.
//
// Parameters:
//   - doc: the full document text (required for if-block context detection)
//   - lineNum: 0-based line number of the cursor
//   - col: 0-based cursor column in UTF-16 code units (as used by LSP)
//   - workspaceAccounts: account names from the journal workspace (may be nil)
//   - resolvedIncludes: the transitively-resolved include chain of this .rules
//     file, produced by rules.Loader. When non-nil, declared-field lookups walk
//     Primary plus every included file (issue #24). When nil, behaviour is
//     identical to pre-#24: only fields declared in doc itself are visible.
func Complete(doc string, lineNum int, col int, workspaceAccounts []string, resolvedIncludes *ResolvedRules) []CompletionItem {
	lines := strings.Split(doc, "\n")
	line := ""
	if lineNum >= 0 && lineNum < len(lines) {
		line = lines[lineNum]
	}

	// No completions in comments
	if len(line) > 0 && (line[0] == '#' || line[0] == ';' || line[0] == '*') {
		return nil
	}

	byteCol := lsputil.UTF16OffsetToByteOffset(line, col)
	prefix := line[:byteCol]

	// Lazy parse: declaredFields is computed on first access. Used by
	// pattern-context and value-side `%fieldname` completions. When the
	// caller supplies a resolved include chain, transitively-declared fields
	// are merged in (issue #24).
	var declaredFields []string
	var declaredFieldsLoaded bool
	getDeclaredFields := func() []string {
		if !declaredFieldsLoaded {
			rf, _ := Parse(doc)
			declaredFields = collectDeclaredFieldsResolved(resolvedIncludes, rf)
			declaredFieldsLoaded = true
		}
		return declaredFields
	}

	// Cursor on the `if` line itself, in pattern position (after `if<space>`
	// or `if<tab>`). The rest of the inline pattern list — including raw
	// regex, `&&`, `!`, etc. — is treated as pattern context here, since
	// hledger allows patterns to begin on the same line as `if`.
	if isInlineIfPatternPosition(prefix) {
		return fieldReferenceCompletions(getDeclaredFields())
	}

	// Inside an if block, non-indented lines are patterns referencing fields via %name.
	// Indented lines inside the block are assignments (account1 …) and use the
	// regular indented-line path so that account value completions still work.
	if isInsideIfBlock(lines, lineNum) {
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			return completeIndentedLine(prefix, workspaceAccounts, getDeclaredFields)
		}
		return fieldReferenceCompletions(getDeclaredFields())
	}

	// Start of line (empty or no significant prefix) -> directives
	if strings.TrimSpace(prefix) == "" {
		return directiveCompletions()
	}

	// Indented line: field name value
	if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
		return completeIndentedLine(prefix, workspaceAccounts, getDeclaredFields)
	}

	// Top-level directive value completions
	return completeDirectiveValue(prefix)
}

// isInsideIfBlock reports whether the cursor on line lineNum is inside an if block.
//
// The `if` line itself is NOT considered "inside" — the block opens for lines
// below it. Therefore lineNum == 0 can never be inside a block.
//
// The cursor line's own content is checked first: if it is clearly a top-level
// closer (unindented known directive or field assignment, not a pattern/continuation),
// the block is closed and we return false without scanning upward. This lets
// us correctly handle the transition moment when the user types a new directive
// that terminates the preceding block.
//
// Otherwise, scan lines upward from lineNum-1, skipping lines that can
// legitimately appear inside an if block (blank, comment, continuation '&'/'&&',
// pattern '%', negation '!', raw regex, or indented assignment). The first
// "significant" unindented line we hit decides: if it starts with 'if', the
// cursor is inside a block; any KnownDirective or builtin field assignment
// means we were never in one.
//
// This scan is intentionally independent of the parser's IfBlock.Range: while
// the user is typing, the current block may not yet be closed, so the parsed
// Range can end before the cursor line. Line-level scanning stays correct in
// that partial-file state.
func isInsideIfBlock(lines []string, lineNum int) bool {
	if lineNum <= 0 {
		return false
	}

	// The cursor line itself may close the block if it is an unindented
	// top-level directive / assignment (not a pattern, continuation, comment
	// or 'if' itself).
	if lineNum < len(lines) {
		cursorLine := lines[lineNum]
		if !isContinuationOfIfBlock(cursorLine) && isTopLevelCloser(cursorLine) {
			return false
		}
	}

	for i := lineNum - 1; i >= 0; i-- {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Blank lines and comments do not close an if block.
		if trimmed == "" || isCommentLine(trimmed) {
			continue
		}

		// Indented lines: either assignments inside the block or continuation
		// of something higher. Keep scanning upward until we hit an unindented
		// anchor.
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			continue
		}

		// Unindented line: this is the decisive anchor.
		if trimmed == "if" || strings.HasPrefix(trimmed, "if ") || strings.HasPrefix(trimmed, "if\t") {
			return true
		}
		// A pattern operator line (`%`, `&`, `&&`, `!`, `&& !`) means we are
		// still inside a block opened above — keep scanning upward.
		if looksLikePatternLine(trimmed) {
			continue
		}
		// Raw regex pattern: any unindented text whose first word is not a
		// known top-level directive or builtin field assignment is treated as
		// a whole-record pattern, so we keep scanning upward.
		if !firstWordIsTopLevelKeyword(trimmed) {
			continue
		}
		// Known top-level directive or field assignment: closes the block.
		return false
	}
	return false
}

// isContinuationOfIfBlock reports whether the line is a pattern, continuation,
// comment, blank line, or indented — i.e., it does not itself close an if block.
func isContinuationOfIfBlock(line string) bool {
	if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
		return true
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || isCommentLine(trimmed) {
		return true
	}
	return looksLikePatternLine(trimmed)
}

// isTopLevelCloser reports whether the line has unindented content that would
// terminate an if block (a known directive, a builtin field assignment, or
// the 'if' keyword starting a new block).
//
// Unknown unindented text is NOT a closer — it is treated as a possible
// raw-regex pattern (whole-record match), per hledger CSV rules grammar.
func isTopLevelCloser(line string) bool {
	if len(line) == 0 {
		return false
	}
	if line[0] == ' ' || line[0] == '\t' {
		return false
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || isCommentLine(trimmed) {
		return false
	}
	if looksLikePatternLine(trimmed) {
		return false
	}
	// Only known top-level directives or builtin field assignments close
	// the block. Anything else is a possible raw-regex pattern.
	return firstWordIsTopLevelKeyword(trimmed)
}

// firstWordIsTopLevelKeyword reports whether the first word of trimmed is
// a known top-level keyword (directive, `if`, `end`) or a builtin field name
// (which would form an unindented field assignment). Anything else is treated
// as a raw-regex pattern by the if-block heuristic.
func firstWordIsTopLevelKeyword(trimmed string) bool {
	word := firstWord(trimmed)
	if word == "" {
		return false
	}
	if word == "if" || word == "end" {
		return true
	}
	if knownDirectives[word] {
		return true
	}
	return isBuiltinField(word)
}

// isInlineIfPatternPosition reports whether `prefix` (the part of the cursor
// line up to the cursor) places the cursor in the inline pattern position of
// an `if` line — i.e. the line begins with `if` followed by whitespace, and
// the cursor is past that whitespace.
//
// The bare keyword (`if`, `if|`) is intentionally NOT in pattern position:
// the user is still typing the keyword and should see top-level directive
// completions there. The space after `if` is the trigger.
func isInlineIfPatternPosition(prefix string) bool {
	if len(prefix) < 3 {
		return false
	}
	if prefix[0] != 'i' || prefix[1] != 'f' {
		return false
	}
	if prefix[2] != ' ' && prefix[2] != '\t' {
		return false
	}
	return true
}

// looksLikePatternLine reports whether a trimmed line looks like a pattern
// (or pattern operator) that can appear inside an if block. Recognised forms:
//
//   - `%fieldname …` — named-field match
//   - `&  …` / `&& …` — AND continuation (single or double ampersand)
//   - `!  …` — NOT negation
//   - `&& !` / `& !` — AND-NOT continuation (covered by the `&` prefix)
//
// Raw-regex patterns (`something` without any operator prefix) are NOT
// classified here; they are handled separately by the blacklist check on
// top-level keywords because they look identical to unknown directives.
func looksLikePatternLine(trimmed string) bool {
	if trimmed == "" {
		return false
	}
	switch trimmed[0] {
	case '%', '&', '!':
		return true
	}
	return false
}

func isCommentLine(trimmed string) bool {
	return strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "*")
}

// fieldReferenceCompletions returns %-prefixed completion items for the given
// declared field names. If declared is empty, it falls back to the builtin
// field names so that users still get meaningful suggestions in an otherwise
// empty file.
func fieldReferenceCompletions(declared []string) []CompletionItem {
	if len(declared) > 0 {
		items := make([]CompletionItem, 0, len(declared))
		for _, name := range declared {
			items = append(items, CompletionItem{
				Label:  "%" + name,
				Detail: "field reference",
				Kind:   KindField,
			})
		}
		return items
	}
	items := make([]CompletionItem, 0, len(BuiltinFieldNames))
	for _, name := range BuiltinFieldNames {
		items = append(items, CompletionItem{
			Label:  "%" + name,
			Detail: "builtin field reference",
			Kind:   KindField,
		})
	}
	return items
}

// collectDeclaredFields returns the union of field names declared by all
// fields directives in the rules file, in first-seen order.
func collectDeclaredFields(rf *RulesFile) []string {
	if rf == nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	appendFieldsFrom(rf, seen, &out)
	return out
}

// collectDeclaredFieldsResolved returns the union of field names declared by
// fields directives across a transitively-resolved include chain. Primary is
// walked first, then Files in FileOrder, so that the primary file's fields
// win the first-seen slots and included files contribute only previously
// unseen names.
//
// When resolved is nil, behaviour is identical to collectDeclaredFields(fallback),
// which guarantees regression safety for callers that do not yet have a
// resolved include chain (e.g. unit tests that pass a bare document).
func collectDeclaredFieldsResolved(resolved *ResolvedRules, fallback *RulesFile) []string {
	if resolved == nil {
		return collectDeclaredFields(fallback)
	}
	seen := make(map[string]bool)
	var out []string
	appendFieldsFrom(resolved.Primary, seen, &out)
	for _, path := range resolved.FileOrder {
		appendFieldsFrom(resolved.Files[path], seen, &out)
	}
	return out
}

// appendFieldsFrom adds every non-empty field name declared in rf to out,
// skipping names already present in seen. rf may be nil.
func appendFieldsFrom(rf *RulesFile, seen map[string]bool, out *[]string) {
	if rf == nil {
		return
	}
	for _, fd := range rf.FieldsDefs {
		for _, name := range fd.Names {
			name = strings.TrimSpace(name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			*out = append(*out, name)
		}
	}
}

func completeDirectiveValue(prefix string) []CompletionItem {
	switch {
	case strings.HasPrefix(prefix, "fields "):
		return fieldNameCompletions()

	case strings.HasPrefix(prefix, "separator "):
		return separatorCompletions()

	case strings.HasPrefix(prefix, "date-format "):
		return dateFormatCompletions()

	case strings.HasPrefix(prefix, "decimal-mark "):
		return decimalMarkCompletions()

	case strings.HasPrefix(prefix, "balance-type "):
		return balanceTypeCompletions()

	case strings.HasPrefix(prefix, "include "):
		return nil

	case strings.HasPrefix(prefix, "source "):
		return nil
	}

	return directiveCompletions()
}

func completeIndentedLine(prefix string, workspaceAccounts []string, declaredFields func() []string) []CompletionItem {
	trimmed := strings.TrimLeft(prefix, " \t")
	if trimmed == "" {
		return fieldNameCompletions()
	}

	word, rest := splitWord(trimmed)
	if isBuiltinField(word) && rest != "" {
		// After a field name we are in value position. If the user is
		// currently typing a `%`-prefixed token (interpolation reference),
		// offer field references — this overrides the account completion
		// because account names cannot start with `%`.
		if cursorWordStartsWithPercent(prefix) {
			return fieldReferenceCompletions(declaredFields())
		}
		// Otherwise complete based on the field type.
		if strings.HasPrefix(word, "account") {
			return accountCompletions(workspaceAccounts)
		}
		return nil
	}

	// Partial field name
	return fieldNameCompletions()
}

// cursorWordStartsWithPercent reports whether the last whitespace-delimited
// word in `prefix` (the line up to the cursor) begins with `%`. This is the
// signal that the user is typing a field reference (`%payee`, `%date`, …).
func cursorWordStartsWithPercent(prefix string) bool {
	// Walk back from end of prefix until we hit whitespace.
	end := len(prefix)
	start := end
	for start > 0 {
		c := prefix[start-1]
		if c == ' ' || c == '\t' {
			break
		}
		start--
	}
	if start == end {
		return false
	}
	return prefix[start] == '%'
}

func directiveCompletions() []CompletionItem {
	// KnownDirectives covers all top-level directives; "if" and "end" are added as keywords.
	items := make([]CompletionItem, 0, len(KnownDirectives)+2)
	for _, d := range KnownDirectives {
		items = append(items, CompletionItem{Label: d.Name, Detail: d.Detail, Kind: KindKeyword})
	}
	items = append(items, CompletionItem{Label: "if", Detail: "conditional block", Kind: KindKeyword})
	items = append(items, CompletionItem{Label: "end", Detail: "stop processing", Kind: KindKeyword})
	return items
}

func fieldNameCompletions() []CompletionItem {
	items := make([]CompletionItem, len(BuiltinFieldNames))
	for i, f := range BuiltinFieldNames {
		items[i] = CompletionItem{Label: f, Detail: "field name", Kind: KindField}
	}
	return items
}

func separatorCompletions() []CompletionItem {
	seps := []struct{ label, detail string }{
		{",", "comma"},
		{";", "semicolon"},
		{"|", "pipe"},
		{"TAB", "tab character"},
		{"SPACE", "space character"},
	}
	items := make([]CompletionItem, len(seps))
	for i, s := range seps {
		items[i] = CompletionItem{Label: s.label, Detail: s.detail, Kind: KindValue}
	}
	return items
}

func dateFormatCompletions() []CompletionItem {
	formats := []string{
		"%Y-%m-%d",
		"%m/%d/%Y",
		"%d/%m/%Y",
		"%Y/%m/%d",
		"%d-%m-%Y",
		"%m-%d-%Y",
		"%d.%m.%Y",
		"%Y.%m.%d",
	}
	items := make([]CompletionItem, len(formats))
	for i, f := range formats {
		items[i] = CompletionItem{Label: f, Detail: "date format", Kind: KindValue}
	}
	return items
}

func decimalMarkCompletions() []CompletionItem {
	return []CompletionItem{
		{Label: ".", Detail: "period (US/UK)", Kind: KindValue},
		{Label: ",", Detail: "comma (EU)", Kind: KindValue},
	}
}

func balanceTypeCompletions() []CompletionItem {
	return []CompletionItem{
		{Label: "=", Detail: "partial balance assertion", Kind: KindValue},
		{Label: "==", Detail: "total balance assertion", Kind: KindValue},
		{Label: "=*", Detail: "partial balance assignment", Kind: KindValue},
		{Label: "==*", Detail: "total balance assignment", Kind: KindValue},
	}
}

func accountCompletions(workspaceAccounts []string) []CompletionItem {
	if len(workspaceAccounts) == 0 {
		return nil
	}
	items := make([]CompletionItem, len(workspaceAccounts))
	for i, acc := range workspaceAccounts {
		items[i] = CompletionItem{Label: acc, Detail: "account", Kind: KindVariable}
	}
	return items
}
