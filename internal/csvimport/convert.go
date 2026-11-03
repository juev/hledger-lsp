package csvimport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/juev/hledger-lsp/internal/textutil"
)

// Two outputs of the same conversion are read together. `print -O json` gives
// exact quantities (mantissa and decimal places), but it fills in every amount
// hledger inferred, including the result of a balance assignment computed
// against the CSV data alone, which is wrong once the data is included after
// other transactions. Plain `print` keeps amounts implicit where they were
// implicit, but writes numbers in display styles a reader can misread (1,000
// under a comma decimal mark). So the text decides which postings carry an
// amount, an assertion or a cost, and the JSON supplies every value.

type jsonTransaction struct {
	Index       int           `json:"tindex"`
	Date        string        `json:"tdate"`
	Postings    []jsonPosting `json:"tpostings"`
	SourcePos   []jsonPos     `json:"tsourcepos"`
	Description *string       `json:"tdescription"`
}

type jsonPosting struct {
	Account   *string        `json:"paccount"`
	Amount    []jsonAmount   `json:"pamount"`
	Assertion *jsonAssertion `json:"pbalanceassertion"`
	// The posting type is "ptype" up to 1.52 and "preal" in the 2.0 previews.
	Type string `json:"ptype"`
	Real string `json:"preal"`
}

type jsonAmount struct {
	Commodity string          `json:"acommodity"`
	Quantity  *jsonQuantity   `json:"aquantity"`
	Cost      *jsonCost       `json:"acost"`
	CostBasis json.RawMessage `json:"acostbasis"`
	Style     struct {
		Side string `json:"ascommodityside"`
	} `json:"astyle"`
}

type jsonQuantity struct {
	Mantissa json.Number `json:"decimalMantissa"`
	Places   json.Number `json:"decimalPlaces"`
}

type jsonCost struct {
	Tag      string     `json:"tag"`
	Contents jsonAmount `json:"contents"`
}

type jsonAssertion struct {
	Amount    jsonAmount `json:"baamount"`
	Inclusive bool       `json:"bainclusive"`
	Total     bool       `json:"batotal"`
}

type jsonPos struct {
	Name string `json:"sourceName"`
	Line int    `json:"sourceLine"`
}

// maxMantissaDigits bounds a quantity read back from hledger; the static
// checks keep inputs well below it.
const maxMantissaDigits = 300

var errUnrepresentable = errors.New("unrepresentable")

// converted is the transactions of one conversion and the data file they are in.
type converted struct {
	dataPath     string
	transactions []ast.Transaction
}

// convert reads hledger's two outputs into transactions whose ranges point at
// the records in the data file. data holds the normalized content of every
// file hledger may have read.
func convert(jsonOutput, textOutput []byte, data map[string]string) (converted, error) {
	decoded, err := decodeJSON(jsonOutput)
	if err != nil {
		return converted{}, err
	}
	written, parseErrs := parser.Parse(textutil.NormalizeLineEndings(string(textOutput)))
	if len(parseErrs) > 0 || written == nil {
		message := "no journal"
		if len(parseErrs) > 0 {
			message = parseErrs[0].Message
		}
		return converted{}, fmt.Errorf("cannot read hledger's print output: %s", message)
	}
	if len(written.Transactions) != len(decoded) {
		return converted{}, fmt.Errorf("hledger printed %d transactions as text and %d as JSON", len(written.Transactions), len(decoded))
	}

	var result converted
	type ordered struct {
		index       int
		transaction ast.Transaction
	}
	transactions := make([]ordered, 0, len(decoded))
	seen := make(map[int]bool, len(decoded))
	for i := range decoded {
		source := decoded[i]
		if seen[source.Index] {
			return converted{}, fmt.Errorf("hledger reported transaction index %d twice", source.Index)
		}
		seen[source.Index] = true
		if len(source.SourcePos) == 0 {
			return converted{}, errors.New("hledger reported a transaction without a source position")
		}
		path := filepath.Clean(source.SourcePos[0].Name)
		if result.dataPath == "" {
			result.dataPath = path
		} else if path != result.dataPath {
			return converted{}, fmt.Errorf("hledger read transactions from both %s and %s", filepath.Base(result.dataPath), filepath.Base(path))
		}
		content, ok := data[path]
		if !ok {
			return converted{}, fmt.Errorf("hledger read %s, which the import did not check", filepath.Base(path))
		}
		transaction, err := merge(written.Transactions[i], source)
		if err != nil {
			return converted{}, fmt.Errorf("transaction at %s line %d: %w", filepath.Base(path), source.SourcePos[0].Line, err)
		}
		placeTransaction(&transaction, recordRange(content, source.SourcePos))
		transactions = append(transactions, ordered{index: source.Index, transaction: transaction})
	}
	// print sorts by date; the journal keeps hledger's reading order, which the
	// transaction index records.
	sort.SliceStable(transactions, func(i, j int) bool { return transactions[i].index < transactions[j].index })
	result.transactions = make([]ast.Transaction, len(transactions))
	for i := range transactions {
		result.transactions[i] = transactions[i].transaction
	}
	return result, nil
}

