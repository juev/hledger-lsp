package include

import (
	"container/list"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/parser"
)

const (
	// parseCacheBudget bounds the source text the cache retains. The bound is on
	// source rather than on the parsed result because source is what can be
	// measured without walking the AST, and the two are proportional in
	// practice: a 10k-transaction journal is about 1MB of text.
	//
	// The parsed AST is several times its source, so this budget is deliberately
	// small. NFR-1.4 caps the whole process at 200MB and a workspace over a 10k
	// journal already accounts for ~43MB of it.
	parseCacheBudget = 4 << 20
	// parseCacheMaxEntry skips caching a source larger than this, so one huge
	// file cannot evict every other entry for a single reuse.
	parseCacheMaxEntry = 2 << 20
)

type parseCacheEntry struct {
	content string
	journal *ast.Journal
	errs    []parser.ParseError
	elem    *list.Element
}

// ParseCached parses content with no include context, remembering the result
// and keyed by the content itself.
//
// Several consumers need the same document parsed with no context: the
// workspace index builds a FileIndex from it, and the server parses it to
// answer a request. Without a shared cache each of them parses the version
// afresh, so one edit costs two or three full parses of the same bytes.
//
// The result is shared, not copied, and must be treated as immutable.
func (l *Loader) ParseCached(content string) (*ast.Journal, []parser.ParseError) {
	l.parseMu.Lock()
	if entry, ok := l.parses[content]; ok {
		l.parseLRU.MoveToFront(entry.elem)
		journal, errs := entry.journal, entry.errs
		l.parseMu.Unlock()
		return journal, errs
	}
	l.parseMu.Unlock()

	// Parse outside the lock: parsing is the expensive part and two goroutines
	// racing on the same content should both make progress rather than queue.
	journal, errs := parser.Parse(content)

	if len(content) > parseCacheMaxEntry {
		return journal, errs
	}

	l.parseMu.Lock()
	defer l.parseMu.Unlock()
	if existing, ok := l.parses[content]; ok {
		// Lost the race; keep the entry that got there first so every caller
		// shares one journal.
		return existing.journal, existing.errs
	}

	entry := &parseCacheEntry{content: content, journal: journal, errs: errs}
	entry.elem = l.parseLRU.PushFront(entry)
	l.parses[content] = entry
	l.parseBytes += len(content)
	l.evictLocked()

	return journal, errs
}

// evictLocked drops least-recently-used entries until the budget fits.
func (l *Loader) evictLocked() {
	for l.parseBytes > parseCacheBudget {
		back := l.parseLRU.Back()
		if back == nil {
			return
		}
		entry, ok := back.Value.(*parseCacheEntry)
		if !ok {
			l.parseLRU.Remove(back)
			continue
		}
		l.parseLRU.Remove(back)
		delete(l.parses, entry.content)
		l.parseBytes -= len(entry.content)
	}
}

// parseCacheStats reports the retained source bytes and entry count, for tests.
func (l *Loader) parseCacheStats() (bytes int, entries int) {
	l.parseMu.Lock()
	defer l.parseMu.Unlock()
	return l.parseBytes, len(l.parses)
}
