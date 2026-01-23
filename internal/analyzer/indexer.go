package analyzer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/juev/hledger-lsp/internal/ast"
)

func CollectAccounts(journal *ast.Journal) *AccountIndex {
	idx := NewAccountIndex()
	seen := make(map[string]bool)

	for _, dir := range journal.Directives {
		if accDir, ok := dir.(ast.AccountDirective); ok {
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

	collectTagsFrom := func(tagList []ast.Tag) {
		for _, tag := range tagList {
			if tag.Name != "" && !seen[tag.Name] {
				seen[tag.Name] = true
				tags = append(tags, tag.Name)
			}
		}
	}

	for _, tx := range journal.Transactions {
		collectTagsFrom(tx.Tags)
		for _, comment := range tx.Comments {
			collectTagsFrom(comment.Tags)
		}
		for _, posting := range tx.Postings {
			collectTagsFrom(posting.Tags)
		}
	}

	return tags
}

func CollectPayeeTemplates(journal *ast.Journal) map[string][]PostingTemplate {
	type txData struct {
		postings []PostingTemplate
		pattern  string
	}

	payeeTxs := make(map[string][]txData)

	for _, tx := range journal.Transactions {
		payee := tx.Payee
		if payee == "" {
			payee = tx.Description
		}
		if payee == "" {
			continue
		}

		if len(tx.Postings) == 0 {
			continue
		}

		var postings []PostingTemplate
		var accounts []string
		for _, p := range tx.Postings {
			pt := PostingTemplate{
				Account: p.Account.Name,
			}
			if p.Amount != nil {
				pt.Amount = p.Amount.RawQuantity
				if pt.Amount == "" {
					pt.Amount = p.Amount.Quantity.String()
				}
				pt.Commodity = p.Amount.Commodity.Symbol
				pt.CommodityLeft = p.Amount.Commodity.Position == ast.CommodityLeft
			}
			postings = append(postings, pt)
			accounts = append(accounts, p.Account.Name)
		}

		sort.Strings(accounts)
		pattern := strings.Join(accounts, "|")

		payeeTxs[payee] = append(payeeTxs[payee], txData{
			postings: postings,
			pattern:  pattern,
		})
	}

	result := make(map[string][]PostingTemplate)

	// Sort payees for deterministic iteration
	payeeNames := make([]string, 0, len(payeeTxs))
	for payee := range payeeTxs {
		payeeNames = append(payeeNames, payee)
	}
	sort.Strings(payeeNames)

	for _, payee := range payeeNames {
		txs := payeeTxs[payee]
		if len(txs) == 0 {
			continue
		}

		patternCount := make(map[string]int)
		patternLastIdx := make(map[string]int)

		for i, tx := range txs {
			patternCount[tx.pattern]++
			patternLastIdx[tx.pattern] = i
		}

		// Sort patterns for deterministic selection when counts are equal
		patterns := make([]string, 0, len(patternCount))
		for pattern := range patternCount {
			patterns = append(patterns, pattern)
		}
		sort.Strings(patterns)

		bestCount := 0
		bestLastIdx := -1

		for _, pattern := range patterns {
			count := patternCount[pattern]
			lastIdx := patternLastIdx[pattern]
			if count > bestCount || (count == bestCount && lastIdx > bestLastIdx) {
				bestCount = count
				bestLastIdx = lastIdx
			}
		}

		if bestLastIdx >= 0 {
			result[payee] = txs[bestLastIdx].postings
		}
	}

	return result
}

func CollectDates(journal *ast.Journal) []string {
	seen := make(map[string]bool)
	var dates []string

	for _, tx := range journal.Transactions {
		dateStr := formatDate(tx.Date)
		if dateStr != "" && !seen[dateStr] {
			seen[dateStr] = true
			dates = append(dates, dateStr)
		}
	}

	return dates
}

func formatDate(d ast.Date) string {
	if d.Year == 0 && d.Month == 0 && d.Day == 0 {
		return ""
	}
	if d.Month < 1 || d.Month > 12 || d.Day < 1 || d.Day > 31 {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
}

func CollectTagValues(journal *ast.Journal) map[string][]string {
	result := make(map[string][]string)
	seen := make(map[string]map[string]bool)

	addTagValue := func(tagList []ast.Tag) {
		for _, tag := range tagList {
			if tag.Name == "" {
				continue
			}
			if tag.Value == "" {
				if _, ok := result[tag.Name]; !ok {
					result[tag.Name] = []string{}
				}
				continue
			}
			if seen[tag.Name] == nil {
				seen[tag.Name] = make(map[string]bool)
			}
			if !seen[tag.Name][tag.Value] {
				seen[tag.Name][tag.Value] = true
				result[tag.Name] = append(result[tag.Name], tag.Value)
			}
		}
	}

	for _, tx := range journal.Transactions {
		addTagValue(tx.Tags)
		for _, comment := range tx.Comments {
			addTagValue(comment.Tags)
		}
		for _, posting := range tx.Postings {
			addTagValue(posting.Tags)
		}
	}

	return result
}

func CollectAccountCounts(journal *ast.Journal) map[string]int {
	counts := make(map[string]int)
	for _, tx := range journal.Transactions {
		for _, posting := range tx.Postings {
			if posting.Account.Name != "" {
				counts[posting.Account.Name]++
			}
		}
	}
	return counts
}

func CollectPayeeCounts(journal *ast.Journal) map[string]int {
	counts := make(map[string]int)
	for _, tx := range journal.Transactions {
		name := tx.Payee
		if name == "" {
			name = tx.Description
		}
		if name != "" {
			counts[name]++
		}
	}
	return counts
}

func CollectCommodityCounts(journal *ast.Journal) map[string]int {
	counts := make(map[string]int)
	for _, tx := range journal.Transactions {
		for _, posting := range tx.Postings {
			if posting.Amount != nil && posting.Amount.Commodity.Symbol != "" {
				counts[posting.Amount.Commodity.Symbol]++
			}
			if posting.Cost != nil && posting.Cost.Amount.Commodity.Symbol != "" {
				counts[posting.Cost.Amount.Commodity.Symbol]++
			}
		}
	}
	return counts
}

func CollectTagCounts(journal *ast.Journal) map[string]int {
	counts := make(map[string]int)

	countTags := func(tagList []ast.Tag) {
		for _, tag := range tagList {
			if tag.Name != "" {
				counts[tag.Name]++
			}
		}
	}

	for _, tx := range journal.Transactions {
		countTags(tx.Tags)
		for _, comment := range tx.Comments {
			countTags(comment.Tags)
		}
		for _, posting := range tx.Postings {
			countTags(posting.Tags)
		}
	}
	return counts
}

func CollectTagValueCounts(journal *ast.Journal) map[string]map[string]int {
	counts := make(map[string]map[string]int)

	countTagValues := func(tagList []ast.Tag) {
		for _, tag := range tagList {
			if tag.Name == "" || tag.Value == "" {
				continue
			}
			if counts[tag.Name] == nil {
				counts[tag.Name] = make(map[string]int)
			}
			counts[tag.Name][tag.Value]++
		}
	}

	for _, tx := range journal.Transactions {
		countTagValues(tx.Tags)
		for _, comment := range tx.Comments {
			countTagValues(comment.Tags)
		}
		for _, posting := range tx.Postings {
			countTagValues(posting.Tags)
		}
	}
	return counts
}
