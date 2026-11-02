// Package stdin provides the server's standard input.
//
// Natively it is os.Stdin. Under WASI preview 1 (GOOS=wasip1) a Go program
// runs on a single thread, and a read from stdin that has no data blocks the
// whole runtime: no goroutine runs and no timer fires until the editor sends
// the next message, so debounced diagnostics are never published on their own.
// Reader keeps the scheduler running by reading only when the host reports
// data, and sleeping in between.
package stdin

import (
	"errors"
	"syscall"
	"time"
)

// Idle waits start at minIdleWait and double up to maxIdleWait, which bounds
// the latency a message waits before it is read. The ceiling trades latency
// for idle CPU: measured on an idle server, 10ms costs 1.6% of a core under
// wasmtime and 4.4% under Node, 25ms 0.8% and 2.1%.
const (
	minIdleWait = time.Millisecond
	maxIdleWait = 25 * time.Millisecond
)

// Host is the set of host operations Reader is built from.
type Host struct {
	// Ready reports whether stdin has data or has reached EOF. It may wait
	// briefly but must not block. An error means the host cannot tell.
	Ready func() (bool, error)
	// Read reads from stdin once.
	Read func(p []byte) (int, error)
	// Sleep pauses the calling goroutine and lets the others run.
	Sleep func(d time.Duration)
}

type mode int

const (
	// modePoll reads only after Ready reports data, so the read cannot block.
	modePoll mode = iota
	// modeNonBlocking retries reads that the host answered with EAGAIN.
	modeNonBlocking
	// modeBlocking reads directly because the host cannot report readiness.
	modeBlocking
)

// Reader reads stdin through a Host without blocking the Go scheduler.
type Reader struct {
	host Host
	mode mode
	wait time.Duration
}

// NewReader returns a Reader over host.
func NewReader(host Host) *Reader {
	return &Reader{host: host, mode: modePoll, wait: minIdleWait}
}

// Read implements io.Reader.
func (r *Reader) Read(p []byte) (int, error) {
	for {
		if r.mode == modePoll {
			ready, err := r.host.Ready()
			if err != nil {
				r.mode = modeBlocking
			} else if !ready {
				r.idle()
				continue
			}
		}

		n, err := r.host.Read(p)
		if errors.Is(err, syscall.EAGAIN) {
			r.mode = modeNonBlocking
			r.idle()
			continue
		}
		r.wait = minIdleWait
		return n, err
	}
}

func (r *Reader) idle() {
	r.host.Sleep(r.wait)
	r.wait = min(2*r.wait, maxIdleWait)
}
