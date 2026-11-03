package csvimport

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/juev/hledger-lsp/internal/textutil"
)

// Static limits checked before hledger runs. The binary has no memory cap the
// server can set (the stable release refuses GHC runtime options), so input
// that could make it allocate without bound is refused here instead.
const (
	// maxRulesBytes bounds the rules text hledger inlines, counting a file once
	// per include of it: repeated includes expand, and depth alone does not
	// bound that.
	maxRulesBytes = 1 << 20
	// maxRulesIncludes bounds the include edges of a rules tree, repeats counted.
	maxRulesIncludes = 256
	maxRulesDepth    = 16
	// maxSourceMatches bounds the files a `source` glob may name.
	maxSourceMatches = 64
	// maxExponent is the largest decimal exponent accepted in data or rules:
	// hledger expands 1E999999999 into an integer with a billion digits.
	maxExponent = 99
	// maxDigitRun is the longest run of digits accepted.
	maxDigitRun = 255
)

// Request names one included file to convert.
type Request struct {
	Reader Reader
	// Path is the absolute include target: the data file for the csv, ssv and
	// tsv readers, the rules file for the rules reader.
	Path string
	// MainJournal is the root journal of the load, whose data/ directory
	// hledger searches for a source given by a bare or relative name.
	MainJournal string
	MaxFileSize int64
	// Buffer returns the unsaved editor content of path, if an editor holds it.
	// The import always reads the saved files; a differing buffer is reported.
	Buffer func(path string) (string, bool)
}

// plan is everything decided about a conversion before hledger runs.
type plan struct {
	dir  string
	file string // the -f argument
	// rules, when set, is passed as --rules: the data file was found by the
	// server in data/, so hledger is given it with the rules that convert it
	// instead of searching for it again from the rules file.
	rules string
	// data maps every data file hledger may read to the text its positions are
	// counted in (CoordinateText), for locating the transactions it reports.
	data map[string]string
	// sources lists every file or glob the conversion depends on, including
	// ones that do not exist yet, in the order they were found.
	sources  []string
	warnings []string
	// material identifies the inputs: equal material means hledger reads the
	// same bytes.
	material string
}

// planError is a refusal found before running hledger. The plan built so far
// still carries the sources, so a later change to them is noticed.
type planError struct{ message string }

func (e *planError) Error() string { return e.message }

type planner struct {
	request  Request
	plan     *plan
	hash     *strings.Builder
	seen     map[string]bool
	inlined  int
	includes int
}

func buildPlan(request Request) (*plan, error) {
	p := &plan{dir: filepath.Dir(request.Path), data: make(map[string]string)}
	b := &planner{request: request, plan: p, hash: &strings.Builder{}, seen: make(map[string]bool)}
	fmt.Fprintf(b.hash, "reader=%s\x00target=%s\x00max=%d\x00main=%s\x00", request.Reader, request.Path, request.MaxFileSize, request.MainJournal)
	err := b.build()
	p.material = b.hash.String()
	return p, err
}

func (b *planner) build() error {
	request := b.request
	switch request.Reader {
	case ReaderCSV, ReaderSSV, ReaderTSV:
		b.plan.file = string(request.Reader) + ":" + request.Path
		if err := b.readData(request.Path); err != nil {
			return err
		}
		rulesPath := request.Path + ".rules"
		// hledger ignores `source` when it is given the data file itself
		// (checked on 1.52.4 and 1.99.5), but a source command is still refused.
		if _, err := b.scanRules(rulesPath, nil); err != nil {
			return err
		}
		return nil
	case ReaderRules:
		b.plan.file = request.Path
		sources, err := b.scanRules(request.Path, nil)
		if err != nil {
			return err
		}
		if len(sources) == 0 {
			return &planError{fmt.Sprintf("%s has no source rule naming its data file", filepath.Base(request.Path))}
		}
		// For most rules the last declaration wins.
		return b.readSource(sources[len(sources)-1])
	}
	return &planError{fmt.Sprintf("unknown reader %q", request.Reader)}
}

