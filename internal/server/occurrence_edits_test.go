package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/testutil"
)

// occurrenceLayout is a workspace in which shared.journal is included more than
// once under different reading rules, the situation of issue #116.
type occurrenceLayout struct {
	name  string
	files func(shared string) map[string]string
}

func includeTwice(prefix, firstMark, secondMark string) func(shared string) map[string]string {
	return func(shared string) map[string]string {
		return map[string]string{
			"main.journal":   prefix + "include first.journal\ninclude second.journal\n",
			"first.journal":  firstMark + "include shared.journal\n",
			"second.journal": secondMark + "include shared.journal\n",
			"shared.journal": shared,
		}
	}
}

// conflictingMarkLayouts read the shared file with `,` at one occurrence and `.`
// at the other, in both include orders and from two workspace roots.
func conflictingMarkLayouts() []occurrenceLayout {
	return []occurrenceLayout{
		{"comma then dot", includeTwice("", "decimal-mark ,\n", "decimal-mark .\n")},
		{"dot then comma", includeTwice("", "decimal-mark .\n", "decimal-mark ,\n")},
		{"commodity declared in the parent", includeTwice("commodity 1.000,00 EUR\n\n", "decimal-mark ,\n", "decimal-mark .\n")},
		{"two workspace roots", func(shared string) map[string]string {
			return map[string]string{
				"b-root.journal": "decimal-mark ,\ninclude shared.journal\n",
				"z-root.journal": "decimal-mark .\ninclude shared.journal\n",
				"shared.journal": shared,
			}
		}},
	}
}

// amountsAtEveryOccurrence reads text as shared.journal at every occurrence of
// the file in every tree of the workspace and lists the posting amounts it finds
// there, one group per occurrence. It reads through the loader's own occurrence
// contexts, independent of the code under test.
func amountsAtEveryOccurrence(t *testing.T, ts *testServer, path, text string) [][]string {
	t.Helper()
	var groups [][]string
	for _, tree := range ts.workspace.GetIncludeTreesForFile(path) {
		for _, occurrence := range tree.Resolved.OccurrencesForPath(path) {
			journal, _, ok := tree.Resolved.ParseOccurrence(occurrence.ID, text)
			require.True(t, ok)
			var amounts []string
			for i := range journal.Transactions {
				for j := range journal.Transactions[i].Postings {
					for _, amount := range postingAmounts(&journal.Transactions[i].Postings[j]) {
						if amount != nil {
							amounts = append(amounts, amount.Quantity.String()+" "+amount.Commodity.Symbol)
						}
					}
				}
			}
			groups = append(groups, amounts)
		}
	}
	require.Greater(t, len(groups), 1, "the layout must include the file more than once")
	return groups
}

func setupOccurrences(t *testing.T, layout occurrenceLayout, shared string) (*testServer, string, uri.URI, string) {
	t.Helper()
	dir := t.TempDir()
	writeJournals(t, dir, layout.files(shared))
	path := filepath.Join(dir, "shared.journal")
	ts, docURI := formatServer(t, dir, path, shared)
	return ts, dir, docURI, path
}

// Writing a number from the first occurrence's reading rules changes its value
// at an occurrence that reads it differently: `10 EUR` becoming `10,00 EUR`
// reads as 1000 under a dot mark. Format must leave such numbers as written.
func TestFormat_KeepsNumbersThatOccurrencesReadDifferently(t *testing.T) {
	tests := []struct {
		name   string
		shared string
	}{
		{"integer under a comma format", "2024-01-01 x\n    expenses:food   10 EUR\n    assets:cash\n"},
		{"ambiguous group", "2024-01-01 x\n    expenses:food   1,500 EUR\n    assets:cash\n"},
		{"cyrillic", "2024-01-01 x\n    расходы:еда   1,500 РУБ\n    активы:касса\n"},
		{"cjk", "2024-01-01 x\n    費用:食費   1,500 元\n    資産:現金\n"},
		{"cost and assertion", "2024-01-01 x\n    a   10 EUR @ 2 USD\n    b   -20 USD  =  -20 USD\n"},
	}

	for _, layout := range conflictingMarkLayouts() {
		for _, tt := range tests {
			for _, crlf := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/crlf=%v", layout.name, tt.name, crlf), func(t *testing.T) {
					shared := tt.shared
					if tt.name == "integer under a comma format" || tt.name == "cost and assertion" {
						shared = "commodity 1.000,00 EUR\ncommodity 1.000,00 USD\n" + shared
					}
					ts, dir, docURI, path := setupOccurrences(t, layout, shared)
					text := shared
					if crlf {
						text = strings.ReplaceAll(shared, "\n", "\r\n")
					}
					ts.documents.Store(docURI, strings.ReplaceAll(text, "\r\n", "\n"))
					before := amountsAtEveryOccurrence(t, ts, path, strings.ReplaceAll(text, "\r\n", "\n"))

					once := formatted(t, ts, docURI)
					assert.Equal(t, before, amountsAtEveryOccurrence(t, ts, path, once), "Format changed a value at some occurrence")

					ts.documents.Store(docURI, once)
					assert.Equal(t, once, formatted(t, ts, docURI), "formatting must be idempotent")

					_ = dir
				})
			}
		}
	}
}

