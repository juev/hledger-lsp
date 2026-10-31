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

	ctx, ok := resolved.FormattingContext(child, content)
	require.True(t, ok)
	assert.Empty(t, ctx.ParseErrors)
	require.Len(t, ctx.Readings, 1)
	assert.Contains(t, ctx.Readings[0].Formats, "FDRXX")
	require.Len(t, ctx.Readings[0].Journal.Transactions, 1)

	ctx, ok = resolved.FormattingContext(child, "2024-13-45 bad\n")
	require.True(t, ok)
	assert.NotEmpty(t, ctx.ParseErrors)

	_, ok = resolved.FormattingContext(filepath.Join(dir, "elsewhere.journal"), content)
	assert.False(t, ok)
}
