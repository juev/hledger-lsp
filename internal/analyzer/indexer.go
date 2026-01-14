package analyzer

import (
	"strings"

	"github.com/juev/hledger-lsp/internal/ast"
)

func CollectAccounts(journal *ast.Journal) *AccountIndex {
	idx := NewAccountIndex()
	seen := make(map[string]bool)

	for _, dir := range journal.Directives {
		if accDir, ok := dir.(*ast.AccountDirective); ok {
			addAccount(idx, seen, accDir.Account.Name)
		}
	}

	for _, tx := range journal.Transactions {
		for _, posting := range tx.Postings {
			addAccount(idx, seen, posting.Account.Name)
		}
	}

	return idx
}

func addAccount(idx *AccountIndex, seen map[string]bool, name string) {
	if name == "" || seen[name] {
		return
	}
	seen[name] = true
	addAccountToIndex(idx, name)
}

func addAccountToIndex(idx *AccountIndex, name string) {
	idx.All = append(idx.All, name)

	parts := strings.Split(name, ":")
	for i := 1; i < len(parts); i++ {
		prefix := strings.Join(parts[:i], ":") + ":"
		idx.ByPrefix[prefix] = append(idx.ByPrefix[prefix], name)
	}
}

func CollectPayees(journal *ast.Journal) []string {
	seen := make(map[string]bool)
	var payees []string

	for _, tx := range journal.Transactions {
		name := tx.Payee
		if name == "" {
			name = tx.Description
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		payees = append(payees, name)
	}

	return payees
}

func CollectCommodities(journal *ast.Journal) []string {
	seen := make(map[string]bool)
	var commodities []string

	for _, dir := range journal.Directives {
		if cd, ok := dir.(ast.CommodityDirective); ok {
			if cd.Commodity.Symbol != "" && !seen[cd.Commodity.Symbol] {
				seen[cd.Commodity.Symbol] = true
				commodities = append(commodities, cd.Commodity.Symbol)
			}
		}
	}

	for _, tx := range journal.Transactions {
		for _, posting := range tx.Postings {
			if posting.Amount != nil {
				symbol := posting.Amount.Commodity.Symbol
				if symbol != "" && !seen[symbol] {
					seen[symbol] = true
					commodities = append(commodities, symbol)
				}
			}
			if posting.Cost != nil {
				symbol := posting.Cost.Amount.Commodity.Symbol
				if symbol != "" && !seen[symbol] {
					seen[symbol] = true
					commodities = append(commodities, symbol)
				}
			}
		}
	}

	return commodities
}

func CollectTags(journal *ast.Journal) []string {
	seen := make(map[string]bool)
	var tags []string

	for _, tx := range journal.Transactions {
		for _, tag := range tx.Tags {
			if tag.Name != "" && !seen[tag.Name] {
				seen[tag.Name] = true
				tags = append(tags, tag.Name)
			}
		}
		for _, posting := range tx.Postings {
			for _, tag := range posting.Tags {
				if tag.Name != "" && !seen[tag.Name] {
					seen[tag.Name] = true
					tags = append(tags, tag.Name)
				}
			}
		}
	}

	return tags
}
