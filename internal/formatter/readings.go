package formatter

import (
	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/ast"
)

// Reading is one way a document is read: its parse under one include context,
// with the commodity formats in force there. A file included more than once is
// read once per distinct context, and hledger reads the same text as different
// values under different contexts.
type Reading struct {
	Journal *ast.Journal
	Formats map[string]CommodityFormat
}

// FormatDocumentInReadings formats content so that every reading keeps the
// values it had. Rewriting a number from one reading's formats can change what
// another reading makes of it (`10 EUR` rendered as `10,00 EUR` is a thousand
// under a `.` decimal mark), so the numbers are rewritten only when every
// reading renders every line to the same text. When the readings disagree
// anywhere, the document keeps its numbers as written and only the layout
// (indent, spacing, alignment) is normalised: alignment columns follow the
// widths of the text that is actually written, which keeps the result
// idempotent.
//
// With a single reading it is FormatDocumentWithOptions.
func FormatDocumentInReadings(readings []Reading, content string, opts Options) []protocol.TextEdit {
	if len(readings) == 0 {
		return nil
	}
	first := readings[0]
	if len(readings) == 1 {
		return FormatDocumentWithOptions(first.Journal, content, first.Formats, opts)
	}

	if opts.IndentSize <= 0 {
		opts.IndentSize = defaultIndentSize
	}

	// One shared layout makes the renderings comparable line by line.
	alignment := ComputeAlignment(first.Journal, content, formatsOrDeclared(first), opts)

	var base []protocol.TextEdit
	var baseText map[uint32]string
	for index, reading := range readings {
		edits := formatDocument(reading.Journal, content, formatsOrDeclared(reading), opts, &alignment)
		if index == 0 {
			base, baseText = edits, textByLine(edits)
			continue
		}
		if !sameText(baseText, textByLine(edits), first.Journal) {
			// No format at all: every amount falls back to the text it was
			// written with.
			return formatDocument(first.Journal, content, map[string]CommodityFormat{}, opts, nil)
		}
	}
	return base
}

func formatsOrDeclared(reading Reading) map[string]CommodityFormat {
	if reading.Formats != nil {
		return reading.Formats
	}
	return extractCommodityFormats(reading.Journal)
}

func textByLine(edits []protocol.TextEdit) map[uint32]string {
	texts := make(map[uint32]string, len(edits))
	for _, edit := range edits {
		texts[edit.Range.Start.Line] = edit.NewText
	}
	return texts
}

// sameText reports whether two renderings agree on every posting line of
// journal, a rendering that leaves a line alone counting as one of its own.
func sameText(want, got map[uint32]string, journal *ast.Journal) bool {
	for _, posting := range AllPostings(journal) {
		line := uint32(posting.Range.Start.Line - 1)
		wantText, wantOK := want[line]
		if gotText, ok := got[line]; ok != wantOK || gotText != wantText {
			return false
		}
	}
	return true
}
