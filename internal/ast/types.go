package ast

import "github.com/shopspring/decimal"

type Range struct {
	Start Position
	End   Position
}

type Position struct {
	Line   int
	Column int
	Offset int
}

type Journal struct {
	Transactions         []Transaction
	PeriodicTransactions []PeriodicTransaction
	AutoPostingRules     []AutoPostingRule
	Directives           []Directive
	Comments             []Comment
	Includes             []Include
}

type Transaction struct {
	Date             Date
	Date2            *Date
	Status           Status
	Code             string
	Description      string
	DescriptionRange Range
	Payee            string
	Note             string
	Postings         []Posting
	Tags             []Tag
	Comments         []Comment
	Range            Range
}

type PeriodicTransaction struct {
	Period           string
	Status           Status
	Code             string
	Description      string
	DescriptionRange Range
	Payee            string
	Note             string
	Postings         []Posting
	Tags             []Tag
	Comments         []Comment
	Range            Range
}

type AutoPostingRule struct {
	Query    string
	Postings []Posting
	Comments []Comment
	Range    Range
}

type Date struct {
	Year  int
	Month int
	Day   int
	Range Range
}

type Status int

const (
	StatusNone Status = iota
	StatusPending
	StatusCleared
)

type Posting struct {
	Status           Status
	Account          Account
	Amount           *Amount
	BalanceAssertion *BalanceAssertion
	Cost             *Cost
	LotPrice         *LotPrice
	Comment          string
	// CommentRange is the range of the inline comment token, used to preserve a
	// hand-aligned comment column.
	CommentRange Range
	Tags         []Tag
	Virtual      VirtualType
	Range        Range
	// UnparsedTail marks text on the posting line that the parser did not
	// consume (for example `:=` balance assignment, which hledger 1.52 rejects).
	// The formatter leaves such lines untouched instead of rebuilding them from
	// the AST and losing the text.
	UnparsedTail Range
}

type VirtualType int

const (
	VirtualNone VirtualType = iota
	VirtualBalanced
	VirtualUnbalanced
)

type Account struct {
	Name         string // Original name from file (for formatting)
	ResolvedName string // Full name with apply account prefix (for validation)
	Range        Range
}

// GetResolvedName returns ResolvedName if set, otherwise Name
func (a Account) GetResolvedName() string {
	if a.ResolvedName != "" {
		return a.ResolvedName
	}
	return a.Name
}

type Amount struct {
	Quantity            decimal.Decimal
	RawQuantity         string
	Commodity           Commodity
	SignBeforeCommodity bool
	Range               Range
}

type Commodity struct {
	Symbol string
	Quoted bool
	// Inferred marks an amount that was written without a commodity symbol and
	// inherited the journal's default commodity (the `D` directive) instead.
	// The amount belongs to Symbol for every semantic purpose, but it has no
	// commodity text of its own: Range is the zero Range and features that
	// point at written text must use WrittenSymbol.
	Inferred bool
	Position CommodityPosition
	Range    Range
}

// WrittenSymbol returns the commodity symbol as it appears in the source, or
// "" when the amount was written without one. Use it wherever a range or a
// rendered amount is meant to mirror the document; use Symbol for semantics.
func (c Commodity) WrittenSymbol() string {
	if c.Inferred {
		return ""
	}
	return c.Symbol
}

type CommodityPosition int

const (
	CommodityLeft CommodityPosition = iota
	CommodityRight
)

type Cost struct {
	Amount  Amount
	IsTotal bool
	Range   Range
}

type LotPrice struct {
	Cost    *Amount
	IsTotal bool
	// Fixed marks the `{=PRICE}` / `{{=PRICE}}` form, which states the lot cost
	// outright instead of the per-unit price.
	Fixed bool
	Date  string
	Label string
	Range Range
}

type BalanceAssertion struct {
	Amount      Amount
	Cost        *Cost
	LotPrice    *LotPrice
	IsStrict    bool
	IsInclusive bool
	Range       Range
}

type Directive interface {
	directive()
	GetRange() Range
}

type AccountDirective struct {
	Account Account
	Tags    []Tag
	Comment string
	Subdirs map[string]string
	Range   Range
}

func (AccountDirective) directive()        {}
func (d AccountDirective) GetRange() Range { return d.Range }

type CommodityDirective struct {
	Commodity Commodity
	Format    string
	Alias     []string
	Note      string
	Subdirs   map[string]string
	Range     Range
}

func (CommodityDirective) directive()        {}
func (d CommodityDirective) GetRange() Range { return d.Range }

type Include struct {
	Path  string
	Range Range
}

func (Include) directive()        {}
func (i Include) GetRange() Range { return i.Range }

type PriceDirective struct {
	Date      Date
	Commodity Commodity
	Price     Amount
	Range     Range
}

func (PriceDirective) directive()        {}
func (d PriceDirective) GetRange() Range { return d.Range }

type YearDirective struct {
	Year  int
	Range Range
}

func (YearDirective) directive()        {}
func (d YearDirective) GetRange() Range { return d.Range }

type DefaultCommodityDirective struct {
	Symbol string
	Format string
	Range  Range
}

func (DefaultCommodityDirective) directive()        {}
func (d DefaultCommodityDirective) GetRange() Range { return d.Range }

type DecimalMarkDirective struct {
	Mark  string
	Range Range
}

func (DecimalMarkDirective) directive()        {}
func (d DecimalMarkDirective) GetRange() Range { return d.Range }

type PayeeDirective struct {
	Name  string
	Range Range
}

func (PayeeDirective) directive()        {}
func (d PayeeDirective) GetRange() Range { return d.Range }

type TagDirective struct {
	Name  string
	Range Range
}

func (TagDirective) directive()        {}
func (d TagDirective) GetRange() Range { return d.Range }

type AliasDirective struct {
	Original string
	Alias    string
	IsRegex  bool
	Range    Range
}

func (AliasDirective) directive()        {}
func (d AliasDirective) GetRange() Range { return d.Range }

type Comment struct {
	Text  string
	Tags  []Tag
	Range Range
}

type Tag struct {
	Name  string
	Value string
	Range Range
}
