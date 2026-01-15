package server

import (
	"context"
	"strings"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/lsputil"
	"github.com/juev/hledger-lsp/internal/parser"
)

type CompletionContextType int

const (
	ContextUnknown CompletionContextType = iota
	ContextAccount
	ContextPayee
	ContextCommodity
)

func (s *Server) Completion(ctx context.Context, params *protocol.CompletionParams) (*protocol.CompletionList, error) {
	doc, ok := s.GetDocument(params.TextDocument.URI)
	if !ok {
		return &protocol.CompletionList{Items: []protocol.CompletionItem{}}, nil
	}

	var result *analyzer.AnalysisResult
	if resolved := s.GetResolved(params.TextDocument.URI); resolved != nil {
		result = s.analyzer.AnalyzeResolved(resolved)
	} else {
		journal, _ := parser.Parse(doc)
		result = s.analyzer.Analyze(journal)
	}

	completionCtx := determineCompletionContext(doc, params.Position, params.Context)
	items := generateCompletionItems(completionCtx, result, doc, params.Position)
	settings := s.getSettings()
	if settings.Completion.MaxResults > 0 && len(items) > settings.Completion.MaxResults {
		items = items[:settings.Completion.MaxResults]
	}

	return &protocol.CompletionList{
		IsIncomplete: false,
		Items:        items,
	}, nil
}

func determineCompletionContext(content string, pos protocol.Position, ctx *protocol.CompletionContext) CompletionContextType {
	lines := strings.Split(content, "\n")
	if int(pos.Line) >= len(lines) {
		return ContextUnknown
	}

	line := lines[pos.Line]

	if ctx != nil && ctx.TriggerCharacter == ":" {
		return ContextAccount
	}

	if ctx != nil && (ctx.TriggerCharacter == "@" || ctx.TriggerCharacter == "=") {
		return ContextCommodity
	}

	if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
		return ContextAccount
	}

	if len(line) > 0 && line[0] >= '0' && line[0] <= '9' {
		return ContextPayee
	}

	return ContextAccount
}

func generateCompletionItems(ctxType CompletionContextType, result *analyzer.AnalysisResult, content string, pos protocol.Position) []protocol.CompletionItem {
	var items []protocol.CompletionItem

	switch ctxType {
	case ContextAccount:
		prefix := extractAccountPrefix(content, pos)
		accounts := getAccountsForPrefix(result.Accounts, prefix)
		for _, acc := range accounts {
			items = append(items, protocol.CompletionItem{
				Label:  acc,
				Kind:   protocol.CompletionItemKindVariable,
				Detail: "Account",
			})
		}

	case ContextPayee:
		for _, payee := range result.Payees {
			items = append(items, protocol.CompletionItem{
				Label:  payee,
				Kind:   protocol.CompletionItemKindClass,
				Detail: "Payee",
			})
		}

	case ContextCommodity:
		for _, commodity := range result.Commodities {
			items = append(items, protocol.CompletionItem{
				Label:  commodity,
				Kind:   protocol.CompletionItemKindEnum,
				Detail: "Commodity",
			})
		}

	default:
		for _, acc := range result.Accounts.All {
			items = append(items, protocol.CompletionItem{
				Label:  acc,
				Kind:   protocol.CompletionItemKindVariable,
				Detail: "Account",
			})
		}
	}

	return items
}

func extractAccountPrefix(content string, pos protocol.Position) string {
	lines := strings.Split(content, "\n")
	if int(pos.Line) >= len(lines) {
		return ""
	}

	line := lines[pos.Line]
	byteCol := lsputil.UTF16OffsetToByteOffset(line, int(pos.Character))
	if byteCol > len(line) {
		byteCol = len(line)
	}

	beforeCursor := strings.TrimSpace(line[:byteCol])

	lastColon := strings.LastIndex(beforeCursor, ":")
	if lastColon == -1 {
		return ""
	}

	start := strings.LastIndexAny(beforeCursor[:lastColon], " \t")
	if start == -1 {
		return beforeCursor[:lastColon+1]
	}
	return beforeCursor[start+1 : lastColon+1]
}

func getAccountsForPrefix(accounts *analyzer.AccountIndex, prefix string) []string {
	if prefix == "" {
		return accounts.All
	}

	if accs, ok := accounts.ByPrefix[prefix]; ok {
		return accs
	}

	return accounts.All
}
