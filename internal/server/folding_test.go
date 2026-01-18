package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func TestFoldingRanges_Transaction(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 grocery store
    expenses:food  $50
    assets:cash

2024-01-16 rent
    expenses:rent  $1000
    assets:checking`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.FoldingRangeParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		},
	}

	result, err := srv.FoldingRanges(context.Background(), params)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(result), 2)

	var txFolds []protocol.FoldingRange
	for _, r := range result {
		if r.Kind == protocol.RegionFoldingRange {
			txFolds = append(txFolds, r)
		}
	}
	require.Len(t, txFolds, 2)

	assert.Equal(t, uint32(0), txFolds[0].StartLine)
	assert.GreaterOrEqual(t, txFolds[0].EndLine, uint32(2))

	assert.Equal(t, uint32(4), txFolds[1].StartLine)
	assert.GreaterOrEqual(t, txFolds[1].EndLine, uint32(6))
}

func TestFoldingRanges_Directive(t *testing.T) {
	srv := NewServer()
	content := `account expenses:food
    format 1,000.00 USD
    note Food and groceries

account assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.FoldingRangeParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		},
	}

	result, err := srv.FoldingRanges(context.Background(), params)
	require.NoError(t, err)
	require.NotEmpty(t, result)

	assert.Equal(t, uint32(0), result[0].StartLine)
	assert.Equal(t, uint32(2), result[0].EndLine)
}

func TestFoldingRanges_CommentBlock(t *testing.T) {
	srv := NewServer()
	content := `; This is a comment block
; that spans multiple lines
; explaining something important

2024-01-15 test
    expenses:food  $50
    assets:cash`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.FoldingRangeParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		},
	}

	result, err := srv.FoldingRanges(context.Background(), params)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(result), 1)

	var commentFold *protocol.FoldingRange
	for i := range result {
		if result[i].Kind == protocol.CommentFoldingRange {
			commentFold = &result[i]
			break
		}
	}
	require.NotNil(t, commentFold, "expected to find a comment fold")
	assert.Equal(t, uint32(0), commentFold.StartLine)
	assert.Equal(t, uint32(2), commentFold.EndLine)
}

func TestFoldingRanges_EmptyDocument(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, "")

	params := &protocol.FoldingRangeParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		},
	}

	result, err := srv.FoldingRanges(context.Background(), params)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestFoldingRanges_DocumentNotFound(t *testing.T) {
	srv := NewServer()

	params := &protocol.FoldingRangeParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: "file:///nonexistent.journal"},
		},
	}

	result, err := srv.FoldingRanges(context.Background(), params)
	require.NoError(t, err)
	assert.Nil(t, result)
}
