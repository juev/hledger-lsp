package analyzer

import (
	"fmt"
	"sort"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/include"
)

func CollectPayeeAccounts(journal *ast.Journal) map[string][]string {
	seen := make(map[string]map[string]bool)

	for _, tx := range journal.Transactions {
		payee := tx.Payee
		if payee == "" {
			payee = tx.Description
		}
		if payee == "" {
			continue
		}

		if seen[payee] == nil {
			seen[payee] = make(map[string]bool)
		}

		for _, posting := range tx.Postings {
			account := posting.Account.GetResolvedName()
			if account != "" {
				seen[payee][account] = true
			}
		}
	}

	result := make(map[string][]string)
	for payee, accounts := range seen {
		accountList := make([]string, 0, len(accounts))
		for account := range accounts {
			accountList = append(accountList, account)
		}
		sort.Strings(accountList)
		result[payee] = accountList
	}

	return result
}

func CollectPayeeAccountPairUsage(journal *ast.Journal) map[string]int {
	counts := make(map[string]int)

	for _, tx := range journal.Transactions {
		payee := tx.Payee
		if payee == "" {
			payee = tx.Description
		}
		if payee == "" {
			continue
		}

		for _, posting := range tx.Postings {
			account := posting.Account.GetResolvedName()
			if account != "" {
				key := payee + "::" + account
				counts[key]++
			}
		}
	}

	return counts
}

func collectPayeeAccountsFromResolved(resolved *include.ResolvedJournal) map[string][]string {
	seen := make(map[string]map[string]bool)

	mergeAccounts := func(journal *ast.Journal) {
		if journal == nil {
			return
		}
		payeeAccounts := CollectPayeeAccounts(journal)
		for payee, accounts := range payeeAccounts {
			if seen[payee] == nil {
				seen[payee] = make(map[string]bool)
			}
			for _, account := range accounts {
				seen[payee][account] = true
			}
		}
	}

	for _, journal := range resolved.SourceJournals() {
		mergeAccounts(journal)
	}

	result := make(map[string][]string)
	for payee, accounts := range seen {
		accountList := make([]string, 0, len(accounts))
		for account := range accounts {
			accountList = append(accountList, account)
		}
		sort.Strings(accountList)
		result[payee] = accountList
	}

	return result
}

func collectPayeeAccountPairUsageFromResolved(resolved *include.ResolvedJournal) map[string]int {
	counts := make(map[string]int)

	mergeCounts := func(journal *ast.Journal) {
		if journal == nil {
			return
		}
		for k, v := range CollectPayeeAccountPairUsage(journal) {
			counts[k] += v
		}
	}

	for _, journal := range resolved.SourceJournals() {
		mergeCounts(journal)
	}

	return counts
}

func formatDateKey(d ast.Date) string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
}

func CollectPayeeAccountLastUsed(journal *ast.Journal) map[string]string {
	lastUsed := make(map[string]string)

	for _, tx := range journal.Transactions {
		payee := tx.Payee
		if payee == "" {
			payee = tx.Description
		}
		if payee == "" {
			continue
		}

		dateKey := formatDateKey(tx.Date)

		for _, posting := range tx.Postings {
			account := posting.Account.GetResolvedName()
			if account == "" {
				continue
			}
			key := payee + "::" + account
			if existing, ok := lastUsed[key]; !ok || dateKey > existing {
				lastUsed[key] = dateKey
			}
		}
	}

	return lastUsed
}

func collectPayeeAccountLastUsedFromResolved(resolved *include.ResolvedJournal) map[string]string {
	lastUsed := make(map[string]string)

	merge := func(journal *ast.Journal) {
		if journal == nil {
			return
		}
		for k, v := range CollectPayeeAccountLastUsed(journal) {
			if existing, ok := lastUsed[k]; !ok || v > existing {
				lastUsed[k] = v
			}
		}
	}

	for _, journal := range resolved.SourceJournals() {
		merge(journal)
	}

	return lastUsed
}