// The numbers stay, but the rest of the line is still the formatter's: indent
// and alignment are normalised whatever the occurrences read.
func TestFormat_StillNormalisesWhitespaceAroundKeptNumbers(t *testing.T) {
	shared := "commodity 1.000,00 EUR\n2024-01-01 x\n  expenses:food     1,500 EUR\n  assets:cash\n"
	ts, _, docURI, _ := setupOccurrences(t, conflictingMarkLayouts()[0], shared)

	got := formatted(t, ts, docURI)
	assert.Contains(t, got, "\n    expenses:food")
	assert.Contains(t, got, "1,500 EUR\n")
	assert.Contains(t, got, "\n    assets:cash\n")
}

// One disputed number keeps the numbers of the whole document as written: the
// alignment follows the text that is written, which keeps formatting idempotent.
func TestFormat_KeepsEveryNumberOfADocumentWithADisputedOne(t *testing.T) {
	shared := "commodity 1.000,00 EUR\n" +
		"2024-01-01 disputed\n    expenses:food   1,500 EUR\n    assets:cash\n\n" +
		"2024-01-02 agreed\n    expenses:food   0 EUR\n    assets:cash\n"
	ts, _, docURI, path := setupOccurrences(t, conflictingMarkLayouts()[0], shared)
	before := amountsAtEveryOccurrence(t, ts, path, shared)

	got := formatted(t, ts, docURI)
	assert.Contains(t, got, "1,500 EUR")
	assert.Contains(t, got, "  0 EUR\n")
	assert.Equal(t, before, amountsAtEveryOccurrence(t, ts, path, got))
}

// Occurrences that differ in how they read other text, but render the file the
// same way, still get the formatter's output: nothing is held back for them.
func TestFormat_WritesNumbersWhenEveryOccurrenceRendersThemAlike(t *testing.T) {
	layout := occurrenceLayout{"apply account", includeTwice("commodity 1,000.00 EUR\n\n", "apply account a\n", "apply account b\n")}
	shared := "2024-01-01 x\n    expenses:food   1,500 EUR\n    assets:cash\n"
	ts, _, docURI, path := setupOccurrences(t, layout, shared)

	got := formatted(t, ts, docURI)
	assert.Contains(t, got, "1,500.00 EUR")
	assert.NotContains(t, got, "1,500 EUR")
	groups := amountsAtEveryOccurrence(t, ts, path, got)
	assert.Equal(t, groups[0], groups[1])
}

// A file included twice with equal contexts is the common case: it formats
// exactly as a file included once.
func TestFormat_EqualContextsFormatAsOneOccurrence(t *testing.T) {
	layout := occurrenceLayout{"equal", includeTwice("commodity 1,000.00 EUR\n\n", "", "")}
	shared := "2024-01-01 x\n    expenses:food   1500 EUR\n    assets:cash\n"
	ts, _, docURI, _ := setupOccurrences(t, layout, shared)

	assert.Contains(t, formatted(t, ts, docURI), "1,500.00 EUR")
}

