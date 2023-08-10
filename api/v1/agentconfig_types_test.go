package v1

import (
	"testing"

	"get.porter.sh/porter/pkg/plugins"
	portertest "get.porter.sh/porter/pkg/test"
	portertests "get.porter.sh/porter/tests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestAgentConfigSpecAdapter_GetPorterImage(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		c := AgentConfigSpecAdapter{}
		assert.Equal(t, DefaultPorterAgentRepository+":"+DefaultPorterAgentVersion, c.GetPorterImage())
	})

	t.Run("porter version set", func(t *testing.T) {
		c := AgentConfigSpec{PorterVersion: "canary"}
		cl := NewAgentConfigSpecAdapter(c)

		assert.Equal(t, DefaultPorterAgentRepository+":canary", cl.GetPorterImage())
	})

	t.Run("porter repository set", func(t *testing.T) {
		// Test if someone has mirrored porter's agent to another registry
		c := AgentConfigSpec{PorterRepository: "localhost:5000/myporter"}
		cl := NewAgentConfigSpecAdapter(c)
		assert.Equal(t, "localhost:5000/myporter:"+DefaultPorterAgentVersion, cl.GetPorterImage())
	})

	t.Run("porter repository and version set", func(t *testing.T) {
		c := AgentConfigSpec{PorterRepository: "localhost:5000/myporter", PorterVersion: "v1.2.3"}
		cl := NewAgentConfigSpecAdapter(c)
		assert.Equal(t, "localhost:5000/myporter:v1.2.3", cl.GetPorterImage())
	})

	t.Run("digest set", func(t *testing.T) {
		c := AgentConfigSpec{
			PorterVersion: "sha256:ea7d328dc6b65e4b62a971ba8436f89d5857c2878c211312aaa5e2db2e47a2da",
		}
		cl := NewAgentConfigSpecAdapter(c)
		assert.Equal(t, DefaultPorterAgentRepository+"@sha256:ea7d328dc6b65e4b62a971ba8436f89d5857c2878c211312aaa5e2db2e47a2da", cl.GetPorterImage())
	})
}

func TestAgentConfigSpecAdapter_GetPullPolicy(t *testing.T) {
	testcases := map[string]v1.PullPolicy{
		"":       v1.PullIfNotPresent,
		"latest": v1.PullAlways,
		"canary": v1.PullAlways,
		"v1.2.3": v1.PullIfNotPresent,
	}

	for version, wantPullPolicy := range testcases {
		t.Run("version "+version, func(t *testing.T) {
			c := AgentConfigSpec{PorterVersion: version}
			cl := NewAgentConfigSpecAdapter(c)
			assert.Equal(t, wantPullPolicy, cl.GetPullPolicy())
		})

	}
}

func TestAgentConfigSpecAdapter_GetStorageClassName(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		c := AgentConfigSpec{}
		cl := NewAgentConfigSpecAdapter(c)
		assert.Equal(t, "", cl.GetStorageClassName())
	})
	t.Run("azureblob-nfs-premium", func(t *testing.T) {
		c := AgentConfigSpec{StorageClassName: "azureblob-nfs-premium"}
		cl := NewAgentConfigSpecAdapter(c)
		assert.Equal(t, "azureblob-nfs-premium", cl.GetStorageClassName())
	})
}

func TestAgentConfigSpecAdapter_GetVolumeSize(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		c := AgentConfigSpec{}
		cl := NewAgentConfigSpecAdapter(c)
		assert.Equal(t, resource.MustParse("64Mi"), cl.GetVolumeSize())
	})

	t.Run("quantity set", func(t *testing.T) {
		qty := resource.MustParse("128Mi")
		c := AgentConfigSpec{VolumeSize: "128Mi"}
		cl := NewAgentConfigSpecAdapter(c)
		assert.Equal(t, qty, cl.GetVolumeSize())
	})
}

