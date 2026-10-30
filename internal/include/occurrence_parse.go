package include

import "github.com/juev/hledger-lsp/internal/parser"

// occurrenceParse is the parse of one file under one include context.
type occurrenceParse struct {
	content string
	context parser.Context
	result  parser.ParseResult
	errs    []parser.ParseError
}

// reusableParse returns the remembered parse of canonical when it was made from
// the same text under the same context. The result is shared between loads and
// must be treated as immutable.
//
// A load re-resolves the whole tree, so without this one edit re-parses every
// file in it. A journal's text is usually unchanged by that edit, and text plus
// context fully determine a parse.
func (l *Loader) reusableParse(canonical, content string, context parser.Context) (parser.ParseResult, []parser.ParseError, bool) {
	l.occurrenceMu.Lock()
	defer l.occurrenceMu.Unlock()
	entry, ok := l.occurrenceParses[canonical]
	if !ok || entry.content != content || !entry.context.Equal(context) {
		return parser.ParseResult{}, nil, false
	}
	return entry.result, entry.errs, true
}

// rememberParse keeps one parse per file. That bounds the retained memory by the
// journals the resolved trees hold anyway: an entry shares its AST with them
// rather than adding a copy.
//
// A file with includes is never kept. Its parse runs the included files through
// the resolver, so its text alone does not determine the load, and skipping the
// parse would skip them too.
func (l *Loader) rememberParse(canonical, content string, context parser.Context, result parser.ParseResult, errs []parser.ParseError) {
	l.occurrenceMu.Lock()
	defer l.occurrenceMu.Unlock()
	if len(result.IncludeSites) > 0 {
		delete(l.occurrenceParses, canonical)
		return
	}
	l.occurrenceParses[canonical] = &occurrenceParse{content: content, context: context, result: result, errs: errs}
}
