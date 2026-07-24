package server

import (
	"strings"

	"go.lsp.dev/protocol"
)

func documentationMarkupContent(value string, formats []protocol.MarkupKind) *protocol.MarkupContent {
	kind := preferredMarkupKind(formats)
	if kind == protocol.MarkupKindPlainText {
		value = plainTextDocumentation(value)
	}
	return &protocol.MarkupContent{Kind: kind, Value: value}
}

func preferredMarkupKind(formats []protocol.MarkupKind) protocol.MarkupKind {
	for _, kind := range formats {
		switch kind {
		case protocol.MarkupKindMarkdown, protocol.MarkupKindPlainText:
			return kind
		}
	}
	return protocol.MarkupKindPlainText
}

func plainTextDocumentation(value string) string {
	value = strings.ReplaceAll(value, "**", "")
	value = strings.ReplaceAll(value, "`", "")
	return strings.ReplaceAll(value, "*(empty)*", "(empty)")
}
