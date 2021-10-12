package v1

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
)

func TestPorterConfigSpec_MergeConfig(t *testing.T) {
	t.Run("empty is ignored", func(t *testing.T) {
		nsConfig := PorterConfigSpec{
			Debug: pointer.BoolPtr(true),
		}

		instConfig := PorterConfigSpec{}

		config, err := nsConfig.MergeConfig(instConfig)
		require.NoError(t, err)
		assert.Equal(t, pointer.BoolPtr(true), config.Debug)
	})

	t.Run("override", func(t *testing.T) {
		nsConfig := PorterConfigSpec{
			Debug: pointer.BoolPtr(true),
		}

		instConfig := PorterConfigSpec{
			Debug: pointer.BoolPtr(false),
		}

		config, err := nsConfig.MergeConfig(instConfig)
		require.NoError(t, err)
		assert.Equal(t, pointer.BoolPtr(false), config.Debug)
	})
}

func TestPorterConfigSpec_Marshal(t *testing.T) {
	// Check that we can marshal from the CRD representation to Porter's
	cfg := PorterConfigSpec{
		Debug:                pointer.BoolPtr(true),
		DebugPlugins:         pointer.BoolPtr(true),
		Namespace:            pointer.StringPtr("test"),
		Experimental:         []string{"build-drivers"},
		BuildDriver:          pointer.StringPtr("buildkit"),
		DefaultStorage:       pointer.StringPtr("in-cluster-mongodb"),
		DefaultSecrets:       pointer.StringPtr("keyvault"),
		DefaultStoragePlugin: pointer.StringPtr("mongodb"),
		DefaultSecretsPlugin: pointer.StringPtr("kubernetes.secrets"),
		Storage: []StorageConfig{
			{PluginConfig{
				Name:         "in-cluster-mongodb",
				PluginSubKey: "mongodb",
				Config:       runtime.RawExtension{Raw: []byte(`{"url":"mongodb://..."}`)},
			}},
		},
		Secrets: []SecretsConfig{
			{PluginConfig{
				Name:         "keyvault",
				PluginSubKey: "azure.keyvault",
				Config:       runtime.RawExtension{Raw: []byte(`{"vault": "mysecrets"}`)},
			}},
		},
	}

	b, err := cfg.ToPorterDocument()
	require.NoError(t, err)
	CompareGoldenFile(t, "testdata/porter-config.yaml", string(b))
}

// TODO: Export this from Porter
func CompareGoldenFile(t *testing.T, goldenFile string, got string) {
	wantSchema, err := ioutil.ReadFile(goldenFile)
	require.NoError(t, err)

	if os.Getenv("PORTER_UPDATE_TEST_FILES") == "true" {
		t.Logf("Updated test file %s to match latest test output", goldenFile)
		require.NoError(t, ioutil.WriteFile(goldenFile, []byte(got), 0755), "could not update golden file %s", goldenFile)
	} else {
		assert.Equal(t, string(wantSchema), got, "The test output doesn't match the expected output in %s. If this was intentional, run mage updateTestfiles to fix the tests.", goldenFile)
	}
}
