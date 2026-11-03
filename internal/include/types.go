package include

import (
	"context"
	"errors"
	"io/fs"
	"maps"
	"os"
	"slices"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/formatter"
	"github.com/juev/hledger-lsp/internal/parser"
)

type ErrorKind int

const (
	ErrorFileNotFound ErrorKind = iota
	ErrorCycleDetected
	ErrorParseError
	ErrorReadError
	ErrorFileTooLarge
	ErrorPathTraversal
	ErrorNotJournal
	ErrorDepthExceeded
	// ErrorImport is an included CSV, SSV, TSV or rules file that produced no
	// transactions: the CLI is missing or refused, or hledger failed.
	ErrorImport
	// ErrorImportUnsaved is an import source whose editor buffer differs from
	// the saved file the import read.
	ErrorImportUnsaved
)

type LoadError struct {
	Kind       ErrorKind
	Path       string
	SourcePath string
	Message    string
	Range      ast.Range
	Provenance IncludeProvenance
	// Code carries the parser's diagnostic code for parse errors, so clients see
	// the same code as for a parse error in the document itself.
	Code string
}

func (e LoadError) Error() string {
	return e.Message
}

// ErrorCode returns a stable identifier for the failure kind. The server
// publishes it as the diagnostic code so clients can filter include failures
// without matching message text.
func (e LoadError) ErrorCode() string {
	if e.Code != "" {
		return e.Code
	}
	switch e.Kind {
	case ErrorFileNotFound:
		return "INCLUDE_NOT_FOUND"
	case ErrorCycleDetected:
		return "INCLUDE_CYCLE"
	case ErrorParseError:
		return "INCLUDE_PARSE_ERROR"
	case ErrorReadError:
		return "INCLUDE_READ_ERROR"
	case ErrorFileTooLarge:
		return "INCLUDE_FILE_TOO_LARGE"
	case ErrorPathTraversal:
		return "INCLUDE_PATH_TRAVERSAL"
	case ErrorNotJournal:
		return "INCLUDE_NOT_JOURNAL"
	case ErrorDepthExceeded:
		return "INCLUDE_DEPTH_EXCEEDED"
	case ErrorImport:
		return "INCLUDE_IMPORT_ERROR"
	case ErrorImportUnsaved:
		return "INCLUDE_IMPORT_UNSAVED"
	default:
		return "INCLUDE_ERROR"
	}
}

type FileSource struct {
	Path    string
	Content string
}

type OverlayEntry struct {
	SourcePath string
	Content    string
	Revision   uint64
}

// LoadOptions provides a per-load immutable editor-content snapshot.
type LoadOptions struct {
	Overlays map[string]OverlayEntry
	// Context bounds the hledger runs of included CSV files; nil means none
	// beyond the CLI timeout.
	Context context.Context
}

// overlay looks up editor content for path. The canonical form must come from
// the same loader that built the overlay map, or the keys would not match.
func (o LoadOptions) overlay(l *Loader, path string) (string, bool) {
	entry, ok := o.Overlays[l.canonicalPath(path)]
	return entry.Content, ok
}

// OccurrenceID uniquely identifies a journal occurrence within one resolved journal.
// The root occurrence has ID 1.
type OccurrenceID uint64

// IncludeProvenance identifies the include site that created an occurrence.
// ParentID is zero only for the root occurrence.
type IncludeProvenance struct {
	ParentID     OccurrenceID
	SourcePath   string
	IncludeRange ast.Range
	// Range is retained while loader call sites migrate to IncludeRange.
	Range      ast.Range
	RawPattern string
	MatchIndex int
	Depth      int
}

// JournalOccurrence is one inclusion of a journal source. The same source can
// have several occurrences with different provenance and parser context.
type JournalOccurrence struct {
	ID             OccurrenceID
	Path           string
	CanonicalPath  string
	Journal        *ast.Journal
	InitialContext parser.Context
	Checkpoints    []parser.ContextCheckpoint
	Exports        parser.ContextExports
	Via            IncludeProvenance
	Items          []ResolvedItem
	// Import is set for transactions hledger converted from separated data. Its
	// Path is the data file, which is not journal text: nothing may parse it as
	// one or edit it.
	Import *ImportedSource
}

