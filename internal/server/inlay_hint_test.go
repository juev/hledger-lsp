package server

import (
	"context"
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

func TestInlayHint_PostingDateOverrideSuppressesOnlyRunningBalance(t *testing.T) {
	srv := NewServer()
	settings := srv.getSettings()
	settings.InlayHints.RunningBalances = true
	srv.setSettings(settings)
	docURI := uri.URI("file:///test.journal")
	content := "2024-01-01 test\n    expenses:food  $10  ; date:2024-01-02\n    assets:cash\n"
	srv.StoreDocument(docURI, content)

	hints, err := srv.InlayHint(context.Background(), &protocol.InlayHintParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
		Range:        protocol.Range{End: protocol.Position{Line: 10}},
	})
	require.NoError(t, err)
	for _, hint := range hints {
		assert.NotContains(t, hint.Label, "balance:")
	}
	assert.Len(t, hints, 1)
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
	assert.Equal(t, uint32(13), hints[0].Position.Character)
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