func TestAgentConfigSpecAdapter_GetPVCName(t *testing.T) {
	t.Run("no plugins defined", func(t *testing.T) {
		c := AgentConfigSpec{}
		cl := NewAgentConfigSpecAdapter(c)
		assert.Empty(t, cl.GetPluginsPVCName("default"))
	})

	t.Run("one plugins defined", func(t *testing.T) {
		c := AgentConfigSpec{
			PluginConfigFile: &PluginFileSpec{
				Plugins: map[string]Plugin{
					"kubernetes": {Version: "v1.0.0", FeedURL: "https://test"},
				},
			},
		}
		cl := NewAgentConfigSpecAdapter(c)
		assert.Equal(t, "porter-04ddd41f06d1720a7467dadc464d8077", cl.GetPluginsPVCName("default"))
	})

	t.Run("multiple plugins defined", func(t *testing.T) {
		c := AgentConfigSpec{
			PluginConfigFile: &PluginFileSpec{
				Plugins: map[string]Plugin{
					"kubernetes": {Version: "v1.0.0", FeedURL: "https://test"},
					"azure":      {Version: "v1.0.0", URL: "https://test"},
					"hashicorp":  {Version: "v1.0.0", Mirror: "https://test"},
				},
			},
		}
		cl := NewAgentConfigSpecAdapter(c)
		assert.Equal(t, "porter-a5bc533e0e249e10c7cf442be42d6ae2", cl.GetPluginsPVCName("default"))

		// change the order of the plugins should not affect the name output.
		c2 := AgentConfigSpec{
			PluginConfigFile: &PluginFileSpec{
				Plugins: map[string]Plugin{
					"azure":      {Version: "v1.0.0", FeedURL: "https://test"},
					"hashicorp":  {Version: "v1.0.0", URL: "https://test"},
					"kubernetes": {Version: "v1.0.0", Mirror: "https://test"},
				},
			},
		}
		cl2 := NewAgentConfigSpecAdapter(c2)
		assert.Equal(t, "porter-a5bc533e0e249e10c7cf442be42d6ae2", cl2.GetPluginsPVCName("default"))
	})
}

func TestAgentConfigSpecAdapter_GetPluginsLabels(t *testing.T) {
	t.Run("no plugins defined", func(t *testing.T) {
		c := AgentConfigSpec{}
		cl := NewAgentConfigSpecAdapter(c)
		assert.Nil(t, cl.Plugins.GetLabels())
	})

	t.Run("one plugin defined", func(t *testing.T) {
		onePluginCfg := AgentConfigSpec{
			PluginConfigFile: &PluginFileSpec{
				Plugins: map[string]Plugin{
					"kubernetes": {Version: "v1.0.0", FeedURL: "https://test"},
				},
			},
		}
		cl := NewAgentConfigSpecAdapter(onePluginCfg)
		assert.Equal(t, map[string]string{LabelManaged: "true", LabelPluginsHash: "b1c683cd14c4e4a242c43ccd2f57a696"}, cl.Plugins.GetLabels())
	})

	t.Run("multiple plugins defined", func(t *testing.T) {
		multiplePluginsCfg := AgentConfigSpec{
			PluginConfigFile: &PluginFileSpec{
				Plugins: map[string]Plugin{
					"kubernetes": {Version: "v1.0.0", FeedURL: "https://test"},
					"azure":      {Version: "v1.2.0", URL: "https://test1"},
					"hashicorp":  {Version: "v1.0.0", FeedURL: "https://test"},
				},
			},
		}
		mcl := NewAgentConfigSpecAdapter(multiplePluginsCfg)
		assert.Equal(t, map[string]string{LabelManaged: "true", LabelPluginsHash: "d8dbdcb6a9de4e60ef7886f90cbe73f4"}, mcl.Plugins.GetLabels())

		multiplePluginsCfgWithDifferentOrder := AgentConfigSpec{
			PluginConfigFile: &PluginFileSpec{
				Plugins: map[string]Plugin{
					"hashicorp":  {Version: "v1.0.0", FeedURL: "https://test"},
					"azure":      {Version: "v1.2.0", URL: "https://test1"},
					"kubernetes": {Version: "v1.0.0", FeedURL: "https://test"},
				},
			},
		}
		mclWithDifferentOrder := NewAgentConfigSpecAdapter(multiplePluginsCfgWithDifferentOrder)
		assert.Equal(t, map[string]string{LabelManaged: "true", LabelPluginsHash: "d8dbdcb6a9de4e60ef7886f90cbe73f4"}, mclWithDifferentOrder.Plugins.GetLabels())
	})
}

