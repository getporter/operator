package docs

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/carolynvs/magex/shx"
	"github.com/stretchr/testify/require"
)

func TestEnsurePorterRepository(t *testing.T) {
	t.Run("has local repo", func(t *testing.T) {
		tmp, err := ioutil.TempDir("", "porter-docs-test")
		require.NoError(t, err)
		defer os.RemoveAll(tmp)

		resolvedPath, err := ensurePorterRepositoryIn(tmp, "")
		require.NoError(t, err)
		require.Equal(t, tmp, resolvedPath)
	})

	t.Run("missing local repo", func(t *testing.T) {
		tmp, err := ioutil.TempDir("", "porter-docs-test")
		require.NoError(t, err)
		defer os.RemoveAll(tmp)

		resolvedPath, err := ensurePorterRepositoryIn("missing", tmp)
		require.NoError(t, err)
		require.Equal(t, tmp, resolvedPath)
	})

	t.Run("local repo unset", func(t *testing.T) {
		tmp, err := ioutil.TempDir("", "porter-docs-test")
		require.NoError(t, err)
		defer os.RemoveAll(tmp)

		resolvedPath, err := ensurePorterRepositoryIn("", tmp)
		require.NoError(t, err)
		require.Equal(t, tmp, resolvedPath)
	})

	t.Run("empty default path clones repo", func(t *testing.T) {
		tmp, err := ioutil.TempDir("", "porter-docs-test")
		require.NoError(t, err)
		defer os.RemoveAll(tmp)

		resolvedPath, err := ensurePorterRepositoryIn("", tmp)
		require.NoError(t, err)
		require.Equal(t, tmp, resolvedPath)

		err = shx.Command("git", "status").In(resolvedPath).RunE()
		require.NoError(t, err, "clone failed")
	})

	t.Run("changes in default path are reset", func(t *testing.T) {
		tmp, err := ioutil.TempDir("", "porter-docs-test")
		require.NoError(t, err)
		defer os.RemoveAll(tmp)

		repoPath, err := ensurePorterRepositoryIn("", tmp)
		require.NoError(t, err)

		// make a change
		readme := filepath.Join(repoPath, "README.md")
		require.NoError(t, os.Remove(readme))

		// Make sure rerunning resets the change
		repoPath, err = ensurePorterRepositoryIn("", tmp)
		require.NoError(t, err)
		require.FileExists(t, readme)
	})
}
