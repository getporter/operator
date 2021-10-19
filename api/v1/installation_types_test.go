package v1

import (
	"testing"

	"get.porter.sh/porter/pkg/test"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestInstallationSpec_ToPorterDocument(t *testing.T) {
	// Validate the special handling for the arbitrary parameters
	// which the CRD can't directly represent as map[string]interface{}
	spec := InstallationSpec{
		SchemaVersion: "1.0.0",
		Name:          "mybuns",
		Namespace:     "dev",
		Bundle: OCIReferenceParts{
			Repository: "getporter/porter-hello",
			Version:    "0.1.0",
		},
		Parameters: runtime.RawExtension{
			Raw: []byte(`{"name":"Porter Operator"}`),
		},
	}

	b, err := spec.ToPorterDocument()
	require.NoError(t, err)

	test.CompareGoldenFile(t, "testdata/installation.yaml", string(b))
}
