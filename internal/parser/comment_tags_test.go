package parser

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/testutil"
)

// commentTagCases are comment texts and the tags hledger 1.52.4 reads in them
// (`print -O json`). A tag name is the last word before a colon, so prose may come
// before it and a comma glued to the word belongs to the name; a value runs to
// the next comma.
var commentTagCases = []struct {
	name    string
	comment string
	want    [][2]string
}{
	{"date tag after words", "a date:2024-01-19", [][2]string{{"date", "2024-01-19"}}},
	{"tags after prose and a comma", "paid on date:2024-01-19, ref:5", [][2]string{{"date", "2024-01-19"}, {"ref", "5"}}},
	{"word glued by a comma", "x,date:2024-01-19", [][2]string{{"x,date", "2024-01-19"}}},
	{"leading comma", ",date:2024-01-19", [][2]string{{",date", "2024-01-19"}}},
	{"words glued by commas", "a,b,c:1", [][2]string{{"a,b,c", "1"}}},
	{"colon inside a value", "foo:bar baz:qux", [][2]string{{"foo", "bar baz:qux"}}},
	{"second tag after words", "foo:bar, see baz:qux", [][2]string{{"foo", "bar"}, {"baz", "qux"}}},
	{"empty name", ":x, y:1", [][2]string{{"y", "1"}}},
	{"space before a colon", "a :foo bar:1", [][2]string{{"bar", "1"}}},
	{"empty name before a date tag", ": date:2024-01-19", [][2]string{{"date", "2024-01-19"}}},
	{"empty name glued to a date tag", ":date:2024-01-19", [][2]string{{"date", "2024-01-19"}}},
	{"tag without a value after words", "foo bar:", [][2]string{{"bar", ""}}},
	{"value with spaces around and a spaced name", "k:   v  ,  j : w", [][2]string{{"k", "v"}}},
	{"value with words", "заметка тег:значение с пробелами, 日本 タグ:値", [][2]string{{"тег", "значение с пробелами"}, {"タグ", "値"}}},
	{"tab before a name", "a\tdate:2024-01-19", [][2]string{{"date", "2024-01-19"}}},
	{"no-break space before a name", "a\u00a0date:2024-01-19", [][2]string{{"date", "2024-01-19"}}},
	{"ideographic space before a name", "a\u3000date:2024-01-19", [][2]string{{"date", "2024-01-19"}}},
	{"zero-width space is part of a name", "a\u200bdate:2024-01-19", [][2]string{{"a\u200bdate", "2024-01-19"}}},
	{"next line character is part of a name", "a\u0085date:2024-01-19", [][2]string{{"a\u0085date", "2024-01-19"}}},
	{"line separator is part of a name", "a\u2028date:2024-01-19", [][2]string{{"a\u2028date", "2024-01-19"}}},
	{"brackets in a name", "[y]z:1", [][2]string{{"[y]z", "1"}}},
	{"bracket before a name", "x [y] z:1", [][2]string{{"z", "1"}}},
	{"bracket split by a colon", "a[b:1]", [][2]string{{"a[b", "1]"}}},
	{"double colon", "a::b", [][2]string{{"a", ":b"}}},
	{"commas without spaces", "foo:x,y:2", [][2]string{{"foo", "x"}, {"y", "2"}}},
	{"double comma", "foo:x,, y:2", [][2]string{{"foo", "x"}, {"y", "2"}}},
	{"date tag swallows the rest", "date:2024-01-19 extra date2:2024-01-20", [][2]string{{"date", "2024-01-19 extra date2:2024-01-20"}}},
	{"semicolon inside a value", "a:b ; c:d", [][2]string{{"a", "b ; c:d"}}},
	{"emoji before a tag", "🍕 food:x", [][2]string{{"food", "x"}}},
	{"no tags", "just words", nil},
}

func tagPairs(t *testing.T, text string) [][2]string {
	t.Helper()
	var pairs [][2]string
	for _, tag := range ScanCommentTags(text) {
		pairs = append(pairs, [2]string{tag.Name, tag.Value})
	}
	return pairs
}

func TestScanCommentTags_ReadsTagsLikeHledger(t *testing.T) {
	for _, tc := range commentTagCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tagPairs(t, " "+tc.comment))
		})
	}
}

