package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// defaultTimeout bounds the availability probe when no timeout is configured.
const defaultTimeout = 5 * time.Second

// maxVersionOutput bounds what the availability probe keeps of `--version`.
const maxVersionOutput = 4096

// waitDelay bounds how long a run waits for the output pipes after the process
// is killed: a child the binary started could otherwise hold them open.
const waitDelay = 2 * time.Second

var (
	// ErrUnavailable means the configured binary did not answer `--version`.
	ErrUnavailable = errors.New("hledger is not available")
	// ErrUnsupportedPlatform means this build cannot start processes at all.
	ErrUnsupportedPlatform = errors.New("running hledger is not supported on this platform")
	// ErrTimeout means the process ran past the configured timeout and was killed.
	ErrTimeout = errors.New("hledger timed out")
	// ErrOutputLimit means the process wrote more than the caller allowed and
	// was killed.
	ErrOutputLimit = errors.New("hledger output exceeds the limit")
)

// ExitError is a run that finished with a nonzero status.
type ExitError struct {
	Stderr string
	Err    error
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("hledger error: %s: %v", e.Stderr, e.Err)
}

func (e *ExitError) Unwrap() error { return e.Err }

// ProcessesSupported reports whether this build can run hledger at all.
func ProcessesSupported() bool { return processSupported }

type Client struct {
	path      string
	timeout   time.Duration
	probeOnce sync.Once
	available bool
	version   string
}

func NewClient(path string, timeout time.Duration) *Client {
	// The availability probe is deferred to the first Available call: the server
	// constructs a client while handling initialize, and a slow or hanging
	// hledger binary must not delay the handshake.
	return &Client{
		path:    path,
		timeout: timeout,
	}
}

// Path returns the configured binary.
func (c *Client) Path() string { return c.path }

// Timeout returns the per-process deadline.
func (c *Client) Timeout() time.Duration { return c.timeout }

func (c *Client) Available() bool {
	c.probeOnce.Do(func() {
		c.available, c.version = c.checkAvailable()
	})
	return c.available
}

// Version returns the version number the binary reported, such as "1.99.5",
// or "" when it is unavailable or printed something else.
func (c *Client) Version() string {
	c.Available()
	return c.version
}

func (c *Client) Run(ctx context.Context, file string, args ...string) (string, error) {
	if !c.available {
		return "", fmt.Errorf("hledger not available at path: %s", c.path)
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	cmdArgs := make([]string, 0, len(args)+2)
	if file != "" {
		cmdArgs = append(cmdArgs, "-f", file)
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.CommandContext(ctx, c.path, cmdArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("command timed out after %v", c.timeout)
		}
		if ctx.Err() == context.Canceled {
			return "", fmt.Errorf("command cancelled: %w", ctx.Err())
		}
		return stdout.String(), fmt.Errorf("hledger error: %s: %w", stderr.String(), err)
	}

	return stdout.String(), nil
}

// BoundedRun describes one run whose output must stay within limits.
type BoundedRun struct {
	// Dir is the working directory; empty means the server's own.
	Dir       string
	Args      []string
	MaxStdout int
	MaxStderr int
}

// RunBounded runs the binary with structured arguments, no shell and no
// standard input. The process is killed when it outlives the client's timeout,
// when ctx is cancelled, or when it writes more than the run allows; each case
// returns its own error so a caller never mistakes a cut-off output for a
// complete one. Available must have reported true first.
func (c *Client) RunBounded(ctx context.Context, run BoundedRun) ([]byte, error) {
	if !processSupported {
		return nil, ErrUnsupportedPlatform
	}
	if !c.Available() {
		return nil, fmt.Errorf("%w at path: %s", ErrUnavailable, c.path)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	timeout := c.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, c.path, run.Args...)
	cmd.Dir = run.Dir
	cmd.WaitDelay = waitDelay
	stdout := &limitedBuffer{max: run.MaxStdout, cancel: cancel}
	stderr := &limitedBuffer{max: run.MaxStderr, cancel: cancel}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	switch {
	case stdout.exceeded || stderr.exceeded:
		return nil, ErrOutputLimit
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case runCtx.Err() == context.DeadlineExceeded:
		return nil, fmt.Errorf("%w after %v", ErrTimeout, timeout)
	case err != nil:
		return nil, &ExitError{Stderr: strings.TrimSpace(stderr.buf.String()), Err: err}
	}
	return stdout.buf.Bytes(), nil
}

// limitedBuffer keeps at most max bytes and kills the run on the first byte
// past that, rather than letting the process fill memory.
type limitedBuffer struct {
	buf      bytes.Buffer
	max      int
	exceeded bool
	cancel   context.CancelFunc
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.buf.Len()+len(p) > b.max {
		b.exceeded = true
		b.cancel()
		return 0, ErrOutputLimit
	}
	return b.buf.Write(p)
}

var versionPattern = regexp.MustCompile(`^hledger (\d+(?:\.\d+)*)`)

// checkAvailable probes the configured binary. The probe is time-boxed: a client
// whose hledger path points at a slow or hanging binary must not block the
// server's initialization.
func (c *Client) checkAvailable() (bool, string) {
	if !processSupported {
		return false, ""
	}
	timeout := c.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.path, "--version")
	cmd.WaitDelay = waitDelay
	stdout := &limitedBuffer{max: maxVersionOutput, cancel: cancel}
	cmd.Stdout = stdout
	if err := cmd.Run(); err != nil || stdout.exceeded {
		return false, ""
	}
	match := versionPattern.FindSubmatch(bytes.TrimSpace(stdout.buf.Bytes()))
	if match == nil {
		return true, ""
	}
	return true, string(match[1])
}
