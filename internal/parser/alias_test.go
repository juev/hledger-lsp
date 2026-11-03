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
	"github.com/juev/hledger-lsp/internal/textutil"
)

// aliasJournals are alias directives with the postings they rename. hledger
// 1.52.4 reads every one of them; the ones it refuses are in aliasErrors.
var aliasJournals = []string{
	"alias /food$/ = еда\n",
	"alias /^exp/ = расходы\n",
	"alias /CHECK/ = x:y\n",
	"alias /ing$/ = ING \n",
	"alias /(ch)(eck)/ = \\2\\1\n",
	"alias /a\\/b/ = z\n",
	"alias /e/ = E\n",
	"alias /c/ = C\n",
	"alias /[:]/ = .\n",
	"alias /^(.*):(.*)$/ = \\2:\\1\n",
	"alias /\\\\/ = backslash\n",
	"alias /o/ = \\o\n",
	"alias checking = assets:bank  ; note\n",
	"alias checking=assets:bank\n",
	"alias expenses = расходы\nalias /^расходы/ = траты\n",
	"alias /^expenses/ = расходы\nalias расходы = траты\n",
	"alias /現金/ = cash\n",
	"alias /food/ = еда\nend aliases\n",
}

// aliasErrors are alias directives hledger 1.52.4 refuses to load.
var aliasErrors = []string{
	"alias /c(h)/ = \\2\n",
	"alias /ch[/ = z\n",
	"alias // = z\n",
	"alias /unclosed = z\n",
	"alias /x/ z\n",
	"alias nothing\n",
}

const aliasPostings = "2024-01-01 t\n    expenses:food  5 USD\n    checking  1 USD\n    a/b  1 USD\n    資産:現金:food  1 USD\n    assets\n"

func ourAccounts(t *testing.T, input string) ([]string, bool) {
	t.Helper()
	journal, errs := Parse(input)
	if len(errs) > 0 {
		return nil, false
	}
	var accounts []string
	for _, posting := range journal.Transactions[0].Postings {
		accounts = append(accounts, posting.Account.ResolvedName)
	}
	return accounts, true
}

func hledgerAccounts(t *testing.T, hledgerPath, input string) ([]string, bool) {
	t.Helper()
	cmd := exec.Command(hledgerPath, "-f", "-", "print", "-O", "json")
	cmd.Stdin = strings.NewReader(input)
	output, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	var transactions []struct {
		Postings []struct {
			Account string `json:"paccount"`
		} `json:"tpostings"`
	}
	require.NoError(t, json.Unmarshal(output, &transactions))
	var accounts []string
	for _, posting := range transactions[0].Postings {
		accounts = append(accounts, posting.Account)
	}
	return accounts, true
}

func TestOracle_AliasesMatchHledger(t *testing.T) {
	hledgerPath := testutil.RequireHledger(t)
	for _, directives := range append(append([]string(nil), aliasJournals...), aliasErrors...) {
		t.Run(strings.TrimSpace(directives), func(t *testing.T) {
			input := directives + "\n" + aliasPostings
			want, wantOK := hledgerAccounts(t, hledgerPath, input)
			got, gotOK := ourAccounts(t, input)
			require.Equal(t, wantOK, gotOK, "accepted by hledger: %v; journal:\n%s", wantOK, input)
			assert.Equal(t, want, got)
		})
	}
}

func TestParse_RegexAliases(t *testing.T) {
	for directives, want := range map[string][]string{
		"alias /food$/ = еда\n":              {"expenses:еда", "checking", "a/b", "資産:現金:еда", "assets"},
		"alias /(ch)(eck)/ = \\2\\1\n":       {"expenses:food", "eckching", "a/b", "資産:現金:food", "assets"},
		"alias /CHECK/ = x:y\n":              {"expenses:food", "x:ying", "a/b", "資産:現金:food", "assets"},
		"alias /a\\/b/ = z\n":                {"expenses:food", "checking", "z", "資産:現金:food", "assets"},
		"alias /現金/ = cash\n":                {"expenses:food", "checking", "a/b", "資産:cash:food", "assets"},
		"alias /food/ = еда\nend aliases\n":  {"expenses:food", "checking", "a/b", "資産:現金:food", "assets"},
		"alias /^exp/ = x\nalias /^x/ = y\n": {"xenses:food", "checking", "a/b", "資産:現金:food", "assets"},
	} {
		for _, newline := range []string{"\n", "\r\n"} {
			input := textutil.NormalizeLineEndings(strings.ReplaceAll(directives+"\n"+aliasPostings, "\n", newline))
			got, ok := ourAccounts(t, input)
			require.True(t, ok, "%q", input)
			assert.Equal(t, want, got, "%q", directives)
		}
	}
}

func TestParse_AliasErrorsAreReported(t *testing.T) {
	for directive, message := range map[string]string{
		"alias /ch[/ = z\n":    "this regular expression is invalid or unsupported: ch[",
		"alias // = z\n":       "expected /REGEX/ = REPLACEMENT",
		"alias /x/ z\n":        "expected = in alias directive",
		"alias nothing\n":      "expected = in alias directive",
		"alias /c(h)/ = \\2\n": `problem in account alias applied to checking: no match group exists for backreference "\2"`,
	} {
		_, errs := Parse(directive + "\n" + aliasPostings)
		require.NotEmpty(t, errs, "%q", directive)
		assert.Contains(t, errs[0].Message, message, "%q", directive)
	}

	_, errs := Parse("alias /ch[/ = z\n")
	require.Len(t, errs, 1)
	assert.Equal(t, 1, errs[0].Pos.Line)
	assert.Equal(t, 8, errs[0].Pos.Column, "the error covers the expression")
	assert.Equal(t, 11, errs[0].End.Column)
}

func TestParser_RegexAliasDirective(t *testing.T) {
	journal, errs := Parse("alias /foo/ = bar\n")
	require.Empty(t, errs)
	require.Len(t, journal.Directives, 1)
	directive, ok := journal.Directives[0].(ast.AliasDirective)
	require.True(t, ok)
	assert.Equal(t, "foo", directive.Original)
	assert.Equal(t, "bar", directive.Alias)
	assert.True(t, directive.IsRegex)
}
