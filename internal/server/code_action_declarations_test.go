package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/workspace"
)

func accountDirectiveAt(line int) ast.AccountDirective {
	return ast.AccountDirective{Range: ast.Range{
		Start: ast.Position{Line: line},
		End:   ast.Position{Line: line + 1, Column: 1},
	}}
}

func commodityDirectiveAt(line int) ast.CommodityDirective {
	return ast.CommodityDirective{Range: ast.Range{
		Start: ast.Position{Line: line},
		End:   ast.Position{Line: line + 1, Column: 1},
	}}
}

func TestSelectDeclarationInsertion_PrimaryAppendsAfterLastAccountDirective(t *testing.T) {
	primary := &ast.Journal{Directives: []ast.Directive{
		accountDirectiveAt(1),
		accountDirectiveAt(3),
	}}
	resolved := include.NewResolvedJournal(primary)

	target := selectDeclarationInsertion(resolved, "file:///main.journal", kindAccount)
	assert.Equal(t, uri.URI("file:///main.journal"), target.URI)
	assert.Equal(t, uint32(3), target.Position.Line, "after the last account directive")
	assert.Equal(t, uint32(0), target.Position.Character)
}

func TestSelectDeclarationInsertion_FallsBackToIncludedFile(t *testing.T) {
	primary := &ast.Journal{} // no account directives (only an include)
	included := &ast.Journal{Directives: []ast.Directive{
		accountDirectiveAt(5), // 1-based line 5
	}}
	incPath := "/tmp/inc.journal"
	resolved := &include.ResolvedJournal{
		Primary:   primary,
		Files:     map[string]*ast.Journal{incPath: included},
		FileOrder: []string{incPath},
	}

	target := selectDeclarationInsertion(resolved, "file:///main.journal", kindAccount)
	assert.Equal(t, pathToURI(incPath), target.URI)
	assert.Equal(t, uint32(5), target.Position.Line, "after the included account directive")
}

func TestSelectDeclarationInsertion_NoDeclarationsAnywhere(t *testing.T) {
	primary := &ast.Journal{}
	resolved := include.NewResolvedJournal(primary)

	target := selectDeclarationInsertion(resolved, "file:///main.journal", kindAccount)
	assert.Equal(t, uri.URI("file:///main.journal"), target.URI)
	assert.Equal(t, uint32(0), target.Position.Line)
	assert.Equal(t, uint32(0), target.Position.Character)
}

func TestSelectDeclarationInsertion_NilResolved(t *testing.T) {
	target := selectDeclarationInsertion(nil, "file:///main.journal", kindAccount)
	assert.Equal(t, uri.URI("file:///main.journal"), target.URI)
	assert.Equal(t, uint32(0), target.Position.Line)
}

func TestSelectDeclarationInsertion_KindCommodityMatchesCommodityDirective(t *testing.T) {
	primary := &ast.Journal{Directives: []ast.Directive{
		accountDirectiveAt(2),   // account directive — must be skipped for commodity
		commodityDirectiveAt(4), // 1-based line 4
	}}
	resolved := include.NewResolvedJournal(primary)

	target := selectDeclarationInsertion(resolved, "file:///main.journal", kindCommodity)
	assert.Equal(t, uri.URI("file:///main.journal"), target.URI)
	assert.Equal(t, uint32(4), target.Position.Line, "after the commodity directive, skipping account directive")
}

func TestFormatCommodityDirectiveText(t *testing.T) {
	cases := []struct {
		name   string
		symbol string
		want   string
	}{
		{"single rune bare", "$", "commodity $"},
		{"euro bare", "€", "commodity €"},
		{"bare ticker", "EUR", "commodity EUR"},
		{"numeric symbol quoted", "123", "commodity \"123\""},
		{"digit prefix quoted", "123USD", "commodity \"123USD\""},
		{"with space quoted", "TEST B", "commodity \"TEST B\""},
		{"with dot quoted", "TEST.A", "commodity \"TEST.A\""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, formatCommodityDirectiveText(tc.symbol))
		})
	}
}

func TestExtractQuotedName_RequiresOwnedDiagnosticFormat(t *testing.T) {
	name, ok := extractQuotedName("account 'custom:thing' is not declared", "account '", "' is not declared")
	require.True(t, ok)
	assert.Equal(t, "custom:thing", name)

	_, ok = extractQuotedName("account 'custom:thing' is not declared\ninclude secrets.journal", "account '", "' is not declared")
	assert.False(t, ok)
}