// ImportedSource describes the include that produced an imported occurrence.
type ImportedSource struct {
	// Target is the included file: the data file, or the rules file naming it.
	Target string
	Reader string
	// Sources are the files the conversion depends on.
	Sources []string
}

// Occurrence is retained as a concise alias for JournalOccurrence.
type Occurrence = JournalOccurrence

// ResolvedItemKind identifies a source node in an occurrence journal.
type ResolvedItemKind uint8

const (
	ResolvedItemTransaction ResolvedItemKind = iota
	ResolvedItemPeriodicTransaction
	ResolvedItemAutoPostingRule
	ResolvedItemDirective
	ResolvedItemComment
	ResolvedItemInclude
)

const (
	ItemTransaction         = ResolvedItemTransaction
	ItemPeriodicTransaction = ResolvedItemPeriodicTransaction
	ItemAutoPostingRule     = ResolvedItemAutoPostingRule
	ItemDirective           = ResolvedItemDirective
	ItemComment             = ResolvedItemComment
	ItemInclude             = ResolvedItemInclude
)

// ResolvedItem references one item in an occurrence journal. ResolvedJournal.Items
// stores these references in textual order with children inlined at include sites.
type ResolvedItem struct {
	OccurrenceID OccurrenceID
	Kind         ResolvedItemKind
	Index        int
}

// Item is retained as a concise alias for ResolvedItem.
type Item = ResolvedItem

type ResolvedJournal struct {
	Occurrences []JournalOccurrence
	Items       []ResolvedItem
	ByPath      map[string][]OccurrenceID
	ByCanonical map[string][]OccurrenceID

	// Legacy fields are a compile-only first-occurrence projection. New code must
	// use Occurrences and Items for resolved journal semantics.
	Primary   *ast.Journal
	Files     map[string]*ast.Journal
	FileOrder []string
	Errors    []LoadError

	// ImportSources lists every file an import of this load depends on, failed
	// imports included, for watching and for protection from journal edits.
	ImportSources []string
	// PendingImports counts the imports whose conversion is still running in
	// the background (csvimport.Config.Background). Their transactions are
	// missing, so the journal must not be judged until they are done.
	PendingImports int
}

func NewResolvedJournal(primary *ast.Journal) *ResolvedJournal {
	return &ResolvedJournal{
		Primary:     primary,
		Files:       make(map[string]*ast.Journal),
		ByPath:      make(map[string][]OccurrenceID),
		ByCanonical: make(map[string][]OccurrenceID),
	}
}

// InvalidateOccurrences clears the occurrence-based semantic source so the All*
// helpers fall back to the legacy Primary/Files/FileOrder projection. Consumers
// that still mutate the legacy projection directly (workspace incremental
// updates) call this to keep All* consistent until they migrate to occurrences.
func (r *ResolvedJournal) InvalidateOccurrences() {
	r.Items = nil
	r.Occurrences = nil
	r.ByPath = nil
	r.ByCanonical = nil
}

// SourceJournals returns each journal to aggregate, in semantic order. When
// occurrences are present (fresh loader output) every occurrence's journal is
// returned once, so repeated includes are counted per occurrence. Otherwise it
// falls back to the legacy Primary/Files/FileOrder projection for consumers that
// still mutate it directly.
func (r *ResolvedJournal) SourceJournals() []*ast.Journal {
	if len(r.Occurrences) > 0 {
		journals := make([]*ast.Journal, 0, len(r.Occurrences))
		for index := range r.Occurrences {
			if r.Occurrences[index].Journal != nil {
				journals = append(journals, r.Occurrences[index].Journal)
			}
		}
		return journals
	}

	var journals []*ast.Journal
	if r.Primary != nil {
		journals = append(journals, r.Primary)
	}
	for _, path := range r.FileOrder {
		if journal, ok := r.Files[path]; ok {
			journals = append(journals, journal)
		}
	}
	return journals
}

