package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/shopspring/decimal"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/filetype"
	"github.com/juev/hledger-lsp/internal/formatter"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/lsputil"
)

type hledgerCommand struct {
	cmd   string
	title string
}

func boolPtr(value bool) *bool { return &value }

func stringPtr(value string) *string { return &value }

func codeActionKind(value protocol.CodeActionKind) *protocol.CodeActionKind { return &value }

// codeActionKindAllowed reports whether an action of this kind may be returned
// for the given CodeActionContext.Only filter. Kinds form a dotted hierarchy,
// so "quickfix" also selects "quickfix.hledger.insertInferredAmount". An empty
// filter selects everything; an action without a kind matches no filter.
func codeActionKindAllowed(kind *protocol.CodeActionKind, only []protocol.CodeActionKind) bool {
	if len(only) == 0 {
		return true
	}
	if kind == nil {
		return false
	}
	for _, filter := range only {
		if *kind == filter || strings.HasPrefix(string(*kind), string(filter)+".") {
			return true
		}
	}
	return false
}

func getHledgerCommands() []hledgerCommand {
	return []hledgerCommand{
		{cmd: "bal", title: "Run hledger bal (balance)"},
		{cmd: "reg", title: "Run hledger reg (register)"},
		{cmd: "is", title: "Run hledger is (income statement)"},
		{cmd: "bs", title: "Run hledger bs (balance sheet)"},
		{cmd: "cf", title: "Run hledger cf (cash flow)"},
	}
}

func (s *Server) CodeAction(ctx context.Context, params *protocol.CodeActionParams) ([]protocol.CommandOrCodeAction, error) {
	if filetype.IsRules(string(params.TextDocument.URI)) {
		return nil, nil
	}

	// Each source is skipped up front when the filter excludes its kind:
	// quick fixes may run a full document analysis and the inferred-amount
	// source parses and computes alignment, and a client asking for one kind
	// should not pay for the others.
	only := params.Context.Only

	var actions []protocol.CodeAction
	if codeActionKindAllowed(codeActionKind("source.hledger"), only) {
		var err error
		actions, err = s.getCodeActions(params.TextDocument.URI)
		if err != nil {
			return nil, err
		}
	}
	var quickFixes []protocol.CodeAction
	if codeActionKindAllowed(codeActionKind(protocol.CodeActionKindQuickFix), only) {
		quickFixes = s.getQuickFixCodeActions(params)
	}
	var inferred []protocol.CodeAction
	if codeActionKindAllowed(codeActionKind(insertInferredAmountKind), only) {
		inferred = s.getInferredAmountCodeActions(params)
	}

	generated := make([]protocol.CodeAction, 0, len(actions)+len(quickFixes)+len(inferred))
	generated = append(generated, quickFixes...)
	generated = append(generated, inferred...)
	generated = append(generated, actions...)

	// Filtered before codeActionResult: the command-only arm drops the kind.
	filtered := generated[:0]
	for _, action := range generated {
		if codeActionKindAllowed(action.Kind, only) {
			filtered = append(filtered, action)
		}
	}
	return s.codeActionResult(filtered), nil
}

func (s *Server) codeActionResult(actions []protocol.CodeAction) []protocol.CommandOrCodeAction {
	result := make([]protocol.CommandOrCodeAction, 0, len(actions))
	if s.clientCapabilities.supportsCodeActionLiterals {
		for _, action := range actions {
			if !s.clientCapabilities.supportsCodeActionIsPreferred {
				action.IsPreferred = nil
			}
			result = append(result, &action)
		}
		return result
	}

	for _, action := range actions {
		if action.Command.Command == "" {
			continue
		}
		command := action.Command
		result = append(result, &command)
	}

	return result
}

