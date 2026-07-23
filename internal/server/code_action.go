package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/shopspring/decimal"
	"go.lsp.dev/protocol"

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

func getHledgerCommands() []hledgerCommand {
	return []hledgerCommand{
		{cmd: "bal", title: "Run hledger bal (balance)"},
		{cmd: "reg", title: "Run hledger reg (register)"},
		{cmd: "is", title: "Run hledger is (income statement)"},
		{cmd: "bs", title: "Run hledger bs (balance sheet)"},
		{cmd: "cf", title: "Run hledger cf (cash flow)"},
	}
}

func (s *Server) CodeAction(ctx context.Context, params *protocol.CodeActionParams) ([]protocol.CodeAction, error) {
	if filetype.IsRules(string(params.TextDocument.URI)) {
		return nil, nil
	}

	actions := s.getCodeActions(params.TextDocument.URI)
	quickFixes := s.getQuickFixCodeActions(params)

	result := make([]protocol.CodeAction, 0, len(actions)+len(quickFixes))
	result = append(result, quickFixes...)
	for _, action := range actions {
		a := action
		a.Diagnostics = nil
		result = append(result, a)
	}

	return result, nil
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
		return actions[i].IsPreferred && !actions[j].IsPreferred
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
	uri protocol.DocumentURI,
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

	tx := findTransactionForDiagnostic(journal.Transactions, diag.Range)
	if tx == nil {
		return protocol.CodeAction{}, false
	}

	edit, title, ok := buildUnbalancedFix(uri, doc, mapper, commodityFormats, balanceTolerance, tx)
	if !ok {
		return protocol.CodeAction{}, false
	}
	return protocol.CodeAction{
		Title:       title,
		Kind:        protocol.QuickFix,
		Diagnostics: []protocol.Diagnostic{diag},
		IsPreferred: true,
		Edit:        edit,
	}, true
}

// buildUnbalancedFix computes the WorkspaceEdit that corrects the final real
// posting of a single-commodity unbalanced transaction. Shared by the
// UNBALANCED quickfix and the hledger.fixUnbalanced code-lens command.
func buildUnbalancedFix(
	uri protocol.DocumentURI,
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
		Changes: map[protocol.DocumentURI][]protocol.TextEdit{
			uri: {lineEdit},
		},
	}, title, true
}

func (s *Server) getCodeActions(uri protocol.DocumentURI) []protocol.CodeAction {
	settings := s.getSettings()
	if s.cliClient == nil || !s.cliClient.Available() || !settings.CLI.Enabled {
		return nil
	}

	commands := getHledgerCommands()
	actions := make([]protocol.CodeAction, 0, len(commands))

	for _, cmd := range commands {
		actions = append(actions, protocol.CodeAction{
			Title: cmd.title,
			Kind:  "source.hledger",
			Command: &protocol.Command{
				Title:   cmd.title,
				Command: "hledger.run",
				// The invoking document URI is carried alongside the command so
				// ExecuteCommand can target the journal the action came from
				// (LSP ExecuteCommandParams has no URI field of its own).
				Arguments: []any{
					cmd.cmd,
					string(uri),
				},
			},
		})
	}

	return actions
}

func (s *Server) ExecuteCommand(ctx context.Context, params *protocol.ExecuteCommandParams) (any, error) {
	switch params.Command {
	case "hledger.run":
		return s.executeRunCommand(ctx, params)
	case "hledger.fixUnbalanced":
		return s.executeFixUnbalanced(ctx, params)
	default:
		return nil, fmt.Errorf("unknown command: %s", params.Command)
	}
}

func (s *Server) executeRunCommand(ctx context.Context, params *protocol.ExecuteCommandParams) (any, error) {
	if len(params.Arguments) < 1 {
		return nil, fmt.Errorf("missing command argument")
	}

	cmd, ok := params.Arguments[0].(string)
	if !ok {
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
		return formatOutputAsComment(cmd, fmt.Sprintf("Error: %v", err)), nil
	}

	return formatOutputAsComment(cmd, output), nil
}

// executeFixUnbalanced resolves the transaction targeted by the unbalanced
// code lens and applies the balance quickfix via
// workspace/applyEdit so clicking the lens fixes the transaction in place.
func (s *Server) executeFixUnbalanced(ctx context.Context, params *protocol.ExecuteCommandParams) (any, error) {
	if len(params.Arguments) < 2 {
		return nil, fmt.Errorf("missing arguments")
	}

	uriRaw, ok := params.Arguments[0].(string)
	if !ok {
		return nil, fmt.Errorf("invalid uri argument")
	}
	uri := protocol.DocumentURI(uriRaw)

	txRange, ok := commandRangeArg(params.Arguments[1])
	if !ok {
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

	tx := findTransactionByRange(journal.Transactions, txRange)
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
		return edit, nil
	}

	applied, err := s.client.ApplyEdit(ctx, &protocol.ApplyWorkspaceEditParams{
		Label: "hledger-lsp: fix unbalanced transaction",
		Edit:  *edit,
	})
	if err != nil {
		return nil, fmt.Errorf("apply workspace edit: %w", err)
	}
	if !applied {
		return nil, fmt.Errorf("client rejected the edit")
	}
	return nil, nil
}

