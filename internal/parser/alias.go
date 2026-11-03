package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// accountAlias is an alias directive in force: a basic alias, which renames an
// account and its subaccounts, or a regular expression alias, which replaces
// every match in an account name.
type accountAlias struct {
	original    string // the account, or the regular expression
	replacement string
	regex       bool
}

// aliasRegexps caches compiled alias expressions by their text, so that a
// context holding aliases stays comparable and cheap to copy.
var aliasRegexps sync.Map // string → *regexp.Regexp

// compileAliasRegex compiles an alias expression the way hledger does: case
// insensitive, and leftmost-longest like the POSIX engine hledger uses.
func compileAliasRegex(pattern string) (*regexp.Regexp, error) {
	if cached, ok := aliasRegexps.Load(pattern); ok {
		return cached.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil, err
	}
	re.Longest()
	aliasRegexps.Store(pattern, re)
	return re, nil
}

// applyAliases rewrites an account name with the aliases in force, the most
// recent first, as hledger does. A regular expression alias can fail on a name
// whose match lacks a group its replacement refers to.
func applyAliases(aliases []accountAlias, name string) (string, error) {
	for i := len(aliases) - 1; i >= 0; i-- {
		alias := aliases[i]
		if !alias.regex {
			if name == alias.original {
				name = alias.replacement
			} else if strings.HasPrefix(name, alias.original+":") {
				name = alias.replacement + strings.TrimPrefix(name, alias.original)
			}
			continue
		}
		re, err := compileAliasRegex(alias.original)
		if err != nil {
			continue // reported where the directive is parsed
		}
		replaced, err := regexReplace(re, alias.replacement, name)
		if err != nil {
			return name, err
		}
		name = replaced
	}
	return name, nil
}

// regexReplace replaces every match of re in s with replacement, in which \N
// stands for group N of the match and any other backslash is kept as written.
//
// One difference from hledger remains for expressions that can match nothing:
// Go skips an empty match right after a match, hledger's engine does not, so
// /x*/ = - turns "ex" into "-e-" here and "-e--" there.
func regexReplace(re *regexp.Regexp, replacement, s string) (string, error) {
	matches := re.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return s, nil
	}
	var b strings.Builder
	last := 0
	for _, match := range matches {
		b.WriteString(s[last:match[0]])
		for i := 0; i < len(replacement); i++ {
			if replacement[i] != '\\' {
				b.WriteByte(replacement[i])
				continue
			}
			end := i + 1
			for end < len(replacement) && replacement[end] >= '0' && replacement[end] <= '9' {
				end++
			}
			if end == i+1 {
				b.WriteByte('\\')
				continue
			}
			group, err := strconv.Atoi(replacement[i+1 : end])
			if err != nil || group > re.NumSubexp() {
				return s, fmt.Errorf("no match group exists for backreference \"%s\"", replacement[i:end])
			}
			if match[2*group] >= 0 {
				b.WriteString(s[match[2*group]:match[2*group+1]])
			}
			i = end - 1
		}
		last = match[1]
	}
	b.WriteString(s[last:])
	return b.String(), nil
}

// parseAliasText reads what follows `alias ` on a directive line with hledger's
// grammar. A regular expression alias is /REGEX/ = REPLACEMENT, where \/ is a
// slash inside the expression and the replacement is the rest of the line as
// written; a basic alias is OLD = NEW, both trimmed at the end. Neither form has
// a comment: a ; is part of the name. regexStart and regexEnd are the byte
// offsets of the expression in text, for its error.
func parseAliasText(text string) (alias accountAlias, regexStart, regexEnd int, problem string) {
	if strings.HasPrefix(text, "/") {
		var expression strings.Builder
		i := 1
		closed := false
		for i < len(text) {
			c := text[i]
			if c == '/' {
				closed = true
				break
			}
			if c == '\\' && i+1 < len(text) {
				expression.WriteString(text[i : i+2])
				i += 2
				continue
			}
			expression.WriteByte(c)
			i++
		}
		regexStart, regexEnd = 1, i
		if !closed || expression.Len() == 0 {
			return accountAlias{}, regexStart, regexEnd, "expected /REGEX/ = REPLACEMENT in alias directive"
		}
		rest := strings.TrimLeft(text[i+1:], " \t")
		if !strings.HasPrefix(rest, "=") {
			return accountAlias{}, regexStart, regexEnd, "expected = in alias directive"
		}
		alias = accountAlias{original: expression.String(), replacement: strings.TrimLeft(rest[1:], " \t"), regex: true}
		if _, err := compileAliasRegex(alias.original); err != nil {
			return accountAlias{}, regexStart, regexEnd, "this regular expression is invalid or unsupported: " + alias.original
		}
		return alias, regexStart, regexEnd, ""
	}

	original, replacement, ok := strings.Cut(text, "=")
	original = strings.TrimRight(original, " \t")
	if !ok || original == "" {
		return accountAlias{}, 0, len(text), "expected = in alias directive"
	}
	return accountAlias{original: original, replacement: strings.TrimRight(strings.TrimLeft(replacement, " \t"), " \t")}, 0, 0, ""
}