func TestParser_TagsAfterProseInEveryComment(t *testing.T) {
	input := "account assets:x  ; number is acct:123 45\n\n" +
		"2024-01-01 t  ; some words tt:1, more uu:two words\n" +
		"    ; and prose vv:3\n" +
		"    assets:x  4 GBP  ; paid on date:2024-01-19\n" +
		"    ; see also ref:7\n" +
		"    b\n"
	journal, errs := Parse(input)
	require.Empty(t, errs)

	var account []string
	for _, dir := range journal.Directives {
		if acc, ok := dir.(ast.AccountDirective); ok {
			for _, tag := range acc.Tags {
				account = append(account, tag.Name+"="+tag.Value)
			}
		}
	}
	assert.Equal(t, []string{"acct=123 45"}, account)

	tx := journal.Transactions[0]
	var comments []string
	for _, comment := range tx.Comments {
		for _, tag := range comment.Tags {
			comments = append(comments, tag.Name+"="+tag.Value)
		}
	}
	assert.Equal(t, []string{"tt=1", "uu=two words", "vv=3", "ref=7"}, comments)
	require.Len(t, tx.Postings[0].Tags, 1)
	assert.Equal(t, "date", tx.Postings[0].Tags[0].Name)
	assert.Equal(t, "2024-01-19", tx.Postings[0].Tags[0].Value)
}

// A tag range starts at its name, not at the prose before it, and ends after its
// value. Columns count runes and offsets count bytes.
func TestParser_TagRangeAfterNonASCIIProse(t *testing.T) {
	input := "2024-01-01 t\n    a  4 GBP  ; оплата 日本 клиент:Иван Петров, x\n    b\n"
	journal, errs := Parse(input)
	require.Empty(t, errs)

	tags := journal.Transactions[0].Postings[0].Tags
	require.Len(t, tags, 1)
	tag := tags[0]
	assert.Equal(t, "клиент", tag.Name)
	assert.Equal(t, "Иван Петров", tag.Value)

	line := strings.Split(input, "\n")[1]
	nameByte := strings.Index(line, "клиент")
	endByte := strings.Index(line, ", x")
	assert.Equal(t, 2, tag.Range.Start.Line)
	assert.Equal(t, 1+len([]rune(line[:nameByte])), tag.Range.Start.Column)
	assert.Equal(t, 1+len([]rune(line[:endByte])), tag.Range.End.Column)
	assert.Equal(t, len("2024-01-01 t\n")+nameByte, tag.Range.Start.Offset)
	assert.Equal(t, len("2024-01-01 t\n")+endByte, tag.Range.End.Offset)
}

// hledgerTags returns the tags hledger reads on the transaction and on its first
// posting.
func hledgerTags(t *testing.T, hledgerPath, input string) (txTags, postingTags [][2]string) {
	t.Helper()
	cmd := exec.Command(hledgerPath, "-f", "-", "print", "-O", "json")
	cmd.Stdin = strings.NewReader(input)
	output, err := cmd.Output()
	require.NoError(t, err, "hledger refused:\n%s", input)

	var transactions []struct {
		Tags     [][2]string `json:"ttags"`
		Postings []struct {
			Tags [][2]string `json:"ptags"`
		} `json:"tpostings"`
	}
	require.NoError(t, json.Unmarshal(output, &transactions))
	require.Len(t, transactions, 1)
	return transactions[0].Tags, transactions[0].Postings[0].Tags
}

func TestOracle_CommentTagsMatchHledger(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	for _, tc := range commentTagCases {
		t.Run(tc.name, func(t *testing.T) {
			input := "2024-01-01 t  ; " + tc.comment + "\n    c  4 GBP  ; " + tc.comment + "\n    b\n"
			txTags, postingTags := hledgerTags(t, hledgerPath, input)
			assert.Equal(t, tc.want, emptyAsNil(postingTags), "hledger and the case table differ")

			journal, errs := Parse(input)
			require.Empty(t, errs)
			tx := journal.Transactions[0]
			var ours [][2]string
			for _, tag := range tx.Postings[0].Tags {
				ours = append(ours, [2]string{tag.Name, tag.Value})
			}
			assert.Equal(t, emptyAsNil(postingTags), ours, "posting comment:\n%s", input)

			ours = nil
			for _, comment := range tx.Comments {
				for _, tag := range comment.Tags {
					ours = append(ours, [2]string{tag.Name, tag.Value})
				}
			}
			assert.Equal(t, emptyAsNil(txTags), ours, "transaction comment:\n%s", input)
		})
	}
}

func emptyAsNil(tags [][2]string) [][2]string {
	if len(tags) == 0 {
		return nil
	}
	return tags
}

