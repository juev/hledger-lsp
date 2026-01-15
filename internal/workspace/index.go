package workspace

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/juev/hledger-lsp/internal/analyzer"
	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/parser"
)

type TransactionEntry struct {
	Key         string
	FilePath    string
	Range       ast.Range
	Date        ast.Date
	Payee       string
	Description string
}

type FileIndex struct {
	Accounts     []string
	Payees       []string
	Commodities  []string
	Tags         []string
	TagValues    map[string][]string
	Transactions []TransactionEntry
	Includes     []string
}

type IndexSnapshot struct {
	Accounts     *analyzer.AccountIndex
	Payees       []string
	Commodities  []string
	Tags         []string
	TagValues    map[string][]string
	Transactions map[string][]TransactionEntry
}

type WorkspaceIndex struct {
	fileIndexes       map[string]*FileIndex
	accountCounts     map[string]int
	payeeCounts       map[string]int
	commodityCounts   map[string]int
	tagCounts         map[string]int
	tagValueCounts    map[string]map[string]int
	transactionsByKey map[string][]TransactionEntry
	accounts          *analyzer.AccountIndex
	payees            []string
	commodities       []string
	tags              []string
	tagValues         map[string][]string
}

func NewWorkspaceIndex() *WorkspaceIndex {
	return &WorkspaceIndex{
		fileIndexes:       make(map[string]*FileIndex),
		accountCounts:     make(map[string]int),
		payeeCounts:       make(map[string]int),
		commodityCounts:   make(map[string]int),
		tagCounts:         make(map[string]int),
		tagValueCounts:    make(map[string]map[string]int),
		transactionsByKey: make(map[string][]TransactionEntry),
		accounts:          analyzer.NewAccountIndex(),
		tagValues:         make(map[string][]string),
	}
}

func (idx *WorkspaceIndex) Snapshot() IndexSnapshot {
	return IndexSnapshot{
		Accounts:     cloneAccountIndex(idx.accounts),
		Payees:       append([]string(nil), idx.payees...),
		Commodities:  append([]string(nil), idx.commodities...),
		Tags:         append([]string(nil), idx.tags...),
		TagValues:    cloneTagValues(idx.tagValues),
		Transactions: cloneTransactions(idx.transactionsByKey),
	}
}

func (idx *WorkspaceIndex) FileIndex(path string) *FileIndex {
	if fi, ok := idx.fileIndexes[path]; ok {
		return fi
	}
	return nil
}

func (idx *WorkspaceIndex) SetFileIndex(path string, fi *FileIndex) {
	if path == "" || fi == nil {
		return
	}
	if existing := idx.fileIndexes[path]; existing != nil {
		idx.removeFileIndex(path, existing)
	}
	idx.addFileIndex(path, fi)
}

func (idx *WorkspaceIndex) RemoveFile(path string) {
	if existing := idx.fileIndexes[path]; existing != nil {
		idx.removeFileIndex(path, existing)
	}
}

func (idx *WorkspaceIndex) addFileIndex(path string, fi *FileIndex) {
	idx.fileIndexes[path] = fi
	for _, name := range fi.Accounts {
		idx.accountCounts[name]++
	}
	for _, name := range fi.Payees {
		idx.payeeCounts[name]++
	}
	for _, name := range fi.Commodities {
		idx.commodityCounts[name]++
	}
	for _, name := range fi.Tags {
		idx.tagCounts[name]++
	}
	for tagName, values := range fi.TagValues {
		if idx.tagValueCounts[tagName] == nil {
			idx.tagValueCounts[tagName] = make(map[string]int)
		}
		for _, value := range values {
			idx.tagValueCounts[tagName][value]++
		}
	}
	for _, entry := range fi.Transactions {
		idx.transactionsByKey[entry.Key] = append(idx.transactionsByKey[entry.Key], entry)
	}
	idx.refreshDerived()
}

