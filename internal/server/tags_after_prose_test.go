package server

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/lsputil"
)

// hledger reads a tag as the last word before a colon, so a tag may follow prose
// in a comment (issue #151). These tests cover the server features that read
// tags, on LF and CRLF documents with non-ASCII prose.

var lineEndings = []struct {
	name    string
	newline string
}{
	{name: "LF", newline: "\n"},
	{name: "CRLF", newline: "\r\n"},
}

// utf16Col returns the UTF-16 column of the first occurrence of what in line.
func utf16Col(t *testing.T, line, what string) uint32 {
	t.Helper()
	at := strings.Index(line, what)
	require.GreaterOrEqual(t, at, 0, "%q not in %q", what, line)
	return uint32(lsputil.UTF16Len(line[:at]))
}

func TestDetermineContext_TagAfterProse(t *testing.T) {
	tests := []struct {
		name string
		line string
		want CompletionContextType
	}{
		{"name after words", "2024-01-15 test  ; paid on cl", ContextTagName},
		{"value after words", "2024-01-15 test  ; paid on client:ac", ContextTagValue},
		{"value after Cyrillic words", "2024-01-15 test  ; оплата клиент:", ContextTagValue},
		{"colon inside a value", "2024-01-15 test  ; foo:bar baz:qu", ContextTagValue},
		{"name after an empty tag name", "2024-01-15 test  ; :x", ContextTagName},
		{"name after a space before a colon", "2024-01-15 test  ; a :fo", ContextTagName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos := protocol.Position{Line: 0, Character: uint32(lsputil.UTF16Len(tt.line))}
			assert.Equal(t, tt.want, determineCompletionContext(tt.line, pos))
		})
	}
}

func TestExtractCurrentTagName_AfterProse(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{"words before the tag", "; paid on client:", "client"},
		{"Cyrillic and CJK words before the tag", "; оплата 日本 клиент:Ив", "клиент"},
		{"colon inside a value", "; foo:bar baz:", "foo"},
		{"words after a value", "; foo:bar, then status:", "status"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractCurrentTagName(tt.line, lsputil.UTF16Len(tt.line), false))
		})
	}
}

func TestCompletion_TagNameAfterProse_QueryAndRange(t *testing.T) {
	tests := []struct {
		name string
		line string
		word string
	}{
		{"words before the name", "2024-01-16 another ; paid on cl", "cl"},
		{"Cyrillic words before the name", "2024-01-16 описание ; оплата за кли", "кли"},
		{"emoji before the name", "2024-01-16 another ; 🍕 pro", "pro"},
		{"words after a value", "2024-01-16 another ; project:alpha, see st", "st"},
		{"after an empty tag name", "2024-01-16 another ; :st", "st"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			end := uint32(lsputil.UTF16Len(tt.line))
			pos := protocol.Position{Line: 0, Character: end}
			assert.Equal(t, tt.word, extractQueryText(tt.line, pos, ContextTagName))

			r := calculateTextEditRange(tt.line, pos, ContextTagName)
			require.NotNil(t, r)
			assert.Equal(t, utf16Col(t, tt.line, tt.word), r.Start.Character)
			assert.Equal(t, end, r.End.Character)
		})
	}
}

func TestCompletion_TagValueAfterProse_QueryAndRange(t *testing.T) {
	line := "2024-01-16 описание ; оплата клиент:Ив"
	end := uint32(lsputil.UTF16Len(line))
	pos := protocol.Position{Line: 0, Character: end}
	assert.Equal(t, "Ив", extractQueryText(line, pos, ContextTagValue))

	r := calculateTextEditRange(line, pos, ContextTagValue)
	require.NotNil(t, r)
	assert.Equal(t, utf16Col(t, line, "Ив"), r.Start.Character)
	assert.Equal(t, end, r.End.Character)
}