// commentContextCases are comments that hledger 1.52.4 reads differently on a
// transaction and on a posting: after a colon that names nothing, a posting
// comment skips the spaces and one comma before the next name.
var commentContextCases = []struct {
	name        string
	comment     string
	transaction [][2]string
	posting     [][2]string
}{
	{"comma after an empty name", ":,x:1", [][2]string{{",x", "1"}}, [][2]string{{"x", "1"}}},
	{"space and comma after an empty name", ": ,x:1", [][2]string{{",x", "1"}}, [][2]string{{"x", "1"}}},
	{"ideographic space and comma after an empty name", ":　,x:1", [][2]string{{",x", "1"}}, [][2]string{{"x", "1"}}},
	{"two commas after an empty name", ":,,x:1", [][2]string{{",,x", "1"}}, [][2]string{{",x", "1"}}},
	{"comma after a spaced colon", "a :,x:1", [][2]string{{",x", "1"}}, [][2]string{{"x", "1"}}},
	{"date tag after an empty name and a comma", ":,date:2024-01-19", [][2]string{{",date", "2024-01-19"}}, [][2]string{{"date", "2024-01-19"}}},
	{"word after an empty name", ": y,x:1", [][2]string{{"y,x", "1"}}, [][2]string{{"y,x", "1"}}},
	{"comma after an empty value", "foo:,,bar:1", [][2]string{{"foo", ""}, {",bar", "1"}}, [][2]string{{"foo", ""}, {",bar", "1"}}},
	{"Cyrillic prose before an empty name and a comma", "заметка :,тег:значение с пробелами", [][2]string{{",тег", "значение с пробелами"}}, [][2]string{{"тег", "значение с пробелами"}}},
	{"emoji prose before an empty name, a wide space and a comma", "🍕 :\u3000,ключ:日本", [][2]string{{",ключ", "日本"}}, [][2]string{{"ключ", "日本"}}},
}

func TestScanCommentTags_PostingCommentSkipsACommaAfterAnEmptyName(t *testing.T) {
	for _, tc := range commentContextCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.transaction, tagPairs(t, " "+tc.comment), "transaction")
			var posting [][2]string
			for _, tag := range ScanPostingCommentTags(" " + tc.comment) {
				posting = append(posting, [2]string{tag.Name, tag.Value})
			}
			assert.Equal(t, tc.posting, posting, "posting")
		})
	}
}

// commentContextJournal puts the comment on the transaction line, on a comment
// line before the first posting (a transaction comment), on the posting line
// and on a ; line below the posting (both posting comments).
func commentContextJournal(comment string) string {
	return "2024-01-01 t  ; " + comment + "\n    ; " + comment + "\n    c  4 GBP  ; " + comment + "\n    ; " + comment + "\n    b\n"
}

func TestParser_ReadsPostingCommentsAsHledgerDoes(t *testing.T) {
	for _, tc := range commentContextCases {
		t.Run(tc.name, func(t *testing.T) {
			journal, errs := Parse(commentContextJournal(tc.comment))
			require.Empty(t, errs)
			tx := journal.Transactions[0]
			require.Len(t, tx.Comments, 3)

			pairs := func(tags []ast.Tag) [][2]string {
				var out [][2]string
				for _, tag := range tags {
					out = append(out, [2]string{tag.Name, tag.Value})
				}
				return out
			}
			assert.Equal(t, tc.transaction, pairs(tx.Comments[0].Tags), "transaction line")
			assert.Equal(t, tc.transaction, pairs(tx.Comments[1].Tags), "comment line before the postings")
			assert.Equal(t, tc.posting, pairs(tx.Postings[0].Tags), "posting line")
			assert.Equal(t, tc.posting, pairs(tx.Comments[2].Tags), "comment line below the posting")
		})
	}
}

func TestOracle_PostingCommentTagsMatchHledger(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	for _, tc := range commentContextCases {
		t.Run(tc.name, func(t *testing.T) {
			txTags, postingTags := hledgerTags(t, hledgerPath, commentContextJournal(tc.comment))
			assert.Equal(t, append(append([][2]string{}, tc.transaction...), tc.transaction...), txTags)
			assert.Equal(t, append(append([][2]string{}, tc.posting...), tc.posting...), postingTags)
		})
	}
}

// periodicJournal and autoRuleJournal put the comment on the header, on a
// comment line before the first posting, on a posting line and on a ; line below
// that posting of a periodic transaction and of an auto posting rule. hledger
// reads the last two as posting comments there too.
func periodicJournal(comment string) string {
	return "~ monthly from 2024-01 to 2024-02  ; " + comment + "\n    ; " + comment + "\n    c  4 GBP  ; " + comment + "\n    ; " + comment + "\n    b\n"
}

