//go:build !linux

package fmtcmd

// mayHaveExtendedAttributes reports true: extended attributes and ACLs are not
// checked here, so a file is always written in place, which keeps them.
func mayHaveExtendedAttributes(string) bool { return true }
