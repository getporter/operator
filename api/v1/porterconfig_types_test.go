package v1

import (
	"testing"

	"get.porter.sh/porter/pkg/test"
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

func TestPorterConfigSpec_ToPorterDocument(t *testing.T) {
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
	test.CompareGoldenFile(t, "testdata/porter-config.yaml", string(b))
}
