package csvimport

import (
	"container/list"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/cli"
)

const (
	// maxWorkers bounds the hledger processes running at once.
	maxWorkers = 2
	// maxJSONOutput and maxTextOutput bound what one conversion may print.
	maxJSONOutput = 64 << 20
	maxTextOutput = 16 << 20
	maxStderr     = 64 << 10
	// maxMessage bounds the hledger error text carried into a diagnostic.
	maxMessage = 2000
	// cacheEntries bounds the conversions kept.
	cacheEntries = 16
	// attempts is how often a conversion is retried when its inputs change
	// while hledger reads them.
	attempts = 2
)

// minimumVersion is the first hledger known to include CSV files from a
// journal; 1.52.4 refuses with "CSV files can't be included yet". Earlier 2.0
// previews were not checked, so they are not assumed to.
var minimumVersion = []int{1, 99, 5}

// Config is the CLI configuration an Importer runs with.
type Config struct {
	Enabled bool
	Path    string
	Timeout time.Duration
	// Background makes Import return at once when the conversion is not
	// cached: it starts hledger in the background and reports the result as
	// Pending. The language server uses it so that no request, and no rebuild
	// of the workspace under its lock, waits up to hledger.cli.timeout for a
	// subprocess. OnReady is called when such a conversion has finished, so the
	// caller can load again and find it cached.
	Background bool
	OnReady    func()
}

// Result is one conversion. Sources is filled even when the conversion
// failed, so the caller can watch the files and protect them from edits.
type Result struct {
	// DataPath is the file the transactions come from; empty when there are
	// none.
	DataPath     string
	Transactions []ast.Transaction
	Sources      []string
	// Err explains why there are no transactions; empty on success.
	Err string
	// Warnings are problems that do not stop the conversion.
	Warnings []string
	// Pending marks a conversion that runs in the background (Config.Background):
	// there are no transactions yet, and no verdict on the file either.
	Pending bool
}

// Importer runs conversions for one CLI configuration. It is safe for
// concurrent use.
type Importer struct {
	config  Config
	workers chan struct{}

	mu       sync.Mutex
	client   *cli.Client
	identity string
	cache    map[string]*list.Element
	lru      *list.List
	inflight map[string]*call
	// background holds the keys of the conversions started for Config.Background.
	background map[string]bool
}

type cacheEntry struct {
	key    string
	result Result
}

type call struct {
	done   chan struct{}
	result Result
	// shared marks a validated result the waiters may use.
	shared bool
}

func New(config Config) *Importer {
	return &Importer{
		config:     config,
		workers:    make(chan struct{}, maxWorkers),
		cache:      make(map[string]*list.Element),
		lru:        list.New(),
		inflight:   make(map[string]*call),
		background: make(map[string]bool),
	}
}

// Import converts the file request names. It never fails silently: every path
// that yields no transactions sets Err.
func (im *Importer) Import(ctx context.Context, request Request) Result {
	request.Path = filepath.Clean(request.Path)
	p, err := buildPlan(request)
	result := Result{Sources: p.sources, Warnings: p.warnings}
	if err != nil {
		result.Err = err.Error()
		return result
	}
	if !im.config.Enabled {
		result.Err = "including CSV files needs the hledger CLI, which is disabled (hledger.cli.enabled is false)"
		return result
	}
	if !cli.ProcessesSupported() {
		result.Err = "including CSV files needs the hledger CLI, which cannot be run on this platform (WASI has no subprocesses)"
		return result
	}
	client, identity := im.currentClient()
	if !client.Available() {
		result.Err = fmt.Sprintf("including CSV files needs the hledger CLI, which is not available at %q", im.config.Path)
		return result
	}
	version := client.Version()
	if !SupportsInclude(version) {
		if version == "" {
			version = "of unknown version"
		}
		result.Err = fmt.Sprintf("hledger %s cannot include CSV files; hledger 1.99.5 or newer is needed", version)
		return result
	}

	if im.config.Background {
		key := cacheKey(identity, version, im.config.Timeout, p.material)
		if converted, ok := im.cached(key); ok {
			return withPlan(converted, result)
		}
		im.startBackground(key, request)
		result.Pending = true
		return result
	}
	return im.convert(ctx, request, p, result, client, identity, version)
}

