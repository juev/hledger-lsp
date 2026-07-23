package server

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestServer_Initialize_AdvertisesQuickFixCodeActions(t *testing.T) {
	srv := NewServer()

	result, err := srv.Initialize(context.Background(), &protocol.InitializeParams{})
	require.NoError(t, err)
	require.NotNil(t, result)

	opts, ok := result.Capabilities.CodeActionProvider.(*protocol.CodeActionOptions)
	require.True(t, ok, "expected *protocol.CodeActionOptions, got %T", result.Capabilities.CodeActionProvider)

	assert.Contains(t, opts.CodeActionKinds, protocol.CodeActionKindQuickFix)
	assert.Contains(t, opts.CodeActionKinds, protocol.CodeActionKind("source.hledger"))
}

func TestServer_CodeAction_QuickFixForUnbalancedFinalPosting(t *testing.T) {
	ts := newTestServer()
	ts.cliClient = nil

	uri := uri.URI("file:///test.journal")
	content := `2024-01-15 lunch
    expenses:food  $10.00
    assets:cash    $-9.00`

	diagnostics, err := ts.openAndWait(uri, content)
	require.NoError(t, err)

	diag := requireDiagnosticByCode(t, diagnostics, "UNBALANCED")
	actions, err := codeActionsForDiagnostics(ts, uri, []protocol.Diagnostic{diag})
	require.NoError(t, err)

	action := requireQuickFixAction(t, actions)
	require.NotNil(t, action.IsPreferred)
	require.True(t, *action.IsPreferred)
	require.Len(t, action.Diagnostics, 1)
	assert.Equal(t, "UNBALANCED", diagnosticCodeString(action.Diagnostics[0].Code))

	// Quick Fix is a local edit (issue #25). For commodity-left amounts
	// (`$`) the `$` column is preserved, so `$-9.00` → `$-10.00` grows
	// the line by one rune at the tail (pre-amount whitespace is kept
	// intact). Alignment across postings is the Format Document's job.
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

	uri := uri.URI("file:///test.journal")
	content := `2024-01-15 lunch
    expenses:food  $10.00
    assets:cash    $-9.00`

	diagnostics, err := ts.openAndWait(uri, content)
	require.NoError(t, err)
	diag := requireDiagnosticByCode(t, diagnostics, "UNBALANCED")

	entries, err := ts.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range:        diag.Range,
		Context:      protocol.CodeActionContext{},
	})
	require.NoError(t, err)
	actions := codeActionEntries(entries)

	action := requireQuickFixAction(t, actions)
	fixed := applyWorkspaceEditToContent(t, content, uri, action.Edit)
	assert.Equal(t, `2024-01-15 lunch
    expenses:food  $10.00
    assets:cash    $-10.00`, fixed)
}