// sourceRule is a `source` rule and the rules file that declares it.
type sourceRule struct {
	pattern string
	in      string
}

// scanRules reads a rules file and everything it includes, as hledger inlines
// them, and returns the `source` rules in order. It refuses what the server
// cannot run safely or reproduce: a shell command, a search outside the rules
// file's directory, unbounded expansion.
func (b *planner) scanRules(path string, stack []string) ([]sourceRule, error) {
	b.addSource(path)
	for _, active := range stack {
		if active == path {
			return nil, &planError{fmt.Sprintf("rules file %s includes itself", filepath.Base(path))}
		}
	}
	if len(stack) >= maxRulesDepth {
		return nil, &planError{fmt.Sprintf("rules includes nest deeper than %d", maxRulesDepth)}
	}
	content, err := b.readFile(path, "rules file")
	if err != nil {
		return nil, err
	}
	b.inlined += len(content)
	if b.inlined > maxRulesBytes {
		return nil, &planError{fmt.Sprintf("rules files expand to more than %d bytes", maxRulesBytes)}
	}

	stack = append(stack, path)
	var sources []sourceRule
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.ContainsRune("#;*", rune(trimmed[0])) {
			continue
		}
		keyword, argument := trimmed, ""
		if end := strings.IndexAny(trimmed, " \t"); end >= 0 {
			keyword, argument = trimmed[:end], strings.TrimSpace(trimmed[end:])
		}
		switch keyword {
		case "source":
			if strings.Contains(argument, "|") {
				return nil, &planError{fmt.Sprintf("%s: a source rule that runs a command is not run by the language server", filepath.Base(path))}
			}
			sources = append(sources, sourceRule{pattern: argument, in: path})
		case "include":
			if b.includes++; b.includes > maxRulesIncludes {
				return nil, &planError{fmt.Sprintf("rules files include more than %d files", maxRulesIncludes)}
			}
			target, err := resolveRulesInclude(path, argument)
			if err != nil {
				return nil, err
			}
			included, err := b.scanRules(target, stack)
			if err != nil {
				return nil, err
			}
			sources = append(sources, included...)
		}
	}
	return sources, nil
}

func resolveRulesInclude(from, argument string) (string, error) {
	switch {
	case argument == "":
		return "", &planError{fmt.Sprintf("%s: include without a file", filepath.Base(from))}
	case strings.HasPrefix(argument, "~"):
		return "", &planError{fmt.Sprintf("%s: a rules include under ~ is not supported", filepath.Base(from))}
	case strings.ContainsAny(argument, "*?[{"):
		return "", &planError{fmt.Sprintf("%s: a rules include with a glob pattern is not supported", filepath.Base(from))}
	}
	if !filepath.IsAbs(argument) {
		argument = filepath.Join(filepath.Dir(from), argument)
	}
	return filepath.Clean(argument), nil
}