func (idx *WorkspaceIndex) removeFileIndex(path string, fi *FileIndex) {
	delete(idx.fileIndexes, path)
	for _, name := range fi.Accounts {
		idx.decrement(idx.accountCounts, name)
	}
	for _, name := range fi.Payees {
		idx.decrement(idx.payeeCounts, name)
	}
	for _, name := range fi.Commodities {
		idx.decrement(idx.commodityCounts, name)
	}
	for _, name := range fi.Tags {
		idx.decrement(idx.tagCounts, name)
	}
	for tagName, values := range fi.TagValues {
		for _, value := range values {
			idx.decrementTagValue(tagName, value)
		}
	}
	for _, entry := range fi.Transactions {
		entries := idx.transactionsByKey[entry.Key]
		idx.transactionsByKey[entry.Key] = filterTransactions(entries, path)
		if len(idx.transactionsByKey[entry.Key]) == 0 {
			delete(idx.transactionsByKey, entry.Key)
		}
	}
	idx.refreshDerived()
}

func (idx *WorkspaceIndex) decrement(counts map[string]int, key string) {
	if counts[key] <= 1 {
		delete(counts, key)
		return
	}
	counts[key]--
}

func (idx *WorkspaceIndex) decrementTagValue(tagName, value string) {
	if idx.tagValueCounts[tagName] == nil {
		return
	}
	if idx.tagValueCounts[tagName][value] <= 1 {
		delete(idx.tagValueCounts[tagName], value)
		if len(idx.tagValueCounts[tagName]) == 0 {
			delete(idx.tagValueCounts, tagName)
		}
		return
	}
	idx.tagValueCounts[tagName][value]--
}

func (idx *WorkspaceIndex) refreshDerived() {
	idx.accounts = buildAccountIndex(idx.accountCounts)
	idx.payees = sortedKeys(idx.payeeCounts)
	idx.commodities = sortedKeys(idx.commodityCounts)
	idx.tags = sortedKeys(idx.tagCounts)
	idx.tagValues = buildTagValues(idx.tagValueCounts)
}

func buildTagValues(counts map[string]map[string]int) map[string][]string {
	result := make(map[string][]string, len(counts))
	for tagName, valueCounts := range counts {
		result[tagName] = sortedKeys(valueCounts)
	}
	return result
}

func buildAccountIndex(counts map[string]int) *analyzer.AccountIndex {
	accountIdx := analyzer.NewAccountIndex()
	names := sortedKeys(counts)
	for _, name := range names {
		accountIdx.All = append(accountIdx.All, name)
		parts := strings.Split(name, ":")
		for i := 1; i < len(parts); i++ {
			prefix := strings.Join(parts[:i], ":") + ":"
			accountIdx.ByPrefix[prefix] = append(accountIdx.ByPrefix[prefix], name)
		}
	}
	return accountIdx
}