func (s *Server) getQuickFixCodeActions(params *protocol.CodeActionParams) []protocol.CodeAction {
	doc, ok := s.getJournalDoc(params.TextDocument.URI)
	if !ok {
		return nil
	}

	settings := s.getSettings()

	var commodityFormats map[string]formatter.CommodityFormat
	if s.workspace != nil {
		commodityFormats = s.workspace.GetCommodityFormatsForFile(uriToPath(params.TextDocument.URI))
	}

	diagnostics := s.quickFixDiagnostics(params, doc)
	actions := make([]protocol.CodeAction, 0, len(diagnostics))
	mapper := lsputil.NewPositionMapper(doc)
	for _, diag := range diagnostics {
		var action protocol.CodeAction
		switch fmt.Sprint(diag.Code) {
		case "UNBALANCED":
			journal, _ := s.cachedJournal(params.TextDocument.URI, doc)
			if journal == nil {
				continue
			}
			formats := commodityFormats
			if formats == nil {
				formats = formatter.ExtractCommodityFormats(journal.Directives)
			}
			var ok bool
			action, ok = s.quickFixForUnbalanced(params.TextDocument.URI, doc, mapper, journal, formats, settings.Diagnostics.BalanceTolerance, diag)
			if !ok {
				continue
			}
		case "UNDECLARED_ACCOUNT":
			var ok bool
			action, ok = s.quickFixForUndeclaredAccount(params.TextDocument.URI, diag)
			if !ok {
				continue
			}
		case "UNDECLARED_COMMODITY":
			var ok bool
			action, ok = s.quickFixForUndeclaredCommodity(params.TextDocument.URI, diag)
			if !ok {
				continue
			}
		default:
			continue
		}
		actions = append(actions, action)
	}

	// Preferred quickfixes first (the single correct amount fix) so the editor
	// surfaces the best action when several quickfixes are available.
	sort.SliceStable(actions, func(i, j int) bool {
		return actions[i].IsPreferred != nil && *actions[i].IsPreferred &&
			(actions[j].IsPreferred == nil || !*actions[j].IsPreferred)
	})

	return actions
}

func (s *Server) quickFixDiagnostics(params *protocol.CodeActionParams, doc string) []protocol.Diagnostic {
	if len(params.Context.Diagnostics) > 0 {
		filtered := make([]protocol.Diagnostic, 0, len(params.Context.Diagnostics))
		for _, diag := range params.Context.Diagnostics {
			if isQuickFixableCode(fmt.Sprint(diag.Code)) {
				filtered = append(filtered, diag)
			}
		}
		return filtered
	}

	diagnostics := s.analyze(params.TextDocument.URI, uriToPath(params.TextDocument.URI), doc)
	filtered := make([]protocol.Diagnostic, 0, len(diagnostics))
	for _, diag := range diagnostics {
		if !isQuickFixableCode(fmt.Sprint(diag.Code)) {
			continue
		}
		if rangesOverlap(diag.Range, params.Range) {
			filtered = append(filtered, diag)
		}
	}
	return filtered
}

func isQuickFixableCode(code string) bool {
	switch code {
	case "UNBALANCED", "UNDECLARED_ACCOUNT", "UNDECLARED_COMMODITY":
		return true
	}
	return false
}

func (s *Server) quickFixForUnbalanced(
	uri uri.URI,
	doc string,
	mapper *lsputil.PositionMapper,
	journal *ast.Journal,
	commodityFormats map[string]formatter.CommodityFormat,
	balanceTolerance float64,
	diag protocol.Diagnostic,
) (protocol.CodeAction, bool) {
	if fmt.Sprint(diag.Code) != "UNBALANCED" {
		return protocol.CodeAction{}, false
	}

	tx := findTransactionForDiagnostic(mapper, journal.Transactions, diag.Range)
	if tx == nil {
		return protocol.CodeAction{}, false
	}

	edit, title, ok := buildUnbalancedFix(uri, doc, mapper, commodityFormats, balanceTolerance, tx)
	if !ok {
		return protocol.CodeAction{}, false
	}
	return protocol.CodeAction{
		Title:       title,
		Kind:        codeActionKind(protocol.CodeActionKindQuickFix),
		Diagnostics: []protocol.Diagnostic{diag},
		IsPreferred: boolPtr(true),
		Edit:        edit,
	}, true
}

