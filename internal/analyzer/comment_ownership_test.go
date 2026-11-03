package analyzer

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/parser"
	"github.com/juev/hledger-lsp/internal/textutil"
)

// ownedLinesOneByOne is the ownership rule written per posting: the ; lines
// below the posting and above the next one.
func ownedLinesOneByOne(postings []ast.Posting, comments []ast.Comment, index int) []string {
	first := postingRange(&postings[index]).Start.Line
	next := int(^uint(0) >> 1)
	if index+1 < len(postings) {
		next = postingRange(&postings[index+1]).Start.Line
	}
	var lines []string
	for _, line := range comments {
		if line.Marker == ';' && line.Range.Start.Line > first && line.Range.Start.Line < next {
			lines = append(lines, line.Text)
		}
	}
	return lines
}

func ownedTexts(lines []ast.Comment) []string {
	var texts []string
	for _, line := range lines {
		texts = append(texts, line.Text)
	}
	return texts
}

var commentOwnershipBodies = []string{
	"    ; перед первым date:2024-01-02\n    расход:еда  5 USD  ; inline [2024-01-03]\n    ; под расходом\n    ; 日本 date:2024-01-04\n    b\n    ; под b\n",
	"    a  1 USD\n    b  2 USD\n    ; only b\n    c\n",
	"    a  1 USD\n    # hash comment\n    ; semicolon\n    * star\n    b\n",
	"    a  1 USD\n    b\n",
	"    ; one\n    ; two\n    a  1 USD\n    b\n",
}

func TestPostingCommentLines_AssignsEachLineToThePostingAboveIt(t *testing.T) {
	headers := map[string]string{
		"transaction": "2024-01-01 описание  ; header date:2024-01-09\n",
		"periodic":    "~ monthly  ; header\n",
		"auto":        "= расход\n",
	}
	for kind, header := range headers {
		for i, body := range commentOwnershipBodies {
			for _, newline := range []string{"\n", "\r\n"} {
				t.Run(fmt.Sprintf("%s %d %q", kind, i, newline), func(t *testing.T) {
					if kind == "auto" {
						body = strings.ReplaceAll(strings.ReplaceAll(body, "1 USD", "*1"), "5 USD", "*5")
					}
					input := textutil.NormalizeLineEndings(strings.ReplaceAll(header+body, "\n", newline))
					journal, errs := parser.Parse(input)
					require.Empty(t, errs)

					var postings []ast.Posting
					var comments []ast.Comment
					switch kind {
					case "transaction":
						require.Len(t, journal.Transactions, 1)
						postings, comments = journal.Transactions[0].Postings, journal.Transactions[0].Comments
					case "periodic":
						require.Len(t, journal.PeriodicTransactions, 1)
						postings, comments = journal.PeriodicTransactions[0].Postings, journal.PeriodicTransactions[0].Comments
					case "auto":
						require.Len(t, journal.AutoPostingRules, 1)
						postings, comments = journal.AutoPostingRules[0].Postings, journal.AutoPostingRules[0].Comments
					}

					owned := postingCommentLines(postings, comments)
					require.Len(t, owned, len(postings))
					for p := range postings {
						assert.Equal(t, ownedLinesOneByOne(postings, comments, p), ownedTexts(owned[p]), "posting %d", p)
					}
				})
			}
		}
	}
}

func TestPostingCommentLines_FirstBodyOwnership(t *testing.T) {
	journal, errs := parser.Parse("2024-01-01 t\n" + commentOwnershipBodies[0])
	require.Empty(t, errs)
	tx := journal.Transactions[0]
	owned := postingCommentLines(tx.Postings, tx.Comments)
	require.Len(t, owned, 2)
	require.Len(t, owned[0], 2)
	assert.Contains(t, owned[0][0].Text, "под расходом")
	assert.Contains(t, owned[0][1].Text, "日本")
	require.Len(t, owned[1], 1)
	assert.Contains(t, owned[1][0].Text, "под b")
}

func TestPostingCommentLines_UnsortedCommentsAreStillAssigned(t *testing.T) {
	journal, errs := parser.Parse("2024-01-01 t\n    a  1 USD\n    ; under a\n    b\n    ; under b\n")
	require.Empty(t, errs)
	tx := journal.Transactions[0]
	reversed := []ast.Comment{tx.Comments[1], tx.Comments[0]}
	owned := postingCommentLines(tx.Postings, reversed)
	assert.Equal(t, []string{ownedTexts(tx.Comments)[0]}, ownedTexts(owned[0]))
	assert.Equal(t, []string{ownedTexts(tx.Comments)[1]}, ownedTexts(owned[1]))
}

// BenchmarkAnalyze_ManyPostingComments analyses one transaction whose postings
// each carry a comment line with a bracketed date. Ownership is worked out once
// per transaction, so the time grows with the size of the transaction, not with
// its square.
func BenchmarkAnalyze_ManyPostingComments(b *testing.B) {
	for _, n := range []int{500, 2000} {
		var text strings.Builder
		text.WriteString("2024-01-01 t\n")
		for i := 0; i < n; i++ {
			fmt.Fprintf(&text, "    a%d  1 USD\n    ; note [2024-01-05]\n", i)
		}
		text.WriteString("    z\n")
		journal, errs := parser.Parse(text.String())
		require.Empty(b, errs)
		b.Run(fmt.Sprintf("postings=%d", n), func(b *testing.B) {
			for b.Loop() {
				New().Analyze(journal)
			}
		})
	}
}