func TestServer_CodeAction_DeclareAccountQuickFix(t *testing.T) {
	ts := newTestServer()
	ts.cliClient = nil

	uri := uri.URI("file:///test.journal")
	content := `account assets:cash

2024-01-15 grocery
    custom:thing  $50
    assets:cash`

	diagnostics, err := ts.openAndWait(uri, content)
	require.NoError(t, err)

	diag := requireDiagnosticByCode(t, diagnostics, "UNDECLARED_ACCOUNT")
	actions, err := codeActionsForDiagnostics(ts, uri, []protocol.Diagnostic{diag})
	require.NoError(t, err)

	action := requireQuickFixAction(t, actions)
	assert.Contains(t, action.Title, "Declare account custom:thing")

	fixed := applyWorkspaceEditToContent(t, content, uri, action.Edit)
	assert.Equal(t, `account assets:cash
account custom:thing

2024-01-15 grocery
    custom:thing  $50
    assets:cash`, fixed)

	// After applying, the account is declared — no UNDECLARED_ACCOUNT diagnostic.
	diagnostics, err = ts.replaceAndWait(uri, fixed)
	require.NoError(t, err)
	_, err = findDiagnosticByCode(diagnostics, "UNDECLARED_ACCOUNT")
	assert.Error(t, err)
}

func TestServer_CodeAction_DeclareCommodityQuickFix(t *testing.T) {
	ts := newTestServer()
	ts.cliClient = nil

	uri := uri.URI("file:///test.journal")
	content := `commodity EUR

2024-01-15 grocery
    assets:cash  50 "TEST B"
    equity:open`

	diagnostics, err := ts.openAndWait(uri, content)
	require.NoError(t, err)

	diag := requireDiagnosticByCode(t, diagnostics, "UNDECLARED_COMMODITY")
	actions, err := codeActionsForDiagnostics(ts, uri, []protocol.Diagnostic{diag})
	require.NoError(t, err)

	action := requireQuickFixAction(t, actions)
	assert.Contains(t, action.Title, "Declare commodity TEST B")

	fixed := applyWorkspaceEditToContent(t, content, uri, action.Edit)
	assert.Equal(t, `commodity EUR
commodity "TEST B"

2024-01-15 grocery
    assets:cash  50 "TEST B"
    equity:open`, fixed)

	diagnostics, err = ts.replaceAndWait(uri, fixed)
	require.NoError(t, err)
	_, err = findDiagnosticByCode(diagnostics, "UNDECLARED_COMMODITY")
	assert.Error(t, err)
}

func TestServer_CodeAction_DeclareAccount_CJKName(t *testing.T) {
	ts := newTestServer()
	ts.cliClient = nil

	uri := uri.URI("file:///test.journal")
	content := `account assets:cash

2024-01-15 покупка
    расходы:еда  $50
    assets:cash`

	diagnostics, err := ts.openAndWait(uri, content)
	require.NoError(t, err)

	diag := requireDiagnosticByCode(t, diagnostics, "UNDECLARED_ACCOUNT")
	assert.Contains(t, diag.Message, "расходы:еда")

	actions, err := codeActionsForDiagnostics(ts, uri, []protocol.Diagnostic{diag})
	require.NoError(t, err)

	action := requireQuickFixAction(t, actions)
	fixed := applyWorkspaceEditToContent(t, content, uri, action.Edit)
	assert.Equal(t, `account assets:cash
account расходы:еда

2024-01-15 покупка
    расходы:еда  $50
    assets:cash`, fixed)
}

func TestServer_CodeAction_DeclareAccount_CRLF(t *testing.T) {
	ts := newTestServer()
	ts.cliClient = nil

	uri := uri.URI("file:///test.journal")
	content := "account assets:cash\r\n\r\n2024-01-15 grocery\r\n    custom:thing  $50\r\n    assets:cash"

	diagnostics, err := ts.openAndWait(uri, content)
	require.NoError(t, err)

	diag := requireDiagnosticByCode(t, diagnostics, "UNDECLARED_ACCOUNT")
	actions, err := codeActionsForDiagnostics(ts, uri, []protocol.Diagnostic{diag})
	require.NoError(t, err)

	action := requireQuickFixAction(t, actions)
	// The server normalizes ingestion to LF; the edit is against the
	// normalized document, so the applied result uses LF.
	normalized := "account assets:cash\n\n2024-01-15 grocery\n    custom:thing  $50\n    assets:cash"
	fixed := applyWorkspaceEditToContent(t, normalized, uri, action.Edit)
	assert.Equal(t, "account assets:cash\naccount custom:thing\n\n2024-01-15 grocery\n    custom:thing  $50\n    assets:cash", fixed)
}