// cached returns the conversion stored under key.
func (im *Importer) cached(key string) (Result, bool) {
	im.mu.Lock()
	defer im.mu.Unlock()
	element, ok := im.cache[key]
	if !ok {
		return Result{}, false
	}
	im.lru.MoveToFront(element)
	return element.Value.(*cacheEntry).result, true
}

// startBackground converts request in the background, once per key, and calls
// OnReady when the conversion is done. The run is bounded by the CLI timeout,
// not by any request: the requests that asked for it have long been answered.
func (im *Importer) startBackground(key string, request Request) {
	im.mu.Lock()
	if im.background[key] {
		im.mu.Unlock()
		return
	}
	im.background[key] = true
	im.mu.Unlock()

	go func() {
		ctx := context.Background()
		if p, err := buildPlan(request); err == nil {
			client, identity := im.currentClient()
			if client.Available() {
				im.convert(ctx, request, p, Result{Sources: p.sources, Warnings: p.warnings}, client, identity, client.Version())
			}
		}
		im.mu.Lock()
		delete(im.background, key)
		im.mu.Unlock()
		if im.config.OnReady != nil {
			im.config.OnReady()
		}
	}()
}

// convert runs the conversion for a plan, waiting for hledger, and plans again
// when the inputs change while hledger reads them.
func (im *Importer) convert(ctx context.Context, request Request, p *plan, result Result, client *cli.Client, identity, version string) Result {
	var err error
	for attempt := 0; ; attempt++ {
		key := cacheKey(identity, version, im.config.Timeout, p.material)
		converted, outcome := im.convertOnce(ctx, key, client, p, request)
		if outcome != outcomeStale {
			return withPlan(converted, result)
		}
		if attempt+1 >= attempts {
			result.Err = "the files changed while hledger was reading them"
			return result
		}
		// The inputs moved under a conversion, ours or one we waited on: plan
		// again from the bytes on disk now.
		if p, err = buildPlan(request); err != nil {
			result.Sources, result.Warnings = p.sources, p.warnings
			result.Err = err.Error()
			return result
		}
		result.Sources, result.Warnings = p.sources, p.warnings
	}
}

func withPlan(converted, planned Result) Result {
	converted.Sources = planned.Sources
	converted.Warnings = planned.Warnings
	return converted
}

// outcome says what a conversion result is worth.
type outcome int

const (
	// outcomeValid is a result for exactly the bytes the plan checked.
	outcomeValid outcome = iota
	// outcomeTransient is a result that says nothing about the inputs, such as
	// a cancellation; it is returned but never shared.
	outcomeTransient
	// outcomeStale means the inputs changed while hledger read them: the
	// result is discarded and the caller must plan again.
	outcomeStale
)

// convertOnce returns the conversion for key, running hledger only when no
// cached or in-flight conversion has it. A result reaches the cache and the
// callers waiting on it only after the inputs were read again and found
// unchanged since the plan, so no one is ever served a conversion of bytes
// other than the ones key names.
func (im *Importer) convertOnce(ctx context.Context, key string, client *cli.Client, p *plan, request Request) (Result, outcome) {
	im.mu.Lock()
	if element, ok := im.cache[key]; ok {
		im.lru.MoveToFront(element)
		result := element.Value.(*cacheEntry).result
		im.mu.Unlock()
		return result, outcomeValid
	}
	if pending, ok := im.inflight[key]; ok {
		im.mu.Unlock()
		select {
		case <-pending.done:
		case <-ctx.Done():
			return Result{Err: "the import was cancelled"}, outcomeTransient
		}
		if pending.shared {
			return pending.result, outcomeValid
		}
		// The conversion went stale or was cancelled for its own caller; this
		// caller plans again rather than inherit either.
		return Result{}, outcomeStale
	}
	pending := &call{done: make(chan struct{})}
	im.inflight[key] = pending
	im.mu.Unlock()

	result, keep := im.run(ctx, client, p)
	verdict := outcomeTransient
	if keep {
		// hledger read the files after the plan did; a change in between means
		// the output may not describe the bytes the plan checked.
		verdict = outcomeStale
		if after, err := buildPlan(request); err == nil && after.material == p.material {
			verdict = outcomeValid
		}
	}

	im.mu.Lock()
	delete(im.inflight, key)
	if verdict == outcomeValid {
		im.cache[key] = im.lru.PushFront(&cacheEntry{key: key, result: result})
		for im.lru.Len() > cacheEntries {
			oldest := im.lru.Back()
			im.lru.Remove(oldest)
			delete(im.cache, oldest.Value.(*cacheEntry).key)
		}
	}
	pending.result, pending.shared = result, verdict == outcomeValid
	im.mu.Unlock()
	close(pending.done)
	return result, verdict
}

