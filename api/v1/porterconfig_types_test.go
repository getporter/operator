package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

func TestPorterConfigSpec_MergeConfig(t *testing.T) {
	t.Run("empty is ignored", func(t *testing.T) {
		nsConfig := PorterConfigSpec{
			Verbosity: ptr.To("info"),
		}

		instConfig := PorterConfigSpec{}

		config, err := nsConfig.MergeConfig(instConfig)
		require.NoError(t, err)
		assert.Equal(t, ptr.To("info"), config.Verbosity)
	})

	t.Run("override", func(t *testing.T) {
		nsConfig := PorterConfigSpec{
			Verbosity: ptr.To("info"),
		}

		instConfig := PorterConfigSpec{
			Verbosity: ptr.To("debug"),
			Telemetry: TelemetryConfig{
				Enabled: ptr.To(true),
			},
		}

		config, err := nsConfig.MergeConfig(instConfig)
		require.NoError(t, err)
		assert.Equal(t, ptr.To("debug"), config.Verbosity)
		assert.Equal(t, ptr.To(true), config.Telemetry.Enabled)
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
				Verbosity:            ptr.To("debug"),
				Namespace:            ptr.To("test"),
				Experimental:         []string{"build-drivers"},
				BuildDriver:          ptr.To("buildkit"),
				DefaultStorage:       ptr.To("in-cluster-mongodb"),
				DefaultSecrets:       ptr.To("keyvault"),
				DefaultStoragePlugin: ptr.To("mongodb"),
				DefaultSecretsPlugin: ptr.To("kubernetes.secrets"),
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
			expDocument: []byte(`verbosity: debug
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
			name: "Storage config not provided",
			cfg: PorterConfigSpec{
				DefaultSecretsPlugin: ptr.To("kubernetes.secrets"),
				DefaultStorage:       ptr.To("in-cluster-mongodb"),
				Storage: []StorageConfig{
					{PluginConfig{
						Name:         "in-cluster-mongodb",
						PluginSubKey: "mongodb",
					}},
				},
			},
			expDocument: []byte(`default-storage: in-cluster-mongodb
default-secrets-plugin: kubernetes.secrets
storage:
    - name: in-cluster-mongodb
      plugin: mongodb
`),
		},
		{
			name: "Secrets config not provided",
			cfg: PorterConfigSpec{
				DefaultStorage: ptr.To("in-cluster-mongodb"),
				DefaultSecrets: ptr.To("kubernetes-secrets"),
				Storage: []StorageConfig{
					{PluginConfig{
						Name:         "in-cluster-mongodb",
						PluginSubKey: "mongodb",
						Config:       runtime.RawExtension{Raw: []byte(`{"url": "mongodb://..."}`)},
					}},
				},
				Secrets: []SecretsConfig{
					{PluginConfig{
						Name:         "kubernetes-secrets",
						PluginSubKey: "kubernetes.secrets",
					}},
				},
			},
			expDocument: []byte(`default-storage: in-cluster-mongodb
default-secrets: kubernetes-secrets
storage:
    - config:
        url: mongodb://...
      name: in-cluster-mongodb
      plugin: mongodb
secrets:
    - name: kubernetes-secrets
      plugin: kubernetes.secrets
`),
		},
		{
			name: "All Telemetry config provided",
			cfg: PorterConfigSpec{
				DefaultStorage: ptr.To("in-cluster-mongodb"),
				DefaultSecrets: ptr.To("kubernetes-secrets"),
				Storage: []StorageConfig{
					{PluginConfig{
						Name:         "in-cluster-mongodb",
						PluginSubKey: "mongodb",
						Config:       runtime.RawExtension{Raw: []byte(`{"url": "mongodb://..."}`)},
					}},
				},
				Secrets: []SecretsConfig{
					{PluginConfig{
						Name:         "kubernetes-secrets",
						PluginSubKey: "kubernetes.secrets",
					}},
				},
				Telemetry: TelemetryConfig{
					Enabled:        ptr.To(true),
					Protocol:       ptr.To("grpc"),
					Endpoint:       ptr.To("127.0.0.1:4317"),
					Insecure:       ptr.To(true),
					Compression:    ptr.To("gzip"),
					Timeout:        ptr.To("3s"),
					StartTimeout:   ptr.To("100ms"),
					RedirectToFile: ptr.To("foo"),
				},
			},
			expDocument: []byte(`default-storage: in-cluster-mongodb
default-secrets: kubernetes-secrets
storage:
    - config:
        url: mongodb://...
      name: in-cluster-mongodb
      plugin: mongodb
secrets:
    - name: kubernetes-secrets
      plugin: kubernetes.secrets
telemetry:
    enabled: true
    endpoint: 127.0.0.1:4317
    protocol: grpc
    insecure: true
    timeout: 3s
    compression: gzip
    start-timeout: 100ms
    redirect-to-file: foo
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