func TestAgentConfigSpec_MergeConfig(t *testing.T) {
	t.Run("empty is ignored", func(t *testing.T) {
		nsConfig := AgentConfigSpec{
			ServiceAccount:             "porter-agent",
			InstallationServiceAccount: "installation-service-account",
			PluginConfigFile:           &PluginFileSpec{Plugins: map[string]Plugin{"plugin1": {Version: "1.0.0"}}},
		}

		instConfig := AgentConfigSpec{}

		config, err := nsConfig.MergeConfig(instConfig)
		require.NoError(t, err)
		assert.Equal(t, "porter-agent", config.ServiceAccount)
	})

	t.Run("overrides", func(t *testing.T) {
		systemConfig := AgentConfigSpec{}

		nsConfig := AgentConfigSpec{
			PorterRepository:           "base",
			PorterVersion:              "base",
			ServiceAccount:             "base",
			VolumeSize:                 "1Mi",
			PullPolicy:                 v1.PullIfNotPresent,
			InstallationServiceAccount: "base",
			PluginConfigFile:           &PluginFileSpec{Plugins: map[string]Plugin{"test-plugin": {FeedURL: "localhost:5000"}, "kubernetes": {}}},
		}

		instConfig := AgentConfigSpec{
			PorterRepository:           "override",
			PorterVersion:              "override",
			ServiceAccount:             "override",
			VolumeSize:                 "2Mi",
			PullPolicy:                 v1.PullAlways,
			InstallationServiceAccount: "override",
			PluginConfigFile:           &PluginFileSpec{Plugins: map[string]Plugin{"azure": {FeedURL: "localhost:6000"}}},
		}

		config, err := systemConfig.MergeConfig(nsConfig, instConfig)
		require.NoError(t, err)
		assert.Equal(t, "override", config.PorterRepository)
		assert.Equal(t, "override", config.PorterVersion)
		assert.Equal(t, "override", config.ServiceAccount)
		assert.Equal(t, "2Mi", config.VolumeSize)
		assert.Equal(t, v1.PullAlways, config.PullPolicy)
		assert.Equal(t, "override", config.InstallationServiceAccount)
		assert.Equal(t, &PluginFileSpec{Plugins: map[string]Plugin{"azure": {FeedURL: "localhost:6000"}}}, config.PluginConfigFile)
	})
}