// Occurrence returns the occurrence with id, if present.
func (r *ResolvedJournal) Occurrence(id OccurrenceID) *JournalOccurrence {
	// The loader numbers occurrences from 1 in the order it appends them.
	if index := int(id) - 1; index >= 0 && index < len(r.Occurrences) && r.Occurrences[index].ID == id {
		return &r.Occurrences[index]
	}
	for index := range r.Occurrences {
		if r.Occurrences[index].ID == id {
			return &r.Occurrences[index]
		}
	}
	return nil
}

// ParseOccurrence parses content as the text of occurrence id: with the context
// the occurrence inherited, and with what its includes export at each include.
// Unlike the occurrence's own journal it reflects content, so a buffer with
// unsaved edits reads its amounts the way the tree would. An include added since
// the tree was resolved has no occurrence yet and contributes nothing.
//
// The bool reports whether the occurrence exists and is journal text; an
// imported occurrence is not.
func (r *ResolvedJournal) ParseOccurrence(id OccurrenceID, content string) (*ast.Journal, []parser.ParseError, bool) {
	occurrence := r.Occurrence(id)
	if occurrence == nil || occurrence.Import != nil {
		return nil, nil, false
	}
	result, errs := parser.ParseWithContext(content, occurrence.InitialContext, func(site parser.IncludeSite) parser.ContextExports {
		var exports []parser.ContextExports
		for index := range r.Occurrences {
			child := &r.Occurrences[index]
			if child.Via.ParentID == id && child.Via.RawPattern == site.Include.Path {
				exports = append(exports, child.Exports)
			}
		}
		return parser.MergeContextExports(exports...)
	})
	return result.Journal, errs, true
}

// FormattingContext is what the formatter needs to rewrite a document: one
// reading per distinct parse context the tree includes it under, for
// formatter.FormatDocumentInReadings, which only rewrites a number when every
// reading keeps its value.
type FormattingContext struct {
	Readings []formatter.Reading
	// ParseErrors joins the errors of every reading, each reported once.
	ParseErrors []parser.ParseError
}

// ErrNotIncluded reports that a file is not part of the include tree.
var ErrNotIncluded = errors.New("not part of the include tree")

// ErrImportedData reports that the tree reads a file as separated data or CSV
// rules, which is not journal text and must not be formatted as one.
var ErrImportedData = errors.New("read as imported CSV data, not as a journal")

// FormattingContext reads content as each occurrence of the file at path, with
// the context the occurrence inherited (see ParseOccurrence). Occurrences with
// equal contexts read content alike and share a reading. It returns
// ErrNotIncluded when the tree does not include the file.
func (r *ResolvedJournal) FormattingContext(path, content string) (FormattingContext, error) {
	occurrences, err := r.OccurrencesOfFile(path)
	if err != nil {
		return FormattingContext{}, err
	}
	if len(occurrences) == 0 {
		return FormattingContext{}, ErrNotIncluded
	}

	for _, occurrence := range occurrences {
		if occurrence.Import != nil {
			return FormattingContext{}, ErrImportedData
		}
	}

	var ctx FormattingContext
	var contexts []parser.Context
	for _, occurrence := range occurrences {
		if slices.ContainsFunc(contexts, occurrence.InitialContext.Equal) {
			continue
		}
		journal, parseErrs, ok := r.ParseOccurrence(occurrence.ID, content)
		if !ok {
			continue
		}
		contexts = append(contexts, occurrence.InitialContext)
		// A large offset collects all directives and merge-forward commodity
		// declarations for the whole document.
		ctx.Readings = append(ctx.Readings, formatter.Reading{Journal: journal, Formats: r.FormatsAt(occurrence.ID, 1<<30)})
		for _, parseErr := range parseErrs {
			if !slices.Contains(ctx.ParseErrors, parseErr) {
				ctx.ParseErrors = append(ctx.ParseErrors, parseErr)
			}
		}
	}
	return ctx, nil
}

