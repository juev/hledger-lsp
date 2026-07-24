package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func TestServer_Initialize_CapabilityProfiles(t *testing.T) {
	trueValue := true

	tests := []struct {
		name               string
		capabilities       protocol.ClientCapabilities
		wantRenameOptions  bool
		wantCodeActionOpts bool
		wantSnapshot       clientCapabilities
	}{
		{
			name:         "minimal",
			wantSnapshot: clientCapabilities{},
		},
		{
			name: "full",
			capabilities: protocol.ClientCapabilities{
				Workspace: &protocol.WorkspaceClientCapabilities{
					DidChangeWatchedFiles: &protocol.DidChangeWatchedFilesClientCapabilities{
						DynamicRegistration: &trueValue,
					},
				},
				TextDocument: &protocol.TextDocumentClientCapabilities{
					Rename: &protocol.RenameClientCapabilities{PrepareSupport: &trueValue},
					CodeAction: &protocol.CodeActionClientCapabilities{
						CodeActionLiteralSupport: protocol.ClientCodeActionLiteralOptions{
							CodeActionKind: protocol.ClientCodeActionKindOptions{
								ValueSet: []protocol.CodeActionKind{protocol.CodeActionKindQuickFix},
							},
						},
						IsPreferredSupport: &trueValue,
					},
					DocumentSymbol: &protocol.DocumentSymbolClientCapabilities{
						HierarchicalDocumentSymbolSupport: &trueValue,
					},
					Completion: &protocol.CompletionClientCapabilities{
						CompletionItem: &protocol.ClientCompletionItemOptions{
							DocumentationFormat: []protocol.MarkupKind{protocol.MarkupKindMarkdown},
						},
					},
					Hover: &protocol.HoverClientCapabilities{
						ContentFormat: []protocol.MarkupKind{protocol.MarkupKindPlainText},
					},
				},
			},
			wantRenameOptions:  true,
			wantCodeActionOpts: true,
			wantSnapshot: clientCapabilities{
				supportsRenamePrepare:               true,
				supportsCodeActionLiterals:          true,
				supportsCodeActionIsPreferred:       true,
				supportsHierarchicalDocumentSymbols: true,
				supportsDynamicFileWatchers:         true,
				completionDocumentationFormats:      []protocol.MarkupKind{protocol.MarkupKindMarkdown},
				hoverContentFormats:                 []protocol.MarkupKind{protocol.MarkupKindPlainText},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewServer()
			result, err := srv.Initialize(context.Background(), &protocol.InitializeParams{Capabilities: tt.capabilities})
			require.NoError(t, err)

			assert.Equal(t, tt.wantSnapshot, srv.clientCapabilities)
			assert.Empty(t, result.Capabilities.Experimental)
			require.IsType(t, &protocol.InlineCompletionOptions{}, result.Capabilities.InlineCompletionProvider)

			if tt.wantRenameOptions {
				renameOptions, ok := result.Capabilities.RenameProvider.(*protocol.RenameOptions)
				require.True(t, ok)
				require.NotNil(t, renameOptions.PrepareProvider)
				assert.True(t, *renameOptions.PrepareProvider)
			} else {
				assert.Equal(t, protocol.Boolean(true), result.Capabilities.RenameProvider)
			}

			if tt.wantCodeActionOpts {
				codeActionOptions, ok := result.Capabilities.CodeActionProvider.(*protocol.CodeActionOptions)
				require.True(t, ok)
				assert.Equal(t, []protocol.CodeActionKind{protocol.CodeActionKindQuickFix, "source.hledger"}, codeActionOptions.CodeActionKinds)
			} else {
				assert.Equal(t, protocol.Boolean(true), result.Capabilities.CodeActionProvider)
			}
		})
	}
}
