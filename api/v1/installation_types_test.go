package v1

import (
	"testing"

	"get.porter.sh/porter/pkg/yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestInstallationSpec_MarshalYAML(t *testing.T) {
	// Validate the special handling for the arbitrary parameters
	// which the CRD can't directly represent as map[string]interface{}
	spec := InstallationSpec{
		SchemaVersion:    "1.0.0",
		InstallationName: "mybuns",
		TargetNamespace:  "dev",
		BundleRepository: "getporter/porter-hello",
		BundleVersion:    "0.1.0",
		Parameters: runtime.RawExtension{
			Raw: []byte(`{"name":"Porter Operator"}`),
		},
	}

	b, err := yaml.Marshal(spec)
	require.NoError(t, err)

	want := `schemaVersion: 1.0.0
name: mybuns
namespace: dev
bundleRepository: getporter/porter-hello
bundleVersion: 0.1.0
parameters:
  name: Porter Operator
`
	got := string(b)
	assert.Equal(t, want, got)
}