func autoRuleJournal(comment string) string {
	return "= c  ; " + comment + "\n    ; " + comment + "\n    d  *2  ; " + comment + "\n    ; " + comment + "\n    e  *-2\n\n2024-01-01 t\n    c  4 GBP\n    b\n"
}

func tagPairsOf(tags []ast.Tag) [][2]string {
	var out [][2]string
	for _, tag := range tags {
		out = append(out, [2]string{tag.Name, tag.Value})
	}
	return out
}

func TestParser_PeriodicAndAutoRuleCommentsBelowAPostingArePostingComments(t *testing.T) {
	for _, tc := range commentContextCases {
		t.Run(tc.name, func(t *testing.T) {
			journal, errs := Parse(periodicJournal(tc.comment))
			require.Empty(t, errs)
			require.Len(t, journal.PeriodicTransactions, 1)
			ptx := journal.PeriodicTransactions[0]
			require.Len(t, ptx.Comments, 3, "periodic transaction comments")
			assert.Equal(t, tc.transaction, tagPairsOf(ptx.Comments[0].Tags), "periodic header")
			assert.Equal(t, tc.transaction, tagPairsOf(ptx.Comments[1].Tags), "periodic comment line before the postings")
			assert.Equal(t, tc.posting, tagPairsOf(ptx.Postings[0].Tags), "periodic posting line")
			assert.Equal(t, tc.posting, tagPairsOf(ptx.Comments[2].Tags), "periodic comment line below the posting")

			journal, errs = Parse(autoRuleJournal(tc.comment))
			require.Empty(t, errs)
			require.Len(t, journal.AutoPostingRules, 1)
			rule := journal.AutoPostingRules[0]
			require.Len(t, rule.Comments, 3, "auto rule comments")
			assert.Equal(t, tc.transaction, tagPairsOf(rule.Comments[0].Tags), "auto rule header")
			assert.Equal(t, tc.transaction, tagPairsOf(rule.Comments[1].Tags), "auto rule comment line before the postings")
			assert.Equal(t, tc.posting, tagPairsOf(rule.Postings[0].Tags), "auto rule posting line")
			assert.Equal(t, tc.posting, tagPairsOf(rule.Comments[2].Tags), "auto rule comment line below the posting")
		})
	}
}

// hledgerGenerated returns the tags hledger gives the first transaction it prints
// and its posting to account, leaving out the tags hledger adds itself (names
// starting with "_").
func hledgerGenerated(t *testing.T, hledgerPath, input, account string, args ...string) (txTags, postingTags [][2]string) {
	t.Helper()
	cmd := exec.Command(hledgerPath, append([]string{"-f", "-", "print", "-O", "json"}, args...)...)
	cmd.Stdin = strings.NewReader(input)
	output, err := cmd.Output()
	require.NoError(t, err, "hledger refused:\n%s", input)

	var transactions []struct {
		Tags     [][2]string `json:"ttags"`
		Postings []struct {
			Account string      `json:"paccount"`
			Tags    [][2]string `json:"ptags"`
		} `json:"tpostings"`
	}
	require.NoError(t, json.Unmarshal(output, &transactions))
	require.NotEmpty(t, transactions)
	own := func(tags [][2]string) [][2]string {
		var out [][2]string
		for _, tag := range tags {
			if !strings.HasPrefix(tag[0], "_") {
				out = append(out, tag)
			}
		}
		return out
	}
	for _, posting := range transactions[0].Postings {
		if posting.Account == account {
			return own(transactions[0].Tags), own(posting.Tags)
		}
	}
	t.Fatalf("no generated posting in:\n%s", output)
	return nil, nil
}

func TestOracle_PeriodicAndAutoRuleCommentTagsMatchHledger(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)

	for _, tc := range commentContextCases {
		t.Run(tc.name, func(t *testing.T) {
			twice := func(tags [][2]string) [][2]string {
				return append(append([][2]string{}, tags...), tags...)
			}
			txTags, postingTags := hledgerGenerated(t, hledgerPath, periodicJournal(tc.comment), "c", "--forecast=2024-01..2024-02")
			assert.Equal(t, twice(tc.transaction), txTags, "periodic transaction")
			assert.Equal(t, twice(tc.posting), postingTags, "periodic posting")

			_, postingTags = hledgerGenerated(t, hledgerPath, autoRuleJournal(tc.comment), "d", "--auto")
			assert.Equal(t, twice(tc.posting), postingTags, "auto posting")
		})
	}
}
