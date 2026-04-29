package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/server"
)

func TestServerDispatcher_CodeAction_DelegatesToServer(t *testing.T) {
	srv := server.NewServer()
	dispatcher := newServerDispatcher(srv)

	uri := protocol.DocumentURI("file:///test.journal")
	srv.StoreDocument(uri, `2024-01-15 lunch
    expenses:food  $10.00
    assets:cash    $-9.00`)

	actions, err := dispatcher.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   protocol.Position{Line: 0, Character: 10},
		},
		Context: protocol.CodeActionContext{},
	})
	require.NoError(t, err)
	require.NotEmpty(t, actions)

	foundQuickFix := false
	for _, action := range actions {
		if action.Kind == protocol.QuickFix {
			foundQuickFix = true
			break
		}
	}
	assert.True(t, foundQuickFix, "expected quickfix action in delegated response")
}

func TestServerDispatcher_ExecuteCommand_DelegatesToServer(t *testing.T) {
	srv := server.NewServer()
	dispatcher := newServerDispatcher(srv)

	_, err := dispatcher.ExecuteCommand(context.Background(), &protocol.ExecuteCommandParams{
		Command: "unknown.command",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

func TestExitCodeForConnErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "nil",
			err:  nil,
			want: 0,
		},
		{
			name: "closed stdin EOF",
			err:  fmt.Errorf("failed reading header line: %w", io.EOF),
			want: 0,
		},
		{
			name: "protocol error",
			err:  errors.New("missing Content-Length header"),
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, exitCodeForConnErr(tt.err))
		})
	}
}
