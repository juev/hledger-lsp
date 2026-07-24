package server

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/ast"
)

type completionResolveData struct {
	Kind   string  `json:"kind"`
	Label  string  `json:"label"`
	DocURI uri.URI `json:"docURI"`
}

func attachResolveData(items []protocol.CompletionItem, ctxType CompletionContextType, docURI uri.URI) {
	var kind string
	switch ctxType {
	case ContextAccount:
		kind = "account"
	case ContextPayee:
		kind = "payee"
	case ContextCommodity:
		kind = "commodity"
	case ContextTagName:
		kind = "tag"
	default:
		return
	}

	for i := range items {
		data := completionResolveData{
			Kind:   kind,
			Label:  items[i].Label,
			DocURI: docURI,
		}
		if raw, err := protocol.Marshal(data); err == nil {
			items[i].Data = protocol.LSPAny(raw)
		}
	}
}

func (s *Server) CompletionResolve(_ context.Context, item *protocol.CompletionItem) (*protocol.CompletionItem, error) {
	if len(item.Data) == 0 {
		return item, nil
	}

	var data completionResolveData
	if err := protocol.Unmarshal(item.Data, &data); err != nil {
		return item, nil //nolint:nilerr // LSP: return unresolved item gracefully
	}

	if data.Kind == "" {
		return item, nil
	}

	resolved := s.getWorkspaceResolved(data.DocURI)
	var allTransactions []ast.Transaction
	if resolved != nil {
		allTransactions = resolved.AllTransactions()
	} else {
		doc, ok := s.GetDocument(data.DocURI)
		if !ok {
			return item, nil
		}
		journal, _ := s.cachedJournal(data.DocURI, doc)
		allTransactions = journal.Transactions
	}

	var documentation string
	switch data.Kind {
	case "account":
		documentation = buildAccountResolveDoc(data.Label, allTransactions)
	case "payee":
		documentation = buildPayeeResolveDoc(data.Label, allTransactions)
	case "commodity":
		documentation = buildCommodityResolveDoc(data.Label, allTransactions)
	case "tag":
		documentation = buildTagResolveDoc(data.Label, allTransactions)
	default:
		return item, nil
	}

	if documentation != "" {
		item.Documentation = documentationMarkupContent(documentation, s.clientCapabilities.completionDocumentationFormats)
	}

	return item, nil
}

func buildAccountResolveDoc(name string, transactions []ast.Transaction) string {
	balances := analyzer.CalculateAccountBalancesFromTransactions(transactions)

	var sb strings.Builder
	fmt.Fprintf(&sb, "**Account:** `%s`\n\n", name)

	if commodityBalances, ok := balances[name]; ok && len(commodityBalances) > 0 {
		sb.WriteString("**Balance:**\n")
		commodities := make([]string, 0, len(commodityBalances))
		for c := range commodityBalances {
			commodities = append(commodities, c)
		}
		sort.Strings(commodities)
		for _, c := range commodities {
			bal := commodityBalances[c]
			fmt.Fprintf(&sb, "- %s %s\n", bal.String(), c)
		}
		sb.WriteString("\n")
	}

	count := countPostingsForAccountInTransactions(name, transactions)
	fmt.Fprintf(&sb, "**Postings:** %d", count)

	return sb.String()
}

func buildPayeeResolveDoc(payee string, transactions []ast.Transaction) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "**Payee:** %s\n\n", payee)

	count := 0
	var lastDate *ast.Date
	for i := range transactions {
		tx := &transactions[i]
		if tx.Payee == payee || tx.Description == payee {
			count++
			lastDate = &tx.Date
		}
	}

	fmt.Fprintf(&sb, "**Transactions:** %d", count)
	if lastDate != nil {
		fmt.Fprintf(&sb, "\n\n**Last:** %04d-%02d-%02d", lastDate.Year, lastDate.Month, lastDate.Day)
	}

	return sb.String()
}

func buildCommodityResolveDoc(symbol string, transactions []ast.Transaction) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "**Commodity:** `%s`\n\n", symbol)

	balances := analyzer.CalculateAccountBalancesFromTransactions(transactions)
	accounts := make([]string, 0)
	for account, commodityBalances := range balances {
		if _, ok := commodityBalances[symbol]; ok {
			accounts = append(accounts, account)
		}
	}
	sort.Strings(accounts)

	if len(accounts) > 0 {
		sb.WriteString("**Accounts:**\n")
		for _, account := range accounts {
			bal := balances[account][symbol]
			fmt.Fprintf(&sb, "- `%s`: %s\n", account, bal.String())
		}
	}

	return sb.String()
}

func buildTagResolveDoc(tagName string, transactions []ast.Transaction) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "**Tag:** `%s`\n\n", tagName)

	count := countTagUsage(tagName, transactions)
	fmt.Fprintf(&sb, "**Usage:** %d", count)

	return sb.String()
}