// Quick Fix is a local edit (issue #25): it does not invoke the formatter
// and therefore does not re-align postings across a transaction. The
// formatter settings (AmountAlignmentMode, MinAlignmentColumn) apply only
// to Format Document. What Quick Fix must guarantee:
//   - lines other than the edited posting remain byte-equal to the input;
//   - the edited posting's commodity format is honoured (via extracted
//     commodity formats from the journal's D/commodity directives).
func TestServer_CodeAction_QuickFix_IsLocalAndPreservesFormats(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		configure  func(serverSettings) serverSettings
		assertEdit func(t *testing.T, content, fixed string)
	}{
		{
			name: "untouched postings remain byte-equal in right mode",
			content: `2024-01-15 groceries
    expenses:food:groceries  $10.00
    assets:cash              $-9.00`,
			configure: func(settings serverSettings) serverSettings {
				settings.Formatting.AmountAlignmentMode = "right"
				return settings
			},
			assertEdit: func(t *testing.T, content, fixed string) {
				in := strings.Split(content, "\n")
				out := strings.Split(fixed, "\n")
				require.Len(t, out, 3)
				assert.Equal(t, in[0], out[0], "date line untouched")
				assert.Equal(t, in[1], out[1], "first posting untouched")
				assert.Contains(t, out[2], "$-10.00")
			},
		},
		{
			name: "same-width fix preserves decimal column in decimal mode",
			content: `2024-01-15 groceries
    expenses:small     $12.30
    expenses:big    $1200.00
    assets:cash     $-1210.00`,
			configure: func(settings serverSettings) serverSettings {
				settings.Formatting.AmountAlignmentMode = "decimal"
				return settings
			},
			assertEdit: func(t *testing.T, content, fixed string) {
				in := strings.Split(content, "\n")
				out := strings.Split(fixed, "\n")
				require.Len(t, out, 4)
				// Lines other than the fixed posting must be byte-equal.
				for i := 0; i < 3; i++ {
					assert.Equal(t, in[i], out[i], "line %d untouched", i)
				}
				// Fix: -1210.00 → -1212.30 (same width, 8 runes). For
				// commodity-left the `$` column is preserved and the
				// decimal column therefore stays put.
				assert.Equal(t, strings.LastIndex(in[3], "."), strings.LastIndex(out[3], "."))
				assert.Contains(t, out[3], "$-1212.30")
			},
		},
		{
			name: "min alignment column is ignored (applies only in Format Document)",
			content: `2024-01-15 groceries
    a  $10
    b  $-9`,
			configure: func(settings serverSettings) serverSettings {
				settings.Formatting.MinAlignmentColumn = 30
				return settings
			},
			assertEdit: func(t *testing.T, content, fixed string) {
				in := strings.Split(content, "\n")
				out := strings.Split(fixed, "\n")
				require.Len(t, out, 3)
				assert.Equal(t, in[0], out[0])
				assert.Equal(t, in[1], out[1])
				// $-9 (3 runes) → $-10 (4 runes). Pre-space already at minSpaces=2,
				// so the amount grows in place, keeping $ at its original column.
				assert.Equal(t, strings.Index(in[2], "$"), strings.Index(out[2], "$"))
				assert.Contains(t, out[2], "$-10")
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
			assertEdit: func(t *testing.T, _, fixed string) {
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
			assertEdit: func(t *testing.T, _, fixed string) {
				assert.Contains(t, fixed, "-1 234,56 RUB")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestServer()
			ts.cliClient = nil
			ts.setSettings(tt.configure(ts.getSettings()))

			uri := uri.URI("file:///formatting.journal")
			diagnostics, err := ts.openAndWait(uri, tt.content)
			require.NoError(t, err)

			diag := requireDiagnosticByCode(t, diagnostics, "UNBALANCED")
			actions, err := codeActionsForDiagnostics(ts, uri, []protocol.Diagnostic{diag})
			require.NoError(t, err)

			action := requireQuickFixAction(t, actions)
			fixed := applyWorkspaceEditToContent(t, tt.content, uri, action.Edit)
			tt.assertEdit(t, tt.content, fixed)
		})
	}
}

// Issue #25: Quick Fix must preserve user's hand-formatted layout.
// For commodity-on-right amounts users align by USD end column.
// The edit must not shift surrounding postings.
func TestServer_CodeAction_QuickFix_CommodityRight_PreservesLayout(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "same width amount preserves end column",
			content: "2024-01-15 lunch\n" +
				"    food      -12.60 USD\n" +
				"    cash       12.00 USD",
			want: "2024-01-15 lunch\n" +
				"    food      -12.60 USD\n" +
				"    cash       12.60 USD",
		},
		{
			name: "wider amount eats one pre-amount space",
			content: "2024-01-15 a\n" +
				"    food     -10 USD\n" +
				"    cash       9 USD",
			want: "2024-01-15 a\n" +
				"    food     -10 USD\n" +
				"    cash      10 USD",
		},
		{
			name: "narrower amount adds one pre-amount space",
			content: "2024-01-15 b\n" +
				"    food     -10 USD\n" +
				"    cash     100 USD",
			want: "2024-01-15 b\n" +
				"    food     -10 USD\n" +
				"    cash      10 USD",
		},
		{
			name: "untouched postings remain byte-equal",
			content: "2024-01-15 multi\n" +
				"    a         1.00 USD\n" +
				"    b         2.00 USD\n" +
				"    c        -2.50 USD",
			want: "2024-01-15 multi\n" +
				"    a         1.00 USD\n" +
				"    b         2.00 USD\n" +
				"    c        -3.00 USD",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestServer()
			ts.cliClient = nil

			uri := uri.URI("file:///issue25.journal")
			diagnostics, err := ts.openAndWait(uri, tt.content)
			require.NoError(t, err)

			diag := requireDiagnosticByCode(t, diagnostics, "UNBALANCED")
			actions, err := codeActionsForDiagnostics(ts, uri, []protocol.Diagnostic{diag})
			require.NoError(t, err)

			action := requireQuickFixAction(t, actions)
			fixed := applyWorkspaceEditToContent(t, tt.content, uri, action.Edit)
			assert.Equal(t, tt.want, fixed)
		})
	}
}

