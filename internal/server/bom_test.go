package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// A journal the server does not have open is read from disk, where no client
// has counted the mark: the text handed to position-based features is
// stripped of it.
func TestBOM_DocumentSourceFromDiskDropsTheMark(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disk.journal")
	require.NoError(t, os.WriteFile(path, []byte(textutil.BOM+"2024-01-01 кафе\r\n    a  1\r\n    b\r\n"), 0o644))

	ts := newTestServer()
	content, ok := ts.documentSource(uri.File(path))
	require.True(t, ok)
	assert.Equal(t, "2024-01-01 кафе\n    a  1\n    b\n", content)
}
