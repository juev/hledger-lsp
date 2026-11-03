// Package fmtcmd implements the hledger-fmt command line: it formats journal
// files with the same formatter the language server uses for
// textDocument/formatting.
package fmtcmd

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/juev/hledger-lsp/internal/csvimport"
	"github.com/juev/hledger-lsp/internal/filetype"
	"github.com/juev/hledger-lsp/internal/formatter"
	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/lsputil"
	"github.com/juev/hledger-lsp/internal/textutil"
)

// Exit codes. ExitUnformatted is only returned with --check.
const (
	ExitOK          = 0
	ExitUnformatted = 1
	ExitError       = 2
)

const usage = `Usage: hledger-fmt [flags] [file ...]

Formats hledger journal files. With no files, or with "-", reads standard input.
Without -w or -c, the formatted journal is written to standard output.

Exit status: 0 on success, 1 if --check found unformatted files, 2 on error.

Flags:
`

// stdinName labels standard input in messages. stdinFile is the path it is
// treated as for resolving relative include directives.
const (
	stdinName = "<stdin>"
	stdinFile = "stdin.journal"
)

// DefaultOptions returns the options the language server formats with when the
// client configures nothing, so both produce the same output out of the box.
func DefaultOptions() formatter.Options {
	opts := formatter.DefaultOptions()
	opts.AmountAlignmentMode = "right"
	return opts
}

type config struct {
	write bool
	check bool
	root  string
	opts  formatter.Options
}

type runner struct {
	cfg         config
	loader      *include.Loader
	rootJournal *include.ResolvedJournal
	stdin       io.Reader
	stdout      io.Writer
	stderr      io.Writer
}

// Run executes hledger-fmt with args (excluding the program name) and returns
// the process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cfg, files, err := parseArgs(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return ExitOK
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "hledger-fmt: %v\n", err)
		return ExitError
	}

	r := &runner{cfg: cfg, loader: include.NewLoader(), stdin: stdin, stdout: stdout, stderr: stderr}
	if cfg.root != "" {
		var loadErrs []include.LoadError
		r.rootJournal, loadErrs = r.loader.Load(cfg.root)
		if r.rootJournal == nil {
			for _, loadErr := range loadErrs {
				_, _ = fmt.Fprintf(stderr, "hledger-fmt: %s: %s\n", cfg.root, loadErr.Message)
			}
			return ExitError
		}
		r.warn(loadErrs, "")
	}

	code := ExitOK
	for _, file := range files {
		changed, err := r.processFile(file)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			code = ExitError
			continue
		}
		if changed && cfg.check && code == ExitOK {
			code = ExitUnformatted
		}
	}
	return code
}

func parseArgs(args []string, stderr io.Writer) (config, []string, error) {
	cfg := config{opts: DefaultOptions()}
	noAlign := false

	fs := flag.NewFlagSet("hledger-fmt", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
	}
	fs.BoolVar(&cfg.write, "w", false, "write the result back to each file instead of standard output")
	fs.BoolVar(&cfg.write, "write", false, "same as -w")
	fs.BoolVar(&cfg.check, "c", false, "list unformatted files and exit 1 if there are any; change nothing")
	fs.BoolVar(&cfg.check, "check", false, "same as -c")
	fs.StringVar(&cfg.root, "root", "", "root `journal` that includes the files, for commodity directives declared outside them")
	fs.IntVar(&cfg.opts.IndentSize, "indent-size", cfg.opts.IndentSize, "number of spaces for posting indent")
	fs.BoolVar(&noAlign, "no-align", false, "do not align amounts across postings")
	fs.IntVar(&cfg.opts.MinAlignmentColumn, "min-alignment-column", 0, "minimum column for amount alignment, 0 for auto")
	fs.IntVar(&cfg.opts.AmountAlignmentColumn, "amount-alignment-column", 0, "fixed alignment column for the selected mode, 0 to disable")
	fs.StringVar(&cfg.opts.AmountAlignmentMode, "amount-alignment-mode", cfg.opts.AmountAlignmentMode, "alignment `mode`: left, right or decimal")
	fs.StringVar(&cfg.opts.AmountAlignmentTarget, "amount-alignment-target", formatter.AlignTargetCost, "amount that anchors cost notation: cost or posting")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return cfg, nil, err
		}
		// The flag package already reported the problem and printed usage.
		return cfg, nil, errors.New("invalid arguments")
	}
	cfg.opts.AlignAmounts = !noAlign

	if err := validate(cfg, fs.Args()); err != nil {
		return cfg, nil, err
	}

	files := fs.Args()
	if len(files) == 0 {
		files = []string{"-"}
	}
	return cfg, files, nil
}