func TestCompletion_TagsAfterProse(t *testing.T) {
	for _, le := range lineEndings {
		t.Run(le.name, func(t *testing.T) {
			lines := []string{
				"2024-01-15 test  ; оплата 日本 клиент:Иван Петров, status:done",
				"    expenses:food  $50  ; paid on project:alpha",
				"    assets:cash",
				"",
				"2024-01-16 another ; see also кли",
				"    expenses:food  $50  ; paid on project:",
				"    assets:cash",
			}
			ts := newTestServer()
			docURI := uri.URI("file:///tags-after-prose.journal")
			require.NoError(t, ts.openDocument(docURI, strings.Join(lines, le.newline)))

			complete := func(line int) []string {
				result, err := ts.Server.completion(context.Background(), &protocol.CompletionParams{
					TextDocumentPositionParams: protocol.TextDocumentPositionParams{
						TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
						Position:     protocol.Position{Line: uint32(line), Character: uint32(lsputil.UTF16Len(lines[line]))},
					},
				})
				require.NoError(t, err)
				require.NotNil(t, result)
				return extractLabels(result.Items)
			}

			names := complete(4)
			assert.Contains(t, names, "клиент", "a tag written after prose is indexed and offered")
			assert.NotContains(t, names, "оплата 日本 клиент")

			values := complete(5)
			assert.Contains(t, values, "alpha")
			assert.NotContains(t, values, "done", "only values of the tag under the cursor")
			assert.NotContains(t, values, "Иван Петров")
		})
	}
}

func TestHover_TagAfterProse(t *testing.T) {
	for _, le := range lineEndings {
		t.Run(le.name, func(t *testing.T) {
			lines := []string{
				"2024-01-15 grocery ; 🍕 оплата за project:home",
				"    expenses:food  $50  ; paid on 🍕food:x",
				"    assets:cash",
				"",
				"2024-01-16 restaurant ; project:work",
				"    expenses:food  $30",
				"    assets:cash",
			}
			ts := newTestServer()
			docURI := uri.URI("file:///hover-tags.journal")
			require.NoError(t, ts.openDocument(docURI, strings.Join(lines, le.newline)))

			hover := func(line int, char uint32) *protocol.Hover {
				result, err := ts.Hover(context.Background(), &protocol.HoverParams{
					TextDocumentPositionParams: protocol.TextDocumentPositionParams{
						TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
						Position:     protocol.Position{Line: uint32(line), Character: char},
					},
				})
				require.NoError(t, err)
				require.NotNil(t, result, "no hover at %d:%d", line, char)
				return result
			}

			name := hover(0, utf16Col(t, lines[0], "project"))
			assert.Contains(t, hoverContent(name), "Tag: project")
			assert.Contains(t, hoverContent(name), "Usage: 2")
			require.NotNil(t, name.Range)
			assert.Equal(t, utf16Col(t, lines[0], "project"), name.Range.Start.Character)
			assert.Equal(t, utf16Col(t, lines[0], ":home"), name.Range.End.Character)

			value := hover(0, utf16Col(t, lines[0], "home"))
			assert.Contains(t, hoverContent(value), "Value: home")
			require.NotNil(t, value.Range)
			assert.Equal(t, utf16Col(t, lines[0], "home"), value.Range.Start.Character)
			assert.Equal(t, uint32(lsputil.UTF16Len(lines[0])), value.Range.End.Character)

			assert.Contains(t, hoverContent(hover(1, utf16Col(t, lines[1], "food:x"))), "Tag: 🍕food")
			valueAfterEmoji := hoverContent(hover(1, utf16Col(t, lines[1], ":x")+1))
			assert.Contains(t, valueAfterEmoji, "Value: x", "the value after an emoji name")

			result, err := ts.Hover(context.Background(), &protocol.HoverParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
					Position:     protocol.Position{Line: 0, Character: utf16Col(t, lines[0], "оплата")},
				},
			})
			require.NoError(t, err)
			if result != nil {
				assert.NotContains(t, hoverContent(result), "Tag", "prose before a tag is not part of it")
			}
		})
	}
}

