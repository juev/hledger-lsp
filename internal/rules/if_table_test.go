package rules

import (
	"reflect"
	"strings"
	"testing"
)

// issue87Rules is the reproduction from issue #87.
const issue87Rules = `skip 1
fields date, description, amount
account1 assets:checking

if,account2,comment
WHOLE FOODS,expenses:groceries,food
SHELL,expenses:auto:fuel,
NETFLIX,expenses:subscriptions,streaming

if;description
%description groceries|food;expenses:food
`

func diagCodes(diags []Diagnostic) []string {
	codes := make([]string, 0, len(diags))
	for _, d := range diags {
		codes = append(codes, d.Code)
	}
	return codes
}

func TestIfTableSeparator(t *testing.T) {
	tests := []struct {
		line    string
		wantSep rune
		wantOK  bool
	}{
		{"if,account2", ',', true},
		{"if;description", ';', true},
		{"if|account2|comment", '|', true},
		{"if%description", '%', true},
		{"if→account2", '→', true},
		{"if", 0, false},
		{"if %payee Amazon", 0, false},
		{"if\tAmazon", 0, false},
		{"iffy,foo", 0, false},
		{"if1,foo", 0, false},
		{"description if,foo", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			sep, ok := ifTableSeparator(tt.line)
			if sep != tt.wantSep || ok != tt.wantOK {
				t.Errorf("ifTableSeparator(%q) = %q, %v; want %q, %v", tt.line, sep, ok, tt.wantSep, tt.wantOK)
			}
		})
	}
}

func TestLexer_IfTable(t *testing.T) {
	input := "if,account2,comment\nWHOLE FOODS,expenses:groceries,food\n# note\nSHELL,expenses:auto:fuel,\n\nWHOLE"
	tokens := collectTokens(NewLexer(input))
	want := []TokenType{
		TokenIfKeyword, TokenSeparator, TokenFieldName, TokenSeparator, TokenFieldName, TokenNewline,
		TokenRegex, TokenSeparator, TokenText, TokenSeparator, TokenText, TokenNewline,
		TokenComment, TokenNewline,
		TokenRegex, TokenSeparator, TokenText, TokenSeparator, TokenNewline,
		TokenNewline,
		// After the blank line the table is over, so this is plain text again.
		TokenText,
		TokenEOF,
	}
	if got := tokenTypes(tokens); !reflect.DeepEqual(got, want) {
		t.Fatalf("token types:\ngot  %v\nwant %v", got, want)
	}

	row := tokens[6]
	if row.Value != "WHOLE FOODS" || row.Pos.Column != 1 || row.End.Column != 12 {
		t.Errorf("matcher token: got %q at %d-%d", row.Value, row.Pos.Column, row.End.Column)
	}
	value := tokens[8]
	if value.Value != "expenses:groceries" || value.Pos.Column != 13 || value.Pos.Offset != 20+12 {
		t.Errorf("value token: got %q at column %d offset %d", value.Value, value.Pos.Column, value.Pos.Offset)
	}
}

func TestLexer_IfTableMultiByteSeparator(t *testing.T) {
	tokens := collectTokens(NewLexer("if→account2\nfoo→expenses:x"))
	want := []TokenType{
		TokenIfKeyword, TokenSeparator, TokenFieldName, TokenNewline,
		TokenRegex, TokenSeparator, TokenText, TokenEOF,
	}
	if got := tokenTypes(tokens); !reflect.DeepEqual(got, want) {
		t.Fatalf("token types:\ngot  %v\nwant %v", got, want)
	}
	field := tokens[2]
	if field.Value != "account2" || field.Pos.Column != 1+len("if→") {
		t.Errorf("field token: got %q at column %d", field.Value, field.Pos.Column)
	}
}