func TestRangeFormat_KeepsNumbersThatOccurrencesReadDifferently(t *testing.T) {
	for _, layout := range conflictingMarkLayouts() {
		t.Run(layout.name, func(t *testing.T) {
			shared := "commodity 1.000,00 EUR\n2024-01-01 x\n    expenses:food   1,500 EUR\n    assets:cash\n"
			ts, _, docURI, path := setupOccurrences(t, layout, shared)
			before := amountsAtEveryOccurrence(t, ts, path, shared)

			edits, err := ts.RangeFormat(context.Background(), &protocol.DocumentRangeFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
				Range:        protocol.Range{End: protocol.Position{Line: 10}},
			})
			require.NoError(t, err)
			assert.Equal(t, before, amountsAtEveryOccurrence(t, ts, path, applyTextEdits(shared, edits)))
		})
	}
}

func TestOnTypeFormatting_KeepsNumbersThatOccurrencesReadDifferently(t *testing.T) {
	for _, layout := range conflictingMarkLayouts() {
		t.Run(layout.name, func(t *testing.T) {
			shared := "commodity 1.000,00 EUR\n2024-01-01 x\n    expenses:food   1,500 EUR\n    assets:cash\n\n"
			ts, _, docURI, path := setupOccurrences(t, layout, shared)
			before := amountsAtEveryOccurrence(t, ts, path, shared)

			edits, err := ts.onTypeFormatting(docURI, 3, "\n")
			require.NoError(t, err)
			assert.Equal(t, before, amountsAtEveryOccurrence(t, ts, path, applyTextEdits(shared, edits)))
		})
	}
}

// Oracle: hledger reads the same amounts after Format as before, at every
// occurrence of the file.
func TestOracle_FormatKeepsEveryOccurrenceValueInHledger(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)
	shareds := []string{
		"commodity 1.000,00 EUR\n2024-01-01 x\n    expenses:food   10 EUR\n    assets:cash\n",
		"2024-01-01 x\n    expenses:food   1,500 EUR\n    assets:cash\n",
		"2024-01-01 x\n    расходы:еда   1,500 EUR\n    активы:касса\n",
	}
	for _, layout := range conflictingMarkLayouts() {
		for index, shared := range shareds {
			t.Run(fmt.Sprintf("%s/sample %d", layout.name, index), func(t *testing.T) {
				ts, dir, docURI, path := setupOccurrences(t, layout, shared)
				roots := []string{"main.journal"}
				if layout.name == "two workspace roots" {
					roots = []string{"b-root.journal", "z-root.journal"}
				}

				want := make(map[string][]string)
				for _, root := range roots {
					amounts, ok := testutil.HledgerPostingAmounts(t, hledgerPath, filepath.Join(dir, root))
					require.True(t, ok)
					want[root] = amounts
				}

				require.NoError(t, os.WriteFile(path, []byte(formatted(t, ts, docURI)), 0o644))
				for _, root := range roots {
					got, ok := testutil.HledgerPostingAmounts(t, hledgerPath, filepath.Join(dir, root))
					require.True(t, ok)
					assert.Equal(t, want[root], got, root)
				}
			})
		}
	}
}

func codeActionsOfKind(t *testing.T, ts *testServer, docURI uri.URI, line uint32, kind protocol.CodeActionKind) []protocol.CodeAction {
	t.Helper()
	ts.cliClient = nil
	ts.clientCapabilities.supportsCodeActionLiterals = true
	entries, err := ts.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
		Range:        protocol.Range{Start: protocol.Position{Line: line}, End: protocol.Position{Line: line}},
	})
	require.NoError(t, err)
	var matched []protocol.CodeAction
	for _, action := range codeActionEntries(entries) {
		if action.Kind != nil && *action.Kind == kind {
			matched = append(matched, action)
		}
	}
	return matched
}

func crlfVariants(text string) map[string]string {
	return map[string]string{"lf": text, "crlf": strings.ReplaceAll(text, "\n", "\r\n")}
}

// The inferred amount is written once and read at every occurrence. When the
// occurrences infer amounts that no single text expresses, the amount that
// balances one of them unbalances another, so no edit is offered: 1,500 and 2
// sum to 1502 under a dot mark and to 3.5 under a comma mark.
func TestInsertInferredAmount_NotOfferedWhenNoTextBalancesEveryOccurrence(t *testing.T) {
	for _, layout := range conflictingMarkLayouts() {
		for name, shared := range crlfVariants("2024-01-01 x\n    a  1,500 USD\n    b  2 USD\n    c\n") {
			t.Run(layout.name+"/"+name, func(t *testing.T) {
				ts, _, docURI, _ := setupOccurrences(t, layout, shared)
				assert.Empty(t, codeActionsOfKind(t, ts, docURI, 3, insertInferredAmountKind))
			})
		}
	}
}

