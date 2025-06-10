package docs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uwu-tools/magex/shx"
)

func TestEnsurePorterRepository(t *testing.T) {
	t.Run("has local repo", func(t *testing.T) {
		tmp := t.TempDir()

		resolvedPath, err := ensurePorterRepositoryIn(tmp, "")
		require.NoError(t, err)
		require.Equal(t, tmp, resolvedPath)
	})

	t.Run("missing local repo", func(t *testing.T) {
		tmp := t.TempDir()

		resolvedPath, err := ensurePorterRepositoryIn("missing", tmp)
		require.NoError(t, err)
		require.Equal(t, tmp, resolvedPath)
	})

	t.Run("local repo unset", func(t *testing.T) {
		tmp := t.TempDir()
		resolvedPath, err := ensurePorterRepositoryIn("", tmp)
		require.NoError(t, err)
		require.Equal(t, tmp, resolvedPath)
	})

	t.Run("empty default path clones repo", func(t *testing.T) {
		tmp := t.TempDir()

		resolvedPath, err := ensurePorterRepositoryIn("", tmp)
		require.NoError(t, err)
		require.Equal(t, tmp, resolvedPath)

		err = shx.Command("git", "status").In(resolvedPath).RunE()
		require.NoError(t, err, "clone failed")
	})

	t.Run("changes in default path are reset", func(t *testing.T) {
		tmp := t.TempDir()

		repoPath, err := ensurePorterRepositoryIn("", tmp)
		require.NoError(t, err)

		// make a change
		readme := filepath.Join(repoPath, "README.md")
		require.NoError(t, os.Remove(readme))

		// Make sure rerunning resets the change
		_, err = ensurePorterRepositoryIn("", tmp)
		require.NoError(t, err)
		require.FileExists(t, readme)
	})
}