// Issue #25: commodity-right amounts always align their commodity symbol
// with sibling postings. Even when the user's original input had amounts
// aligned by start column (because signs pushed one amount further left),
// Quick Fix produces a result aligned by end column — the same
// invariant the Format Document would enforce. Pre-amount whitespace is
// recomputed to hit the sibling's end column.
func TestServer_CodeAction_QuickFix_CommodityRight_AlignsWithSibling(t *testing.T) {
	ts := newTestServer()
	ts.cliClient = nil

	uri := uri.URI("file:///start-aligned.journal")
	content := "commodity RUB\n" +
		"  format 1 000,00 RUB\n" +
		"\n" +
		"2024-01-15 groceries\n" +
		"    expenses:food  1 234,56 RUB\n" +
		"    assets:cash    -1234 RUB"

	diagnostics, err := ts.openAndWait(uri, content)
	require.NoError(t, err)
	diag := requireDiagnosticByCode(t, diagnostics, "UNBALANCED")

	actions, err := codeActionsForDiagnostics(ts, uri, []protocol.Diagnostic{diag})
	require.NoError(t, err)
	action := requireQuickFixAction(t, actions)
	fixed := applyWorkspaceEditToContent(t, content, uri, action.Edit)

	// RUB ends at the same column on both postings (col 31 in 1-indexed
	// runes), matching what Format Document would produce — no post-fix
	// reformatting needed.
	want := "commodity RUB\n" +
		"  format 1 000,00 RUB\n" +
		"\n" +
		"2024-01-15 groceries\n" +
		"    expenses:food  1 234,56 RUB\n" +
		"    assets:cash   -1 234,56 RUB"
	assert.Equal(t, want, fixed)
	// RUB end column matches on both postings.
	lines := strings.Split(fixed, "\n")
	assert.Equal(t, strings.LastIndex(lines[4], "RUB")+3, strings.LastIndex(lines[5], "RUB")+3)
}

// Issue #25: for commodity-left amounts (`$`, `€`) Quick Fix preserves
// the commodity-symbol column (start of amount). Width changes grow or
// shrink the line at the tail, not the start.
func TestServer_CodeAction_QuickFix_CommodityLeft_PreservesStartColumn(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "wider amount grows tail, $ column stable",
			content: "2024-01-15 lunch\n" +
				"    expenses:food  $10.00\n" +
				"    assets:cash    $-9.00",
			want: "2024-01-15 lunch\n" +
				"    expenses:food  $10.00\n" +
				"    assets:cash    $-10.00",
		},
		{
			name: "narrower amount shrinks tail, $ column stable",
			content: "2024-01-15 x\n" +
				"    a  $100.00\n" +
				"    b  $-99.50",
			want: "2024-01-15 x\n" +
				"    a  $100.00\n" +
				"    b  $-100.00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestServer()
			ts.cliClient = nil

			uri := uri.URI("file:///left.journal")
			diagnostics, err := ts.openAndWait(uri, tt.content)
			require.NoError(t, err)

			diag := requireDiagnosticByCode(t, diagnostics, "UNBALANCED")
			actions, err := codeActionsForDiagnostics(ts, uri, []protocol.Diagnostic{diag})
			require.NoError(t, err)

			action := requireQuickFixAction(t, actions)
			fixed := applyWorkspaceEditToContent(t, tt.content, uri, action.Edit)
			assert.Equal(t, tt.want, fixed)
		})
	}
}

