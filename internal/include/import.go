package include

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/csvimport"
	"github.com/juev/hledger-lsp/internal/parser"
)

// Importer converts an included CSV, SSV, TSV or rules file into transactions.
// *csvimport.Importer is the production implementation.
type Importer interface {
	Import(ctx context.Context, request csvimport.Request) csvimport.Result
}

// SetImporter turns on hledger 2 inclusion of separated data, converted by
// importer; nil restores the hledger 1 behaviour, where such an include is
// refused as not a journal. buffer returns unsaved editor content for a path,
// so an import can report sources whose buffer differs from the saved file.
func (l *Loader) SetImporter(importer Importer, buffer func(path string) (string, bool)) {
	l.importMu.Lock()
	defer l.importMu.Unlock()
	l.importer = importer
	l.buffer = buffer
}

func (l *Loader) currentImporter() Importer {
	l.importMu.RLock()
	defer l.importMu.RUnlock()
	return l.importer
}

// IsImportSource reports whether path is a file some load read, or would read,
// as separated data or CSV rules rather than as a journal: an include target
// with a data extension or reader prefix, or a file an import depends on. Such
// a file must never be formatted or edited as a journal. A symlink or hard link
// to a recorded file counts as the file itself.
//
// Records are kept for the loader's lifetime. A file that stopped being
// imported stays protected, which only withholds journal features from it.
func (l *Loader) IsImportSource(path string) bool {
	if path == "" {
		return false
	}
	canonical := l.canonicalPath(path)
	l.importMu.RLock()
	_, known := l.protected[canonical]
	l.importMu.RUnlock()
	if known {
		return true
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	l.importMu.RLock()
	defer l.importMu.RUnlock()
	for _, recorded := range l.protected {
		if recorded != nil && os.SameFile(info, recorded) {
			return true
		}
	}
	return false
}

func (l *Loader) protect(path string) {
	canonical := l.canonicalPath(path)
	info, err := os.Stat(canonical)
	if err != nil {
		info = nil
	}
	l.importMu.Lock()
	defer l.importMu.Unlock()
	l.protected[canonical] = info
}

// protectPrefixed records the target of an include with an explicit reader
// prefix, whatever the target version: under hledger 1 the include is refused,
// yet the file is still data.
func (l *Loader) protectPrefixed(basePath string, reader csvimport.Reader, pattern string) {
	if reader == "" || IsGlobPattern(pattern) {
		return
	}
	if path, err := ResolvePathSafe(basePath, pattern); err == nil {
		l.protect(absoluteClean(path))
	}
}

func (l *Loader) importBuffer() func(path string) (string, bool) {
	l.importMu.RLock()
	defer l.importMu.RUnlock()
	return l.buffer
}

// loadImport adds the transactions hledger converts from path as an occurrence
// of the data file they come from. A failed conversion adds nothing and is
// reported at the include, so missing data is never mistaken for a journal
// that balances.
func (s *loadState) loadImport(site parser.IncludeSite, provenance IncludeProvenance, path string, reader csvimport.Reader, inherited parser.Context, depth int, children map[int][]OccurrenceID, includeIndex int) {
	basePath := provenance.SourcePath
	if depth+1 > s.limits.MaxIncludeDepth {
		s.errors = append(s.errors, LoadError{Kind: ErrorDepthExceeded, Path: path, SourcePath: basePath, Message: fmt.Sprintf("include depth limit exceeded (%d)", s.limits.MaxIncludeDepth), Range: site.Include.Range, Provenance: provenance})
		return
	}
	ctx := s.options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	buffer := s.loader.importBuffer()
	mainJournal := ""
	if len(s.result.Occurrences) > 0 {
		mainJournal = s.result.Occurrences[0].Path
	}
	result := s.importer.Import(ctx, csvimport.Request{
		Reader:      reader,
		Path:        path,
		MainJournal: mainJournal,
		MaxFileSize: s.limits.MaxFileSizeBytes,
		Buffer: func(source string) (string, bool) {
			if content, ok := s.options.overlay(s.loader, source); ok {
				return content, true
			}
			if buffer != nil {
				return buffer(source)
			}
			return "", false
		},
	})
	for _, source := range result.Sources {
		s.loader.protect(source)
		if !contains(s.result.ImportSources, source) {
			s.result.ImportSources = append(s.result.ImportSources, source)
		}
	}
	if result.Pending {
		s.result.PendingImports++
		return
	}
	for _, warning := range result.Warnings {
		s.errors = append(s.errors, LoadError{Kind: ErrorImportUnsaved, Path: path, SourcePath: basePath, Message: warning, Range: site.Include.Range, Provenance: provenance})
	}
	if result.Err != "" {
		s.errors = append(s.errors, LoadError{Kind: ErrorImport, Path: path, SourcePath: basePath, Message: fmt.Sprintf("cannot import %s: %s", filepath.Base(path), result.Err), Range: site.Include.Range, Provenance: provenance})
		return
	}
	if len(result.Transactions) == 0 {
		return
	}

	journal := &ast.Journal{Transactions: make([]ast.Transaction, len(result.Transactions))}
	for i, transaction := range result.Transactions {
		// The result may be shared with a cache: copy what is rewritten.
		transaction.Postings = append([]ast.Posting(nil), transaction.Postings...)
		for j := range transaction.Postings {
			account := &transaction.Postings[j].Account
			account.ResolvedName = parser.ApplyAliases(inherited, account.Name)
		}
		journal.Transactions[i] = transaction
	}

	dataPath := absoluteClean(result.DataPath)
	id := OccurrenceID(len(s.result.Occurrences) + 1)
	children[includeIndex] = append(children[includeIndex], id)
	occurrence := Occurrence{
		ID: id, Path: dataPath, CanonicalPath: s.loader.canonicalPath(dataPath), Journal: journal,
		InitialContext: inherited, Via: provenance,
		Import: &ImportedSource{Target: path, Reader: string(reader), Sources: result.Sources},
	}
	for index := range journal.Transactions {
		occurrence.Items = append(occurrence.Items, Item{OccurrenceID: id, Kind: ItemTransaction, Index: index})
	}
	s.result.Occurrences = append(s.result.Occurrences, occurrence)
	s.result.ByPath[dataPath] = append(s.result.ByPath[dataPath], id)
	s.result.ByCanonical[occurrence.CanonicalPath] = append(s.result.ByCanonical[occurrence.CanonicalPath], id)
	if _, exists := s.result.Files[dataPath]; !exists {
		s.result.FileOrder = append(s.result.FileOrder, dataPath)
		s.result.Files[dataPath] = journal
	}
	s.children[id] = map[int][]OccurrenceID{}
}