func TestParse_Issue87IfTables(t *testing.T) {
	rf, parseDiags := Parse(issue87Rules)
	if diags := Diagnostics(rf, parseDiags); len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}

	if len(rf.IfTables) != 2 {
		t.Fatalf("if tables: got %d, want 2", len(rf.IfTables))
	}
	if got := rf.IfTables[0].Fields; !reflect.DeepEqual(got, []string{"account2", "comment"}) {
		t.Errorf("first table fields: got %v", got)
	}
	if got := rf.IfTables[1].Fields; !reflect.DeepEqual(got, []string{"description"}) {
		t.Errorf("second table fields: got %v", got)
	}

	type assignment struct{ Field, Value string }
	type block struct {
		Patterns    []string
		Assignments []assignment
	}
	want := []block{
		{[]string{"WHOLE FOODS"}, []assignment{{"account2", "expenses:groceries"}, {"comment", "food"}}},
		{[]string{"SHELL"}, []assignment{{"account2", "expenses:auto:fuel"}, {"comment", ""}}},
		{[]string{"NETFLIX"}, []assignment{{"account2", "expenses:subscriptions"}, {"comment", "streaming"}}},
		{[]string{"%description groceries|food"}, []assignment{{"description", "expenses:food"}}},
	}
	var got []block
	for _, b := range rf.IfBlocks {
		var as []assignment
		for _, a := range b.Assignments {
			as = append(as, assignment{a.Field, a.Value})
		}
		got = append(got, block{b.Patterns, as})
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("if blocks:\ngot  %+v\nwant %+v", got, want)
	}

	// Rows are single-line blocks; the table spans its header to its last row.
	first := rf.IfBlocks[0]
	if first.Range.Start.Line != 6 || first.Range.End.Line != 6 {
		t.Errorf("first row range: got lines %d-%d, want 6-6", first.Range.Start.Line, first.Range.End.Line)
	}
	if r := rf.IfTables[0].Range; r.Start.Line != 5 || r.End.Line != 8 {
		t.Errorf("first table range: got lines %d-%d, want 5-8", r.Start.Line, r.End.Line)
	}

	// Directives and top-level assignments around the tables still parse.
	if len(rf.Directives) != 1 || len(rf.FieldsDefs) != 1 || len(rf.Assignments) != 1 {
		t.Errorf("got %d directives, %d fields, %d assignments; want 1 each",
			len(rf.Directives), len(rf.FieldsDefs), len(rf.Assignments))
	}
}

func TestParse_IfTableEmptyValueRange(t *testing.T) {
	rf, _ := Parse("if,account2,comment\nSHELL,expenses:auto:fuel,")
	comment := rf.IfBlocks[0].Assignments[1]
	if comment.Range.Start != comment.Range.End || comment.Range.Start.Column != len("SHELL,expenses:auto:fuel,")+1 {
		t.Errorf("empty value range: got %+v", comment.Range)
	}
}

func TestParse_IfTableCommentsAndFollowingRules(t *testing.T) {
	input := "if,account2\n# about amazon\nAMAZON,expenses:shopping\n  ; indented comment\nAUDIBLE,expenses:books\n\naccount1 assets:bank\nif %payee foo\n  account2 expenses:foo"
	rf, parseDiags := Parse(input)
	if len(parseDiags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", parseDiags)
	}
	if len(rf.Comments) != 2 {
		t.Errorf("comments: got %d, want 2", len(rf.Comments))
	}
	if len(rf.IfTables) != 1 || len(rf.IfTables[0].Rows) != 2 {
		t.Fatalf("expected one table with two rows, got %+v", rf.IfTables)
	}
	if len(rf.IfBlocks) != 3 {
		t.Errorf("if blocks: got %d, want 3 (two rows and one block)", len(rf.IfBlocks))
	}
	if len(rf.Assignments) != 1 || rf.Assignments[0].Field != "account1" {
		t.Errorf("top-level assignments: got %+v", rf.Assignments)
	}
}

func TestParse_IfTableAndConditions(t *testing.T) {
	rf, parseDiags := Parse("if,account2,comment2\nA && B,foo,bar")
	if len(parseDiags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", parseDiags)
	}
	if got := rf.IfBlocks[0].Patterns; !reflect.DeepEqual(got, []string{"A && B"}) {
		t.Errorf("patterns: got %v", got)
	}
}

