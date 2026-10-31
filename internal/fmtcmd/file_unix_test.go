//go:build unix

package fmtcmd

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func statOf(t *testing.T, path string) *syscall.Stat_t {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Sys().(*syscall.Stat_t)
}

// otherGroup returns a group of the current user that files in dir do not get
// by default, so a file moved to it shows whether formatting keeps its group.
func otherGroup(t *testing.T, dir string) int {
	t.Helper()
	groups, err := os.Getgroups()
	require.NoError(t, err)
	probe := writeJournal(t, dir, ".probe", "")
	defaultGid := int(statOf(t, probe).Gid)
	require.NoError(t, os.Remove(probe))
	for _, gid := range groups {
		if gid != defaultGid {
			return gid
		}
	}
	t.Skip("the current user is in no group other than the default one")
	return 0
}

func assertNoLeftovers(t *testing.T, dir string, want int) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, want, "no temporary or backup file may be left behind")
}

func TestWriteKeepsHardLinks(t *testing.T) {
	dir := t.TempDir()
	h1 := writeJournal(t, dir, "h1.journal", unformatted)
	h2 := filepath.Join(dir, "h2.journal")
	require.NoError(t, os.Link(h1, h2))
	inode := statOf(t, h1).Ino

	code, _, stderr := run(t, "", "-w", h1)

	require.Equal(t, ExitOK, code, stderr)
	assert.Equal(t, formatted, readFile(t, h1))
	assert.Equal(t, formatted, readFile(t, h2), "the other link must see the new contents")
	assert.Equal(t, inode, statOf(t, h1).Ino)
	assert.EqualValues(t, 2, statOf(t, h1).Nlink)
	assertNoLeftovers(t, dir, 2)

	code, stdout, _ := run(t, "", "-c", h2)
	assert.Equal(t, ExitOK, code, stdout)
}

func TestWriteKeepsGroup(t *testing.T) {
	dir := t.TempDir()
	path := writeJournal(t, dir, "a.journal", unformatted)
	gid := otherGroup(t, dir)
	require.NoError(t, os.Chown(path, -1, gid))
	inode := statOf(t, path).Ino

	code, _, stderr := run(t, "", "-w", path)

	require.Equal(t, ExitOK, code, stderr)
	assert.Equal(t, formatted, readFile(t, path))
	assert.Equal(t, uint32(gid), statOf(t, path).Gid)
	assert.NotEqual(t, inode, statOf(t, path).Ino, "a file whose owner can be kept is replaced by rename")
	assertNoLeftovers(t, dir, 1)
}

// A file another user owns, for example one written with sudo, cannot be given
// to its owner by a plain user, so it is written in place.
func TestWriteInPlaceWhenOwnerCannotBeKept(t *testing.T) {
	saved := chown
	t.Cleanup(func() { chown = saved })
	chown = func(*os.File, int, int) error { return syscall.EPERM }

	dir := t.TempDir()
	path := writeJournal(t, dir, "a.journal", unformatted)
	gid := otherGroup(t, dir)
	require.NoError(t, os.Chown(path, -1, gid))
	require.NoError(t, os.Chmod(path, 0o640))
	inode := statOf(t, path).Ino

	code, _, stderr := run(t, "", "-w", path)

	require.Equal(t, ExitOK, code, stderr)
	assert.Equal(t, formatted, readFile(t, path))
	assert.Equal(t, inode, statOf(t, path).Ino)
	assert.Equal(t, uint32(gid), statOf(t, path).Gid)
	assert.Equal(t, uint32(0o640), statOf(t, path).Mode&0o777)
	assertNoLeftovers(t, dir, 1)
}

func TestWriteRefusesReadOnlyFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write any file")
	}
	dir := t.TempDir()
	path := writeJournal(t, dir, "2020.journal", unformatted)
	require.NoError(t, os.Chmod(path, 0o444))

	code, _, stderr := run(t, "", "-w", path)

	assert.Equal(t, ExitError, code)
	assert.Contains(t, stderr, "permission denied")
	assert.Equal(t, unformatted, readFile(t, path))
	assertNoLeftovers(t, dir, 1)

	code, stdout, _ := run(t, "", "-c", path)
	assert.Equal(t, ExitUnformatted, code, "a read-only file can still be checked")
	assert.Equal(t, path+"\n", stdout)
}
