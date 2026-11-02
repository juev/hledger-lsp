package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/textutil"
)

// hledger accepts a byte order mark at the start of a file. An editor may pass
// it through in didOpen; the document must then parse and format exactly like
// its BOM-less twin.
func TestBOM_OpenDocumentBehavesLikeBOMLessTwin(t *testing.T) {
	cases := map[string]string{
		"ASCII LF":        "2024-01-01 shop\n    expenses:food  1 USD\n    assets:cash\n",
		"Cyrillic CRLF":   "2024-01-01 кафе\r\n    расходы:еда  100 ₽\r\n    активы:наличные\r\n",
		"CJK LF":          "2024-01-01 食品店\n    支出:食品  100 JPY\n    資産:現金\n",
		"leading comment": "; header\n2024-01-01 shop\n    e:x  1 USD\n    a:y\n",
		"directive first": "account assets:cash\n2024-01-01 shop\n    e:x  1 USD\n    assets:cash\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			plain := newTestServer()
			plainURI := uri.URI("file:///plain.journal")
			plainDiags, err := plain.openAndWait(plainURI, content)
			require.NoError(t, err)
			plainEdits, err := plain.format(plainURI)
			require.NoError(t, err)

			withBOM := newTestServer()
			bomURI := uri.URI("file:///bom.journal")
			bomDiags, err := withBOM.openAndWait(bomURI, textutil.BOM+content)
			require.NoError(t, err)
			bomEdits, err := withBOM.format(bomURI)
			require.NoError(t, err)

			assert.Empty(t, codesIn(bomDiags), "a leading BOM must not produce parse errors")
			assert.Equal(t, codesIn(plainDiags), codesIn(bomDiags))
			assert.Equal(t, plainEdits, bomEdits)
		})
	}
}

// The client counts the mark as one UTF-16 unit on its first line, so a range
// that starts at the date must start at character 1.
func TestBOM_FirstLinePositionsCountTheMark(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///bom-positions.journal")
	diags, err := ts.openAndWait(docURI, textutil.BOM+"2024-01-01 кафе\n    a  1 USD\n    b  2 USD\n")
	require.NoError(t, err)

	unbalanced := diagnosticWithCode(diags, "UNBALANCED")
	require.NotNil(t, unbalanced, "got %v", codesIn(diags))
	assert.Equal(t, uint32(0), unbalanced.Range.Start.Line)
	assert.Equal(t, uint32(1), unbalanced.Range.Start.Character)
}

// A mark anywhere but the start of the file stays a syntax error, as in hledger.
func TestBOM_MidFileMarkStaysAnError(t *testing.T) {
	ts := newTestServer()
	docURI := uri.URI("file:///bom-mid.journal")
	diags, err := ts.openAndWait(docURI, "2024-01-01 a\n    e:x  1\n    a:y\n"+textutil.BOM+"2024-01-02 b\n    e:x  1\n    a:y\n")
	require.NoError(t, err)
	assert.NotNil(t, diagnosticWithCode(diags, "PARSE_UNEXPECTED"), "got %v", codesIn(diags))
}

// A journal read from disk keeps its mark, like an open buffer does, so offsets
// agree between the two and the mapper counts the mark as the client does.
func TestBOM_DocumentSourceFromDiskKeepsTheMark(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disk.journal")
	require.NoError(t, os.WriteFile(path, []byte(textutil.BOM+"2024-01-01 кафе\r\n    a  1\r\n    b\r\n"), 0o644))

	ts := newTestServer()
	content, ok := ts.documentSource(uri.File(path))
	require.True(t, ok)
	assert.Equal(t, textutil.BOM+"2024-01-01 кафе\n    a  1\n    b\n", content)
}

const bomMainJournal = "include child.journal\n\n2024-01-01 * x\n    expenses:a  $1\n    assets:cash\n"

func writeBOMTree(t *testing.T, childContent string) (dir string, mainURI, childURI uri.URI) {
	t.Helper()
	dir = t.TempDir()
	mainPath := filepath.Join(dir, "main.journal")
	childPath := filepath.Join(dir, "child.journal")
	require.NoError(t, os.WriteFile(mainPath, []byte(bomMainJournal), 0o644))
	require.NoError(t, os.WriteFile(childPath, []byte(childContent), 0o644))
	return dir, uri.File(mainPath), uri.File(childPath)
}

