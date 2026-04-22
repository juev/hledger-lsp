package server

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func TestServer_Initialize_AdvertisesQuickFixCodeActions(t *testing.T) {
	srv := NewServer()

	result, err := srv.Initialize(context.Background(), &protocol.InitializeParams{})
	require.NoError(t, err)
	require.NotNil(t, result)

	opts, ok := result.Capabilities.CodeActionProvider.(*protocol.CodeActionOptions)
	require.True(t, ok, "expected *protocol.CodeActionOptions, got %T", result.Capabilities.CodeActionProvider)

	assert.Contains(t, opts.CodeActionKinds, protocol.QuickFix)
	assert.Contains(t, opts.CodeActionKinds, protocol.CodeActionKind("source.hledger"))
}

func TestServer_CodeAction_QuickFixForUnbalancedFinalPosting(t *testing.T) {
	ts := newTestServer()
	ts.cliClient = nil

	uri := protocol.DocumentURI("file:///test.journal")
	content := `2024-01-15 lunch
    expenses:food  $10.00
    assets:cash    $-9.00`

	diagnostics, err := ts.openAndWait(uri, content)
	require.NoError(t, err)

	diag := requireDiagnosticByCode(t, diagnostics, "UNBALANCED")
	actions, err := codeActionsForDiagnostics(ts, uri, []protocol.Diagnostic{diag})
	require.NoError(t, err)

	action := requireQuickFixAction(t, actions)
	require.True(t, action.IsPreferred)
	require.Len(t, action.Diagnostics, 1)
	assert.Equal(t, "UNBALANCED", action.Diagnostics[0].Code)

	fixed := applyWorkspaceEditToContent(t, content, uri, action.Edit)
	assert.Equal(t, `2024-01-15 lunch
    expenses:food  $10.00
    assets:cash    $-10.00`, fixed)

	diagnostics, err = ts.replaceAndWait(uri, fixed)
	require.NoError(t, err)
	_, err = findDiagnosticByCode(diagnostics, "UNBALANCED")
	assert.Error(t, err)
}

func TestServer_CodeAction_QuickFixWithoutDiagnosticContext(t *testing.T) {
	ts := newTestServer()
	ts.cliClient = nil

	uri := protocol.DocumentURI("file:///test.journal")
	content := `2024-01-15 lunch
    expenses:food  $10.00
    assets:cash    $-9.00`

	diagnostics, err := ts.openAndWait(uri, content)
	require.NoError(t, err)
	diag := requireDiagnosticByCode(t, diagnostics, "UNBALANCED")

	actions, err := ts.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range:        diag.Range,
		Context:      protocol.CodeActionContext{},
	})
	require.NoError(t, err)

	action := requireQuickFixAction(t, actions)
	fixed := applyWorkspaceEditToContent(t, content, uri, action.Edit)
	assert.Equal(t, `2024-01-15 lunch
    expenses:food  $10.00
    assets:cash    $-10.00`, fixed)
}

func TestServer_CodeAction_QuickFixRespectsFormattingSettings(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		configure  func(serverSettings) serverSettings
		assertEdit func(t *testing.T, fixed string)
	}{
		{
			name: "right alignment keeps amount column",
			content: `2024-01-15 groceries
    expenses:food:groceries  $10.00
    assets:cash  $-9.00`,
			configure: func(settings serverSettings) serverSettings {
				settings.Formatting.AmountAlignmentMode = "right"
				return settings
			},
			assertEdit: func(t *testing.T, fixed string) {
				lines := strings.Split(fixed, "\n")
				require.Len(t, lines, 3)
				assert.Equal(t, strings.Index(lines[1], "$"), strings.Index(lines[2], "$"))
			},
		},
		{
			name: "decimal alignment keeps decimal column",
			content: `D $1,000.00

2024-01-15 groceries
    expenses:small      $12.30
    expenses:big     $1,200.00
    assets:cash  $-1210.00`,
			configure: func(settings serverSettings) serverSettings {
				settings.Formatting.AmountAlignmentMode = "decimal"
				return settings
			},
			assertEdit: func(t *testing.T, fixed string) {
				lines := strings.Split(fixed, "\n")
				require.Len(t, lines, 6)
				decimalCol := strings.LastIndex(lines[3], ".")
				assert.Equal(t, decimalCol, strings.LastIndex(lines[4], "."))
				assert.Equal(t, decimalCol, strings.LastIndex(lines[5], "."))
			},
		},
		{
			name: "min alignment column widens posting",
			content: `2024-01-15 groceries
    a  $10
    b  $-9`,
			configure: func(settings serverSettings) serverSettings {
				settings.Formatting.MinAlignmentColumn = 30
				return settings
			},
			assertEdit: func(t *testing.T, fixed string) {
				lines := strings.Split(fixed, "\n")
				require.Len(t, lines, 3)
				assert.Equal(t, 29, strings.Index(lines[2], "$"))
			},
		},
		{
			name: "left commodity format is preserved",
			content: `D $1,000.00

2024-01-15 groceries
    expenses:food  $10
    assets:cash    $-9`,
			configure: func(settings serverSettings) serverSettings {
				return settings
			},
			assertEdit: func(t *testing.T, fixed string) {
				assert.Contains(t, fixed, "$-10.00")
			},
		},
		{
			name: "right commodity format is preserved",
			content: `commodity RUB
  format 1 000,00 RUB

2024-01-15 groceries
    expenses:food  1 234,56 RUB
    assets:cash    -1234 RUB`,
			configure: func(settings serverSettings) serverSettings {
				return settings
			},
			assertEdit: func(t *testing.T, fixed string) {
				assert.Contains(t, fixed, "-1 234,56 RUB")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestServer()
			ts.cliClient = nil
			ts.setSettings(tt.configure(ts.getSettings()))

			uri := protocol.DocumentURI("file:///formatting.journal")
			diagnostics, err := ts.openAndWait(uri, tt.content)
			require.NoError(t, err)

			diag := requireDiagnosticByCode(t, diagnostics, "UNBALANCED")
			actions, err := codeActionsForDiagnostics(ts, uri, []protocol.Diagnostic{diag})
			require.NoError(t, err)

			action := requireQuickFixAction(t, actions)
			fixed := applyWorkspaceEditToContent(t, tt.content, uri, action.Edit)
			tt.assertEdit(t, fixed)
		})
	}
}

