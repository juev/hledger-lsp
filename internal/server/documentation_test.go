package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.lsp.dev/protocol"
)

func TestDocumentationMarkupContent_UsesFirstRecognizedFormat(t *testing.T) {
	tests := []struct {
		name    string
		formats []protocol.MarkupKind
		want    protocol.MarkupKind
	}{
		{
			name:    "markdown first",
			formats: []protocol.MarkupKind{protocol.MarkupKindMarkdown, protocol.MarkupKindPlainText},
			want:    protocol.MarkupKindMarkdown,
		},
		{
			name:    "plaintext first",
			formats: []protocol.MarkupKind{protocol.MarkupKindPlainText, protocol.MarkupKindMarkdown},
			want:    protocol.MarkupKindPlainText,
		},
		{
			name: "empty defaults to plaintext",
			want: protocol.MarkupKindPlainText,
		},
		{
			name:    "unknown defaults to plaintext",
			formats: []protocol.MarkupKind{"html"},
			want:    protocol.MarkupKindPlainText,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := documentationMarkupContent("**Label:** `value`\n*(empty)*", tt.formats)

			assert.Equal(t, tt.want, content.Kind)
		})
	}
}

func TestDocumentationMarkupContent_PlainTextRemovesOnlySupportedMarkdownMarkers(t *testing.T) {
	content := documentationMarkupContent("**Label:** `value`\n*(empty)*\n- preserve *list*", []protocol.MarkupKind{protocol.MarkupKindPlainText})

	assert.Equal(t, protocol.MarkupKindPlainText, content.Kind)
	assert.Equal(t, "Label: value\n(empty)\n- preserve *list*", content.Value)
}