func TestParse_IfTableSkipAndEndFields(t *testing.T) {
	_, parseDiags := Parse("if,skip\nOPENING BALANCE,")
	if len(parseDiags) != 0 {
		t.Errorf("expected no diagnostics, got %v", parseDiags)
	}
}

func TestParse_IfTableErrors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		message string
	}{
		{
			name:    "header with no rows",
			input:   "if,account2\n\naccount1 assets:bank",
			want:    []string{"IF_TABLE_NO_ROWS"},
			message: "if table has no rows",
		},
		{
			name:    "header at end of file",
			input:   "if,account2",
			want:    []string{"IF_TABLE_NO_ROWS"},
			message: "if table has no rows",
		},
		{
			name:    "too few values",
			input:   "if,account2,comment\nFOO,expenses:foo",
			want:    []string{"IF_TABLE_VALUE_COUNT"},
			message: "if table row should have 2 values, but has 1",
		},
		{
			name:    "too many values",
			input:   "if,account2\nFOO,expenses:foo,extra",
			want:    []string{"IF_TABLE_VALUE_COUNT"},
			message: "if table row should have 1 value, but has 2",
		},
		{
			name:    "row without separator",
			input:   "if,account2\nFOO",
			want:    []string{"IF_TABLE_VALUE_COUNT"},
			message: "if table row should have 1 value, but has 0",
		},
		{
			name:    "indented row",
			input:   "if,account2\n  FOO,expenses:foo",
			want:    []string{"INDENTED_IF_TABLE_ROW"},
			message: "if table rows must not be indented",
		},
		{
			name:    "indented empty matcher",
			input:   "if,account2\n  ,expenses:foo",
			want:    []string{"INDENTED_IF_TABLE_ROW"},
			message: "if table rows must not be indented",
		},
		{
			name:    "unknown header field",
			input:   "if,account2,memo\nFOO,expenses:foo,bar",
			want:    []string{"UNKNOWN_FIELD"},
			message: "unknown field name: memo",
		},
		{
			name:    "missing header field",
			input:   "if,,account2\nFOO,,expenses:foo",
			want:    []string{"EMPTY_FIELD_NAME"},
			message: "missing field name in if table header",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := Parse(tt.input)
			if got := diagCodes(diags); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("diagnostic codes: got %v, want %v (%v)", got, tt.want, diags)
			}
			if diags[0].Message != tt.message {
				t.Errorf("message: got %q, want %q", diags[0].Message, tt.message)
			}
		})
	}
}

// hledger accepts a row with an empty matcher; the row just never matches,
// so it is reported as a warning rather than an error.
func TestParse_IfTableEmptyMatcher(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "ascii", input: "if,account2\n,expenses:foo"},
		{name: "cyrillic", input: "if|account2\n|расходы:еда"},
		{name: "crlf", input: "if,account2\r\n,expenses:foo\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rf, diags := Parse(tt.input)
			if got := diagCodes(diags); !reflect.DeepEqual(got, []string{"EMPTY_PATTERN"}) {
				t.Fatalf("diagnostic codes: got %v (%v)", got, diags)
			}
			d := diags[0]
			if d.Severity != SeverityWarning {
				t.Errorf("severity: got %v, want warning", d.Severity)
			}
			if d.Message != "empty if table pattern: this row never matches" {
				t.Errorf("message: got %q", d.Message)
			}
			if d.Range.Start.Line != 2 || d.Range.Start.Column != 1 {
				t.Errorf("range: got %+v", d.Range)
			}
			if len(rf.IfBlocks) != 1 || len(rf.IfBlocks[0].Assignments) != 1 {
				t.Errorf("row should still be parsed: got %+v", rf.IfBlocks)
			}
		})
	}
}

