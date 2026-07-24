package server

import "go.lsp.dev/protocol"

// clientCapabilities is the subset of client capabilities that shapes server
// responses and feature behavior after initialization.
type clientCapabilities struct {
	supportsConfiguration               bool
	supportsRenamePrepare               bool
	supportsCodeActionLiterals          bool
	supportsCodeActionIsPreferred       bool
	supportsHierarchicalDocumentSymbols bool
	supportsDynamicFileWatchers         bool
	completionDocumentationFormats      []protocol.MarkupKind
	hoverContentFormats                 []protocol.MarkupKind
}

func newClientCapabilities(capabilities protocol.ClientCapabilities) clientCapabilities {
	profile := clientCapabilities{}

	if workspace := capabilities.Workspace; workspace != nil {
		profile.supportsConfiguration = boolValue(workspace.Configuration)
		if watchedFiles := workspace.DidChangeWatchedFiles; watchedFiles != nil {
			profile.supportsDynamicFileWatchers = boolValue(watchedFiles.DynamicRegistration)
		}
	}

	if textDocument := capabilities.TextDocument; textDocument != nil {
		if rename := textDocument.Rename; rename != nil {
			profile.supportsRenamePrepare = boolValue(rename.PrepareSupport)
		}
		if codeAction := textDocument.CodeAction; codeAction != nil {
			profile.supportsCodeActionLiterals = codeAction.CodeActionLiteralSupport.CodeActionKind.ValueSet != nil
			profile.supportsCodeActionIsPreferred = boolValue(codeAction.IsPreferredSupport)
		}
		if documentSymbol := textDocument.DocumentSymbol; documentSymbol != nil {
			profile.supportsHierarchicalDocumentSymbols = boolValue(documentSymbol.HierarchicalDocumentSymbolSupport)
		}
		if completion := textDocument.Completion; completion != nil && completion.CompletionItem != nil {
			profile.completionDocumentationFormats = append([]protocol.MarkupKind(nil), completion.CompletionItem.DocumentationFormat...)
		}
		if hover := textDocument.Hover; hover != nil {
			profile.hoverContentFormats = append([]protocol.MarkupKind(nil), hover.ContentFormat...)
		}
	}

	return profile
}

func boolValue(value *bool) bool {
	return value != nil && *value
}
