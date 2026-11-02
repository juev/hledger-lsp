package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/textutil"
)

func TestParse_LeadingBOMIsSkipped(t *testing.T) {
	cases := map[string]string{
		"ASCII":    "2024-01-01 shop\n    expenses:food  1 USD\n    assets:cash\n",
		"Cyrillic": "2024-01-01 кафе\n    расходы:еда  100 ₽\n    активы:наличные\n",
		"CJK":      "2024-01-01 食品店\n    支出:食品  100 JPY\n    資産:現金\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			plain, plainErrs := Parse(body)
			require.Empty(t, plainErrs)

			journal, errs := Parse(textutil.BOM + body)
			require.Empty(t, errs)
			require.Len(t, journal.Transactions, 1)
			tx := journal.Transactions[0]
			assert.Equal(t, plain.Transactions[0].Description, tx.Description)
			require.Len(t, tx.Postings, 2)
			assert.Equal(t, plain.Transactions[0].Postings[0].Account.Name, tx.Postings[0].Account.Name)

			// Offsets stay valid against the string the caller passed in.
			shift := len(textutil.BOM)
			assert.Equal(t, plain.Transactions[0].Range.Start.Offset+shift, tx.Range.Start.Offset)
			assert.Equal(t, plain.Transactions[0].Postings[0].Range.Start.Offset+shift, tx.Postings[0].Range.Start.Offset)
		})
	}
}

func TestParse_BOMBeforeCommentAndDirective(t *testing.T) {
	for _, first := range []string{"; header\n", "account assets:cash\n", "# note\n"} {
		journal, errs := Parse(textutil.BOM + first + "2024-01-01 x\n    a  1\n    b\n")
		require.Empty(t, errs, first)
		assert.Len(t, journal.Transactions, 1, first)
	}
}

func TestParse_BOMOnlyAtStartOfInput(t *testing.T) {
	_, errs := Parse("2024-01-01 a\n    e:x  1\n    a:y\n" + textutil.BOM + "2024-01-02 b\n    e:x  1\n    a:y\n")
	assert.NotEmpty(t, errs, "hledger rejects a mark that is not at the start of the file")
}

func TestParse_BOMOnlyInput(t *testing.T) {
	journal, errs := Parse(textutil.BOM)
	require.Empty(t, errs)
	assert.Empty(t, journal.Transactions)
}

// Columns count the mark like the client does, so line 1 starts one column in
// and offsets keep the mark's three bytes.
func TestParse_LeadingBOMColumnsMatchTheText(t *testing.T) {
	input := textutil.BOM + "2024-01-01 кафе\n    a  1\n    b\n"
	journal, errs := Parse(input)
	require.Empty(t, errs)
	tx := journal.Transactions[0]
	assert.Equal(t, 2, tx.Range.Start.Column)
	assert.Equal(t, len(textutil.BOM), tx.Range.Start.Offset)
	assert.Equal(t, 1, tx.Postings[0].Range.Start.Column-4, "later lines are not shifted")
	assert.Equal(t, 2, tx.Postings[0].Range.Start.Line)
}

// hledger accepts exactly one mark, so a second one is content.
func TestParse_DoubleBOMIsAnError(t *testing.T) {
	_, errs := Parse(textutil.BOM + textutil.BOM + "2024-01-01 x\n    a  5 RUB\n    b\n")
	assert.NotEmpty(t, errs)
}