func TestSemanticTokens_TagAfterProse(t *testing.T) {
	for _, le := range lineEndings {
		t.Run(le.name, func(t *testing.T) {
			lines := []string{
				"2024-01-15 test",
				"    expenses:food  $50  ; 🍕 оплата клиент:Иван Петров, x,date:1, :y",
				"    assets:cash",
			}
			ts := newTestServer()
			docURI := uri.URI("file:///semantic-tags.journal")
			require.NoError(t, ts.openDocument(docURI, strings.Join(lines, le.newline)))

			var got [][3]uint32
			for _, tok := range semanticTokensForDocument(t, ts.Server, docURI) {
				if tok.line == 1 && (tok.tokenType == TokenTypeTag || tok.tokenType == TokenTypeTagValue) {
					got = append(got, [3]uint32{tok.tokenType, tok.col, tok.length})
				}
			}
			line := lines[1]
			assert.Equal(t, [][3]uint32{
				{TokenTypeTag, utf16Col(t, line, "клиент"), uint32(lsputil.UTF16Len("клиент:"))},
				{TokenTypeTagValue, utf16Col(t, line, "Иван"), uint32(lsputil.UTF16Len("Иван Петров"))},
				{TokenTypeTag, utf16Col(t, line, "x,date"), uint32(lsputil.UTF16Len("x,date:"))},
				{TokenTypeTagValue, utf16Col(t, line, "1"), 1},
			}, got)
		})
	}
}

func TestPublishDiagnostics_DateTagAfterProse(t *testing.T) {
	for _, le := range lineEndings {
		t.Run(le.name, func(t *testing.T) {
			lines := []string{
				"2024-01-01 касса",
				"    現金:口座  = -4 GBP  ; оплата 日本 date:2024-01-19",
				"    капитал",
				"",
				"2024-01-02 t",
				"    a  4 GBP  ; 🍕 note date2:garbage",
				"    b",
				"",
				"2024-01-03 t",
				"    a  = 4 GBP  ; x,date:garbage",
				"    b",
			}
			ts := newTestServer()
			diagnostics, err := ts.openAndWait(uri.URI("file:///date-after-prose.journal"), strings.Join(lines, le.newline))
			require.NoError(t, err)

			refused := diagnosticWithCode(diagnostics, "ASSIGNMENT_POSTING_DATE")
			require.NotNil(t, refused, "got %v", codesIn(diagnostics))
			assert.Equal(t, uint32(1), refused.Range.Start.Line)

			invalid := diagnosticWithCode(diagnostics, "INVALID_DATE_TAG")
			require.NotNil(t, invalid, "got %v", codesIn(diagnostics))
			assert.Equal(t, uint32(5), invalid.Range.Start.Line)
			assert.Equal(t, utf16Col(t, lines[5], "date2"), invalid.Range.Start.Character)
			assert.Equal(t, uint32(lsputil.UTF16Len(lines[5])), invalid.Range.End.Character)

			for _, d := range diagnostics {
				if code := diagnosticCodeString(d.Code); code == "INVALID_DATE_TAG" || code == "ASSIGNMENT_POSTING_DATE" {
					assert.NotEqual(t, uint32(9), d.Range.Start.Line, "x,date: is not a date tag: %s", d.Message)
				}
			}
		})
	}
}

// hledger reads a posting comment like any comment except after a colon that
// names nothing: there it skips one comma, so ":,x:1" is the tag "x" on a
// posting and ",x" on the transaction.
func TestSemanticTokens_PostingCommentSkipsACommaAfterAnEmptyName(t *testing.T) {
	for _, le := range lineEndings {
		t.Run(le.name, func(t *testing.T) {
			lines := []string{
				"2024-01-15 оплата  ; :,x:1",
				"    ; :,x:1",
				"    expenses:food  $50  ; :,x:1",
				"    ; :,x:1",
				"    assets:cash",
				"",
				"    ; :,x:1",
			}
			ts := newTestServer()
			docURI := uri.URI("file:///semantic-posting-tags.journal")
			require.NoError(t, ts.openDocument(docURI, strings.Join(lines, le.newline)))

			tagStarts := map[uint32][]uint32{}
			for _, tok := range semanticTokensForDocument(t, ts.Server, docURI) {
				if tok.tokenType == TokenTypeTag {
					tagStarts[tok.line] = append(tagStarts[tok.line], tok.col)
				}
			}
			// An empty line ends the transaction, so the last comment is no
			// posting's comment.
			for i, want := range map[int]string{0: ",x:1", 1: ",x:1", 2: "x:1", 3: "x:1", 6: ",x:1"} {
				line := lines[i]
				assert.Equal(t, []uint32{uint32(lsputil.UTF16Len(line) - len(want))}, tagStarts[uint32(i)], "line %d", i)
			}
		})
	}
}

