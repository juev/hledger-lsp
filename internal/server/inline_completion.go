package server

import (
	"context"
	"encoding/json"

	"go.lsp.dev/protocol"
)

type InlineCompletionTriggerKind int

const (
	InlineCompletionTriggerInvoked   InlineCompletionTriggerKind = 1
	InlineCompletionTriggerAutomatic InlineCompletionTriggerKind = 2
)

type InlineCompletionParams struct {
	TextDocument protocol.TextDocumentIdentifier `json:"textDocument"`
	Position     protocol.Position               `json:"position"`
	Context      InlineCompletionContext         `json:"context"`
}

type InlineCompletionContext struct {
	TriggerKind InlineCompletionTriggerKind `json:"triggerKind"`
}

type InlineCompletionItem struct {
	InsertText string          `json:"insertText"`
	FilterText string          `json:"filterText,omitempty"`
	Range      *protocol.Range `json:"range,omitempty"`
}

type InlineCompletionList struct {
	Items []InlineCompletionItem `json:"items"`
}

func (s *Server) InlineCompletion(_ context.Context, params json.RawMessage) (*InlineCompletionList, error) {
	var p InlineCompletionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	_, ok := s.GetDocument(p.TextDocument.URI)
	if !ok {
		return &InlineCompletionList{Items: []InlineCompletionItem{}}, nil
	}

	return &InlineCompletionList{Items: []InlineCompletionItem{}}, nil
}
