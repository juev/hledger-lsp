package fmtcmd

import (
	"errors"
	"syscall"
)

// mayHaveExtendedAttributes reports whether the file at path has extended
// attributes, which also hold ACLs and security labels here. A new file renamed
// over it would not have them. An error counts as having some.
func mayHaveExtendedAttributes(path string) bool {
	size, err := syscall.Listxattr(path, nil)
	if errors.Is(err, syscall.ENOTSUP) {
		return false
	}
	return err != nil || size > 0
}