// postingCompletionCases end with the cursor in a tag name written after a colon
// that names nothing and a comma. hledger skips that comma in a posting comment
// (the posting line or a ; line below a posting of a transaction, a periodic
// transaction or an auto posting rule), so the name being written is word;
// elsewhere the comma belongs to the name.
var postingCompletionCases = []struct {
	name    string
	content string
	word    string
}{
	{"posting line", "2024-01-16 t\n    expenses:food  $50  ; оплата :,кл", "кл"},
	{"comment line below a posting", "2024-01-16 t\n    expenses:food  $50\n    ; 🍕 :　,кл", "кл"},
	{"second comment line below a posting", "2024-01-16 t\n    expenses:food  $50\n    ; note\n    ; :,кл", "кл"},
	{"comment line below a periodic posting", "~ monthly  ; заметка\n    expenses:food  $50\n    ; :,кл", "кл"},
	{"comment line below an auto rule posting", "= food\n    expenses:tips  *0.1\n    ; 日本 :,кл", "кл"},
	{"transaction header", "2024-01-16 t  ; :,кл", ",кл"},
	{"comment line before the first posting", "2024-01-16 t\n    ; :,кл", ",кл"},
	{"periodic header", "~ monthly  ; :,кл", ",кл"},
	{"comment line after an empty line", "2024-01-16 t\n    expenses:food  $50\n\n    ; :,кл", ",кл"},
	{"comment line under a commodity directive", "commodity X\n    format 1.00 X\n    ; :,кл", ",кл"},
	{"cleared posting line", "2024-01-16 t\n    * expenses:food  $50  ; оплата :,кл", "кл"},
	{"pending posting line", "2024-01-16 t\n    ! expenses:food  $50  ; 日本 :,кл", "кл"},
	{"comment line below a cleared first posting", "2024-01-16 t\n    * expenses:food  $50\n    ; 🍕 :\u3000,кл", "кл"},
	{"comment line below a cleared periodic posting", "~ monthly\n    * expenses:food  $50\n    ; :,кл", "кл"},
	{"comment line below a cleared auto rule posting", "= food\n    * expenses:tips  *0.1\n    ; :,кл", "кл"},
	{"cleared posting line of an auto rule", "= food\n    * expenses:tips  *0.1  ; :,кл", "кл"},
	{"BOM before a transaction header", "\ufeff2024-01-16 t\n    expenses:food  $50\n    ; заметка :,кл", "кл"},
	{"BOM before a periodic header", "\ufeff~ monthly\n    * expenses:food  $50\n    ; :,кл", "кл"},
	{"BOM before an auto rule header", "\ufeff= food\n    expenses:tips  *0.1\n    ; :,кл", "кл"},
	{"BOM before a transaction header, comment line before the postings", "\ufeff2024-01-16 t\n    ; :,кл", ",кл"},
}

func TestCompletion_TagNameInPostingComment_QueryAndRange(t *testing.T) {
	for _, tc := range postingCompletionCases {
		for _, le := range lineEndings {
			t.Run(tc.name+"/"+le.name, func(t *testing.T) {
				content := strings.ReplaceAll(tc.content, "\n", le.newline)
				lines := strings.Split(tc.content, "\n")
				last := lines[len(lines)-1]
				end := uint32(lsputil.UTF16Len(last))
				pos := protocol.Position{Line: uint32(len(lines) - 1), Character: end}

				assert.Equal(t, ContextTagName, determineCompletionContext(content, pos))
				assert.Equal(t, tc.word, extractQueryText(content, pos, ContextTagName))
				r := calculateTextEditRange(content, pos, ContextTagName)
				require.NotNil(t, r)
				assert.Equal(t, uint32(lsputil.UTF16Len(last)-lsputil.UTF16Len(tc.word)), r.Start.Character)
				assert.Equal(t, end, r.End.Character)
			})
		}
	}
}

