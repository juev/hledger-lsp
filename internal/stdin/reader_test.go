package stdin

import (
	"errors"
	"io"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeHost scripts the answers of a WASI host and records what the reader
// asked for, so each test can state which host calls were made in which order.
type fakeHost struct {
	ready  []readyAnswer
	reads  []readAnswer
	calls  []string
	sleeps []time.Duration
}

type readyAnswer struct {
	ready bool
	err   error
}

type readAnswer struct {
	data string
	err  error
}

func (h *fakeHost) host() Host {
	return Host{
		Ready: func() (bool, error) {
			h.calls = append(h.calls, "ready")
			if len(h.ready) == 0 {
				panic("unexpected Ready call")
			}
			a := h.ready[0]
			h.ready = h.ready[1:]
			return a.ready, a.err
		},
		Read: func(p []byte) (int, error) {
			h.calls = append(h.calls, "read")
			if len(h.reads) == 0 {
				panic("unexpected Read call")
			}
			a := h.reads[0]
			h.reads = h.reads[1:]
			return copy(p, a.data), a.err
		},
		Sleep: func(d time.Duration) {
			h.calls = append(h.calls, "sleep")
			h.sleeps = append(h.sleeps, d)
		},
	}
}

func readString(t *testing.T, r io.Reader) string {
	t.Helper()
	buf := make([]byte, 64)
	n, err := r.Read(buf)
	require.NoError(t, err)
	return string(buf[:n])
}

// On a host whose stdin reads block (wasmtime), a read issued without data
// would freeze every goroutine, timers included. The reader sleeps until the
// host reports data, and only then reads.
func TestReaderWaitsForReadinessBeforeBlockingRead(t *testing.T) {
	h := &fakeHost{
		reads: []readAnswer{{data: "first"}, {data: "second"}},
		ready: []readyAnswer{{ready: true}, {ready: false}, {ready: false}, {ready: true}},
	}
	r := NewReader(h.host())

	assert.Equal(t, "first", readString(t, r))
	assert.Equal(t, "second", readString(t, r))
	assert.Equal(t, []string{"ready", "read", "ready", "sleep", "ready", "sleep", "ready", "read"}, h.calls)
}

// A host with non-blocking stdin answers EAGAIN instead of blocking, even
// after reporting data. The reader then retries after a sleep and stops
// asking for readiness, which such a host does not report reliably.
func TestReaderRetriesAfterEAGAIN(t *testing.T) {
	h := &fakeHost{
		ready: []readyAnswer{{ready: true}},
		reads: []readAnswer{
			{err: syscall.EAGAIN},
			{err: &wrappedErr{syscall.EAGAIN}},
			{data: "init"},
			{err: syscall.EAGAIN},
			{data: "next"},
		},
	}
	r := NewReader(h.host())

	assert.Equal(t, "init", readString(t, r))
	assert.Equal(t, "next", readString(t, r))
	assert.Equal(t, []string{"ready", "read", "sleep", "read", "sleep", "read", "read", "sleep", "read"}, h.calls)
}

// A host that cannot answer readiness falls back to plain reads: requests
// still work, timer-driven work waits for the next message.
func TestReaderFallsBackToPlainReadsWhenReadinessFails(t *testing.T) {
	h := &fakeHost{
		reads: []readAnswer{{data: "first"}, {data: "second"}},
		ready: []readyAnswer{{err: errors.New("poll_oneoff: not supported")}},
	}
	r := NewReader(h.host())

	assert.Equal(t, "first", readString(t, r))
	assert.Equal(t, "second", readString(t, r))
	assert.Equal(t, []string{"ready", "read", "read"}, h.calls)
}

// A host with non-blocking stdin that also cannot answer readiness still
// works: the plain read answers EAGAIN, and the reader retries.
func TestReaderFallbackRetriesAfterEAGAIN(t *testing.T) {
	h := &fakeHost{
		reads: []readAnswer{{err: syscall.EAGAIN}, {data: "init"}},
		ready: []readyAnswer{{err: errors.New("poll_oneoff: not supported")}},
	}
	r := NewReader(h.host())

	assert.Equal(t, "init", readString(t, r))
	assert.Equal(t, []string{"ready", "read", "sleep", "read"}, h.calls)
}

// Idle waits back off up to a ceiling, so an idle server does not spin, and
// start short again after data, so a burst of messages is read promptly.
func TestReaderBacksOffWhileIdleAndResetsOnData(t *testing.T) {
	h := &fakeHost{
		reads: []readAnswer{{data: "a"}, {data: "b"}, {data: "c"}},
		ready: []readyAnswer{{ready: true}},
	}
	const idlePolls = 10
	for range idlePolls {
		h.ready = append(h.ready, readyAnswer{})
	}
	h.ready = append(h.ready, readyAnswer{ready: true}, readyAnswer{}, readyAnswer{ready: true})
	r := NewReader(h.host())

	readString(t, r)
	readString(t, r)
	readString(t, r)

	var want []time.Duration
	for wait := minIdleWait; len(want) < idlePolls; wait = min(2*wait, maxIdleWait) {
		want = append(want, wait)
	}
	want = append(want, minIdleWait)
	assert.Equal(t, want, h.sleeps)
	assert.Equal(t, maxIdleWait, want[idlePolls-1], "idle waits reach the ceiling")
}

func TestReaderPassesThroughEOFAndErrors(t *testing.T) {
	boom := errors.New("boom")
	h := &fakeHost{
		reads: []readAnswer{{data: "x"}, {data: "tail", err: boom}, {err: io.EOF}},
		ready: []readyAnswer{{ready: true}, {ready: true}, {ready: true}},
	}
	r := NewReader(h.host())
	buf := make([]byte, 64)

	assert.Equal(t, "x", readString(t, r))

	n, err := r.Read(buf)
	assert.Equal(t, "tail", string(buf[:n]))
	require.ErrorIs(t, err, boom)

	n, err = r.Read(buf)
	assert.Zero(t, n)
	require.ErrorIs(t, err, io.EOF)
}

type wrappedErr struct{ err error }

func (e *wrappedErr) Error() string { return "read /dev/stdin: " + e.err.Error() }
func (e *wrappedErr) Unwrap() error { return e.err }