// The tree reads the child from disk, where the mark is, and the editor holds
// it open. Clients differ: one sends the buffer with the mark, another (Neovim)
// without it. Ranges computed from the tree must be right for both, so this
// test covers a buffer that keeps the mark; TestBOM_RenameAndDiagnosticsWithOpenChildWithoutTheMark
// covers one without it.
func TestBOM_RenameAcrossFilesWithOpenBOMChild(t *testing.T) {
	childContent := textutil.BOM + "2024-01-02 * кафе\n    expenses:b  $2\n    assets:cash\n"
	dir, mainURI, childURI := writeBOMTree(t, childContent)
	ts := initWorkspaceTestServer(t, dir)
	_, _ = ts.openAndWait(childURI, childContent)
	_, _ = ts.openAndWait(mainURI, bomMainJournal)

	edit, err := ts.Rename(context.Background(), &protocol.RenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: mainURI},
			Position:     protocol.Position{Line: 4, Character: 6},
		},
		NewName: "assets:bank",
	})
	require.NoError(t, err)
	require.NotNil(t, edit)

	childEdits := edit.Changes[childURI]
	require.Len(t, childEdits, 1)
	assert.Equal(t, protocol.Range{
		Start: protocol.Position{Line: 2, Character: 4},
		End:   protocol.Position{Line: 2, Character: 15},
	}, childEdits[0].Range)

	out := childContent
	for i := len(childEdits) - 1; i >= 0; i-- {
		out = applyChange(out, childEdits[i].Range, childEdits[i].NewText)
	}
	assert.Equal(t, textutil.BOM+"2024-01-02 * кафе\n    expenses:b  $2\n    assets:bank\n", out)
}

func TestBOM_CrossFileDiagnosticRangesForOpenBOMChild(t *testing.T) {
	body := "2024-01-02 * ok\n    expenses:b  $2\n    assets:cash\n\n2024-01-03 * bad\n    expenses:c  $2\n    assets:cash  $3\n"
	for _, bom := range []string{"", textutil.BOM} {
		t.Run(map[string]string{"": "plain", textutil.BOM: "BOM"}[bom], func(t *testing.T) {
			dir, mainURI, childURI := writeBOMTree(t, bom+body)
			ts := initWorkspaceTestServer(t, dir)
			_, _ = ts.openAndWait(childURI, bom+body)
			_, _ = ts.openAndWait(mainURI, bomMainJournal)

			diags := waitForDocument(t, ts.client, childURI)
			unbalanced := diagnosticWithCode(diags, "UNBALANCED")
			require.NotNil(t, unbalanced, "got %v", codesIn(diags))
			assert.Equal(t, uint32(4), unbalanced.Range.Start.Line)
			assert.Equal(t, uint32(0), unbalanced.Range.Start.Character)
			assert.Equal(t, uint32(7), unbalanced.Range.End.Line)
		})
	}
}