// readSource resolves the data a rules file names, as hledger 1.99.5 does: an
// absolute path as written, ~/ under the home directory, ./ and ../ next to
// the rules file, and any other name in a data/ directory next to the main
// journal. hledger then also searches ~/Downloads, which the language server
// does not read, so a name not found in data/ is reported instead.
func (b *planner) readSource(rule sourceRule) error {
	pattern := rule.pattern
	inData := false
	switch {
	case pattern == "":
		return &planError{fmt.Sprintf("%s: source rule without a file", filepath.Base(rule.in))}
	case filepath.IsAbs(pattern):
	case pattern == "~" || strings.HasPrefix(pattern, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return &planError{fmt.Sprintf("%s: source %q needs the home directory: %v", filepath.Base(rule.in), rule.pattern, err)}
		}
		pattern = filepath.Join(home, strings.TrimPrefix(pattern, "~"))
	case strings.HasPrefix(pattern, "~"):
		return &planError{fmt.Sprintf("%s: source %q names another user's home directory, which is not supported", filepath.Base(rule.in), rule.pattern)}
	case strings.HasPrefix(pattern, "./"), strings.HasPrefix(pattern, "../"):
		pattern = filepath.Join(filepath.Dir(rule.in), pattern)
	default:
		if b.request.MainJournal == "" {
			return &planError{fmt.Sprintf("%s: source %q is searched for next to the main journal, which this load has none of", filepath.Base(rule.in), rule.pattern)}
		}
		pattern = filepath.Join(filepath.Dir(b.request.MainJournal), "data", pattern)
		inData = true
	}
	pattern = filepath.Clean(pattern)
	if inData {
		return b.readDataSource(rule, pattern)
	}
	b.addSource(pattern)
	fmt.Fprintf(b.hash, "source=%s\x00", pattern)

	if !strings.ContainsAny(pattern, "*?[{") {
		// A missing source file is read as empty data, as hledger does.
		if _, err := os.Lstat(pattern); os.IsNotExist(err) {
			b.hash.WriteString("missing\x00")
			return nil
		}
		return b.readData(pattern)
	}
	matches, err := doublestar.FilepathGlob(pattern)
	if err != nil {
		return &planError{fmt.Sprintf("%s: invalid source pattern: %v", filepath.Base(rule.in), err)}
	}
	if len(matches) > maxSourceMatches {
		return &planError{fmt.Sprintf("%s: source pattern matches more than %d files", filepath.Base(rule.in), maxSourceMatches)}
	}
	sort.Strings(matches)
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			return &planError{fmt.Sprintf("cannot read %s: %v", filepath.Base(match), err)}
		}
		// hledger reads the most recently modified match, so the times are part
		// of what it reads.
		fmt.Fprintf(b.hash, "mtime=%d\x00", info.ModTime().UnixNano())
		if err := b.readData(filepath.Clean(match)); err != nil {
			return err
		}
	}
	return nil
}

// readDataSource reads a source found in data/ next to the main journal: the
// file, or the newest match of a pattern, which hledger is then given itself.
func (b *planner) readDataSource(rule sourceRule, pattern string) error {
	b.addSource(pattern)
	fmt.Fprintf(b.hash, "datasource=%s\x00", pattern)
	notFound := &planError{fmt.Sprintf("%s: source %q is not in %s; hledger would look in ~/Downloads next, which the language server does not read", filepath.Base(rule.in), rule.pattern, filepath.Dir(pattern))}

	path := pattern
	if strings.ContainsAny(pattern, "*?[{") {
		matches, err := doublestar.FilepathGlob(pattern)
		if err != nil {
			return &planError{fmt.Sprintf("%s: invalid source pattern: %v", filepath.Base(rule.in), err)}
		}
		if len(matches) > maxSourceMatches {
			return &planError{fmt.Sprintf("%s: source pattern matches more than %d files", filepath.Base(rule.in), maxSourceMatches)}
		}
		path = ""
		var newest time.Time
		sort.Strings(matches)
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil {
				return &planError{fmt.Sprintf("cannot read %s: %v", filepath.Base(match), err)}
			}
			fmt.Fprintf(b.hash, "mtime=%s:%d\x00", match, info.ModTime().UnixNano())
			if path == "" || info.ModTime().After(newest) {
				path, newest = match, info.ModTime()
			}
		}
		if path == "" {
			return notFound
		}
	} else if _, err := os.Lstat(path); os.IsNotExist(err) {
		return notFound
	}
	path = filepath.Clean(path)
	if err := b.readData(path); err != nil {
		return err
	}
	b.plan.file = dataReaderFor(path) + ":" + path
	b.plan.rules = b.request.Path
	return nil
}

// dataReaderFor names the reader hledger is given a data file with: by its
// extension, CSV otherwise.
func dataReaderFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ssv":
		return string(ReaderSSV)
	case ".tsv":
		return string(ReaderTSV)
	}
	return string(ReaderCSV)
}