func validate(cfg config, files []string) error {
	if cfg.write && cfg.check {
		return errors.New("-w and -c cannot be used together")
	}
	if cfg.opts.IndentSize <= 0 {
		return fmt.Errorf("--indent-size must be positive, got %d", cfg.opts.IndentSize)
	}
	if cfg.opts.MinAlignmentColumn < 0 {
		return fmt.Errorf("--min-alignment-column must not be negative, got %d", cfg.opts.MinAlignmentColumn)
	}
	if cfg.opts.AmountAlignmentColumn < 0 {
		return fmt.Errorf("--amount-alignment-column must not be negative, got %d", cfg.opts.AmountAlignmentColumn)
	}
	switch cfg.opts.AmountAlignmentMode {
	case "left", "right", "decimal":
	default:
		return fmt.Errorf("--amount-alignment-mode must be left, right or decimal, got %q", cfg.opts.AmountAlignmentMode)
	}
	switch cfg.opts.AmountAlignmentTarget {
	case formatter.AlignTargetCost, formatter.AlignTargetPosting:
	default:
		return fmt.Errorf("--amount-alignment-target must be cost or posting, got %q", cfg.opts.AmountAlignmentTarget)
	}

	stdin := len(files) == 0
	for _, file := range files {
		stdin = stdin || file == "-"
	}
	if cfg.write && stdin {
		return errors.New("-w cannot be used with standard input")
	}
	if cfg.root != "" && stdin {
		return errors.New("--root cannot be used with standard input")
	}
	if !cfg.write && !cfg.check && len(files) > 1 {
		return errors.New("multiple files require -w or -c")
	}
	return nil
}

// processFile formats one input and reports whether its formatting differs from
// what was read.
func (r *runner) processFile(file string) (bool, error) {
	name := file
	var path string
	var raw []byte
	var err error
	if file == "-" {
		name = stdinName
		if path, err = filepath.Abs(stdinFile); err != nil {
			return false, fmt.Errorf("%s: %w", name, err)
		}
		raw, err = io.ReadAll(r.stdin)
	} else {
		if filetype.IsRules(file) {
			return false, fmt.Errorf("%s: CSV rules files cannot be formatted", name)
		}
		if csvimport.IsDataPath(file) {
			return false, fmt.Errorf("%s: CSV data files cannot be formatted", name)
		}
		if path, err = filepath.Abs(file); err != nil {
			return false, fmt.Errorf("%s: %w", name, err)
		}
		raw, err = os.ReadFile(file)
	}
	if err != nil {
		return false, fmt.Errorf("hledger-fmt: %w", err)
	}

	original := string(raw)
	formatted, err := r.formatContent(name, path, original)
	if err != nil {
		return false, err
	}

	changed := formatted != original
	switch {
	case r.cfg.check:
		if changed {
			if _, err := fmt.Fprintln(r.stdout, name); err != nil {
				return false, fmt.Errorf("hledger-fmt: %w", err)
			}
		}
	case r.cfg.write:
		if changed {
			if err := replaceFile(file, formatted); err != nil {
				return false, fmt.Errorf("hledger-fmt: %w", err)
			}
		}
	default:
		if _, err := io.WriteString(r.stdout, formatted); err != nil {
			return false, fmt.Errorf("hledger-fmt: %w", err)
		}
	}
	return changed, nil
}

// formatContent returns original formatted, in its own line-ending style. A
// journal with parse errors is refused: unlike an editor buffer, there is no
// undo for a file rewritten from a partially understood journal.
func (r *runner) formatContent(name, path, original string) (string, error) {
	content := textutil.NormalizeLineEndings(original)

	ctx, err := r.formattingContext(name, path, content)
	if err != nil {
		return "", err
	}
	if len(ctx.ParseErrors) > 0 {
		messages := make([]string, 0, len(ctx.ParseErrors))
		for _, parseErr := range ctx.ParseErrors {
			messages = append(messages, fmt.Sprintf("%s:%d:%d: %s", name, parseErr.Pos.Line, parseErr.Pos.Column, parseErr.Message))
		}
		return "", errors.New(strings.Join(messages, "\n"))
	}

	edits := formatter.FormatDocumentInReadings(ctx.Readings, content, r.cfg.opts)
	formatted, err := lsputil.NewPositionMapper(content).ApplyEdits(edits)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return restoreLineEndings(formatted, original), nil
}

// formattingContext reads content within the root journal when one is given, and
// otherwise as the root of its own include tree.
func (r *runner) formattingContext(name, path, content string) (include.FormattingContext, error) {
	if r.rootJournal != nil {
		ctx, err := r.rootJournal.FormattingContext(path, content)
		if errors.Is(err, include.ErrNotIncluded) {
			return ctx, fmt.Errorf("%s: not included by the root journal", name)
		}
		if err != nil {
			return ctx, fmt.Errorf("%s: %w", name, err)
		}
		return ctx, nil
	}

	resolved, loadErrs := r.loader.LoadFromContent(path, content)
	if resolved == nil {
		messages := make([]string, 0, len(loadErrs))
		for _, loadErr := range loadErrs {
			messages = append(messages, fmt.Sprintf("%s: %s", name, loadErr.Message))
		}
		return include.FormattingContext{}, errors.New(strings.Join(messages, "\n"))
	}
	r.warn(loadErrs, path)

	ctx, err := resolved.FormattingContext(path, content)
	if errors.Is(err, include.ErrNotIncluded) {
		return ctx, fmt.Errorf("%s: could not be read", name)
	}
	if err != nil {
		return ctx, fmt.Errorf("%s: %w", name, err)
	}
	return ctx, nil
}

