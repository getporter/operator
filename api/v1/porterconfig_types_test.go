package v1

import (
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

func TestPorterConfigSpec_ToPorterDocument(t *testing.T) {
	// Check that we can marshal from the CRD representation to Porter's
	tests := []struct {
		name        string
		cfg         PorterConfigSpec
		expDocument []byte
	}{
		{
			name: "All fields set",
			cfg: PorterConfigSpec{
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
			},
			expDocument: []byte(`debug: true
debug-plugins: true
namespace: test
experimental:
    - build-drivers
build-driver: buildkit
default-storage: in-cluster-mongodb
default-secrets: keyvault
default-storage-plugin: mongodb
default-secrets-plugin: kubernetes.secrets
storage:
    - config:
        url: mongodb://...
      name: in-cluster-mongodb
      plugin: mongodb
secrets:
    - config:
        vault: mysecrets
      name: keyvault
      plugin: azure.keyvault
`),
		},
		{
			name: "Minimum fields set",
			cfg: PorterConfigSpec{
				DefaultStoragePlugin: pointer.StringPtr("mongodb"),
				DefaultSecretsPlugin: pointer.StringPtr("kubernetes.secrets"),
			},
			expDocument: []byte(`default-storage-plugin: mongodb
default-secrets-plugin: kubernetes.secrets
`),
		},
		{
			name: "Storage Config minimum fields set",
			cfg: PorterConfigSpec{
				DefaultStoragePlugin: pointer.StringPtr("mongodb"),
				DefaultSecretsPlugin: pointer.StringPtr("kubernetes.secrets"),
				DefaultStorage:       pointer.StringPtr("in-cluster-mongodb"),
				Storage: []StorageConfig{
					{PluginConfig{
						Name:         "in-cluster-mongodb",
						PluginSubKey: "mongodb",
					}},
				},
			},
			expDocument: []byte(`default-storage: in-cluster-mongodb 
default-storage-plugin: mongodb
default-secrets-plugin: kubernetes.secrets
storage:
    name: in-cluster-mongodb
		plugin: mongodb
`),
		},
		{
			name: "Secrets Config minimum fields set",
			cfg: PorterConfigSpec{
				DefaultStoragePlugin: pointer.StringPtr("mongodb"),
				DefaultSecretsPlugin: pointer.StringPtr("kubernetes.secrets"),
				DefaultSecrets:       pointer.StringPtr("kubernetes-secrets"),
				Secrets: []SecretsConfig{
					{PluginConfig{
						Name:         "kubernetes-secrets",
						PluginSubKey: "kubernetes.secrets",
					}},
				},
			},
			expDocument: []byte(`default-secrets: kubernetes-secrets
default-storage-plugin: mongodb
default-secrets-plugin: kubernetes.secrets
secrets:
    - name: kubernetes-secrets
		  plugin: kubernetes.secrets
`),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b, err := test.cfg.ToPorterDocument()
			require.NoError(t, err)
			require.Equal(t, string(test.expDocument), string(b))
		})
	}
}