func findTransactionByRange(transactions []ast.Transaction, target protocol.Range) *ast.Transaction {
	for i := range transactions {
		r := astRangeToProtocol(transactions[i].Range)
		if r != nil && *r == target {
			return &transactions[i]
		}
	}
	return nil
}

func commandRangeArg(v any) (protocol.Range, bool) {
	switch value := v.(type) {
	case protocol.Range:
		return value, true
	case *protocol.Range:
		if value != nil {
			return *value, true
		}
		return protocol.Range{}, false
	case map[string]any:
		start, startOK := commandPositionArg(value["start"])
		end, endOK := commandPositionArg(value["end"])
		return protocol.Range{Start: start, End: end}, startOK && endOK
	default:
		return protocol.Range{}, false
	}
}

func commandPositionArg(v any) (protocol.Position, bool) {
	value, ok := v.(map[string]any)
	if !ok {
		return protocol.Position{}, false
	}
	line, lineOK := argUint32(value["line"])
	character, characterOK := argUint32(value["character"])
	return protocol.Position{Line: line, Character: character}, lineOK && characterOK
}

func argUint32(v any) (uint32, bool) {
	switch n := v.(type) {
	case float64:
		if n < 0 || n > float64(^uint32(0)) || n != float64(uint32(n)) {
			return 0, false
		}
		return uint32(n), true
	case float32:
		if n < 0 || float64(n) > float64(^uint32(0)) || n != float32(uint32(n)) {
			return 0, false
		}
		return uint32(n), true
	case int:
		if n < 0 || uint64(n) > uint64(^uint32(0)) {
			return 0, false
		}
		return uint32(n), true
	case int64:
		if n < 0 || uint64(n) > uint64(^uint32(0)) {
			return 0, false
		}
		return uint32(n), true
	case uint32:
		return n, true
	case uint64:
		if n > uint64(^uint32(0)) {
			return 0, false
		}
		return uint32(n), true
	}
	return 0, false
}

// resolveCommandFile determines which journal file an hledger.run command
// targets. getCodeActions passes the invoking document URI as the second
// argument; when present and resolvable it wins, so a command runs against the
// journal it was invoked from even with multiple documents open. Without a
// usable URI (e.g. a client that omits it) it falls back to the first open
// document, preserving the legacy behavior.
func (s *Server) resolveCommandFile(args []any) string {
	if len(args) >= 2 {
		if rawURI, ok := args[1].(string); ok && rawURI != "" {
			if path := uriToPath(protocol.DocumentURI(rawURI)); path != "" {
				return path
			}
		}
	}

	var filePath string
	s.documents.Range(func(key, _ any) bool {
		docURI := key.(protocol.DocumentURI)
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

func findTransactionForDiagnostic(transactions []ast.Transaction, rng protocol.Range) *ast.Transaction {
	for i := range transactions {
		txRange := astRangeToProtocol(transactions[i].Range)
		if txRange != nil && rangesOverlap(*txRange, rng) {
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
	URI      protocol.DocumentURI
	Position protocol.Position
}

func (s *Server) quickFixForUndeclaredAccount(uri protocol.DocumentURI, diag protocol.Diagnostic) (protocol.CodeAction, bool) {
	name, ok := extractQuotedName(diag.Message, "account '", "' is not declared")
	if !ok || name == "" {
		return protocol.CodeAction{}, false
	}
	target := selectDeclarationInsertion(s.declarationResolved(uri), uri, kindAccount)
	return protocol.CodeAction{
		Title:       "Declare account " + name,
		Kind:        protocol.QuickFix,
		Diagnostics: []protocol.Diagnostic{diag},
		Edit:        declarationEdit(target.URI, target.Position, declarationDirectiveText(target.Position, "account "+name)),
	}, true
}

func (s *Server) quickFixForUndeclaredCommodity(uri protocol.DocumentURI, diag protocol.Diagnostic) (protocol.CodeAction, bool) {
	symbol, ok := extractQuotedName(diag.Message, "commodity '", "' has no directive")
	if !ok || symbol == "" {
		return protocol.CodeAction{}, false
	}
	target := selectDeclarationInsertion(s.declarationResolved(uri), uri, kindCommodity)
	return protocol.CodeAction{
		Title:       "Declare commodity " + symbol,
		Kind:        protocol.QuickFix,
		Diagnostics: []protocol.Diagnostic{diag},
		Edit:        declarationEdit(target.URI, target.Position, declarationDirectiveText(target.Position, formatCommodityDirectiveText(symbol))),
	}, true
}

func declarationEdit(uri protocol.DocumentURI, pos protocol.Position, text string) *protocol.WorkspaceEdit {
	return &protocol.WorkspaceEdit{
		Changes: map[protocol.DocumentURI][]protocol.TextEdit{
			uri: {{
				Range:   protocol.Range{Start: pos, End: pos},
				NewText: text,
			}},
		},
	}
}

func (s *Server) declarationResolved(uri protocol.DocumentURI) *include.ResolvedJournal {
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
func selectDeclarationInsertion(resolved *include.ResolvedJournal, currentURI protocol.DocumentURI, kind directiveKind) declarationTarget {
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
