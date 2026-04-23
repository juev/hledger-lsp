package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/filetype"
	"github.com/juev/hledger-lsp/internal/formatter"
	"github.com/juev/hledger-lsp/internal/lsputil"
	"github.com/juev/hledger-lsp/internal/parser"
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

	actions := s.getCodeActions()
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
		journal, _ := parser.Parse(doc)
		if journal == nil {
			continue
		}
		formats := commodityFormats
		if formats == nil {
			formats = formatter.ExtractCommodityFormats(journal.Directives)
		}
		action, ok := s.quickFixForUnbalanced(params.TextDocument.URI, doc, mapper, journal, formats, settings.Diagnostics.BalanceTolerance, diag)
		if ok {
			actions = append(actions, action)
		}
	}

	return actions
}

func (s *Server) quickFixDiagnostics(params *protocol.CodeActionParams, doc string) []protocol.Diagnostic {
	if len(params.Context.Diagnostics) > 0 {
		return params.Context.Diagnostics
	}

	diagnostics := s.analyze(uriToPath(params.TextDocument.URI), doc)
	filtered := make([]protocol.Diagnostic, 0, len(diagnostics))
	for _, diag := range diagnostics {
		if fmt.Sprint(diag.Code) != "UNBALANCED" {
			continue
		}
		if rangesOverlap(diag.Range, params.Range) {
			filtered = append(filtered, diag)
		}
	}
	return filtered
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

	result := analyzer.CheckBalance(tx, decimal.NewFromFloat(balanceTolerance))
	if result.Balanced || result.InferredIdx >= 0 || len(result.Differences) != 1 {
		return protocol.CodeAction{}, false
	}

	var commodity string
	for c := range result.Differences {
		commodity = c
	}
	signedSum := signedCommoditySum(tx, commodity)
	if commodity == "" || signedSum.IsZero() {
		return protocol.CodeAction{}, false
	}

	postingIndex := findLastFixableRealPosting(tx, commodity)
	if postingIndex < 0 {
		return protocol.CodeAction{}, false
	}

	posting := &tx.Postings[postingIndex]
	newAmount := *posting.Amount
	newAmount.Quantity = newAmount.Quantity.Sub(signedSum)
	newAmount.RawQuantity = adjustedRawQuantity(posting.Amount.RawQuantity, newAmount.Quantity)

	lineEdit, ok := buildQuickFixEdit(doc, mapper, tx, postingIndex, &newAmount, commodityFormats)
	if !ok {
		return protocol.CodeAction{}, false
	}

	title := fmt.Sprintf("Fix final posting amount to %s", formatter.FormatAmount(&newAmount, commodityFormats))
	return protocol.CodeAction{
		Title:       title,
		Kind:        protocol.QuickFix,
		Diagnostics: []protocol.Diagnostic{diag},
		IsPreferred: true,
		Edit: &protocol.WorkspaceEdit{
			Changes: map[protocol.DocumentURI][]protocol.TextEdit{
				uri: {lineEdit},
			},
		},
	}, true
}

func (s *Server) getCodeActions() []protocol.CodeAction {
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
				Arguments: []any{
					cmd.cmd,
				},
			},
		})
	}

	return actions
}

func (s *Server) ExecuteCommand(ctx context.Context, params *protocol.ExecuteCommandParams) (any, error) {
	if params.Command != "hledger.run" {
		return nil, fmt.Errorf("unknown command: %s", params.Command)
	}

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

	var filePath string
	s.documents.Range(func(key, _ any) bool {
		docURI := key.(protocol.DocumentURI)
		path := uriToPath(docURI)
		if path != "" {
			filePath = path
			return false
		}
		return true
	})

	if filePath == "" {
		return nil, fmt.Errorf("no document open")
	}

	output, err := s.cliClient.Run(ctx, filePath, cmd)
	if err != nil {
		return formatOutputAsComment(cmd, fmt.Sprintf("Error: %v", err)), nil
	}

	return formatOutputAsComment(cmd, output), nil
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