func decodeJSON(output []byte) ([]jsonTransaction, error) {
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.UseNumber()
	var transactions []jsonTransaction
	if err := decoder.Decode(&transactions); err != nil {
		return nil, fmt.Errorf("cannot read hledger's JSON output: %w", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("cannot read hledger's JSON output: unexpected data after the transactions")
	}
	if transactions == nil {
		return nil, errors.New("cannot read hledger's JSON output: no transaction list")
	}
	return transactions, nil
}

// merge takes the structure of a transaction from the text output and its
// values from the JSON.
func merge(written ast.Transaction, source jsonTransaction) (ast.Transaction, error) {
	if source.Date == "" || source.Description == nil {
		return ast.Transaction{}, errors.New("hledger's JSON output lacks the date or description")
	}
	if got := fmt.Sprintf("%04d-%02d-%02d", written.Date.Year, written.Date.Month, written.Date.Day); got != source.Date {
		return ast.Transaction{}, fmt.Errorf("the printed date %s differs from %s", got, source.Date)
	}
	if len(written.Postings) != len(source.Postings) {
		return ast.Transaction{}, fmt.Errorf("hledger printed %d postings as text and %d as JSON", len(written.Postings), len(source.Postings))
	}
	transaction := written
	transaction.Postings = make([]ast.Posting, len(written.Postings))
	for i, posting := range written.Postings {
		value := source.Postings[i]
		if value.Account == nil || posting.Account.Name != *value.Account {
			return ast.Transaction{}, fmt.Errorf("the printed account %q differs from hledger's", posting.Account.Name)
		}
		if posting.Virtual != virtualType(value) {
			return ast.Transaction{}, fmt.Errorf("account %q: the printed posting type differs from hledger's", posting.Account.Name)
		}
		if posting.LotPrice != nil || posting.UnparsedTail != (ast.Range{}) || posting.Multiplier {
			return ast.Transaction{}, fmt.Errorf("account %q: lot annotations in imported data are not supported", posting.Account.Name)
		}
		posting.Account.ResolvedName = ""
		if posting.Amount != nil {
			amount, cost, err := postingAmount(value.Amount)
			if err != nil {
				return ast.Transaction{}, fmt.Errorf("account %q: %w", posting.Account.Name, err)
			}
			if cost, err = writtenCost(posting.Cost, cost); err != nil {
				return ast.Transaction{}, fmt.Errorf("account %q: %w", posting.Account.Name, err)
			}
			amount.Commodity.Quoted = posting.Amount.Commodity.Quoted
			posting.Amount = &amount
			posting.Cost = cost
		} else if posting.Cost != nil {
			return ast.Transaction{}, fmt.Errorf("account %q: a cost without an amount", posting.Account.Name)
		}
		if posting.BalanceAssertion != nil {
			if value.Assertion == nil {
				return ast.Transaction{}, fmt.Errorf("account %q: the printed balance assertion is missing from hledger's", posting.Account.Name)
			}
			assertion, err := balanceAssertion(*posting.BalanceAssertion, *value.Assertion)
			if err != nil {
				return ast.Transaction{}, fmt.Errorf("account %q: %w", posting.Account.Name, err)
			}
			posting.BalanceAssertion = &assertion
		} else if value.Assertion != nil {
			return ast.Transaction{}, fmt.Errorf("account %q: hledger reports a balance assertion that was not printed", posting.Account.Name)
		}
		transaction.Postings[i] = posting
	}
	return transaction, nil
}

func virtualType(posting jsonPosting) ast.VirtualType {
	kind := posting.Real
	if kind == "" {
		kind = posting.Type
	}
	switch kind {
	case "VirtualPosting":
		return ast.VirtualUnbalanced
	case "BalancedVirtualPosting":
		return ast.VirtualBalanced
	}
	return ast.VirtualNone
}

// postingAmount converts a posting's mixed amount. An empty one is hledger's
// zero; more than one commodity cannot be written as one journal amount.
func postingAmount(amounts []jsonAmount) (ast.Amount, *ast.Cost, error) {
	switch len(amounts) {
	case 0:
		return ast.Amount{Quantity: decimal.Zero, RawQuantity: "0"}, nil, nil
	case 1:
	default:
		return ast.Amount{}, nil, fmt.Errorf("%w: an amount in %d commodities", errUnrepresentable, len(amounts))
	}
	amount, err := simpleAmount(amounts[0])
	if err != nil {
		return ast.Amount{}, nil, err
	}
	if amounts[0].Cost == nil {
		return amount, nil, nil
	}
	cost := &ast.Cost{}
	switch amounts[0].Cost.Tag {
	case "UnitCost":
	case "TotalCost":
		cost.IsTotal = true
	default:
		return ast.Amount{}, nil, fmt.Errorf("%w: cost kind %q", errUnrepresentable, amounts[0].Cost.Tag)
	}
	if amounts[0].Cost.Contents.Cost != nil {
		return ast.Amount{}, nil, fmt.Errorf("%w: a cost with its own cost", errUnrepresentable)
	}
	if cost.Amount, err = simpleAmount(amounts[0].Cost.Contents); err != nil {
		return ast.Amount{}, nil, err
	}
	return amount, cost, nil
}

func balanceAssertion(written ast.BalanceAssertion, source jsonAssertion) (ast.BalanceAssertion, error) {
	if written.IsStrict != source.Total || written.IsInclusive != source.Inclusive {
		return ast.BalanceAssertion{}, errors.New("the printed balance assertion type differs from hledger's")
	}
	amount, cost, err := postingAmount([]jsonAmount{source.Amount})
	if err != nil {
		return ast.BalanceAssertion{}, err
	}
	if cost, err = writtenCost(written.Cost, cost); err != nil {
		return ast.BalanceAssertion{}, err
	}
	written.Amount = amount
	written.Cost = cost
	return written, nil
}

// writtenCost is the cost as the text writes it, with hledger's exact value.
// A cost the text does not write was inferred by hledger from the CSV data
// alone and is left to the analyzer to infer in context. hledger stores a total
// cost with the sign of the amount, so the value's sign comes from the text.
func writtenCost(written, value *ast.Cost) (*ast.Cost, error) {
	switch {
	case written == nil:
		return nil, nil
	case value == nil || written.IsTotal != value.IsTotal:
		return nil, errors.New("the printed cost differs from hledger's")
	}
	quantity := value.Amount.Quantity.Abs()
	if written.Amount.Quantity.IsNegative() {
		quantity = quantity.Neg()
	}
	cost := *value
	cost.Amount.Quantity = quantity
	cost.Amount.RawQuantity = quantity.String()
	return &cost, nil
}

func simpleAmount(source jsonAmount) (ast.Amount, error) {
	if len(source.CostBasis) > 0 && string(source.CostBasis) != "null" {
		return ast.Amount{}, fmt.Errorf("%w: a lot cost basis", errUnrepresentable)
	}
	quantity, err := exactQuantity(source.Quantity)
	if err != nil {
		return ast.Amount{}, err
	}
	position := ast.CommodityLeft
	if source.Style.Side == "R" {
		position = ast.CommodityRight
	}
	return ast.Amount{
		Quantity:    quantity,
		RawQuantity: quantity.String(),
		Commodity:   ast.Commodity{Symbol: source.Commodity, Position: position},
	}, nil
}

func exactQuantity(quantity *jsonQuantity) (decimal.Decimal, error) {
	if quantity == nil {
		return decimal.Decimal{}, errors.New("hledger's JSON output lacks a quantity")
	}
	digits := strings.TrimPrefix(quantity.Mantissa.String(), "-")
	if digits == "" || len(digits) > maxMantissaDigits || strings.ContainsAny(digits, ".eE+-") {
		return decimal.Decimal{}, fmt.Errorf("%w: quantity mantissa %q", errUnrepresentable, quantity.Mantissa)
	}
	mantissa, ok := new(big.Int).SetString(quantity.Mantissa.String(), 10)
	if !ok {
		return decimal.Decimal{}, fmt.Errorf("%w: quantity mantissa %q", errUnrepresentable, quantity.Mantissa)
	}
	places, err := quantity.Places.Int64()
	if err != nil || places < 0 || places > 255 {
		return decimal.Decimal{}, fmt.Errorf("%w: %q decimal places", errUnrepresentable, quantity.Places)
	}
	return decimal.NewFromBigInt(mantissa, -int32(places)), nil
}

// recordRange is the range of the record hledger converted: from the start of
// its first line to the end of its last. hledger 1.99.5 reports the line after
// the record as the end position.
func recordRange(content string, positions []jsonPos) ast.Range {
	first := positions[0].Line
	last := first
	if len(positions) > 1 && positions[1].Line > first {
		last = positions[1].Line - 1
	}
	starts := []int{0}
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	position := func(line int, end bool) ast.Position {
		if line < 1 {
			line = 1
		}
		if line > len(starts) {
			return ast.Position{Line: len(starts), Column: 1 + utf8.RuneCountInString(content[starts[len(starts)-1]:]), Offset: len(content)}
		}
		offset := starts[line-1]
		if !end {
			return ast.Position{Line: line, Column: 1, Offset: offset}
		}
		lineEnd := len(content)
		if line < len(starts) {
			lineEnd = starts[line] - 1
		}
		return ast.Position{Line: line, Column: 1 + utf8.RuneCountInString(content[offset:lineEnd]), Offset: lineEnd}
	}
	return ast.Range{Start: position(first, false), End: position(last, true)}
}

// placeTransaction points every range in the transaction at its record: the
// text the parser read them from is hledger's output, not the user's file.
func placeTransaction(transaction *ast.Transaction, rng ast.Range) {
	transaction.Range = rng
	transaction.DescriptionRange = rng
	transaction.Date.Range = rng
	if transaction.Date2 != nil {
		date2 := *transaction.Date2
		date2.Range = rng
		transaction.Date2 = &date2
	}
	transaction.Tags = placeTags(transaction.Tags, rng)
	transaction.Comments = placeComments(transaction.Comments, rng)
	for i := range transaction.Postings {
		posting := &transaction.Postings[i]
		posting.Range = rng
		posting.Account.Range = rng
		posting.CommentRange = rng
		posting.Tags = placeTags(posting.Tags, rng)
		if posting.Amount != nil {
			posting.Amount.Range = rng
			posting.Amount.Commodity.Range = rng
		}
		if posting.Cost != nil {
			posting.Cost.Range = rng
			posting.Cost.Amount.Range = rng
			posting.Cost.Amount.Commodity.Range = rng
		}
		if posting.BalanceAssertion != nil {
			posting.BalanceAssertion.Range = rng
			posting.BalanceAssertion.Amount.Range = rng
			posting.BalanceAssertion.Amount.Commodity.Range = rng
			if posting.BalanceAssertion.Cost != nil {
				posting.BalanceAssertion.Cost.Range = rng
				posting.BalanceAssertion.Cost.Amount.Range = rng
			}
		}
	}
}

func placeTags(tags []ast.Tag, rng ast.Range) []ast.Tag {
	if len(tags) == 0 {
		return tags
	}
	placed := make([]ast.Tag, len(tags))
	for i, tag := range tags {
		tag.Range = rng
		placed[i] = tag
	}
	return placed
}

func placeComments(comments []ast.Comment, rng ast.Range) []ast.Comment {
	if len(comments) == 0 {
		return comments
	}
	placed := make([]ast.Comment, len(comments))
	for i, comment := range comments {
		comment.Range = rng
		comment.Tags = placeTags(comment.Tags, rng)
		placed[i] = comment
	}
	return placed
}