func TestCompletion_TagValueInPostingComment_QueryAndRange(t *testing.T) {
	tests := []struct {
		name    string
		content string
		tag     string
	}{
		{"comment line below a posting", "2024-01-16 t\n    expenses:food  $50\n    ; заметка :,клиент:Ив", "клиент"},
		{"comment line below a periodic posting", "~ monthly\n    expenses:food  $50\n    ; :,клиент:Ив", "клиент"},
		{"transaction header", "2024-01-16 t  ; заметка :,клиент:Ив", ",клиент"},
		{"cleared posting line", "2024-01-16 t\n    * expenses:food  $50  ; заметка :,клиент:Ив", "клиент"},
		{"comment line below a cleared first posting", "2024-01-16 t\n    * expenses:food  $50\n    ; 🍕 :,клиент:Ив", "клиент"},
		{"comment line below a cleared auto rule posting", "= food\n    * expenses:tips  *0.1\n    ; :,клиент:Ив", "клиент"},
		{"comment line below a cleared periodic posting", "~ monthly\n    * expenses:food  $50\n    ; :,клиент:Ив", "клиент"},
		{"BOM before a transaction header", "\ufeff2024-01-16 t\n    expenses:food  $50\n    ; :,клиент:Ив", "клиент"},
		{"BOM before an auto rule header", "\ufeff= food\n    * expenses:tips  *0.1  ; :,клиент:Ив", "клиент"},
	}
	for _, tt := range tests {
		for _, le := range lineEndings {
			t.Run(tt.name+"/"+le.name, func(t *testing.T) {
				content := strings.ReplaceAll(tt.content, "\n", le.newline)
				lines := strings.Split(tt.content, "\n")
				last := lines[len(lines)-1]
				end := uint32(lsputil.UTF16Len(last))
				pos := protocol.Position{Line: uint32(len(lines) - 1), Character: end}

				assert.Equal(t, ContextTagValue, determineCompletionContext(content, pos))
				posting := isPostingComment(strings.Split(content, "\n"), int(pos.Line))
				assert.Equal(t, tt.tag, extractCurrentTagName(last, int(end), posting))
				assert.Equal(t, "Ив", extractQueryText(content, pos, ContextTagValue))
				r := calculateTextEditRange(content, pos, ContextTagValue)
				require.NotNil(t, r)
				assert.Equal(t, utf16Col(t, last, "Ив"), r.Start.Character)
				assert.Equal(t, end, r.End.Character)
			})
		}
	}
}

func TestCompletion_TagsInPostingCommentAfterAnEmptyName(t *testing.T) {
	for _, le := range lineEndings {
		t.Run(le.name, func(t *testing.T) {
			lines := []string{
				"2024-01-15 test",
				"    expenses:food  $50  ; клиент:Иван Петров",
				"    assets:cash",
				"",
				"2024-01-16 another",
				"    expenses:food  $50",
				"    ; оплата :,кли",
				"    ; 🍕 :,клиент:",
				"    assets:cash",
			}
			ts := newTestServer()
			docURI := uri.URI("file:///posting-tags-completion.journal")
			require.NoError(t, ts.openDocument(docURI, strings.Join(lines, le.newline)))

			complete := func(line int) *protocol.CompletionList {
				result, err := ts.Server.completion(context.Background(), &protocol.CompletionParams{
					TextDocumentPositionParams: protocol.TextDocumentPositionParams{
						TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
						Position:     protocol.Position{Line: uint32(line), Character: uint32(lsputil.UTF16Len(lines[line]))},
					},
				})
				require.NoError(t, err)
				require.NotNil(t, result)
				return result
			}
			itemNamed := func(list *protocol.CompletionList, label string) *protocol.CompletionItem {
				for i := range list.Items {
					if list.Items[i].Label == label {
						return &list.Items[i]
					}
				}
				t.Fatalf("%q not offered, got %v", label, extractLabels(list.Items))
				return nil
			}

			name := itemNamed(complete(6), "клиент")
			require.NotNil(t, name.TextEdit)
			assert.Equal(t, utf16Col(t, lines[6], "кли"), completionTextEdit(*name).Range.Start.Character,
				"the edit replaces only the name, not the comma hledger skips")

			value := itemNamed(complete(7), "Иван Петров")
			require.NotNil(t, value.TextEdit)
			assert.Equal(t, uint32(lsputil.UTF16Len(lines[7])), completionTextEdit(*value).Range.Start.Character)
		})
	}
}

