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

func TestFeatureFlagsTakeEffectWithoutRestart(t *testing.T) {
	// LSP fixes the advertised capability set during initialize, but the requests
	// themselves must honour the current settings: a user who unchecks a feature
	// expects it to stop, not to keep answering.
	ts := newTestServer()
	docURI := uri.URI("file:///flags.journal")
	content := "2024-01-15 shop\n    expenses:food  $50.00\n    assets:cash\n\n2024-01-16 shop\n    exp\n"
	ts.StoreDocument(docURI, content)

	labels := complete(t, ts, docURI, content, 5, 7)
	require.NotEmpty(t, labels, "completion is enabled by default")

	settings := ts.getSettings()
	settings.Features.Completion = false
	settings.Features.Hover = false
	settings.Features.Formatting = false
	ts.setSettings(settings)

	completionResult, err := ts.Completion(context.Background(), &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 5, Character: 7},
		},
	})
	require.NoError(t, err)
	assert.Nil(t, completionResult, "completion must stop once the feature is disabled")

	hover, err := ts.Hover(context.Background(), &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 1, Character: 6},
		},
	})
	require.NoError(t, err)
	assert.Nil(t, hover, "hover must stop once the feature is disabled")

	formatEdits, err := ts.Formatting(context.Background(), &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
	})
	require.NoError(t, err)
	assert.Nil(t, formatEdits, "formatting must stop once the feature is disabled")
}

func TestSettingsIssuesAreReported(t *testing.T) {
	base := defaultServerSettings()

	settings, issues := parseSettingsFromRawWithIssues(base, map[string]interface{}{
		"formatting": map[string]interface{}{"amountAlignmentMode": "centre"},
		"diagnostics": map[string]interface{}{
			"accountCheck": "sometimes",
			"debounceMs":   -1,
		},
		"completion": map[string]interface{}{"accountScope": "everything"},
	})

	require.Len(t, issues, 4, "each rejected value must be reported: %v", issues)
	joined := strings.Join(issues, "\n")
	assert.Contains(t, joined, "amountAlignmentMode")
	assert.Contains(t, joined, "accountCheck")
	assert.Contains(t, joined, "debounceMs")
	assert.Contains(t, joined, "accountScope")

	// The values fall back to the documented defaults.
	assert.Equal(t, "right", settings.Formatting.AmountAlignmentMode)
	assert.Equal(t, accountCheckOff, settings.Diagnostics.AccountCheck)
	assert.Equal(t, 100, settings.Diagnostics.DebounceMs)
	assert.Equal(t, accountScopeNonzero, settings.Completion.AccountScope)
}

func TestSettingsWithoutIssuesReportNothing(t *testing.T) {
	_, issues := parseSettingsFromRawWithIssues(defaultServerSettings(), map[string]interface{}{
		"formatting": map[string]interface{}{"amountAlignmentMode": "decimal"},
		"diagnostics": map[string]interface{}{
			"accountCheck": "strict",
		},
	})
	assert.Empty(t, issues)
}

func TestRunCommandRejectsUnknownCommand(t *testing.T) {
	ts := newTestServer()
	// The client must not be able to turn ExecuteCommand into a command runner.
	_, err := ts.ExecuteCommand(context.Background(), &protocol.ExecuteCommandParams{
		Command:   "hledger.run",
		Arguments: []protocol.LSPAny{protocol.LSPAny(`"rm -rf /"`)},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown hledger command")
}

func TestRunCommandRespectsDisabledCLI(t *testing.T) {
	ts := newTestServer()
	settings := ts.getSettings()
	settings.CLI.Enabled = false
	ts.setSettings(settings)

	_, err := ts.ExecuteCommand(context.Background(), &protocol.ExecuteCommandParams{
		Command:   "hledger.run",
		Arguments: []protocol.LSPAny{protocol.LSPAny(`"bal"`)},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestFormatOutputAsCommentTruncates(t *testing.T) {
	var builder strings.Builder
	for i := 0; i < 500; i++ {
		builder.WriteString("2024-01-15 a very long report line\n")
	}

	comment := formatOutputAsComment("bal", builder.String())
	lines := strings.Split(comment, "\n")

	assert.LessOrEqual(t, len(lines), maxCommandOutputLines+3)
	assert.Contains(t, comment, "output truncated")
	assert.Contains(t, comment, "; === hledger bal ===")
}

func TestFormatOutputAsCommentKeepsShortOutput(t *testing.T) {
	comment := formatOutputAsComment("bal", "1000 USD  assets\n-1000 USD  equity")
	assert.Contains(t, comment, "; 1000 USD  assets")
	assert.NotContains(t, comment, "truncated")
}
