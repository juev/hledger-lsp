package server

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestInlayHint_LocalInferredAmount(t *testing.T) {
	srv := NewServer()
	docURI := uri.URI("file:///test.journal")
	content := "2024-01-15 groceries\n    expenses:food  $50\n    assets:cash\n"
	srv.StoreDocument(docURI, content)

	hints, err := srv.InlayHint(context.Background(), &protocol.InlayHintParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
		Range:        protocol.Range{Start: protocol.Position{}, End: protocol.Position{Line: 10}},
	})
	require.NoError(t, err)
	require.Len(t, hints, 1)
	assert.Equal(t, protocol.String("= -50 $"), hints[0].Label)
	assert.Equal(t, uint32(2), hints[0].Position.Line)
	require.NotNil(t, hints[0].PaddingLeft)
	assert.True(t, *hints[0].PaddingLeft)
}

func TestInlayHint_InferredAmountAnchoredAtLineEnd(t *testing.T) {
	srv := NewServer()
	docURI := uri.URI("file:///test.journal")
	// The trailing spaces are the alignment padding Tab inserts before the user
	// types the amount. Anchoring the hint inside the line would shift that
	// padding — and the cursor sitting at its end — to the right.
	lastLine := "    Expenses:Food          "
	content := "2026-08-03 Good\n    Expenses:Test  -20.50 USD\n" + lastLine + "\n"
	srv.StoreDocument(docURI, content)

	hints, err := srv.InlayHint(context.Background(), &protocol.InlayHintParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
		Range:        protocol.Range{End: protocol.Position{Line: 10}},
	})
	require.NoError(t, err)
	require.Len(t, hints, 1)
	assert.Equal(t, protocol.String("= 20.5 USD"), hints[0].Label)
	assert.Equal(t, uint32(2), hints[0].Position.Line)
	assert.Equal(t, uint32(len(lastLine)), hints[0].Position.Character)
}

func TestInlayHint_CostAndChronologicalRunningBalance(t *testing.T) {
	srv := NewServer()
	settings := srv.getSettings()
	settings.InlayHints.InferredAmounts = false
	settings.InlayHints.CostExpansion = true
	settings.InlayHints.RunningBalances = true
	srv.setSettings(settings)
	docURI := uri.URI("file:///test.journal")
	content := "2024-01-02 later\n    assets:stock  2 AAPL @ $10\n    assets:cash  -$20\n\n2024-01-01 earlier\n    assets:cash  $5\n    income:salary  -$5\n"
	srv.StoreDocument(docURI, content)

	hints, err := srv.InlayHint(context.Background(), &protocol.InlayHintParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
		Range:        protocol.Range{End: protocol.Position{Line: 10}},
	})
	require.NoError(t, err)
	labels := make([]protocol.InlayHintLabel, 0, len(hints))
	for _, hint := range hints {
		labels = append(labels, hint.Label)
	}
	assert.Contains(t, labels, protocol.String("cost: 20 $"))
	assert.Contains(t, labels, protocol.String("balance: -15 $"))
}

func TestInlayHint_RunningBalanceOfDatedPostingUsesItsOwnDate(t *testing.T) {
	srv := NewServer()
	settings := srv.getSettings()
	settings.InlayHints.RunningBalances = true
	srv.setSettings(settings)
	docURI := uri.URI("file:///test.journal")
	settings.InlayHints.InferredAmounts = false
	srv.setSettings(settings)
	content := "2024-01-01 test\n    assets:card  -10 USD  ; date:2024-01-05\n    expenses:food\n\n" +
		"2024-01-02 other\n    assets:card  -1 USD\n    expenses:food\n"
	srv.StoreDocument(docURI, content)

	hints, err := srv.InlayHint(context.Background(), &protocol.InlayHintParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
		Range:        protocol.Range{End: protocol.Position{Line: 10}},
	})
	require.NoError(t, err)
	labels := make(map[uint32]protocol.InlayHintLabel)
	for _, hint := range hints {
		labels[hint.Position.Line] = hint.Label
	}
	assert.Equal(t, protocol.String("balance: -11 USD"), labels[1])
	assert.Equal(t, protocol.String("balance: -1 USD"), labels[5])
}

func TestInlayHint_FeatureAndRangeGates(t *testing.T) {
	srv := NewServer()
	docURI := uri.URI("file:///test.journal")
	srv.StoreDocument(docURI, "2024-01-01 test\n    expenses:food  $10\n    assets:cash\n")
	params := &protocol.InlayHintParams{TextDocument: protocol.TextDocumentIdentifier{URI: docURI}, Range: protocol.Range{End: protocol.Position{Line: 10}}}

	settings := srv.getSettings()
	settings.Features.InlayHints = false
	srv.setSettings(settings)
	hints, err := srv.InlayHint(context.Background(), params)
	require.NoError(t, err)
	assert.Empty(t, hints)

	settings.Features.InlayHints = true
	srv.setSettings(settings)
	params.Range = protocol.Range{End: protocol.Position{Line: 2}}
	hints, err = srv.InlayHint(context.Background(), params)
	require.NoError(t, err)
	assert.Empty(t, hints, "end of requested range is exclusive")
}