// warn reports include failures, except parse errors in the file being formatted,
// which refuse it instead. They do not stop formatting: a commodity declared in
// an unreadable file has no known style, so its amounts keep their written form.
func (r *runner) warn(loadErrs []include.LoadError, path string) {
	for _, loadErr := range loadErrs {
		if loadErr.Kind == include.ErrorParseError && loadErr.Path == path {
			continue
		}
		_, _ = fmt.Fprintf(r.stderr, "hledger-fmt: warning: %s: %s\n", loadErr.SourcePath, loadErr.Message)
	}
}

// restoreLineEndings gives LF-only formatted text the dominant line ending of
// original, so formatting a Windows journal does not rewrite every line.
func restoreLineEndings(formatted, original string) string {
	crlf := strings.Count(original, "\r\n")
	lf := strings.Count(original, "\n") - crlf
	if crlf > lf {
		return strings.ReplaceAll(formatted, "\n", "\r\n")
	}
	return formatted
}

// errKeepFile reports that renaming a new file over the target would lose
// something the target has, so it has to be written in place.
var errKeepFile = errors.New("the file has to be written in place")

// keptMode is the part of a file's mode that a write keeps: the permissions and
// the set-user-ID, set-group-ID and sticky bits.
const keptMode = os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky

// replaceFile replaces the contents of path. Symlinks are followed rather than
// replaced, and a file that cannot be written is refused, as by gofmt -w.
//
// The new contents are renamed over the file, so an interrupted run cannot
// truncate a journal. A rename makes a new file, though, so a file with other
// hard links, extended attributes, or an owner or mode the new file cannot be
// given is written in place instead, with a backup of its old contents kept
// until the write succeeds.
func replaceFile(path, content string) error {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	// Renaming only needs the directory to be writable. Opening the file asks
	// its own permissions, so a journal kept read-only stays untouched.
	f, err := os.OpenFile(target, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_ = f.Close()

	if hasOtherLinks(info) || mayHaveExtendedAttributes(target) {
		return writeInPlace(target, content, info)
	}
	err = renameOver(target, content, info)
	if errors.Is(err, errKeepFile) {
		return writeInPlace(target, content, info)
	}
	return err
}

// renameOver writes content to a temporary file next to target, with target's
// mode and owner, and renames it over target.
func renameOver(target, content string, info os.FileInfo) (err error) {
	tmp, err := createSibling(target, content, info)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	return os.Rename(tmp, target)
}

// writeInPlace overwrites target with content and restores the mode bits the
// write cleared. Its old contents are copied to a backup first, which is
// removed once the write succeeds and named in the error when it does not.
func writeInPlace(target, content string, info os.FileInfo) error {
	old, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	backup, err := createSibling(target, string(old), nil)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(target, os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err == nil {
		_, err = f.WriteString(content)
		if err == nil {
			err = restoreMode(f, info)
		}
		if syncErr := f.Sync(); err == nil {
			err = syncErr
		}
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		return fmt.Errorf("%w (the original contents are in %s)", err, backup)
	}
	return os.Remove(backup)
}

// restoreMode gives f the mode bits of info back. Writing to a file clears its
// set-user-ID bit, and its set-group-ID bit when it is group-executable.
func restoreMode(f *os.File, info os.FileInfo) error {
	now, err := f.Stat()
	if err != nil {
		return err
	}
	if now.Mode()&keptMode == info.Mode()&keptMode {
		return nil
	}
	return f.Chmod(info.Mode() & keptMode)
}

// createSibling writes content to a new file in target's directory and returns
// its name. With info, the file gets the owner and mode of target; errKeepFile
// means either could not be given.
func createSibling(target, content string, info os.FileInfo) (name string, err error) {
	tmp, err := os.CreateTemp(filepath.Dir(target), ".hledger-fmt-*")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	if _, err = tmp.WriteString(content); err != nil {
		return "", err
	}
	if info != nil {
		// The owner goes first: changing it can clear the set-ID bits.
		if err = keepOwner(tmp, info); err != nil {
			return "", err
		}
		if err = tmp.Chmod(info.Mode() & keptMode); err != nil {
			return "", err
		}
		// A set-ID bit the user may not set is dropped without an error.
		var created os.FileInfo
		if created, err = tmp.Stat(); err != nil {
			return "", err
		}
		if created.Mode()&keptMode != info.Mode()&keptMode {
			err = errKeepFile
			return "", err
		}
	}
	if err = tmp.Sync(); err != nil {
		return "", err
	}
	if err = tmp.Close(); err != nil {
		return "", err
	}
	return tmp.Name(), nil
}