// buildUnbalancedFix computes the WorkspaceEdit that corrects the final real
// posting of a single-commodity unbalanced transaction. Shared by the
// UNBALANCED quickfix and the hledger.fixUnbalanced code-lens command.
func buildUnbalancedFix(
	docURI uri.URI,
	doc string,
	mapper *lsputil.PositionMapper,
	commodityFormats map[string]formatter.CommodityFormat,
	balanceTolerance float64,
	tx *ast.Transaction,
) (*protocol.WorkspaceEdit, string, bool) {
	result := analyzer.CheckBalance(tx, decimal.NewFromFloat(balanceTolerance))
	if result.Balanced || result.InferredIdx >= 0 || len(result.Differences) != 1 {
		return nil, "", false
	}

	var commodity string
	for c := range result.Differences {
		commodity = c
	}
	signedSum := signedCommoditySum(tx, commodity)
	if commodity == "" || signedSum.IsZero() {
		return nil, "", false
	}

	postingIndex := findLastFixableRealPosting(tx, commodity)
	if postingIndex < 0 {
		return nil, "", false
	}

	posting := &tx.Postings[postingIndex]
	newAmount := *posting.Amount
	newAmount.Quantity = newAmount.Quantity.Sub(signedSum)
	newAmount.RawQuantity = adjustedRawQuantity(posting.Amount.RawQuantity, newAmount.Quantity)

	lineEdit, ok := buildQuickFixEdit(doc, mapper, tx, postingIndex, &newAmount, commodityFormats)
	if !ok {
		return nil, "", false
	}

	title := fmt.Sprintf("Fix final posting amount to %s", formatter.FormatAmount(&newAmount, commodityFormats))
	return &protocol.WorkspaceEdit{
		Changes: map[uri.URI][]protocol.TextEdit{
			docURI: {lineEdit},
		},
	}, title, true
}

func (s *Server) getCodeActions(uri uri.URI) ([]protocol.CodeAction, error) {
	settings := s.getSettings()
	if s.cliClient == nil || !s.cliClient.Available() || !settings.CLI.Enabled {
		return nil, nil
	}

	commands := getHledgerCommands()
	actions := make([]protocol.CodeAction, 0, len(commands))

	for _, cmd := range commands {
		arguments, err := commandArguments(cmd.cmd, string(uri))
		if err != nil {
			return nil, fmt.Errorf("marshal command arguments: %w", err)
		}
		actions = append(actions, protocol.CodeAction{
			Title: cmd.title,
			Kind:  codeActionKind("source.hledger"),
			Command: protocol.Command{
				Title:   cmd.title,
				Command: "hledger.run",
				// The invoking document URI is carried alongside the command so
				// ExecuteCommand can target the journal the action came from
				// (LSP ExecuteCommandParams has no URI field of its own).
				Arguments: arguments,
			},
		})
	}

	return actions, nil
}

func (s *Server) ExecuteCommand(ctx context.Context, params *protocol.ExecuteCommandParams) (protocol.LSPAny, error) {
	switch params.Command {
	case "hledger.run":
		return s.executeRunCommand(ctx, params)
	case "hledger.fixUnbalanced":
		return s.executeFixUnbalanced(ctx, params)
	default:
		return nil, fmt.Errorf("unknown command: %s", params.Command)
	}
}

func (s *Server) executeRunCommand(ctx context.Context, params *protocol.ExecuteCommandParams) (protocol.LSPAny, error) {
	if len(params.Arguments) < 1 {
		return nil, fmt.Errorf("missing command argument")
	}

	cmd, err := unmarshalLSPAny[string](params.Arguments[0])
	if err != nil {
		return nil, fmt.Errorf("invalid command argument type")
	}

	if s.cliClient == nil || !s.cliClient.Available() {
		return nil, fmt.Errorf("hledger not available")
	}

	filePath := s.resolveCommandFile(params.Arguments)
	if filePath == "" {
		return nil, fmt.Errorf("no document open")
	}

	output, err := s.cliClient.Run(ctx, filePath, cmd)
	if err != nil {
		return marshalLSPAny(formatOutputAsComment(cmd, fmt.Sprintf("Error: %v", err)))
	}

	return marshalLSPAny(formatOutputAsComment(cmd, output))
}

