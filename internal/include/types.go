package include

import (
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
)

type LoadError struct {
	Kind       ErrorKind
	Path       string
	SourcePath string
	Message    string
	Range      ast.Range
	Provenance IncludeProvenance
}

func (e LoadError) Error() string {
	return e.Message
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
}

func (o LoadOptions) overlay(path string) (string, bool) {
	entry, ok := o.Overlays[canonicalPath(path)]
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
	Via            IncludeProvenance
	Items          []ResolvedItem
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
	for index := range r.Occurrences {
		if r.Occurrences[index].ID == id {
			return &r.Occurrences[index]
		}
	}
	return nil
}

// OccurrencesForPath returns all occurrences for an exact source path.
func (r *ResolvedJournal) OccurrencesForPath(path string) []JournalOccurrence {
	return r.occurrencesForIDs(r.ByPath[path])
}

// OccurrencesForCanonical returns all occurrences for a canonical filesystem
// path. The input is canonicalized so a real path finds occurrences included
// through a symlink (and vice versa).
func (r *ResolvedJournal) OccurrencesForCanonical(path string) []JournalOccurrence {
	return r.occurrencesForIDs(r.ByCanonical[canonicalPath(path)])
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
