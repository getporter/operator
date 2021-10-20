package docker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractImageWithDigest(t *testing.T) {
	const inspectOutput = `[
    {
        "RepoDigests": [
            "localhost:5000/porterops-controller@sha256:c742b1cccc5a69abd082b1d61c8ef616a27699b6b52430ac700019f22800c06f"
        ]
    }
]`

	ref, err := ExtractRepoDigest(inspectOutput)
	require.NoError(t, err)
	assert.Equal(t, "localhost:5000/porterops-controller@sha256:c742b1cccc5a69abd082b1d61c8ef616a27699b6b52430ac700019f22800c06f", ref)
}
