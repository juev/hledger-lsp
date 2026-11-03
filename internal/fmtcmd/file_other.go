//go:build !unix

package fmtcmd

import "os"

// hasOtherLinks reports false: the link count is not available here.
func hasOtherLinks(os.FileInfo) bool { return false }

// keepOwner does nothing: a new file is owned by its creator here.
func keepOwner(*os.File, os.FileInfo) error { return nil }