// Issue #25 follow-ups: Quick Fix must cope with posting comments, CRLF
// line endings, and non-ASCII (CJK, Cyrillic) account names without
// corrupting the surrounding text.
func TestServer_CodeAction_QuickFix_EdgeCases(t *testing.T) {
	t.Run("posting comment is preserved", func(t *testing.T) {
		ts := newTestServer()
		ts.cliClient = nil
		uri := uri.URI("file:///comment.journal")
		content := "2024-01-15 lunch\n" +
			"    food      -12.60 USD\n" +
			"    cash       12.00 USD  ; note: salary"

		diagnostics, err := ts.openAndWait(uri, content)
		require.NoError(t, err)
		diag := requireDiagnosticByCode(t, diagnostics, "UNBALANCED")

		actions, err := codeActionsForDiagnostics(ts, uri, []protocol.Diagnostic{diag})
		require.NoError(t, err)
		action := requireQuickFixAction(t, actions)
		fixed := applyWorkspaceEditToContent(t, content, uri, action.Edit)

		want := "2024-01-15 lunch\n" +
			"    food      -12.60 USD\n" +
			"    cash       12.60 USD  ; note: salary"
		assert.Equal(t, want, fixed)
	})

	t.Run("CRLF line endings", func(t *testing.T) {
		ts := newTestServer()
		ts.cliClient = nil
		uri := uri.URI("file:///crlf.journal")
		content := "2024-01-15 lunch\r\n" +
			"    food      -12.60 USD\r\n" +
			"    cash       12.00 USD"

		diagnostics, err := ts.openAndWait(uri, content)
		require.NoError(t, err)
		diag := requireDiagnosticByCode(t, diagnostics, "UNBALANCED")

		actions, err := codeActionsForDiagnostics(ts, uri, []protocol.Diagnostic{diag})
		require.NoError(t, err)
		action := requireQuickFixAction(t, actions)
		// The server normalizes ingestion to LF; the edit is against the
		// normalized document, so the applied result uses LF.
		normalized := "2024-01-15 lunch\n" +
			"    food      -12.60 USD\n" +
			"    cash       12.00 USD"
		fixed := applyWorkspaceEditToContent(t, normalized, uri, action.Edit)

		want := "2024-01-15 lunch\n" +
			"    food      -12.60 USD\n" +
			"    cash       12.60 USD"
		assert.Equal(t, want, fixed)
	})

	t.Run("Cyrillic account names", func(t *testing.T) {
		ts := newTestServer()
		ts.cliClient = nil
		uri := uri.URI("file:///cyrillic.journal")
		content := "2024-01-15 обед\n" +
			"    расходы:еда      -12.60 USD\n" +
			"    активы:наличные   12.00 USD"

		diagnostics, err := ts.openAndWait(uri, content)
		require.NoError(t, err)
		diag := requireDiagnosticByCode(t, diagnostics, "UNBALANCED")

		actions, err := codeActionsForDiagnostics(ts, uri, []protocol.Diagnostic{diag})
		require.NoError(t, err)
		action := requireQuickFixAction(t, actions)
		fixed := applyWorkspaceEditToContent(t, content, uri, action.Edit)

		want := "2024-01-15 обед\n" +
			"    расходы:еда      -12.60 USD\n" +
			"    активы:наличные   12.60 USD"
		assert.Equal(t, want, fixed)
	})
}