// Hit testing, completion and highlights on line 1 of a buffer that starts with
// the mark use the client's columns, where the mark is one character.
func TestBOM_FirstLineFeaturesUseClientColumns(t *testing.T) {
	body := "account expenses:food  ; note\n2024-01-01 кафе\n    expenses:food  1 USD\n    assets:cash\n"
	for name, prefix := range map[string]string{"plain": "", "BOM": textutil.BOM} {
		t.Run(name, func(t *testing.T) {
			shift := uint32(len([]rune(prefix)))
			ts := newTestServer()
			docURI := uri.URI("file:///line1.journal")
			_, err := ts.openAndWait(docURI, prefix+body)
			require.NoError(t, err)

			// "expenses:food" spans characters 8..21 of the line without the mark.
			for _, ch := range []uint32{8, 14, 20} {
				locations, err := ts.definition(docURI, 0, ch+shift)
				require.NoError(t, err)
				assert.Len(t, locations, 1, "definition at %d", ch)

				highlights, err := ts.DocumentHighlight(context.Background(), &protocol.DocumentHighlightParams{
					TextDocumentPositionParams: protocol.TextDocumentPositionParams{
						TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
						Position:     protocol.Position{Line: 0, Character: ch + shift},
					},
				})
				require.NoError(t, err)
				assert.Len(t, highlights, 2, "highlight at %d", ch)
			}
			locations, err := ts.definition(docURI, 0, 7+shift)
			require.NoError(t, err)
			assert.Empty(t, locations, "the space before the name is not the account")

			list, err := ts.completion(docURI, 0, 11+shift)
			require.NoError(t, err)
			require.NotNil(t, list)
			require.NotEmpty(t, list.Items, "completion inside the account name of line 1")
			edit, ok := list.Items[0].TextEdit.(*protocol.TextEdit)
			require.True(t, ok)
			assert.Equal(t, uint32(0), edit.Range.Start.Line)
			assert.Equal(t, 8+shift, edit.Range.Start.Character)
			assert.Equal(t, 21+shift, edit.Range.End.Character)
		})
	}
}

func TestBOM_FoldingRangesOfFirstLineDirective(t *testing.T) {
	body := "commodity USD\n  format 1,000.00 USD\n\n2024-01-01 a\n    e:x  1 USD\n    a:y\n"
	fold := func(prefix string) []protocol.FoldingRange {
		ts := newTestServer()
		docURI := uri.URI("file:///fold.journal")
		_, err := ts.openAndWait(docURI, prefix+body)
		require.NoError(t, err)
		ranges, err := ts.FoldingRanges(context.Background(), &protocol.FoldingRangeParams{TextDocument: protocol.TextDocumentIdentifier{URI: docURI}})
		require.NoError(t, err)
		return ranges
	}
	plain := fold("")
	require.NotEmpty(t, plain)
	assert.Equal(t, plain, fold(textutil.BOM))
}

func TestBOM_RulesBufferFoldingAndSymbols(t *testing.T) {
	body := "skip 1\nfields date,description,amount\nif кафе\n  account2 расходы:еда\n"
	plainFolds := rulesFoldingRanges(body)
	plainSymbols := rulesDocumentSymbols(body)
	require.NotEmpty(t, plainSymbols)

	assert.Equal(t, plainFolds, rulesFoldingRanges(textutil.BOM+body))
	bomSymbols := rulesDocumentSymbols(textutil.BOM + body)
	require.Len(t, bomSymbols, len(plainSymbols))
	for i := range plainSymbols {
		assert.Equal(t, plainSymbols[i].Name, bomSymbols[i].Name)
		assert.Equal(t, plainSymbols[i].Range.Start.Line, bomSymbols[i].Range.Start.Line)
	}
}