func sortedKeys(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func filterTransactions(entries []TransactionEntry, filePath string) []TransactionEntry {
	if len(entries) == 0 {
		return nil
	}
	filtered := entries[:0]
	for _, entry := range entries {
		if entry.FilePath != filePath {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func cloneAccountIndex(idx *analyzer.AccountIndex) *analyzer.AccountIndex {
	if idx == nil {
		return nil
	}
	clone := analyzer.NewAccountIndex()
	clone.All = append([]string(nil), idx.All...)
	for key, values := range idx.ByPrefix {
		clone.ByPrefix[key] = append([]string(nil), values...)
	}
	return clone
}

func cloneTransactions(source map[string][]TransactionEntry) map[string][]TransactionEntry {
	if source == nil {
		return nil
	}
	clone := make(map[string][]TransactionEntry, len(source))
	for key, entries := range source {
		copied := make([]TransactionEntry, len(entries))
		copy(copied, entries)
		clone[key] = copied
	}
	return clone
}

func cloneTagValues(source map[string][]string) map[string][]string {
	if source == nil {
		return nil
	}
	clone := make(map[string][]string, len(source))
	for key, values := range source {
		clone[key] = append([]string(nil), values...)
	}
	return clone
}

func BuildFileIndexFromContent(path, content string) (*FileIndex, *ast.Journal, []string) {
	journal, parseErrs := parser.Parse(content)
	var errors []string
	for _, err := range parseErrs {
		errors = append(errors, err.Message)
	}
	if journal == nil {
		return &FileIndex{}, journal, errors
	}
	return BuildFileIndexFromJournal(path, journal), journal, errors
}

func BuildFileIndexFromJournal(path string, journal *ast.Journal) *FileIndex {
	if journal == nil {
		return &FileIndex{}
	}
	accounts := analyzer.CollectAccounts(journal)
	payees := analyzer.CollectPayees(journal)
	commodities := analyzer.CollectCommodities(journal)
	tags := analyzer.CollectTags(journal)
	tagValues := analyzer.CollectTagValues(journal)
	transactions := collectTransactions(path, journal)
	includes := resolveIncludePaths(path, journal.Includes)
	return &FileIndex{
		Accounts:     accounts.All,
		Payees:       payees,
		Commodities:  commodities,
		Tags:         tags,
		TagValues:    tagValues,
		Transactions: transactions,
		Includes:     includes,
	}
}

func resolveIncludePaths(basePath string, includes []ast.Include) []string {
	if len(includes) == 0 {
		return nil
	}
	dir := filepath.Dir(basePath)
	seen := make(map[string]bool)
	var resolved []string
	for _, inc := range includes {
		if include.IsGlobPattern(inc.Path) {
			pattern := include.ConvertHledgerGlob(inc.Path)
			if !filepath.IsAbs(pattern) {
				pattern = filepath.Join(dir, pattern)
			}
			matches, err := doublestar.FilepathGlob(pattern)
			if err != nil {
				continue
			}
			sort.Strings(matches)
			for _, match := range matches {
				absMatch, _ := filepath.Abs(match)
				if absMatch != "" && absMatch != basePath && !seen[absMatch] {
					seen[absMatch] = true
					resolved = append(resolved, absMatch)
				}
			}
			continue
		}
		resolvedPath, err := include.ResolvePathSafe(basePath, inc.Path)
		if err != nil || resolvedPath == "" {
			continue
		}
		if resolvedPath != basePath && !seen[resolvedPath] {
			seen[resolvedPath] = true
			resolved = append(resolved, resolvedPath)
		}
	}
	sort.Strings(resolved)
	return resolved
}

// Transaction key format:
// YYYY-MM-DD|payeeOrDescription|account|amount|commodity;...
// Posting order is normalized by sorting posting strings, so whitespace/order differences do not affect the key.
func collectTransactions(path string, journal *ast.Journal) []TransactionEntry {
	if journal == nil {
		return nil
	}
	entries := make([]TransactionEntry, 0, len(journal.Transactions))
	for _, tx := range journal.Transactions {
		key := buildTransactionKey(tx)
		entries = append(entries, TransactionEntry{
			Key:         key,
			FilePath:    path,
			Range:       tx.Range,
			Date:        tx.Date,
			Payee:       tx.Payee,
			Description: tx.Description,
		})
	}
	return entries
}

func buildTransactionKey(tx ast.Transaction) string {
	date := fmt.Sprintf("%04d-%02d-%02d", tx.Date.Year, tx.Date.Month, tx.Date.Day)
	payee := tx.Payee
	if payee == "" {
		payee = tx.Description
	}
	postings := make([]string, 0, len(tx.Postings))
	for _, posting := range tx.Postings {
		postingKey := buildPostingKey(posting)
		if postingKey != "" {
			postings = append(postings, postingKey)
		}
	}
	sort.Strings(postings)
	return fmt.Sprintf("%s|%s|%s", date, payee, strings.Join(postings, ";"))
}

func buildPostingKey(posting ast.Posting) string {
	if posting.Account.Name == "" {
		return ""
	}
	amount := ""
	if posting.Amount != nil {
		raw := posting.Amount.RawQuantity
		if raw == "" {
			raw = posting.Amount.Quantity.String()
		}
		if posting.Amount.Commodity.Symbol != "" {
			amount = fmt.Sprintf("%s %s", raw, posting.Amount.Commodity.Symbol)
		} else {
			amount = raw
		}
	}
	if amount == "" {
		return posting.Account.Name
	}
	return fmt.Sprintf("%s|%s", posting.Account.Name, amount)
}