func (b *planner) readData(path string) error {
	b.addSource(path)
	content, err := b.readFile(path, "data file")
	if err != nil {
		return err
	}
	// The number guard and the record positions both need the decoded text,
	// so data that cannot be decoded is not imported at all.
	text, ok := CoordinateText(content)
	if !ok {
		return &planError{fmt.Sprintf("data file %s is in an encoding the language server cannot read (UTF-32, or UTF-16 without a byte order mark)", filepath.Base(path))}
	}
	if problem := numberProblem(text); problem != "" {
		return &planError{fmt.Sprintf("data file %s: %s", filepath.Base(path), problem)}
	}
	b.plan.data[path] = text
	return nil
}

// readFile reads a regular file within the size limit, checks its numbers and
// records its hash and any differing editor buffer.
func (b *planner) readFile(path, what string) (string, error) {
	limit := b.request.MaxFileSize
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			b.noteBuffer(path, "")
			return "", &planError{fmt.Sprintf("%s %s not found", what, filepath.Base(path))}
		}
		return "", &planError{fmt.Sprintf("cannot read %s %s: %v", what, filepath.Base(path), err)}
	}
	if !info.Mode().IsRegular() {
		return "", &planError{fmt.Sprintf("%s %s is not a regular file", what, filepath.Base(path))}
	}
	if limit > 0 && info.Size() > limit {
		return "", &planError{fmt.Sprintf("%s %s is too large: %d bytes (max %d)", what, filepath.Base(path), info.Size(), limit)}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", &planError{fmt.Sprintf("cannot read %s %s: %v", what, filepath.Base(path), err)}
	}
	if limit > 0 && int64(len(raw)) > limit {
		return "", &planError{fmt.Sprintf("%s %s is too large: %d bytes (max %d)", what, filepath.Base(path), len(raw), limit)}
	}
	content := string(raw)
	if !b.seen[path] {
		b.seen[path] = true
		sum := sha256.Sum256(raw)
		fmt.Fprintf(b.hash, "file=%s\x00%s\x00", path, hex.EncodeToString(sum[:]))
		b.noteBuffer(path, content)
	}
	if problem := numberProblem(content); problem != "" {
		return "", &planError{fmt.Sprintf("%s %s: %s", what, filepath.Base(path), problem)}
	}
	return content, nil
}

func (b *planner) noteBuffer(path, disk string) {
	if b.request.Buffer == nil {
		return
	}
	buffer, open := b.request.Buffer(path)
	if !open || textutil.NormalizeLineEndings(buffer) == textutil.NormalizeLineEndings(disk) {
		return
	}
	b.plan.warnings = append(b.plan.warnings, fmt.Sprintf("%s has unsaved changes; the import reads the saved file", filepath.Base(path)))
}

func (b *planner) addSource(path string) {
	for _, existing := range b.plan.sources {
		if existing == path {
			return
		}
	}
	b.plan.sources = append(b.plan.sources, path)
}

// numberProblem reports a number the static checks refuse: a long run of
// digits or a decimal exponent past maxExponent. It looks at the raw text, so
// it also refuses such text in a description; that costs a false refusal, never
// a run that exhausts memory.
func numberProblem(content string) string {
	run := 0
	for i := 0; i < len(content); i++ {
		c := content[i]
		if c >= '0' && c <= '9' {
			if run++; run > maxDigitRun {
				return fmt.Sprintf("a number longer than %d digits is not imported", maxDigitRun)
			}
			continue
		}
		if run > 0 && (c == 'e' || c == 'E') {
			j := i + 1
			if j < len(content) && (content[j] == '+' || content[j] == '-') {
				j++
			}
			exponent := 0
			for ; j < len(content) && content[j] >= '0' && content[j] <= '9'; j++ {
				if exponent = exponent*10 + int(content[j]-'0'); exponent > maxExponent {
					return fmt.Sprintf("a number with an exponent above %d is not imported", maxExponent)
				}
			}
		}
		run = 0
	}
	return ""
}
