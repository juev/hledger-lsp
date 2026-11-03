//go:build unix

package fmtcmd

import (
	"os"
	"syscall"
)

// chown is replaced in tests, which cannot make a chown fail on their own.
var chown = (*os.File).Chown

// hasOtherLinks reports whether another hard link shares the file, which a
// rename would leave with the old contents.
func hasOtherLinks(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink > 1
}

// keepOwner gives f the owner and group of the file info describes. A file made
// by another user, for example with sudo, would otherwise change hands.
func keepOwner(f *os.File, info os.FileInfo) error {
	want, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	created, err := f.Stat()
	if err != nil {
		return err
	}
	if have, ok := created.Sys().(*syscall.Stat_t); ok && have.Uid == want.Uid && have.Gid == want.Gid {
		return nil
	}
	if err := chown(f, int(want.Uid), int(want.Gid)); err != nil {
		return errKeepFile
	}
	return nil
}
