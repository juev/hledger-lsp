package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/csvimport"
)

// The import places a record by offsets in csvimport.CoordinateText of the
// saved file; mapping them through the editor buffer instead would shift every
// position by a byte order mark, and garble UTF-16 entirely.
func TestMapperFor_ImportDataUsesTheTextRecordsArePlacedIn(t *testing.T) {
	const csv = "date,desc,amount\r\n2024-01-02,Кофе Ċ,-3.50\r\n2024-01-03,salary,1000\r\n"
	encode16 := func(text string) string {
		var b strings.Builder
		b.WriteString("\xff\xfe")
		for _, unit := range utf16.Encode([]rune(text)) {
			b.WriteByte(byte(unit))
			b.WriteByte(byte(unit >> 8))
		}
		return b.String()
	}
	for name, raw := range map[string]string{"utf16.csv": encode16(csv), "bom.csv": "\xef\xbb\xbf" + csv} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, name)
			require.NoError(t, os.WriteFile(path, []byte(raw), 0o644))
			srv := NewServer()
			docURI := uri.File(path)
			// The editor shows the decoded text without the byte order mark.
			srv.documents.Store(docURI, strings.ReplaceAll(csv, "\r\n", "\n"))

			text, ok := csvimport.CoordinateText(raw)
			require.True(t, ok)
			third := strings.Index(text, "2024-01-03")
			mapper := srv.mapperFor(docURI)
			require.NotNil(t, mapper)
			assert.Equal(t, protocol.Position{Line: 2, Character: 0}, mapper.ByteToLSP(third))
			lineEnd := third + len("2024-01-03,salary,1000")
			assert.Equal(t, protocol.Position{Line: 2, Character: 22}, mapper.ByteToLSP(lineEnd))
		})
	}
}
