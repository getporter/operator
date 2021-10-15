package v1

import (
	"io/ioutil"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestInstallationSpec_ToPorterDocument(t *testing.T) {
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

	b, err := spec.ToPorterDocument()
	require.NoError(t, err)

	want, err := ioutil.ReadFile("testdata/installation.yaml")
	require.NoError(t, err)

	got := string(b)
	assert.Equal(t, string(want), got)
}