func TestServer_CodeAction_QuickFixGuards(t *testing.T) {
	tests := []struct {
		name           string
		uri            protocol.DocumentURI
		content        string
		diagnosticCode string
	}{
		{
			name: "multiple inferred is ignored",
			uri:  protocol.DocumentURI("file:///multiple-inferred.journal"),
			content: `2024-01-15 groceries
    expenses:food
    assets:cash`,
			diagnosticCode: "MULTIPLE_INFERRED",
		},
		{
			name: "multi commodity imbalance is ignored",
			uri:  protocol.DocumentURI("file:///multi-commodity.journal"),
			content: `2024-01-15 groceries
    assets:cash  $10
    equity:opening  5 EUR`,
			diagnosticCode: "UNBALANCED",
		},
		{
			name: "final posting with cost is ignored",
			uri:  protocol.DocumentURI("file:///costed.journal"),
			content: `2024-01-15 buy
    equity:opening  $-1499
    assets:stocks   10 AAPL @ $150`,
			diagnosticCode: "UNBALANCED",
		},
		{
			name: "final posting with lot price is ignored",
			uri:  protocol.DocumentURI("file:///lot-price.journal"),
			content: `2024-01-15 transfer
    assets:inventory  -9 AAPL
    assets:stocks     10 AAPL {$150}`,
			diagnosticCode: "UNBALANCED",
		},
		{
			name: "final posting with balance assertion is ignored",
			uri:  protocol.DocumentURI("file:///balance-assertion.journal"),
			content: `2024-01-15 adjust
    assets:cash      $-9
    equity:opening   $10 = $10`,
			diagnosticCode: "UNBALANCED",
		},
		{
			name: "rules files never offer quickfix",
			uri:  protocol.DocumentURI("file:///guard.rules"),
			content: `fields date,description,amount
skip 1`,
			diagnosticCode: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestServer()
			ts.cliClient = nil

			var diagnostics []protocol.Diagnostic
			var err error
			if strings.HasSuffix(string(tt.uri), ".rules") {
				require.NoError(t, ts.openDocument(tt.uri, tt.content))
			} else {
				diagnostics, err = ts.openAndWait(tt.uri, tt.content)
				require.NoError(t, err)
				_ = requireDiagnosticByCode(t, diagnostics, tt.diagnosticCode)
			}

			actions, err := codeActionsForDiagnostics(ts, tt.uri, diagnostics)
			require.NoError(t, err)
			_, err = findQuickFixAction(actions)
			assert.Error(t, err)
		})
	}
}

func codeActionsForDiagnostics(ts *testServer, uri protocol.DocumentURI, diagnostics []protocol.Diagnostic) ([]protocol.CodeAction, error) {
	rng := protocol.Range{}
	if len(diagnostics) > 0 {
		rng = diagnostics[0].Range
	}

	return ts.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range:        rng,
		Context: protocol.CodeActionContext{
			Diagnostics: diagnostics,
		},
	})
}

func requireDiagnosticByCode(t *testing.T, diagnostics []protocol.Diagnostic, code string) protocol.Diagnostic {
	t.Helper()

	diag, err := findDiagnosticByCode(diagnostics, code)
	require.NoError(t, err)
	return diag
}

func findDiagnosticByCode(diagnostics []protocol.Diagnostic, code string) (protocol.Diagnostic, error) {
	for _, diag := range diagnostics {
		if diag.Code == code {
			return diag, nil
		}
	}
	return protocol.Diagnostic{}, assert.AnError
}

func requireQuickFixAction(t *testing.T, actions []protocol.CodeAction) protocol.CodeAction {
	t.Helper()

	action, err := findQuickFixAction(actions)
	require.NoError(t, err)
	return action
}

func findQuickFixAction(actions []protocol.CodeAction) (protocol.CodeAction, error) {
	for _, action := range actions {
		if action.Kind == protocol.QuickFix {
			return action, nil
		}
	}
	return protocol.CodeAction{}, assert.AnError
}

func applyWorkspaceEditToContent(t *testing.T, content string, uri protocol.DocumentURI, edit *protocol.WorkspaceEdit) string {
	t.Helper()

	require.NotNil(t, edit)
	changes, ok := edit.Changes[uri]
	require.True(t, ok, "expected workspace edit for %s", uri)
	return applyTextEdits(content, changes)
}
