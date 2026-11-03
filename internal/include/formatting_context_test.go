package include

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormattingContext(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "commodities.journal"), []byte("commodity 0,000.000 FDRXX\n"), 0o644))
	child := filepath.Join(dir, "child.journal")
	const content = "2024-01-01 x\n    a  1.000 FDRXX\n    b\n"
	require.NoError(t, os.WriteFile(child, []byte(content), 0o644))
	root := filepath.Join(dir, "main.journal")
	require.NoError(t, os.WriteFile(root, []byte("include commodities.journal\ninclude child.journal\n"), 0o644))

	resolved, errs := NewLoader().Load(root)
	require.Empty(t, errs)

	ctx, err := resolved.FormattingContext(child, content)
	require.NoError(t, err)
	assert.Empty(t, ctx.ParseErrors)
	require.Len(t, ctx.Readings, 1)
	assert.Contains(t, ctx.Readings[0].Formats, "FDRXX")
	require.Len(t, ctx.Readings[0].Journal.Transactions, 1)

	ctx, err = resolved.FormattingContext(child, "2024-13-45 bad\n")
	require.NoError(t, err)
	assert.NotEmpty(t, ctx.ParseErrors)

	_, err = resolved.FormattingContext(filepath.Join(dir, "elsewhere.journal"), content)
	assert.ErrorIs(t, err, ErrNotIncluded)
}

// A file included through a symlink or a hard link is read at that inclusion
// too, whichever of its names is formatted.
func TestFormattingContextFindsEveryNameOfTheFile(t *testing.T) {
	links := map[string]func(target, alias string) error{
		"symlink":   func(target, alias string) error { return os.Symlink(filepath.Base(target), alias) },
		"hard link": os.Link,
	}
	for name, link := range links {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			const content = "2024-01-01 x\n    a  1,000 EUR\n    b\n"
			tx := filepath.Join(dir, "tx.journal")
			require.NoError(t, os.WriteFile(tx, []byte(content), 0o644))
			alias := filepath.Join(dir, "alias.journal")
			if err := link(tx, alias); err != nil {
				t.Skipf("%s is not available: %v", name, err)
			}
			root := filepath.Join(dir, "main.journal")
			require.NoError(t, os.WriteFile(root, []byte("decimal-mark .\ninclude tx.journal\ndecimal-mark ,\ninclude alias.journal\n"), 0o644))
			resolved, errs := NewLoader().Load(root)
			require.Empty(t, errs)

			for _, path := range []string{tx, alias} {
				ctx, err := resolved.FormattingContext(path, content)
				require.NoError(t, err)
				assert.Len(t, ctx.Readings, 2, "both decimal marks must be read for %s", path)
			}
		})
	}
}

// A name removed since the tree was read no longer reaches the file.
func TestFormattingContextSkipsAnIncludedFileThatIsGone(t *testing.T) {
	dir := t.TempDir()
	const content = "2024-01-01 x\n    a  1 EUR\n    b\n"
	tx := filepath.Join(dir, "tx.journal")
	require.NoError(t, os.WriteFile(tx, []byte(content), 0o644))
	other := filepath.Join(dir, "other.journal")
	require.NoError(t, os.WriteFile(other, []byte(content), 0o644))
	root := filepath.Join(dir, "main.journal")
	require.NoError(t, os.WriteFile(root, []byte("include tx.journal\ninclude other.journal\n"), 0o644))
	resolved, errs := NewLoader().Load(root)
	require.Empty(t, errs)
	require.NoError(t, os.Remove(other))

	ctx, err := resolved.FormattingContext(tx, content)
	require.NoError(t, err)
	assert.Len(t, ctx.Readings, 1)
}

// When an included file cannot be compared with the formatted one, it might be
// another name for it, so formatting is refused.
func TestFormattingContextFailsWhenAnIncludedFileCannotBeChecked(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can stat any file")
	}
	dir := t.TempDir()
	const content = "2024-01-01 x\n    a  1 EUR\n    b\n"
	tx := filepath.Join(dir, "tx.journal")
	require.NoError(t, os.WriteFile(tx, []byte(content), 0o644))
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "other.journal"), []byte(content), 0o644))
	root := filepath.Join(dir, "main.journal")
	require.NoError(t, os.WriteFile(root, []byte("include tx.journal\ninclude sub/other.journal\n"), 0o644))
	resolved, errs := NewLoader().Load(root)
	require.Empty(t, errs)
	require.NoError(t, os.Chmod(sub, 0))
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })
	if _, err := os.Stat(filepath.Join(sub, "other.journal")); err == nil {
		t.Skip("the system still lets the file be checked")
	}

	_, err := resolved.FormattingContext(tx, content)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotIncluded)
}