func TestInlayHint_UsesUTF16PositionAndCRLF(t *testing.T) {
	srv := NewServer()
	docURI := uri.URI("file:///test.journal")
	content := "2024-01-01 🧾\r\n    расходы:еда  $10\r\n    assets:cash\r\n"
	srv.StoreDocument(docURI, content)
	hints, err := srv.InlayHint(context.Background(), &protocol.InlayHintParams{TextDocument: protocol.TextDocumentIdentifier{URI: docURI}, Range: protocol.Range{End: protocol.Position{Line: 10}}})
	require.NoError(t, err)
	require.Len(t, hints, 1)
	assert.Equal(t, uint32(2), hints[0].Position.Line)
	// End of "    assets:cash", the carriage return excluded.
	assert.Equal(t, uint32(15), hints[0].Position.Character)
}

func TestInlayHint_InferFinalPostingAtEOF(t *testing.T) {
	srv := NewServer()
	docURI := uri.URI("file:///test.journal")
	srv.StoreDocument(docURI, "2024-07-23 Метрополитен\n    Расходы:Транспорт  71,00\n    Расходы:Транспорт  71,00\n    Активы:Сбербанк:Текущий")
	hints, err := srv.InlayHint(context.Background(), &protocol.InlayHintParams{TextDocument: protocol.TextDocumentIdentifier{URI: docURI}, Range: protocol.Range{End: protocol.Position{Line: 10}}})
	require.NoError(t, err)
	require.Len(t, hints, 1)
	assert.Equal(t, protocol.String("= -142"), hints[0].Label)
}

func TestInlayHint_RunningBalanceOmittedWhereRegisterHidesThePosting(t *testing.T) {
	for name, eol := range map[string]string{"LF": "\n", "CRLF": "\r\n"} {
		t.Run(name, func(t *testing.T) {
			srv := NewServer()
			settings := srv.getSettings()
			settings.InlayHints.InferredAmounts = false
			settings.InlayHints.RunningBalances = true
			srv.setSettings(settings)
			docURI := uri.URI("file:///test.journal")
			content := strings.ReplaceAll("2024-01-01 сид\n    касса  100 ₽\n    капитал\n\n"+
				"2024-01-02 проверка\n    касса  = 100 ₽\n    капитал\n\n"+
				"2024-01-03 ноль\n    касса  0 ₽\n    капитал\n", "\n", eol)
			require.NoError(t, srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
				TextDocument: protocol.TextDocumentItem{URI: docURI, Text: content},
			}))

			hints, err := srv.InlayHint(context.Background(), &protocol.InlayHintParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
				Range:        protocol.Range{End: protocol.Position{Line: 20}},
			})
			require.NoError(t, err)
			labels := make(map[uint32]protocol.InlayHintLabel)
			for _, hint := range hints {
				labels[hint.Position.Line] = hint.Label
			}
			assert.Equal(t, protocol.String("balance: 100 ₽"), labels[1])
			assert.NotContains(t, labels, uint32(5), "assignment that moves nothing")
			assert.NotContains(t, labels, uint32(6), "elided posting balancing nothing")
			assert.Equal(t, protocol.String("balance: 100 ₽"), labels[9], "written zero")
		})
	}
}

// A balance assignment moves its account by the difference from the balance it
// had, and the elided posting balances that difference, not the assigned
// balance. The amount an assignment moves sits just before its `=`.
func TestInlayHint_BalanceAssignmentShowsTheAmountItMoves(t *testing.T) {
	for name, newline := range map[string]string{"lf": "\n", "crlf": "\r\n"} {
		t.Run(name, func(t *testing.T) {
			srv := NewServer()
			docURI := uri.URI("file:///test.journal")
			lines := []string{
				"commodity 1,000.00 USD",
				"",
				"2026-07-31 Zestimate",
				"    активы:дом  =517,300.00 USD",
				"    equity:gains",
				"",
				"2026-08-03 Zestimate",
				"    активы:дом  =522,000.00 USD",
				"    equity:gains",
				"",
			}
			srv.StoreDocument(docURI, strings.Join(lines, newline))

			hints, err := srv.InlayHint(context.Background(), &protocol.InlayHintParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
				Range:        protocol.Range{End: protocol.Position{Line: 10}},
			})
			require.NoError(t, err)
			type placed struct {
				line, character uint32
				label           protocol.InlayHintLabel
			}
			var got []placed
			for _, hint := range hints {
				got = append(got, placed{hint.Position.Line, hint.Position.Character, hint.Label})
			}
			assignAt := uint32(len("    ") + len([]rune("активы:дом")) + len("  "))
			assert.Equal(t, []placed{
				{3, assignAt, protocol.String("517,300.00 USD")},
				{4, uint32(len("    equity:gains")), protocol.String("= -517,300.00 USD")},
				{7, assignAt, protocol.String("4,700.00 USD")},
				{8, uint32(len("    equity:gains")), protocol.String("= -4,700.00 USD")},
			}, got)
			assert.Nil(t, hints[0].PaddingLeft)
		})
	}
}
