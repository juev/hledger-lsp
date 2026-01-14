package cli

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

type Client struct {
	path      string
	timeout   time.Duration
	available bool
}

func NewClient(path string, timeout time.Duration) *Client {
	c := &Client{
		path:    path,
		timeout: timeout,
	}
	c.available = c.checkAvailable()
	return c
}

func (c *Client) Available() bool {
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

func (c *Client) checkAvailable() bool {
	cmd := exec.Command(c.path, "--version")
	return cmd.Run() == nil
}