func TestParse_IfTableValueCountRange(t *testing.T) {
	_, diags := Parse("if,account2,comment\nFOO,expenses:foo")
	r := diags[0].Range
	if r.Start.Line != 2 || r.Start.Column != 1 || r.End.Line != 2 || r.End.Column != len("FOO,expenses:foo")+1 {
		t.Errorf("range: got %+v", r)
	}
}

func TestFolding_IfTable(t *testing.T) {
	ranges := FoldingRanges(parseRF(issue87Rules))
	want := []FoldingRange{
		{StartLine: 4, EndLine: 7, Kind: FoldingKindRegion},
		{StartLine: 9, EndLine: 10, Kind: FoldingKindRegion},
	}
	if !reflect.DeepEqual(ranges, want) {
		t.Errorf("folding ranges: got %+v, want %+v", ranges, want)
	}
}

func TestSymbols_IfTableRows(t *testing.T) {
	var names []string
	for _, s := range Symbols(parseRF(issue87Rules)) {
		if s.Kind == SymbolKindClass {
			names = append(names, s.Name)
		}
	}
	want := []string{"if WHOLE FOODS", "if SHELL", "if NETFLIX", "if %description groceries|food"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("if symbols: got %v, want %v", names, want)
	}
}

func TestSemanticTokens_IfTable(t *testing.T) {
	tokens := SemanticTokens("if,account2,comment\nWHOLE FOODS,expenses:groceries,")
	type tok struct {
		Line, Col, Length uint32
		Type              SemTokenType
	}
	want := []tok{
		{0, 0, 2, SemTokenKeyword},
		{0, 3, 8, SemTokenParameter},
		{0, 12, 7, SemTokenParameter},
		{1, 0, 11, SemTokenRegexp},
		{1, 12, 18, SemTokenString},
	}
	var got []tok
	for _, st := range tokens {
		got = append(got, tok{st.Line, st.Col, st.Length, st.TokenType})
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("semantic tokens:\ngot  %+v\nwant %+v", got, want)
	}
}

func TestComplete_IfTable(t *testing.T) {
	doc := "fields date, description, amount\n\nif,account2,comment\nWHOLE FOODS,expenses:groceries,food\n"
	accounts := []string{"expenses:groceries", "assets:checking"}
	rowLine := 3

	tests := []struct {
		name     string
		doc      string
		line     int
		col      int
		wantAny  []string
		wantNone bool
	}{
		{name: "header field name", doc: doc, line: 2, col: len("if,acc"), wantAny: []string{"account2", "comment"}},
		{name: "header pseudo-fields", doc: doc, line: 2, col: len("if,"), wantAny: []string{"skip", "end"}},
		{name: "header after separator", doc: doc, line: 2, col: len("if,account2,"), wantAny: []string{"comment"}},
		{name: "matcher column", doc: doc, line: rowLine, col: len("WHOLE"), wantAny: []string{"%description"}},
		{name: "account column", doc: doc, line: rowLine, col: len("WHOLE FOODS,exp"), wantAny: []string{"expenses:groceries"}},
		{name: "comment column", doc: doc, line: rowLine, col: len("WHOLE FOODS,expenses:groceries,fo"), wantNone: true},
		{name: "new row on blank line", doc: doc, line: rowLine + 1, col: 0, wantAny: []string{"%description"}},
		{
			name:    "field reference in value",
			doc:     "fields date, description\nif,comment\nFOO,%desc",
			line:    2,
			col:     len("FOO,%desc"),
			wantAny: []string{"%description"},
		},
		{
			name:    "field reference embedded in account value",
			doc:     "fields date, payee\nif,account2\nFOO,expenses:%pa",
			line:    2,
			col:     len("FOO,expenses:%pa"),
			wantAny: []string{"%payee"},
		},
		{
			name:    "field reference embedded in other value",
			doc:     "fields date, payee\nif,comment\nFOO,paid to %pa",
			line:    2,
			col:     len("FOO,paid to %pa"),
			wantAny: []string{"%payee"},
		},
		{
			name:     "punctuation after percent in other value",
			doc:      "fields date, payee\nif,comment\nFOO,paid (%pa)",
			line:     2,
			col:      len("FOO,paid (%pa)"),
			wantNone: true,
		},
		{
			name:    "negated matcher",
			doc:     "fields date, description\nif,account2\n!%desc",
			line:    2,
			col:     len("!%desc"),
			wantAny: []string{"%description"},
		},
		{
			name:    "account after completed field reference",
			doc:     "fields date, payee\nif,account2\nFOO,expenses:%payee food",
			line:    2,
			col:     len("FOO,expenses:%payee food"),
			wantAny: []string{"expenses:groceries"},
		},
		{
			name:    "after table and blank line",
			doc:     "if,account2\nFOO,bar\n\n",
			line:    3,
			col:     0,
			wantAny: []string{"skip", "fields", "if"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels := itemLabels(Complete(tt.doc, tt.line, tt.col, accounts, nil))
			if tt.wantNone {
				if len(labels) != 0 {
					t.Errorf("expected no completions, got %v", labels)
				}
				return
			}
			for _, want := range tt.wantAny {
				if !contains(labels, want) {
					t.Errorf("expected %q in completions, got %v", want, labels)
				}
			}
		})
	}
}