// OccurrencesOfFile returns every occurrence that reads the file at path, under
// any name: a symlink or a hard link includes the same file, and only the file
// system can tell a hard link from another file. A name that no longer exists
// does not reach the file. A name that cannot be compared is an error rather
// than a guess, because a missed occurrence lets the formatter change what a
// number is worth there.
func (r *ResolvedJournal) OccurrencesOfFile(path string) ([]JournalOccurrence, error) {
	file, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		// Content with no file behind it, such as standard input, has no other name.
		return r.OccurrencesForPath(path), nil
	}
	if err != nil {
		return nil, err
	}

	same := make(map[string]bool)
	var occurrences []JournalOccurrence
	for _, occurrence := range r.Occurrences {
		isSame, known := same[occurrence.Path]
		if !known {
			info, err := os.Stat(occurrence.Path)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return nil, err
			}
			isSame = err == nil && os.SameFile(file, info)
			same[occurrence.Path] = isSame
		}
		if isSame {
			occurrences = append(occurrences, occurrence)
		}
	}
	return occurrences, nil
}

// OccurrencesForPath returns all occurrences for an exact source path.
func (r *ResolvedJournal) OccurrencesForPath(path string) []JournalOccurrence {
	return r.occurrencesForIDs(r.ByPath[path])
}

// OccurrencesForCanonical returns all occurrences for a canonical filesystem
// path. The input is canonicalized so a real path finds occurrences included
// through a symlink (and vice versa).
func (r *ResolvedJournal) OccurrencesForCanonical(path string) []JournalOccurrence {
	return r.occurrencesForIDs(r.ByCanonical[resolveCanonicalPath(absoluteClean(path))])
}

func (r *ResolvedJournal) occurrencesForIDs(ids []OccurrenceID) []JournalOccurrence {
	if len(ids) == 0 {
		return nil
	}

	result := make([]JournalOccurrence, 0, len(ids))
	for _, id := range ids {
		if occurrence := r.Occurrence(id); occurrence != nil {
			result = append(result, *occurrence)
		}
	}
	return result
}

// SourcedTransaction pairs a transaction with the source file that contains it.
// Path is empty for the legacy Primary/Files projection, where the caller knows
// the root document; occurrence-based resolution always fills it in.
type SourcedTransaction struct {
	Path         string
	OccurrenceID OccurrenceID
	// Index is the transaction's position in its source journal.
	Index       int
	Transaction ast.Transaction
}

// TransactionsWithSource returns every transaction of the resolved journal in
// the order hledger processes it (textual order, with included files inlined at
// their include sites), together with the file each one comes from.
func (r *ResolvedJournal) TransactionsWithSource() []SourcedTransaction {
	if r.Items != nil {
		result := make([]SourcedTransaction, 0, len(r.Items))
		for _, item := range r.Items {
			if item.Kind != ResolvedItemTransaction {
				continue
			}
			occurrence := r.Occurrence(item.OccurrenceID)
			if occurrence == nil || occurrence.Journal == nil {
				continue
			}
			if item.Index < 0 || item.Index >= len(occurrence.Journal.Transactions) {
				continue
			}
			result = append(result, SourcedTransaction{
				Path:         occurrence.Path,
				OccurrenceID: occurrence.ID,
				Index:        item.Index,
				Transaction:  occurrence.Journal.Transactions[item.Index],
			})
		}
		return result
	}

	var result []SourcedTransaction
	if r.Primary != nil {
		for i := range r.Primary.Transactions {
			result = append(result, SourcedTransaction{Index: i, Transaction: r.Primary.Transactions[i]})
		}
	}
	for _, path := range r.FileOrder {
		journal, ok := r.Files[path]
		if !ok {
			continue
		}
		for i := range journal.Transactions {
			result = append(result, SourcedTransaction{Path: path, Index: i, Transaction: journal.Transactions[i]})
		}
	}
	return result
}

func (r *ResolvedJournal) AllTransactions() []ast.Transaction {
	if r.Items != nil {
		var result []ast.Transaction
		for _, item := range r.Items {
			if item.Kind != ResolvedItemTransaction {
				continue
			}
			if occurrence := r.Occurrence(item.OccurrenceID); occurrence != nil && occurrence.Journal != nil && item.Index >= 0 && item.Index < len(occurrence.Journal.Transactions) {
				result = append(result, occurrence.Journal.Transactions[item.Index])
			}
		}
		return result
	}

	var result []ast.Transaction
	if r.Primary != nil {
		result = append(result, r.Primary.Transactions...)
	}
	for _, path := range r.FileOrder {
		if j, ok := r.Files[path]; ok {
			result = append(result, j.Transactions...)
		}
	}
	return result
}