// A text exists even when the occurrences infer different values, as long as
// they read it the way they read the numbers it balances: -1,500 balances 1,500
// at both marks.
func TestInsertInferredAmount_WritesTheSpellingTheOccurrencesShare(t *testing.T) {
	for _, layout := range conflictingMarkLayouts() {
		for name, shared := range crlfVariants("2024-01-01 x\n    a  1,500 USD\n    b\n") {
			t.Run(layout.name+"/"+name, func(t *testing.T) {
				ts, _, docURI, path := setupOccurrences(t, layout, shared)
				actions := codeActionsOfKind(t, ts, docURI, 2, insertInferredAmountKind)
				require.Len(t, actions, 1)

				doc, _ := ts.GetDocument(docURI)
				applied := applyTextEdits(doc, actions[0].Edit.Changes[docURI])
				assert.ElementsMatch(t, [][]string{{"1.5 USD", "-1.5 USD"}, {"1500 USD", "-1500 USD"}}, amountsAtEveryOccurrence(t, ts, path, applied))
			})
		}
	}
}

// When every occurrence infers the same amount, one text that all of them read
// as that amount is written, even if no occurrence's own rendering is it.
func TestInsertInferredAmount_WritesATextEveryOccurrenceReadsAlike(t *testing.T) {
	samples := map[string]string{
		"ascii":    "2024-01-01 x\n    expenses:food  10 USD\n    assets:cash\n",
		"cyrillic": "2024-01-01 x\n    расходы:еда  10 USD\n    активы:касса\n",
		"cjk":      "2024-01-01 x\n    費用:食費  10 USD\n    資産:現金\n",
	}
	layouts := append(conflictingMarkLayouts(), occurrenceLayout{
		"commodity format of one mark", includeTwice("commodity 1.000,00 USD\n\n", "decimal-mark ,\n", "decimal-mark .\n"),
	})
	for _, layout := range layouts {
		for sample, text := range samples {
			for variant, shared := range crlfVariants(text) {
				t.Run(layout.name+"/"+sample+"/"+variant, func(t *testing.T) {
					ts, _, docURI, path := setupOccurrences(t, layout, shared)
					actions := codeActionsOfKind(t, ts, docURI, 2, insertInferredAmountKind)
					require.Len(t, actions, 1)

					doc, _ := ts.GetDocument(docURI)
					applied := applyTextEdits(doc, actions[0].Edit.Changes[docURI])
					for _, amounts := range amountsAtEveryOccurrence(t, ts, path, applied) {
						assert.Equal(t, []string{"10 USD", "-10 USD"}, amounts)
					}
				})
			}
		}
	}
}

func TestFixUnbalanced_NotOfferedWhenNoTextBalancesEveryOccurrence(t *testing.T) {
	shared := "2024-01-01 x\n    a  1,500 USD\n    b  2 USD\n    c  -1 USD\n"
	for _, layout := range conflictingMarkLayouts() {
		t.Run(layout.name, func(t *testing.T) {
			ts, _, docURI, _ := setupOccurrences(t, layout, shared)
			assert.Empty(t, codeActionsOfKind(t, ts, docURI, 1, protocol.CodeActionKindQuickFix), "quick fix")

			txRange := protocol.Range{End: protocol.Position{Line: 4}}
			_, err := ts.ExecuteCommand(context.Background(), &protocol.ExecuteCommandParams{
				Command: "hledger.fixUnbalanced",
				Arguments: []protocol.LSPAny{
					mustMarshalLSPAny(t, string(docURI)),
					mustMarshalLSPAny(t, txRange),
				},
			})
			assert.Error(t, err, "code lens command")
			assert.Nil(t, ts.client.lastApplyEdit())
		})
	}
}

