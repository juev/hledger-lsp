package fmtcmd

import (
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setXattr gives path a user extended attribute, or skips the test where the
// file system does not store them.
func setXattr(t *testing.T, path, name, value string) {
	t.Helper()
	if err := syscall.Setxattr(path, name, []byte(value), 0); err != nil {
		t.Skipf("the file system does not store user extended attributes: %v", err)
	}
}

func getXattr(t *testing.T, path, name string) string {
	t.Helper()
	buf := make([]byte, 64)
	n, err := syscall.Getxattr(path, name, buf)
	require.NoError(t, err, "extended attribute %s", name)
	return string(buf[:n])
}

// Extended attributes also hold ACLs and security labels, which a new file
// renamed over the journal would not have, so such a file is written in place.
func TestWriteKeepsExtendedAttributes(t *testing.T) {
	dir := t.TempDir()
	path := writeJournal(t, dir, "a.journal", unformatted)
	setXattr(t, path, "user.hledger-fmt-test", "keep-me")
	inode := statOf(t, path).Ino

	code, _, stderr := run(t, "", "-w", path)

	require.Equal(t, ExitOK, code, stderr)
	assert.Equal(t, formatted, readFile(t, path))
	assert.Equal(t, "keep-me", getXattr(t, path, "user.hledger-fmt-test"))
	assert.Equal(t, inode, statOf(t, path).Ino)
	assertNoLeftovers(t, dir, 1)
}

// Writing to a file clears its set-user-ID bit, so a file written in place gets
// its mode back afterwards.
func TestWriteInPlaceKeepsSpecialModeBits(t *testing.T) {
	dir := t.TempDir()
	path := writeJournal(t, dir, "a.journal", unformatted)
	setXattr(t, path, "user.hledger-fmt-test", "keep-me")
	mode := specialMode(t, path)

	code, _, stderr := run(t, "", "-w", path)

	require.Equal(t, ExitOK, code, stderr)
	assert.Equal(t, formatted, readFile(t, path))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, mode, info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky|os.ModePerm))
	assert.Equal(t, "keep-me", getXattr(t, path, "user.hledger-fmt-test"))
	assertNoLeftovers(t, dir, 1)
}