func (r *ResolvedJournal) AllDirectives() []ast.Directive {
	if r.Items != nil {
		var result []ast.Directive
		for _, item := range r.Items {
			if item.Kind != ResolvedItemDirective {
				continue
			}
			if occurrence := r.Occurrence(item.OccurrenceID); occurrence != nil && occurrence.Journal != nil && item.Index >= 0 && item.Index < len(occurrence.Journal.Directives) {
				result = append(result, occurrence.Journal.Directives[item.Index])
			}
		}
		return result
	}

	var result []ast.Directive
	if r.Primary != nil {
		result = append(result, r.Primary.Directives...)
	}
	for _, path := range r.FileOrder {
		if j, ok := r.Files[path]; ok {
			result = append(result, j.Directives...)
		}
	}
	return result
}

// FormatDirectives returns directives suitable for formatter commodity format extraction.
// It excludes DecimalMarkDirective from included files because decimal-mark is file-scoped
// per hledger semantics, while primary file directives are passed through unfiltered.
func (r *ResolvedJournal) FormatDirectives() []ast.Directive {
	var result []ast.Directive
	if r.Primary != nil {
		result = append(result, r.Primary.Directives...)
	}
	for _, path := range r.FileOrder {
		if j, ok := r.Files[path]; ok {
			for _, d := range j.Directives {
				if _, ok := d.(ast.DecimalMarkDirective); ok {
					continue
				}
				result = append(result, d)
			}
		}
	}
	return result
}

func (r *ResolvedJournal) AllIncludes() []ast.Include {
	if r.Items != nil {
		var result []ast.Include
		for _, item := range r.Items {
			if item.Kind != ResolvedItemInclude {
				continue
			}
			if occurrence := r.Occurrence(item.OccurrenceID); occurrence != nil && occurrence.Journal != nil && item.Index >= 0 && item.Index < len(occurrence.Journal.Includes) {
				result = append(result, occurrence.Journal.Includes[item.Index])
			}
		}
		return result
	}

	var result []ast.Include
	if r.Primary != nil {
		result = append(result, r.Primary.Includes...)
	}
	for _, path := range r.FileOrder {
		if j, ok := r.Files[path]; ok {
			result = append(result, j.Includes...)
		}
	}
	return result
}

// FormatsAt returns the commodity formats in effect at sourceOffset in the
// occurrence identified by id.
//
// For a child occurrence it starts with the formats inherited from the parent
// at the include site, then applies the child's own directives that appear
// before sourceOffset. Child decimal-mark and D stay local to the child.
//
// For the root occurrence it walks the Items stream up to sourceOffset,
// collecting root directives and merge-forward CommodityDirective declarations
// from child occurrences (child D and decimal-mark do not merge forward).
func (r *ResolvedJournal) FormatsAt(id OccurrenceID, sourceOffset int) map[string]formatter.CommodityFormat {
	occ := r.Occurrence(id)
	if occ == nil || occ.Journal == nil {
		return make(map[string]formatter.CommodityFormat)
	}

	// Child occurrence: inherit parent formats at include site, then apply local directives.
	if occ.Via.ParentID != 0 {
		base := r.FormatsAt(occ.Via.ParentID, occ.Via.IncludeRange.Start.Offset)
		var localDirectives []ast.Directive
		for _, dir := range occ.Journal.Directives {
			if dir.GetRange().Start.Offset <= sourceOffset {
				localDirectives = append(localDirectives, dir)
			}
		}
		for k, v := range formatter.ExtractCommodityFormats(localDirectives) {
			base[k] = v
		}
		return base
	}

	// Root occurrence: walk Items stream up to sourceOffset.
	if r.Items == nil {
		return formatter.ExtractCommodityFormats(r.FormatDirectives())
	}

	var directives []ast.Directive
	for _, item := range r.Items {
		itemOcc := r.Occurrence(item.OccurrenceID)
		if itemOcc == nil || itemOcc.Journal == nil {
			continue
		}
		if item.OccurrenceID == id {
			var offset int
			var dir ast.Directive
			switch item.Kind {
			case ResolvedItemDirective:
				if item.Index >= 0 && item.Index < len(itemOcc.Journal.Directives) {
					dir = itemOcc.Journal.Directives[item.Index]
					offset = dir.GetRange().Start.Offset
				}
			case ResolvedItemTransaction:
				if item.Index >= 0 && item.Index < len(itemOcc.Journal.Transactions) {
					offset = itemOcc.Journal.Transactions[item.Index].Range.Start.Offset
				}
			case ResolvedItemInclude:
				if item.Index >= 0 && item.Index < len(itemOcc.Journal.Includes) {
					offset = itemOcc.Journal.Includes[item.Index].Range.Start.Offset
				}
			default:
				continue
			}
			if offset > sourceOffset {
				break
			}
			if dir != nil {
				directives = append(directives, dir)
			}
		} else {
			// Child item: only CommodityDirective merges forward to root.
			if item.Kind == ResolvedItemDirective && item.Index >= 0 && item.Index < len(itemOcc.Journal.Directives) {
				if cd, ok := itemOcc.Journal.Directives[item.Index].(ast.CommodityDirective); ok {
					directives = append(directives, cd)
				}
			}
		}
	}

	return formatter.ExtractCommodityFormats(directives)
}

