package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The smoke test's verdict rests on its stdout parser: a stream that ends
// inside a frame must count as a framing error, not as a clean shutdown.
func TestReadLoopRejectsTruncatedFrames(t *testing.T) {
	for _, stream := range []string{
		"garbage",
		"Content-Length: 100\r\n",
		"Content-Length: 100\r\n\r\n",
		"Content-Length: 100\r\n\r\n{\"jsonrpc\":",
	} {
		t.Run(strings.TrimSpace(stream), func(t *testing.T) {
			c := newFrameParser()
			c.readLoop(strings.NewReader(stream))
			assert.Error(t, c.framingErr)
		})
	}
}

func TestReadLoopAcceptsStreamEndingBetweenFrames(t *testing.T) {
	response := `{"jsonrpc":"2.0","id":1,"result":null}`
	for _, stream := range []string{"", fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(response), response)} {
		c := newFrameParser()
		c.readLoop(strings.NewReader(stream))
		assert.NoError(t, c.framingErr, "stream %q", stream)
	}
}

// waitExit must bound the whole shutdown: a server that closes stdout but
// keeps running, or keeps stdout open, fails within the timeout and is reaped.
func TestWaitExitBoundsShutdown(t *testing.T) {
	for name, script := range map[string]string{
		"exits late after closing stdout": "exec >&-; exec sleep 10",
		"keeps stdout open":               "exec sleep 10",
	} {
		t.Run(name, func(t *testing.T) {
			c := startLSPProcess(t, exec.Command("sh", "-c", script))
			require.NoError(t, c.stdin.Close())

			start := time.Now()
			err := c.waitExit(300 * time.Millisecond)

			assert.Error(t, err)
			assert.Less(t, time.Since(start), 5*time.Second)
			assert.NotNil(t, c.cmd.ProcessState, "process is reaped")
		})
	}
}

// newFrameParser returns an lspProcess with only what readLoop needs.
func newFrameParser() *lspProcess {
	return &lspProcess{
		responses:   map[int]json.RawMessage{},
		diagnostics: map[string][]json.RawMessage{},
		readerDone:  make(chan struct{}),
		changed:     make(chan struct{}, 1),
	}
}
