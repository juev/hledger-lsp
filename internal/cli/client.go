package cli

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// defaultTimeout bounds the availability probe when no timeout is configured.
const defaultTimeout = 5 * time.Second

type Client struct {
	path      string
	timeout   time.Duration
	probeOnce sync.Once
	available bool
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

func (c *Client) Available() bool {
	c.probeOnce.Do(func() {
		c.available = c.checkAvailable()
	})
	return c.available
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

// checkAvailable probes the configured binary. The probe is time-boxed: a client
// whose hledger path points at a slow or hanging binary must not block the
// server's initialization.
func (c *Client) checkAvailable() bool {
	timeout := c.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return exec.CommandContext(ctx, c.path, "--version").Run() == nil
}