// FormatsAtFunc is FormatsAt(id, offset) for many offsets of one occurrence. A
// child occurrence inherits its formats at the include site once, and offsets
// that see the same directives of the occurrence share one result, so asking
// for every posting of a file costs one pass over the tree rather than one per
// posting. The returned maps must not be modified.
func (r *ResolvedJournal) FormatsAtFunc(id OccurrenceID) func(sourceOffset int) map[string]formatter.CommodityFormat {
	occ := r.Occurrence(id)
	if occ == nil || occ.Journal == nil || occ.Via.ParentID == 0 {
		return func(sourceOffset int) map[string]formatter.CommodityFormat { return r.FormatsAt(id, sourceOffset) }
	}

	var inherited map[string]formatter.CommodityFormat
	byCount := make(map[int]map[string]formatter.CommodityFormat)
	return func(sourceOffset int) map[string]formatter.CommodityFormat {
		if inherited == nil {
			inherited = r.FormatsAt(occ.Via.ParentID, occ.Via.IncludeRange.Start.Offset)
		}
		var local []ast.Directive
		for _, dir := range occ.Journal.Directives {
			if dir.GetRange().Start.Offset <= sourceOffset {
				local = append(local, dir)
			}
		}
		if formats, ok := byCount[len(local)]; ok {
			return formats
		}
		formats := maps.Clone(inherited)
		for k, v := range formatter.ExtractCommodityFormats(local) {
			formats[k] = v
		}
		byCount[len(local)] = formats
		return formats
	}
}

// DefaultCommodityAt returns the default commodity symbol (from the D directive)
// in effect at sourceOffset in the occurrence identified by id.
func (r *ResolvedJournal) DefaultCommodityAt(id OccurrenceID, sourceOffset int) string {
	occ := r.Occurrence(id)
	if occ == nil || occ.Journal == nil {
		return ""
	}
	if occ.Via.ParentID != 0 {
		symbol := r.DefaultCommodityAt(occ.Via.ParentID, occ.Via.IncludeRange.Start.Offset)
		for _, dir := range occ.Journal.Directives {
			if dir.GetRange().Start.Offset > sourceOffset {
				break
			}
			if d, ok := dir.(ast.DefaultCommodityDirective); ok {
				symbol = d.Symbol
			}
		}
		return symbol
	}
	var symbol string
	for _, dir := range occ.Journal.Directives {
		if dir.GetRange().Start.Offset > sourceOffset {
			break
		}
		if d, ok := dir.(ast.DefaultCommodityDirective); ok {
			symbol = d.Symbol
		}
	}
	return symbol
}