// Rules files: line-1 ranges, semantic tokens, links and symbols count the mark
// as one character, like the client does.
func TestBOM_RulesFirstLinePositions(t *testing.T) {
	ctx := context.Background()
	docURI := uri.URI("file:///tmp/bom-rules/x.csv.rules")
	open := func(t *testing.T, text string) *testServer {
		t.Helper()
		ts := newTestServer()
		_, err := ts.openAndWait(docURI, text)
		require.NoError(t, err)
		return ts
	}
	identifier := protocol.TextDocumentIdentifier{URI: docURI}

	t.Run("diagnostic", func(t *testing.T) {
		ts := open(t, textutil.BOM+"ski\n")
		params := ts.client.getLastDiagnostics()
		require.NotNil(t, params)
		require.Len(t, params.Diagnostics, 1)
		assert.Equal(t, protocol.Range{
			Start: protocol.Position{Line: 0, Character: 1},
			End:   protocol.Position{Line: 0, Character: 4},
		}, params.Diagnostics[0].Range)
	})

	t.Run("semantic tokens", func(t *testing.T) {
		ts := open(t, textutil.BOM+"skip 1\nfields date,description,amount\n")
		tokens, err := ts.SemanticTokensFull(ctx, &protocol.SemanticTokensParams{TextDocument: identifier})
		require.NoError(t, err)
		require.NotNil(t, tokens)
		require.GreaterOrEqual(t, len(tokens.Data), 5)
		assert.Equal(t, uint32(0), tokens.Data[0], "line")
		assert.Equal(t, uint32(1), tokens.Data[1], "the first token starts after the mark")
		assert.Equal(t, uint32(4), tokens.Data[2], "length of skip")
	})

	t.Run("symbols and links", func(t *testing.T) {
		ts := open(t, textutil.BOM+"skip 1\nsource ./data.csv\n")
		symbols := rulesDocumentSymbols(textutil.BOM + "skip 1\nsource ./data.csv\n")
		var skipRange *protocol.Range
		for i := range symbols {
			if symbols[i].Name == "skip 1" {
				skipRange = &symbols[i].Range
			}
		}
		require.NotNil(t, skipRange)
		assert.Equal(t, protocol.Range{
			Start: protocol.Position{Line: 0, Character: 1},
			End:   protocol.Position{Line: 0, Character: 7},
		}, *skipRange)

		links, err := ts.DocumentLink(ctx, &protocol.DocumentLinkParams{TextDocument: identifier})
		require.NoError(t, err)
		require.Len(t, links, 1)
		assert.Equal(t, uint32(1), links[0].Range.Start.Line)

		ts = open(t, textutil.BOM+"source ./data.csv\n")
		links, err = ts.DocumentLink(ctx, &protocol.DocumentLinkParams{TextDocument: identifier})
		require.NoError(t, err)
		require.Len(t, links, 1)
		assert.Equal(t, protocol.Range{
			Start: protocol.Position{Line: 0, Character: 8},
			End:   protocol.Position{Line: 0, Character: 17 + 1},
		}, links[0].Range, "the path of line 1 shifts by the mark")
	})
}

func TestBOM_RulesCompletionOnFirstLine(t *testing.T) {
	docURI := uri.URI("file:///tmp/bom-rules/c.csv.rules")

	t.Run("directive prefix keeps the mark", func(t *testing.T) {
		ts := newTestServer()
		doc := textutil.BOM + "ski\n"
		_, err := ts.openAndWait(docURI, doc)
		require.NoError(t, err)
		list, err := ts.completion(docURI, 0, 4)
		require.NoError(t, err)
		require.NotNil(t, list)
		require.NotEmpty(t, list.Items)
		edit, ok := list.Items[0].TextEdit.(*protocol.TextEdit)
		require.True(t, ok)
		assert.Equal(t, "skip", list.Items[0].Label)
		assert.Equal(t, textutil.BOM+"skip\n", applyTextEdits(doc, []protocol.TextEdit{*edit}))
	})

	t.Run("no directives inside a path", func(t *testing.T) {
		ts := newTestServer()
		doc := textutil.BOM + "source ./東京.csv\n"
		_, err := ts.openAndWait(docURI, doc)
		require.NoError(t, err)
		list, err := ts.completion(docURI, 0, 11)
		require.NoError(t, err)
		require.NotNil(t, list)
		assert.Empty(t, list.Items)
	})
}

// Pressing Enter after a transaction header on line 1 indents the next line,
// with or without the mark in front of it.
func TestBOM_OnTypeNewlineAfterFirstLineHeader(t *testing.T) {
	for name, prefix := range map[string]string{"plain": "", "BOM": textutil.BOM} {
		t.Run(name, func(t *testing.T) {
			ts := newTestServer()
			docURI := uri.URI("file:///tmp/bom-ontype.journal")
			_, err := ts.openAndWait(docURI, prefix+"2024-01-01 кафе\n\n")
			require.NoError(t, err)
			edits, err := ts.OnTypeFormatting(context.Background(), &protocol.DocumentOnTypeFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
				Position:     protocol.Position{Line: 1, Character: 0},
				Ch:           "\n",
				Options:      protocol.FormattingOptions{TabSize: 4, InsertSpaces: true},
			})
			require.NoError(t, err)
			require.Len(t, edits, 1)
			assert.Equal(t, "    ", edits[0].NewText)
		})
	}
}