// executeFixUnbalanced resolves the transaction targeted by the unbalanced
// code lens and applies the balance quickfix via
// workspace/applyEdit so clicking the lens fixes the transaction in place.
func (s *Server) executeFixUnbalanced(ctx context.Context, params *protocol.ExecuteCommandParams) (protocol.LSPAny, error) {
	if len(params.Arguments) < 2 {
		return nil, fmt.Errorf("missing arguments")
	}

	uriRaw, err := unmarshalLSPAny[string](params.Arguments[0])
	if err != nil {
		return nil, fmt.Errorf("invalid uri argument")
	}
	uri := uri.URI(uriRaw)

	txRange, err := commandRangeArg(params.Arguments[1])
	if err != nil {
		return nil, fmt.Errorf("invalid transaction range argument")
	}

	doc, ok := s.getJournalDoc(uri)
	if !ok {
		return nil, fmt.Errorf("document not open")
	}
	journal, _ := s.cachedJournal(uri, doc)
	if journal == nil {
		return nil, fmt.Errorf("failed to parse document")
	}

	tx := findTransactionByRange(lsputil.NewPositionMapper(doc), journal.Transactions, txRange)
	if tx == nil {
		return nil, fmt.Errorf("transaction not found")
	}

	settings := s.getSettings()
	var commodityFormats map[string]formatter.CommodityFormat
	if s.workspace != nil {
		commodityFormats = s.workspace.GetCommodityFormatsForFile(uriToPath(uri))
	}
	if commodityFormats == nil {
		commodityFormats = formatter.ExtractCommodityFormats(journal.Directives)
	}

	edit, _, ok := buildUnbalancedFix(uri, doc, lsputil.NewPositionMapper(doc), commodityFormats, settings.Diagnostics.BalanceTolerance, tx)
	if !ok {
		return nil, fmt.Errorf("no balance fix available")
	}

	if s.client == nil {
		return marshalLSPAny(edit)
	}

	applied, err := s.client.ApplyEdit(ctx, &protocol.ApplyWorkspaceEditParams{
		Label: stringPtr("hledger-lsp: fix unbalanced transaction"),
		Edit:  *edit,
	})
	if err != nil {
		return nil, fmt.Errorf("apply workspace edit: %w", err)
	}
	if applied == nil || !applied.Applied {
		return nil, fmt.Errorf("client rejected the edit")
	}
	return nil, nil
}

func findTransactionByRange(mapper *lsputil.PositionMapper, transactions []ast.Transaction, target protocol.Range) *ast.Transaction {
	for i := range transactions {
		r := astRangeToLSP(mapper, transactions[i].Range)
		if r == target {
			return &transactions[i]
		}
	}
	return nil
}

func commandRangeArg(v protocol.LSPAny) (protocol.Range, error) {
	return unmarshalLSPAny[protocol.Range](v)
}

func commandArguments(values ...any) ([]protocol.LSPAny, error) {
	arguments := make([]protocol.LSPAny, 0, len(values))
	for _, value := range values {
		encoded, err := marshalLSPAny(value)
		if err != nil {
			return nil, err
		}
		arguments = append(arguments, encoded)
	}
	return arguments, nil
}

func unmarshalLSPAny[T any](value protocol.LSPAny) (T, error) {
	var decoded T
	if err := protocol.Unmarshal(value, &decoded); err != nil {
		return decoded, err
	}
	return decoded, nil
}

func marshalLSPAny(value any) (protocol.LSPAny, error) {
	encoded, err := protocol.Marshal(value)
	if err != nil {
		return nil, err
	}
	return protocol.LSPAny(encoded), nil
}

// resolveCommandFile determines which journal file an hledger.run command
// targets. getCodeActions passes the invoking document URI as the second
// argument; when present and resolvable it wins, so a command runs against the
// journal it was invoked from even with multiple documents open. Without a
// usable URI (e.g. a client that omits it) it falls back to the first open
// document, preserving the legacy behavior.
func (s *Server) resolveCommandFile(args []protocol.LSPAny) string {
	if len(args) >= 2 {
		if rawURI, err := unmarshalLSPAny[string](args[1]); err == nil && rawURI != "" {
			if path := uriToPath(uri.URI(rawURI)); path != "" {
				return path
			}
		}
	}

	var filePath string
	s.documents.Range(func(key, _ any) bool {
		docURI := key.(uri.URI)
		if path := uriToPath(docURI); path != "" {
			filePath = path
			return false
		}
		return true
	})
	return filePath
}

func formatOutputAsComment(cmd, output string) string {
	header := fmt.Sprintf("; === hledger %s ===", cmd)
	footer := "; " + strings.Repeat("=", len(header)-3)

	output = strings.TrimRight(output, "\n\r\t ")

	var lines []string
	if output == "" {
		lines = []string{"(no output)"}
	} else {
		lines = strings.Split(output, "\n")
	}

	var builder strings.Builder
	builder.WriteString(header)
	builder.WriteString("\n")

	for _, line := range lines {
		builder.WriteString("; ")
		builder.WriteString(line)
		builder.WriteString("\n")
	}

	builder.WriteString(footer)

	return builder.String()
}