func TestIsInsideIfBlock_TableHeaderClosesBlock(t *testing.T) {
	doc := "if %payee foo\n  account2 expenses:foo\nif,account2\nBAR,expenses:bar\n\nBAZ"
	lines := strings.Split(doc, "\n")
	if isInsideIfBlock(lines, 5) {
		t.Error("a line after an if table should not be inside the preceding if block")
	}
	if isInsideIfBlock(lines, 2) {
		t.Error("an if table header should close the preceding if block")
	}
}

func TestIfTableEditStart(t *testing.T) {
	tests := []struct {
		name      string
		doc       string
		line      int
		col       int // UTF-16
		wantStart int // UTF-16
		wantOK    bool
	}{
		{name: "header cell", doc: "if,account2,comm", line: 0, col: 16, wantStart: 12, wantOK: true},
		{name: "header multi-byte separator", doc: "if→account2→comm", line: 0, col: 16, wantStart: 12, wantOK: true},
		{name: "matcher word", doc: "if,account2\nWHOLE FO", line: 1, col: 8, wantStart: 6, wantOK: true},
		{name: "account with spaces", doc: "if,account2\nFOO,Expenses:Dining O", line: 1, col: 21, wantStart: 4, wantOK: true},
		{name: "account after leading space", doc: "if,account2\nFOO, Expenses:Din", line: 1, col: 17, wantStart: 5, wantOK: true},
		{name: "embedded field reference", doc: "if,account2\nFOO,expenses:%pa", line: 1, col: 16, wantStart: 13, wantOK: true},
		{name: "non-ASCII before cell", doc: "if,account2\nCAFÉ 😀,Expenses:Din", line: 1, col: 20, wantStart: 8, wantOK: true},
		{name: "matcher keeps negation", doc: "if,account2\n!%desc", line: 1, col: 6, wantStart: 1, wantOK: true},
		{name: "matcher keeps unspaced and", doc: "if,account2\n%payee foo &&%am", line: 1, col: 16, wantStart: 13, wantOK: true},
		{name: "matcher keeps spaced and", doc: "if,account2\n%payee foo && %am", line: 1, col: 17, wantStart: 14, wantOK: true},
		{name: "punctuation after percent is not a reference", doc: "if,account2\nFOO,Expenses:(%pa)", line: 1, col: 18, wantStart: 4, wantOK: true},
		{name: "not in a table", doc: "account1 Expenses:Dining O", line: 0, col: 26, wantOK: false},
		{name: "header before separator", doc: "if", line: 0, col: 2, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, ok := IfTableEditStart(strings.Split(tt.doc, "\n"), tt.line, tt.col)
			if ok != tt.wantOK || (ok && start != tt.wantStart) {
				t.Errorf("IfTableEditStart = %d, %v; want %d, %v", start, ok, tt.wantStart, tt.wantOK)
			}
		})
	}
}