func TestServer_CodeAction_DeclareAccount_TargetsIncludedFile(t *testing.T) {
	tmpDir := t.TempDir()
	accountsPath := filepath.Join(tmpDir, "accounts.journal")
	mainPath := filepath.Join(tmpDir, "main.journal")
	accountsContent := "account assets:cash\n"
	mainContent := `include accounts.journal

2024-01-15 grocery
    custom:thing  $50
    assets:cash`

	require.NoError(t, os.WriteFile(accountsPath, []byte(accountsContent), 0o644))
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o644))

	ts := newTestServer()
	ts.cliClient = nil
	loader := include.NewLoader()
	ts.loader = loader
	ts.workspace = workspace.NewWorkspace(tmpDir, loader)
	require.NoError(t, ts.workspace.Initialize())
	mainURI := pathToURI(mainPath)
	diagnostics, err := ts.openAndWait(mainURI, mainContent)
	require.NoError(t, err)

	diag := requireDiagnosticByCode(t, diagnostics, "UNDECLARED_ACCOUNT")
	actions, err := codeActionsForDiagnostics(ts, mainURI, []protocol.Diagnostic{diag})
	require.NoError(t, err)

	action := requireQuickFixAction(t, actions)
	accountsURI := pathToURI(accountsPath)
	fixed := applyWorkspaceEditToContent(t, accountsContent, accountsURI, action.Edit)
	assert.Equal(t, "account assets:cash\naccount custom:thing\n", fixed)
	assert.NotContains(t, action.Edit.Changes, mainURI)
}

func TestServer_CodeAction_DeclareAccount_PrefersOpenIncludedFile(t *testing.T) {
	tmpDir := t.TempDir()
	mainPath := filepath.Join(tmpDir, "main.journal")
	includedPath := filepath.Join(tmpDir, "included.journal")
	mainContent := "include included.journal\n\naccount assets:root\n"
	includedContent := `account assets:cash

2024-01-15 grocery
    custom:thing  $50
    assets:cash`

	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o644))
	require.NoError(t, os.WriteFile(includedPath, []byte(includedContent), 0o644))

	ts := newTestServer()
	ts.cliClient = nil
	loader := include.NewLoader()
	ts.loader = loader
	ts.workspace = workspace.NewWorkspace(tmpDir, loader)
	require.NoError(t, ts.workspace.Initialize())
	workspaceResolved := ts.workspace.GetResolvedForFile(includedPath)
	require.NotNil(t, workspaceResolved)
	rootDeclaration := workspaceResolved.Primary.Directives[0].(ast.AccountDirective)
	assert.Equal(t, "assets:root", rootDeclaration.Account.Name)
	includedURI := pathToURI(includedPath)
	diagnostics, err := ts.openAndWait(includedURI, includedContent)
	require.NoError(t, err)
	workspaceResolved = ts.workspace.GetResolvedForFile(includedPath)
	rootDeclaration = workspaceResolved.Primary.Directives[0].(ast.AccountDirective)
	assert.Equal(t, "assets:root", rootDeclaration.Account.Name)

	diag := requireDiagnosticByCode(t, diagnostics, "UNDECLARED_ACCOUNT")
	actions, err := codeActionsForDiagnostics(ts, includedURI, []protocol.Diagnostic{diag})
	require.NoError(t, err)

	action := requireQuickFixAction(t, actions)
	fixed := applyWorkspaceEditToContent(t, includedContent, includedURI, action.Edit)
	assert.Contains(t, fixed, "account assets:cash\naccount custom:thing\n")
	assert.NotContains(t, action.Edit.Changes, pathToURI(mainPath))
}

// TestServer_QuickFix_DeclareAccount_TargetsIncludedFile drives the quickfix
// builder directly with a constructed include tree. The analyzer only flags
// undeclared accounts when at least one `account` directive exists (opt-in), so
// an end-to-end "declarations live in an included file" path requires a
// workspace; testing the builder isolates the multi-file targeting logic.
func TestServer_QuickFix_DeclareAccount_TargetsIncludedFile(t *testing.T) {
	ts := newTestServer()
	ts.cliClient = nil

	uri := uri.URI("file:///main.journal")
	// Primary (the open document) declares no accounts; the included file does.
	primary := &ast.Journal{}
	included := &ast.Journal{Directives: []ast.Directive{accountDirectiveAt(2)}}
	incPath := "/tmp/accounts.journal"
	ts.resolved.Store(uri, &include.ResolvedJournal{
		Primary:   primary,
		Files:     map[string]*ast.Journal{incPath: included},
		FileOrder: []string{incPath},
	})

	diag := protocol.Diagnostic{
		Code:    protocol.String("UNDECLARED_ACCOUNT"),
		Message: protocol.String("account 'custom:thing' is not declared"),
	}
	action, ok := ts.quickFixForUndeclaredAccount(uri, diag)
	require.True(t, ok)

	accountsURI := pathToURI(incPath)
	changes, ok := action.Edit.Changes[accountsURI]
	require.True(t, ok, "quickfix should target the included accounts.journal")
	require.Len(t, changes, 1)
	assert.Equal(t, "account custom:thing\n", changes[0].NewText)
	assert.Equal(t, uint32(2), changes[0].Range.Start.Line, "insert after the existing account directive")

	// The current file must not be edited.
	_, currentEdited := action.Edit.Changes[uri]
	assert.False(t, currentEdited, "quickfix must not edit the file that has no declarations")
}