func TestSemanticTokens_PeriodicAndAutoRulePostingComments(t *testing.T) {
	for _, le := range lineEndings {
		t.Run(le.name, func(t *testing.T) {
			lines := []string{
				"~ monthly  ; :,x:1",
				"    ; :,x:1",
				"    expenses:food  $50  ; :,x:1",
				"    ; заметка :,x:1",
				"    assets:cash",
				"",
				"= food  ; :,x:1",
				"    ; :,x:1",
				"    expenses:tips  *0.1  ; :,x:1",
				"    ; 🍕 :,x:1",
				"    assets:cash  *-0.1",
			}
			ts := newTestServer()
			docURI := uri.URI("file:///semantic-periodic-tags.journal")
			require.NoError(t, ts.openDocument(docURI, strings.Join(lines, le.newline)))

			tagStarts := map[uint32][]uint32{}
			for _, tok := range semanticTokensForDocument(t, ts.Server, docURI) {
				if tok.tokenType == TokenTypeTag {
					tagStarts[tok.line] = append(tagStarts[tok.line], tok.col)
				}
			}
			for i, want := range map[int]string{0: ",x:1", 1: ",x:1", 2: "x:1", 3: "x:1", 6: ",x:1", 7: ",x:1", 8: "x:1", 9: "x:1"} {
				line := lines[i]
				assert.Equal(t, []uint32{uint32(lsputil.UTF16Len(line) - len(want))}, tagStarts[uint32(i)], "line %d", i)
			}
		})
	}
}

// The cleared and pending marks start a posting, so a comment on or below such a
// posting is a posting comment; a byte order mark before the first header does
// not hide that header.
func TestCompletion_TagsInClearedPostingCommentsAfterABOM(t *testing.T) {
	for _, le := range lineEndings {
		t.Run(le.name, func(t *testing.T) {
			lines := []string{
				"\ufeff2024-01-15 test",
				"    * expenses:food  $50  ; клиент:Иван Петров",
				"    assets:cash",
				"",
				"2024-01-16 another",
				"    * expenses:food  $50  ; оплата :,кли",
				"    ; 🍕 :,клиент:",
				"    assets:cash",
			}
			ts := newTestServer()
			docURI := uri.URI("file:///cleared-posting-tags.journal")
			require.NoError(t, ts.openDocument(docURI, strings.Join(lines, le.newline)))

			complete := func(line int) *protocol.CompletionList {
				result, err := ts.Server.completion(context.Background(), &protocol.CompletionParams{
					TextDocumentPositionParams: protocol.TextDocumentPositionParams{
						TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
						Position:     protocol.Position{Line: uint32(line), Character: uint32(lsputil.UTF16Len(lines[line]))},
					},
				})
				require.NoError(t, err)
				require.NotNil(t, result)
				return result
			}
			itemNamed := func(list *protocol.CompletionList, label string) *protocol.CompletionItem {
				for i := range list.Items {
					if list.Items[i].Label == label {
						return &list.Items[i]
					}
				}
				t.Fatalf("%q not offered, got %v", label, extractLabels(list.Items))
				return nil
			}

			name := itemNamed(complete(5), "клиент")
			require.NotNil(t, name.TextEdit)
			assert.Equal(t, utf16Col(t, lines[5], "кли"), completionTextEdit(*name).Range.Start.Character,
				"the edit replaces only the name, not the comma hledger skips")

			value := itemNamed(complete(6), "Иван Петров")
			require.NotNil(t, value.TextEdit)
			assert.Equal(t, uint32(lsputil.UTF16Len(lines[6])), completionTextEdit(*value).Range.Start.Character)
		})
	}
}