func TestServer_CodeAction_QuickFixGuards(t *testing.T) {
	tests := []struct {
		name           string
		uri            uri.URI
		content        string
		diagnosticCode string
	}{
		{
			name: "multiple inferred is ignored",
			uri:  uri.URI("file:///multiple-inferred.journal"),
			content: `2024-01-15 groceries
    expenses:food
    assets:cash`,
			diagnosticCode: "MULTIPLE_INFERRED",
		},
		{
			name: "multi commodity imbalance is ignored",
			uri:  uri.URI("file:///multi-commodity.journal"),
			content: `2024-01-15 groceries
    assets:cash  $10
    equity:opening  5 EUR`,
			diagnosticCode: "UNBALANCED",
		},
		{
			name: "final posting with cost is ignored",
			uri:  uri.URI("file:///costed.journal"),
			content: `2024-01-15 buy
    equity:opening  $-1499
    assets:stocks   10 AAPL @ $150`,
			diagnosticCode: "UNBALANCED",
		},
		{
			name: "final posting with lot price is ignored",
			uri:  uri.URI("file:///lot-price.journal"),
			content: `2024-01-15 transfer
    assets:inventory  -9 AAPL
    assets:stocks     10 AAPL {$150}`,
			diagnosticCode: "UNBALANCED",
		},
		{
			name: "final posting with balance assertion is ignored",
			uri:  uri.URI("file:///balance-assertion.journal"),
			content: `2024-01-15 adjust
    assets:cash      $-9
    equity:opening   $10 = $10`,
			diagnosticCode: "UNBALANCED",
		},
		{
			name: "rules files never offer quickfix",
			uri:  uri.URI("file:///guard.rules"),
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
			// These transactions must not offer an UNBALANCED amount quickfix.
			// Declare-commodity quickfixes may appear (e.g. for an undeclared
			// `$`), which is legitimate and unrelated to the amount fix.
			_, err = findQuickFixByDiagnosticCode(actions, "UNBALANCED")
			assert.Error(t, err, "no UNBALANCED amount quickfix should be offered")
		})
	}
}

func codeActionsForDiagnostics(ts *testServer, uri uri.URI, diagnostics []protocol.Diagnostic) ([]protocol.CodeAction, error) {
	rng := protocol.Range{}
	if len(diagnostics) > 0 {
		rng = diagnostics[0].Range
	}

	entries, err := ts.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range:        rng,
		Context: protocol.CodeActionContext{
			Diagnostics: diagnostics,
		},
	})
	if err != nil {
		return nil, err
	}
	return codeActionEntries(entries), nil
}

func codeActionEntries(entries []protocol.CommandOrCodeAction) []protocol.CodeAction {
	actions := make([]protocol.CodeAction, 0, len(entries))
	for _, entry := range entries {
		action, ok := entry.(*protocol.CodeAction)
		if !ok {
			continue
		}
		actions = append(actions, *action)
	}
	return actions
}

func requireDiagnosticByCode(t *testing.T, diagnostics []protocol.Diagnostic, code string) protocol.Diagnostic {
	t.Helper()

	diag, err := findDiagnosticByCode(diagnostics, code)
	require.NoError(t, err)
	return diag
}

func findDiagnosticByCode(diagnostics []protocol.Diagnostic, code string) (protocol.Diagnostic, error) {
	for _, diag := range diagnostics {
		if diagnosticCodeString(diag.Code) == code {
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
		if action.Kind != nil && *action.Kind == protocol.CodeActionKindQuickFix {
			return action, nil
		}
	}
	return protocol.CodeAction{}, assert.AnError
}

// findQuickFixByDiagnosticCode finds a quickfix whose first attached diagnostic
// matches the given code, or an error if none.
func findQuickFixByDiagnosticCode(actions []protocol.CodeAction, code string) (protocol.CodeAction, error) {
	for _, action := range actions {
		if action.Kind == nil || *action.Kind != protocol.CodeActionKindQuickFix {
			continue
		}
		if len(action.Diagnostics) == 0 {
			continue
		}
		if fmt.Sprint(action.Diagnostics[0].Code) == code {
			return action, nil
		}
	}
	return protocol.CodeAction{}, assert.AnError
}

func applyWorkspaceEditToContent(t *testing.T, content string, uri uri.URI, edit *protocol.WorkspaceEdit) string {
	t.Helper()

	require.NotNil(t, edit)
	changes, ok := edit.Changes[uri]
	require.True(t, ok, "expected workspace edit for %s", uri)
	return applyTextEdits(content, changes)
}
