package include

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FormatsAtFunc answers every offset of an occurrence exactly as FormatsAt does.
func TestFormatsAtFunc_MatchesFormatsAtAtEveryOffset(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"main.journal":  "commodity 1.000,00 EUR\ninclude child.journal\ncommodity 1,000.00 USD\ninclude child.journal\n",
		"child.journal": "commodity 1 000.00 РУБ\n\n2024-01-01 x\n    a  1 РУБ\n    b\n\ncommodity 0.000 元\n\n2024-01-02 y\n    a  1 元\n    b\n",
	}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}

	resolved, errs := NewLoader().Load(filepath.Join(dir, "main.journal"))
	require.Empty(t, errs)

	for _, occurrence := range resolved.Occurrences {
		formatsAt := resolved.FormatsAtFunc(occurrence.ID)
		for offset := 0; offset <= len(files["child.journal"])+1; offset++ {
			assert.Equal(t, resolved.FormatsAt(occurrence.ID, offset), formatsAt(offset), "occurrence %d offset %d", occurrence.ID, offset)
		}
	}
}

func TestOccurrence_FindsEveryOccurrenceByID(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.journal"), []byte("include a.journal\ninclude a.journal\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.journal"), []byte("; a\n"), 0o644))

	resolved, errs := NewLoader().Load(filepath.Join(dir, "main.journal"))
	require.Empty(t, errs)
	require.Len(t, resolved.Occurrences, 3)
	for index := range resolved.Occurrences {
		assert.Same(t, &resolved.Occurrences[index], resolved.Occurrence(resolved.Occurrences[index].ID))
	}
	assert.Nil(t, resolved.Occurrence(0))
	assert.Nil(t, resolved.Occurrence(99))
}