func TestFixUnbalanced_WritesATextEveryOccurrenceReadsAlike(t *testing.T) {
	samples := map[string]string{
		"ascii":    "2024-01-01 x\n    a  10 USD\n    b  -5 USD\n",
		"cyrillic": "2024-01-01 x\n    счёт  10 USD\n    расходы  -5 USD\n",
		"cjk":      "2024-01-01 x\n    資産  10 USD\n    費用  -5 USD\n",
	}
	layouts := append(conflictingMarkLayouts(), occurrenceLayout{
		"commodity format of one mark", includeTwice("commodity 1.000,00 USD\n\n", "decimal-mark ,\n", "decimal-mark .\n"),
	})
	for _, layout := range layouts {
		for sample, text := range samples {
			for variant, shared := range crlfVariants(text) {
				t.Run(layout.name+"/"+sample+"/"+variant, func(t *testing.T) {
					ts, _, docURI, path := setupOccurrences(t, layout, shared)
					actions := codeActionsOfKind(t, ts, docURI, 1, protocol.CodeActionKindQuickFix)
					require.Len(t, actions, 1)

					doc, _ := ts.GetDocument(docURI)
					applied := applyTextEdits(doc, actions[0].Edit.Changes[docURI])
					for _, amounts := range amountsAtEveryOccurrence(t, ts, path, applied) {
						assert.Equal(t, []string{"10 USD", "-10 USD"}, amounts)
					}
				})
			}
		}
	}
}

// Inline completion writes the amounts of an earlier transaction. A number the
// occurrences read as different values cannot be copied, so no ghost text is
// offered.
func TestInlineCompletion_NotOfferedWhenOccurrencesReadItsAmountsDifferently(t *testing.T) {
	history := "2024-01-01 shop\n    expenses:food  1,500 USD\n    assets:cash\n\n2024-01-02 shop\n"
	for _, layout := range conflictingMarkLayouts() {
		for name, shared := range crlfVariants(history) {
			t.Run(layout.name+"/"+name, func(t *testing.T) {
				ts, _, docURI, _ := setupOccurrences(t, layout, shared)
				result, err := ts.InlineCompletion(context.Background(), inlineCompletionParams(docURI, 5, 0))
				require.NoError(t, err)
				assert.Empty(t, inlineCompletionItems(t, result))
			})
		}
	}
}

func TestInlineCompletion_OfferedWhenOccurrencesReadItsAmountsAlike(t *testing.T) {
	samples := map[string]string{
		"ascii":    "2024-01-01 shop\n    expenses:food  10 USD\n    assets:cash\n\n2024-01-02 shop\n",
		"cyrillic": "2024-01-01 магазин\n    расходы:еда  10 USD\n    активы:касса\n\n2024-01-02 магазин\n",
		"cjk":      "2024-01-01 店\n    費用:食費  10 USD\n    資産:現金\n\n2024-01-02 店\n",
	}
	for _, layout := range conflictingMarkLayouts() {
		for sample, text := range samples {
			for variant, shared := range crlfVariants(text) {
				t.Run(layout.name+"/"+sample+"/"+variant, func(t *testing.T) {
					ts, _, docURI, _ := setupOccurrences(t, layout, shared)
					result, err := ts.InlineCompletion(context.Background(), inlineCompletionParams(docURI, 5, 0))
					require.NoError(t, err)
					items := inlineCompletionItems(t, result)
					require.Len(t, items, 1)
					assert.Contains(t, items[0].InsertText, "10 USD")
				})
			}
		}
	}
}

// The code lens command takes the same decision as the quick fix: with a text
// that balances every occurrence it applies it.
func TestFixUnbalancedCommand_AppliesATextEveryOccurrenceReadsAlike(t *testing.T) {
	shared := "2024-01-01 x\n    счёт  10 USD\n    расходы  -5 USD\n"
	for _, layout := range conflictingMarkLayouts() {
		t.Run(layout.name, func(t *testing.T) {
			ts, _, docURI, path := setupOccurrences(t, layout, shared)
			_, err := ts.ExecuteCommand(context.Background(), &protocol.ExecuteCommandParams{
				Command: "hledger.fixUnbalanced",
				Arguments: []protocol.LSPAny{
					mustMarshalLSPAny(t, string(docURI)),
					mustMarshalLSPAny(t, protocol.Range{End: protocol.Position{Line: 3}}),
				},
			})
			require.NoError(t, err)

			applied := ts.client.lastApplyEdit()
			require.NotNil(t, applied)
			doc, _ := ts.GetDocument(docURI)
			for _, amounts := range amountsAtEveryOccurrence(t, ts, path, applyTextEdits(doc, applied.Edit.Changes[docURI])) {
				assert.Equal(t, []string{"10 USD", "-10 USD"}, amounts)
			}
		})
	}
}
