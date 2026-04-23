package server

import (
	"strings"
	"unicode/utf8"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/formatter"
	"github.com/juev/hledger-lsp/internal/lsputil"
)

// minPreAmountSpaces is the minimum whitespace between an account and its
// amount enforced by hledger's journal grammar.
const minPreAmountSpaces = 2

// buildQuickFixEdit produces a minimal LSP TextEdit that replaces the
// original amount on a posting line with a rendered newAmount, absorbing
// the width delta into the whitespace between the account and the amount.
//
// Behavior (see issue #25):
//   - equal-width newAmount → edit covers only the amount range;
//   - wider newAmount       → eats spaces from the pre-amount whitespace
//     down to minPreAmountSpaces; any residual width causes the line to
//     grow (comment / commodity symbol shifts right);
//   - narrower newAmount    → prepends spaces inside newText so that the
//     amount's end column stays in place.
//
// The function does NOT mutate the AST or the document; posting is read-only.
func buildQuickFixEdit(
	doc string,
	mapper *lsputil.PositionMapper,
	posting *ast.Posting,
	newAmount *ast.Amount,
	commodityFormats map[string]formatter.CommodityFormat,
) (protocol.TextEdit, bool) {
	if posting == nil || posting.Amount == nil || newAmount == nil || mapper == nil {
		return protocol.TextEdit{}, false
	}

	startByte := posting.Amount.Range.Start.Offset
	endByte := posting.Amount.Range.End.Offset
	if startByte < 0 || endByte <= startByte || endByte > len(doc) {
		return protocol.TextEdit{}, false
	}
	// The parser's Amount.Range.End points to the next token (or end of
	// line), so any trailing whitespace after the commodity — e.g. before
	// a posting comment — is part of the reported range. Trim it so we
	// only replace the amount itself.
	for endByte > startByte {
		b := doc[endByte-1]
		if b != ' ' && b != '\t' {
			break
		}
		endByte--
	}

	oldWidth := utf8.RuneCountInString(doc[startByte:endByte])
	newText := formatter.FormatAmount(newAmount, commodityFormats)
	newWidth := utf8.RuneCountInString(newText)
	delta := newWidth - oldWidth

	preSpaceBytes := countPreAmountSpaces(doc, startByte)

	toEat := 0
	toAdd := 0
	switch {
	case delta > 0:
		if canEat := preSpaceBytes - minPreAmountSpaces; canEat > 0 {
			toEat = canEat
			if toEat > delta {
				toEat = delta
			}
		}
	case delta < 0:
		toAdd = -delta
	}

	editText := newText
	if toAdd > 0 {
		editText = strings.Repeat(" ", toAdd) + newText
	}

	return protocol.TextEdit{
		Range: protocol.Range{
			Start: mapper.ByteToLSP(startByte - toEat),
			End:   mapper.ByteToLSP(endByte),
		},
		NewText: editText,
	}, true
}

// countPreAmountSpaces returns the number of space/tab bytes immediately
// preceding amountStartByte on the same line. Stops at any non-whitespace
// byte or line break.
func countPreAmountSpaces(doc string, amountStartByte int) int {
	n := 0
	for amountStartByte-n-1 >= 0 {
		b := doc[amountStartByte-n-1]
		if b != ' ' && b != '\t' {
			break
		}
		n++
	}
	return n
}
