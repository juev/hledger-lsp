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
// original amount on a posting line with a rendered newAmount (issue #25).
//
// Alignment rules:
//
//   - commodity-left (`$10.00`): the commodity symbol marks the start of
//     the amount, so the `$` column is preserved. Pre-amount whitespace
//     is left untouched; width delta grows or shrinks the line at the
//     tail.
//
//   - commodity-right (`12.00 USD`): the commodity symbol marks the end
//     of the amount, so the commodity-end column is aligned with sibling
//     postings. Pre-amount whitespace is recomputed so that
//     `accountEnd + preSpace + amountWidth = targetEndCol`, clamped at
//     minPreAmountSpaces. When no sibling has an amount the original
//     end column is the target (strict local in-place replacement for
//     same-width amounts).
//
// The function does NOT mutate the AST or the document.
func buildQuickFixEdit(
	doc string,
	mapper *lsputil.PositionMapper,
	tx *ast.Transaction,
	postingIndex int,
	newAmount *ast.Amount,
	commodityFormats map[string]formatter.CommodityFormat,
) (protocol.TextEdit, bool) {
	if tx == nil || postingIndex < 0 || postingIndex >= len(tx.Postings) || newAmount == nil || mapper == nil {
		return protocol.TextEdit{}, false
	}
	posting := &tx.Postings[postingIndex]
	if posting.Amount == nil {
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

	newText := formatter.FormatAmount(newAmount, commodityFormats)
	newWidth := utf8.RuneCountInString(newText)

	toEat := 0
	toAdd := 0
	if posting.Amount.Commodity.Position == ast.CommodityRight {
		origStartCol := posting.Amount.Range.Start.Column - 1 // 1-indexed → 0-indexed
		preSpace := countPreAmountSpaces(doc, startByte)
		accountEndCol := origStartCol - preSpace

		targetEndCol := siblingMaxEndColumn(tx.Postings, postingIndex, commodityFormats)
		if targetEndCol <= 0 {
			// No sibling with an amount — preserve the original end column.
			targetEndCol = origStartCol + utf8.RuneCountInString(doc[startByte:endByte])
		}

		newStartCol := targetEndCol - newWidth
		if newStartCol-accountEndCol < minPreAmountSpaces {
			newStartCol = accountEndCol + minPreAmountSpaces
		}
		switch shift := origStartCol - newStartCol; {
		case shift > 0:
			toEat = shift
		case shift < 0:
			toAdd = -shift
		}
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

// siblingMaxEndColumn returns the largest 0-indexed rune column right
// after the last rune of any sibling posting's amount in the same
// transaction, excluding the edited posting. Returns 0 if no sibling has
// an amount. Used as the alignment target for commodity-right amounts —
// the commodity symbol (USD / RUB / …) of the edited posting is placed
// so that it ends in the same column as the widest sibling's commodity.
func siblingMaxEndColumn(postings []ast.Posting, idx int, commodityFormats map[string]formatter.CommodityFormat) int {
	maxEnd := 0
	for i := range postings {
		if i == idx {
			continue
		}
		other := postings[i].Amount
		if other == nil || other.Range.Start.Column <= 0 {
			continue
		}
		end := other.Range.Start.Column - 1 + utf8.RuneCountInString(formatter.FormatAmount(other, commodityFormats))
		if end > maxEnd {
			maxEnd = end
		}
	}
	return maxEnd
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
