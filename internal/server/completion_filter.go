package server

import (
	"maps"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/include"
)

func findCurrentTransactionIndex(transactions []ast.Transaction, lspLine int) int {
	astLine := lspLine + 1
	for i, tx := range transactions {
		if tx.Range.Start.Line <= astLine && astLine <= tx.Range.End.Line {
			return i
		}
	}
	return -1
}

func journalWithoutTransaction(journal *ast.Journal, txIndex int) *ast.Journal {
	if txIndex < 0 {
		return journal
	}

	filtered := make([]ast.Transaction, 0, len(journal.Transactions)-1)
	filtered = append(filtered, journal.Transactions[:txIndex]...)
	filtered = append(filtered, journal.Transactions[txIndex+1:]...)

	return &ast.Journal{
		Transactions:         filtered,
		PeriodicTransactions: journal.PeriodicTransactions,
		AutoPostingRules:     journal.AutoPostingRules,
		Directives:           journal.Directives,
		Comments:             journal.Comments,
		Includes:             journal.Includes,
	}
}

func resolvedWithoutTransaction(resolved *include.ResolvedJournal, cursorLine int, docURI protocol.DocumentURI) *include.ResolvedJournal {
	docPath := uriToPath(docURI)

	// Occurrence-aware path: filter the transaction from the matching occurrence
	// and preserve the occurrence/item structure so AnalyzeResolved counts each
	// occurrence of a repeated include.
	if len(resolved.Occurrences) > 0 {
		return resolvedWithoutTransactionOccurrences(resolved, cursorLine, docPath)
	}

	for path, journal := range resolved.Files {
		if path == docPath {
			txIdx := findCurrentTransactionIndex(journal.Transactions, cursorLine)
			if txIdx < 0 {
				return resolved
			}
			filtered := journalWithoutTransaction(journal, txIdx)
			newFiles := make(map[string]*ast.Journal, len(resolved.Files))
			maps.Copy(newFiles, resolved.Files)
			newFiles[path] = filtered
			return &include.ResolvedJournal{
				Primary:   resolved.Primary,
				Files:     newFiles,
				FileOrder: resolved.FileOrder,
				Errors:    resolved.Errors,
			}
		}
	}

	txIdx := findCurrentTransactionIndex(resolved.Primary.Transactions, cursorLine)
	if txIdx < 0 {
		return resolved
	}

	filteredPrimary := journalWithoutTransaction(resolved.Primary, txIdx)

	return &include.ResolvedJournal{
		Primary:   filteredPrimary,
		Files:     resolved.Files,
		FileOrder: resolved.FileOrder,
		Errors:    resolved.Errors,
	}
}

func resolvedWithoutTransactionOccurrences(resolved *include.ResolvedJournal, cursorLine int, docPath string) *include.ResolvedJournal {
	targetIdx := -1
	for i := range resolved.Occurrences {
		if resolved.Occurrences[i].Path == docPath {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		return resolved
	}

	occ := resolved.Occurrences[targetIdx]
	if occ.Journal == nil {
		return resolved
	}
	txIdx := findCurrentTransactionIndex(occ.Journal.Transactions, cursorLine)
	if txIdx < 0 {
		return resolved
	}

	filteredJournal := journalWithoutTransaction(occ.Journal, txIdx)

	newOccurrences := make([]include.JournalOccurrence, len(resolved.Occurrences))
	copy(newOccurrences, resolved.Occurrences)
	newOccurrences[targetIdx].Journal = filteredJournal

	// Remove the filtered transaction from Items and adjust subsequent indices.
	var newItems []include.ResolvedItem
	for _, item := range resolved.Items {
		if item.OccurrenceID == occ.ID && item.Kind == include.ResolvedItemTransaction {
			if item.Index == txIdx {
				continue
			}
			if item.Index > txIdx {
				item.Index--
			}
		}
		newItems = append(newItems, item)
	}

	// Update Primary if the filtered occurrence is the root.
	primary := resolved.Primary
	if occ.Via.ParentID == 0 {
		primary = filteredJournal
	}

	return &include.ResolvedJournal{
		Occurrences: newOccurrences,
		Items:       newItems,
		ByPath:      resolved.ByPath,
		ByCanonical: resolved.ByCanonical,
		Primary:     primary,
		Files:       resolved.Files,
		FileOrder:   resolved.FileOrder,
		Errors:      resolved.Errors,
	}
}
