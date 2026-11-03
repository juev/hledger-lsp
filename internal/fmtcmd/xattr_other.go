//go:build !linux

package fmtcmd

// mayHaveExtendedAttributes reports true: extended attributes and ACLs are not
// inspected here. Writing in place avoids discarding them by replacing the
// inode, but attributes managed by the OS remain subject to its write policy.
func mayHaveExtendedAttributes(string) bool { return true }
