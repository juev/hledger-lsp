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
	Transactions []Transaction
	Directives   []Directive
	Comments     []Comment
	Includes     []Include
}

type Transaction struct {
	Date        Date
	Date2       *Date
	Status      Status
	Code        string
	Description string
	Payee       string
	Note        string
	Postings    []Posting
	Tags        []Tag
	Comments    []Comment
	Range       Range
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
	Comment          string
	Tags             []Tag
	Virtual          VirtualType
	Range            Range
}

type VirtualType int

const (
	VirtualNone VirtualType = iota
	VirtualBalanced
	VirtualUnbalanced
)

type Account struct {
	Name  string
	Parts []string
	Range Range
}

type Amount struct {
	Quantity  decimal.Decimal
	Commodity Commodity
	Range     Range
}

type Commodity struct {
	Symbol   string
	Position CommodityPosition
	Range    Range
}

type CommodityPosition int

const (
	CommodityLeft CommodityPosition = iota
	CommodityRight
)

type Cost struct {
	Amount   Amount
	IsTotal  bool
	Range    Range
}

type BalanceAssertion struct {
	Amount     Amount
	IsStrict   bool
	IsInclusive bool
	Range      Range
}

type Directive interface {
	directive()
	GetRange() Range
}

type AccountDirective struct {
	Account Account
	Tags    []Tag
	Range   Range
}

func (AccountDirective) directive() {}
func (d AccountDirective) GetRange() Range { return d.Range }

type CommodityDirective struct {
	Commodity Commodity
	Format    string
	Range     Range
}

func (CommodityDirective) directive() {}
func (d CommodityDirective) GetRange() Range { return d.Range }

type Include struct {
	Path  string
	Range Range
}

func (Include) directive() {}
func (i Include) GetRange() Range { return i.Range }

type PriceDirective struct {
	Date      Date
	Commodity Commodity
	Price     Amount
	Range     Range
}

func (PriceDirective) directive() {}
func (d PriceDirective) GetRange() Range { return d.Range }

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