func findTransactionForDiagnostic(mapper *lsputil.PositionMapper, transactions []ast.Transaction, rng protocol.Range) *ast.Transaction {
	for i := range transactions {
		txRange := astRangeToLSP(mapper, transactions[i].Range)
		if rangesOverlap(txRange, rng) {
			return &transactions[i]
		}
	}
	return nil
}

func signedCommoditySum(tx *ast.Transaction, commodity string) decimal.Decimal {
	sum := decimal.Zero
	for i := range tx.Postings {
		posting := &tx.Postings[i]
		if posting.Virtual != ast.VirtualNone && posting.Virtual != ast.VirtualBalanced {
			continue
		}
		if posting.Amount == nil {
			continue
		}

		if posting.Cost != nil {
			if posting.Cost.Amount.Commodity.Symbol != commodity {
				continue
			}
			quantity := posting.Cost.Amount.Quantity
			if !posting.Cost.IsTotal {
				quantity = quantity.Mul(posting.Amount.Quantity.Abs())
			}
			if posting.Amount.Quantity.IsNegative() {
				quantity = quantity.Neg()
			}
			sum = sum.Add(quantity)
			continue
		}

		if posting.Amount.Commodity.Symbol == commodity {
			sum = sum.Add(posting.Amount.Quantity)
		}
	}

	return sum
}

func findLastFixableRealPosting(tx *ast.Transaction, commodity string) int {
	lastReal := -1
	for i := range tx.Postings {
		if tx.Postings[i].Virtual == ast.VirtualNone || tx.Postings[i].Virtual == ast.VirtualBalanced {
			lastReal = i
		}
	}

	if lastReal < 0 {
		return -1
	}

	posting := tx.Postings[lastReal]
	if posting.Amount == nil || posting.Cost != nil || posting.LotPrice != nil || posting.BalanceAssertion != nil {
		return -1
	}
	if posting.Amount.Commodity.Symbol != commodity {
		return -1
	}

	return lastReal
}

func adjustedRawQuantity(raw string, quantity decimal.Decimal) string {
	if raw == "" {
		return quantity.String()
	}

	format := formatter.ParseNumberFormat(raw)
	if !format.HasDecimal && !quantity.Equal(quantity.Round(0)) {
		return quantity.String()
	}

	return formatter.FormatNumber(quantity, format)
}

// directiveKind distinguishes account vs commodity declarations so the
// insertion-selection heuristic can match directives of the same kind.
type directiveKind int

const (
	kindAccount directiveKind = iota
	kindCommodity
)

// declarationTarget identifies the file and position where a new directive
// should be inserted.
type declarationTarget struct {
	URI      uri.URI
	Position protocol.Position
}

func (s *Server) quickFixForUndeclaredAccount(uri uri.URI, diag protocol.Diagnostic) (protocol.CodeAction, bool) {
	message, ok := diag.Message.(protocol.String)
	if !ok {
		return protocol.CodeAction{}, false
	}
	name, ok := extractQuotedName(string(message), "account '", "' is not declared")
	if !ok || name == "" {
		return protocol.CodeAction{}, false
	}
	target := selectDeclarationInsertion(s.declarationResolved(uri), uri, kindAccount)
	return protocol.CodeAction{
		Title:       "Declare account " + name,
		Kind:        codeActionKind(protocol.CodeActionKindQuickFix),
		Diagnostics: []protocol.Diagnostic{diag},
		Edit:        declarationEdit(target.URI, target.Position, declarationDirectiveText(target.Position, "account "+name)),
	}, true
}

func (s *Server) quickFixForUndeclaredCommodity(uri uri.URI, diag protocol.Diagnostic) (protocol.CodeAction, bool) {
	message, ok := diag.Message.(protocol.String)
	if !ok {
		return protocol.CodeAction{}, false
	}
	symbol, ok := extractQuotedName(string(message), "commodity '", "' has no directive")
	if !ok || symbol == "" {
		return protocol.CodeAction{}, false
	}
	target := selectDeclarationInsertion(s.declarationResolved(uri), uri, kindCommodity)
	return protocol.CodeAction{
		Title:       "Declare commodity " + symbol,
		Kind:        codeActionKind(protocol.CodeActionKindQuickFix),
		Diagnostics: []protocol.Diagnostic{diag},
		Edit:        declarationEdit(target.URI, target.Position, declarationDirectiveText(target.Position, formatCommodityDirectiveText(symbol))),
	}, true
}