// run converts with two hledger processes, one per output, holding a worker
// slot for both. keep is false for an outcome that says nothing about the
// inputs: cancellation.
func (im *Importer) run(ctx context.Context, client *cli.Client, p *plan) (Result, bool) {
	select {
	case im.workers <- struct{}{}:
	case <-ctx.Done():
		return Result{Err: "the import was cancelled"}, false
	}
	defer func() { <-im.workers }()

	base := []string{"--no-conf", "--color=no", "-f", p.file, "print"}
	jsonOutput, err := client.RunBounded(ctx, cli.BoundedRun{Dir: p.dir, Args: append(append([]string(nil), base...), "-O", "json"), MaxStdout: maxJSONOutput, MaxStderr: maxStderr})
	if err != nil {
		return runFailure(ctx, err)
	}
	textOutput, err := client.RunBounded(ctx, cli.BoundedRun{Dir: p.dir, Args: base, MaxStdout: maxTextOutput, MaxStderr: maxStderr})
	if err != nil {
		return runFailure(ctx, err)
	}
	converted, err := convert(jsonOutput, textOutput, p.data, p.known)
	if err != nil {
		return Result{Err: err.Error()}, true
	}
	return Result{DataPath: converted.dataPath, Transactions: converted.transactions}, true
}

func runFailure(ctx context.Context, err error) (Result, bool) {
	var exitErr *cli.ExitError
	switch {
	case ctx.Err() != nil:
		return Result{Err: "the import was cancelled"}, false
	case errors.As(err, &exitErr):
		return Result{Err: "hledger could not convert the file: " + clip(exitErr.Stderr)}, true
	case errors.Is(err, cli.ErrOutputLimit):
		return Result{Err: "hledger's output exceeds the import limit"}, true
	case errors.Is(err, cli.ErrTimeout):
		return Result{Err: "hledger did not finish within hledger.cli.timeout"}, true
	}
	return Result{Err: "cannot run hledger: " + err.Error()}, true
}

// clip keeps hledger's message readable in a diagnostic: bounded, without
// control characters other than newlines.
func clip(message string) string {
	message = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= ' ' && r != 0x7f {
			return r
		}
		return -1
	}, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(message), "hledger: Error:")))
	if len(message) > maxMessage {
		cut := maxMessage
		for cut > 0 && message[cut]&0xC0 == 0x80 {
			cut--
		}
		message = message[:cut] + "…"
	}
	if message == "" {
		return "no error message"
	}
	return message
}

// currentClient returns the client for the binary the configured path names
// now. Replacing the binary yields a new client, so its version is probed
// again and nothing converted by the old one is reused.
func (im *Importer) currentClient() (*cli.Client, string) {
	identity := binaryIdentity(im.config.Path)
	im.mu.Lock()
	defer im.mu.Unlock()
	if im.client == nil || im.identity != identity {
		im.client = cli.NewClient(im.config.Path, im.config.Timeout)
		im.identity = identity
	}
	return im.client, identity
}

func binaryIdentity(path string) string {
	resolved, err := exec.LookPath(path)
	if err != nil {
		return "missing:" + path
	}
	if real, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = real
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "missing:" + resolved
	}
	return fmt.Sprintf("%s|%d|%d", resolved, info.Size(), info.ModTime().UnixNano())
}

func cacheKey(identity, version string, timeout time.Duration, material string) string {
	sum := sha256.Sum256([]byte(identity + "\x00" + version + "\x00" + timeout.String() + "\x00" + material))
	return string(sum[:])
}

// SupportsInclude reports whether hledger of version can include CSV files
// from a journal, as far as the server knows.
func SupportsInclude(version string) bool { return atLeast(version, minimumVersion) }

func atLeast(version string, minimum []int) bool {
	if version == "" {
		return false
	}
	parts := strings.Split(version, ".")
	for i, want := range minimum {
		got := 0
		if i < len(parts) {
			n, err := strconv.Atoi(parts[i])
			if err != nil {
				return false
			}
			got = n
		}
		if got != want {
			return got > want
		}
	}
	return true
}