func TestAgentConfig_MergeConfigs(t *testing.T) {
	t.Run("empty is ignored", func(t *testing.T) {
		nsSpec := AgentConfigSpec{
			ServiceAccount:             "porter-agent",
			InstallationServiceAccount: "installation-service-account",
			PorterVersion:              "v1.0.5",
			PluginConfigFile:           &PluginFileSpec{Plugins: map[string]Plugin{"plugin1": {Version: "1.0.0"}}},
		}
		nsConfig := AgentConfig{Spec: nsSpec}

		instConfig := AgentConfig{}

		config, err := nsConfig.MergeConfigs(instConfig)
		require.NoError(t, err)
		assert.Equal(t, "porter-agent", config.Spec.ServiceAccount)
		assert.Equal(t, "v1.0.5", config.Spec.PorterVersion)
		assert.Equal(t, nsSpec.PluginConfigFile, config.Spec.PluginConfigFile)
	})
	t.Run("overrides", func(t *testing.T) {
		systemConfig := AgentConfig{}

		nsSpec := AgentConfigSpec{
			PorterRepository:           "base",
			PorterVersion:              "base",
			ServiceAccount:             "base",
			VolumeSize:                 "1Mi",
			PullPolicy:                 v1.PullIfNotPresent,
			InstallationServiceAccount: "base",
			PluginConfigFile:           &PluginFileSpec{Plugins: map[string]Plugin{"test-plugin": {FeedURL: "localhost:5000"}, "kubernetes": {}}},
		}

		nsConfig := AgentConfig{Spec: nsSpec}

		instSpec := AgentConfigSpec{
			PorterRepository:           "override",
			PorterVersion:              "override",
			ServiceAccount:             "override",
			VolumeSize:                 "2Mi",
			PullPolicy:                 v1.PullAlways,
			InstallationServiceAccount: "override",
			PluginConfigFile:           &PluginFileSpec{Plugins: map[string]Plugin{"azure": {FeedURL: "localhost:6000"}}},
		}
		instConfig := AgentConfig{Spec: instSpec}

		config, err := systemConfig.MergeConfigs(nsConfig, instConfig)
		require.NoError(t, err)
		assert.Equal(t, "override", config.Spec.PorterRepository)
		assert.Equal(t, "override", config.Spec.PorterVersion)
		assert.Equal(t, "override", config.Spec.ServiceAccount)
		assert.Equal(t, "2Mi", config.Spec.VolumeSize)
		assert.Equal(t, v1.PullAlways, config.Spec.PullPolicy)
		assert.Equal(t, "override", config.Spec.InstallationServiceAccount)
		assert.Equal(t, &PluginFileSpec{Plugins: map[string]Plugin{"azure": {FeedURL: "localhost:6000"}}}, config.Spec.PluginConfigFile)
	})
}

func TestAgentConfigSpecAdapter_ToPorterDocument(t *testing.T) {
	wantGoldenFile := "testdata/plugins.yaml"
	type fields struct {
		SchemaVersion string
		Plugins       map[string]Plugin
	}
	tests := []struct {
		name       string
		fields     fields
		wantFile   string
		wantErrMsg string
	}{
		{
			name: "golden file test",
			fields: fields{
				SchemaVersion: string(plugins.InstallPluginsSchemaVersion),
				Plugins: map[string]Plugin{
					"plugin1": {
						Version: "v1.0.0",
						FeedURL: "http://example.com",
						Mirror:  "http://example.com",
						URL:     "test",
					},
					"plugin2": {
						Version: "v2.0.0",
						FeedURL: "http://example.com",
						Mirror:  "http://example.com",
						URL:     "test",
					},
				},
			},
			wantFile:   wantGoldenFile,
			wantErrMsg: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := PluginFileSpec{
				SchemaVersion: tt.fields.SchemaVersion,
				Plugins:       tt.fields.Plugins,
			}
			adapter := NewAgentConfigSpecAdapter(AgentConfigSpec{PluginConfigFile: &spec})

			got, err := adapter.ToPorterDocument()
			if tt.wantErrMsg == "" {
				require.NoError(t, err)
				portertest.CompareGoldenFile(t, tt.wantFile, string(got))
			} else {
				portertests.RequireErrorContains(t, err, tt.wantErrMsg)
			}
		})
	}
}

func TestAgentConfigSpecAdapter_GetRetryLimit(t *testing.T) {
	var testdataNonZero int32 = 2
	var testdataZero int32 = 0
	testcases := []struct {
		name       string
		retryLimit *int32
		expected   *int32
	}{
		{name: "non-zero value", retryLimit: &testdataNonZero, expected: &testdataNonZero},
		{name: "set to 0", retryLimit: &testdataZero, expected: &testdataZero},
		{name: "not defined", retryLimit: nil, expected: nil},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			adapter := NewAgentConfigSpecAdapter(AgentConfigSpec{
				RetryLimit: tc.retryLimit,
			})
			result := adapter.GetRetryLimit()
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestHashString(t *testing.T) {
	str := hashString("fake-string")
	assert.Equal(t, "ab19e45285992b247dd281213f803479", str)
}
