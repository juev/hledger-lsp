//go:build wasip1

package stdin

import (
	"fmt"
	"io"
	"os"
	"time"
	"unsafe"
)

// New returns the server's standard input.
func New() io.Reader {
	return NewReader(Host{Ready: stdinReady, Read: os.Stdin.Read, Sleep: time.Sleep})
}

//go:wasmimport wasi_snapshot_preview1 poll_oneoff
//go:noescape
func pollOneoff(in, out unsafe.Pointer, nsubscriptions uint32, nevents unsafe.Pointer) uint32

// subscription and event follow the WASI preview 1 layout: a 48-byte
// subscription with its tagged union at offset 16, and a 32-byte event.
type subscription struct {
	userdata uint64
	tag      uint8
	_        [7]byte
	u        [32]byte
}

type event struct {
	userdata uint64
	errno    uint16
	_        [22]byte
}

const (
	eventtypeClock  = 0
	eventtypeFdRead = 1
	clockMonotonic  = 1

	stdinUserdata = 1
	clockUserdata = 2
)

// stdinReady asks the host whether fd 0 can be read without blocking. A clock
// subscription bounds the wait. Its timeout is minIdleWait rather than zero
// because Node's WASI (uvwasi) fires a zero timeout before it looks at the
// descriptor and so never reports stdin as ready.
func stdinReady() (bool, error) {
	var subs [2]subscription
	subs[0].userdata = stdinUserdata
	subs[0].tag = eventtypeFdRead
	*(*uint32)(unsafe.Pointer(&subs[0].u[0])) = 0 // file descriptor
	subs[1].userdata = clockUserdata
	subs[1].tag = eventtypeClock
	*(*uint32)(unsafe.Pointer(&subs[1].u[0])) = clockMonotonic
	*(*uint64)(unsafe.Pointer(&subs[1].u[8])) = uint64(minIdleWait) // relative timeout, ns

	var events [2]event
	var n uint32
	if errno := pollOneoff(unsafe.Pointer(&subs[0]), unsafe.Pointer(&events[0]), uint32(len(subs)), unsafe.Pointer(&n)); errno != 0 {
		return false, fmt.Errorf("poll_oneoff: errno %d", errno)
	}
	for _, ev := range events[:min(int(n), len(events))] {
		if ev.userdata != stdinUserdata {
			continue
		}
		if ev.errno != 0 {
			return false, fmt.Errorf("poll_oneoff stdin: errno %d", ev.errno)
		}
		return true, nil
	}
	return false, nil
}