func declarationEdit(docURI uri.URI, pos protocol.Position, text string) *protocol.WorkspaceEdit {
	return &protocol.WorkspaceEdit{
		Changes: map[uri.URI][]protocol.TextEdit{
			docURI: {{
				Range:   protocol.Range{Start: pos, End: pos},
				NewText: text,
			}},
		},
	}
}

func (s *Server) declarationResolved(uri uri.URI) *include.ResolvedJournal {
	if resolved := s.GetResolved(uri); resolved != nil {
		return resolved
	}
	return s.getWorkspaceResolved(uri)
}

// selectDeclarationInsertion chooses the file and position for a new directive
// of the given kind. The open document is the primary journal in the resolved
// tree, so it wins when it already declares the same kind; otherwise the first
// included file with same-kind declarations is chosen; otherwise the directive
// is inserted at the top of the current file.
func selectDeclarationInsertion(resolved *include.ResolvedJournal, currentURI uri.URI, kind directiveKind) declarationTarget {
	if resolved == nil || resolved.Primary == nil {
		return declarationTarget{URI: currentURI}
	}
	if pos, ok := lastDirectiveEndPosition(resolved.Primary, kind); ok {
		return declarationTarget{URI: currentURI, Position: pos}
	}
	for _, path := range resolved.FileOrder {
		journal, ok := resolved.Files[path]
		if !ok {
			continue
		}
		if pos, ok := lastDirectiveEndPosition(journal, kind); ok {
			return declarationTarget{URI: pathToURI(path), Position: pos}
		}
	}
	return declarationTarget{URI: currentURI}
}

// lastDirectiveEndPosition returns the exclusive end position of the last
// matching directive, converted from the parser's 1-based coordinates to LSP.
func lastDirectiveEndPosition(journal *ast.Journal, kind directiveKind) (protocol.Position, bool) {
	var last ast.Directive
	for _, d := range journal.Directives {
		var match bool
		switch kind {
		case kindAccount:
			_, match = d.(ast.AccountDirective)
		case kindCommodity:
			_, match = d.(ast.CommodityDirective)
		}
		if !match {
			continue
		}
		last = d
	}
	if last == nil {
		return protocol.Position{}, false
	}
	end := last.GetRange().End
	return protocol.Position{
		Line:      uint32(max(0, end.Line-1)),
		Character: uint32(max(0, end.Column-1)),
	}, true
}

func declarationDirectiveText(position protocol.Position, directive string) string {
	if position.Character > 0 {
		return "\n" + directive + "\n"
	}
	return directive + "\n"
}

// formatCommodityDirectiveText emits a valid `commodity` directive for the
// symbol. Bare symbols follow the lexer rules; symbols with spaces or other
// special characters are double-quoted.
func formatCommodityDirectiveText(symbol string) string {
	if commodityNeedsQuotes(symbol) {
		symbol = `"` + symbol + `"`
	}
	return "commodity " + symbol
}

func commodityNeedsQuotes(symbol string) bool {
	runes := []rune(symbol)
	if len(runes) == 0 {
		return true
	}
	if len(runes) == 1 && unicode.Is(unicode.Sc, runes[0]) {
		return false
	}
	if len(runes) == 3 && runes[0] >= 'A' && runes[0] <= 'Z' && runes[1] >= 'A' && runes[1] <= 'Z' && unicode.Is(unicode.Sc, runes[2]) {
		return false
	}
	if !unicode.IsLetter(runes[0]) {
		return true
	}
	for _, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// extractQuotedName accepts only the owned diagnostic shape and returns its
// single-quoted payload. Account names and commodity symbols never contain a
// single quote.
func extractQuotedName(msg, prefix, suffix string) (string, bool) {
	name, ok := strings.CutPrefix(msg, prefix)
	if !ok {
		return "", false
	}
	name, ok = strings.CutSuffix(name, suffix)
	if !ok || name == "" || strings.ContainsAny(name, "'\r\n") {
		return "", false
	}
	return name, true
}
