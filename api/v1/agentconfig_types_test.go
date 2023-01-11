package v1

import (
	"testing"

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
			Plugins: map[string]Plugin{
				"kubernetes": {Version: "v1.0.0", FeedURL: "https://test"},
			},
		}
		cl := NewAgentConfigSpecAdapter(c)
		assert.Equal(t, "porter-922e7fa0a39ba2abcc6456da47290a00", cl.GetPluginsPVCName("default"))
	})

	t.Run("multiple plugins defined", func(t *testing.T) {
		c := AgentConfigSpec{
			Plugins: map[string]Plugin{
				"kubernetes": {Version: "v1.0.0", FeedURL: "https://test"},
				"azure":      {Version: "v1.0.0", URL: "https://test"},
				"hashicorp":  {Version: "v1.0.0", Mirror: "https://test"},
			},
		}
		cl := NewAgentConfigSpecAdapter(c)
		assert.Equal(t, "porter-c5f38cf969f470e9c1e2890ae77d0452", cl.GetPluginsPVCName("default"))

		// change the order of the plugins should not affect the name output.
		c2 := AgentConfigSpec{
			Plugins: map[string]Plugin{
				"azure":      {Version: "v1.0.0", FeedURL: "https://test"},
				"hashicorp":  {Version: "v1.0.0", URL: "https://test"},
				"kubernetes": {Version: "v1.0.0", Mirror: "https://test"},
			},
		}
		cl2 := NewAgentConfigSpecAdapter(c2)
		assert.Equal(t, "porter-c5f38cf969f470e9c1e2890ae77d0452", cl2.GetPluginsPVCName("default"))
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
			Plugins: map[string]Plugin{
				"kubernetes": {Version: "v1.0.0", FeedURL: "https://test"},
			},
		}
		cl := NewAgentConfigSpecAdapter(onePluginCfg)
		assert.Equal(t, map[string]string{LabelManaged: "true", LabelPluginsHash: "kubernetes_test_v1.0.0"}, cl.Plugins.GetLabels())
	})

	t.Run("multiple plugins defined", func(t *testing.T) {
		multiplePluginsCfg := AgentConfigSpec{
			Plugins: map[string]Plugin{
				"kubernetes": {Version: "v1.0.0", FeedURL: "https://test"},
				"azure":      {Version: "v1.2.0", URL: "https://test1"},
				"hashicorp":  {Version: "v1.0.0", FeedURL: "https://test"},
			},
		}
		mcl := NewAgentConfigSpecAdapter(multiplePluginsCfg)
		assert.Equal(t, map[string]string{LabelManaged: "true", LabelPluginsHash: "azure_test1_v1.2.0_hashicorp_test_v1.0.0_kubernetes_test_v1.0.0"}, mcl.Plugins.GetLabels())

		multiplePluginsCfgWithDifferentOrder := AgentConfigSpec{
			Plugins: map[string]Plugin{
				"hashicorp":  {Version: "v1.0.0", FeedURL: "https://test"},
				"azure":      {Version: "v1.2.0", URL: "https://test1"},
				"kubernetes": {Version: "v1.0.0", FeedURL: "https://test"},
			},
		}
		mclWithDifferentOrder := NewAgentConfigSpecAdapter(multiplePluginsCfgWithDifferentOrder)
		assert.Equal(t, map[string]string{LabelManaged: "true", LabelPluginsHash: "azure_test1_v1.2.0_hashicorp_test_v1.0.0_kubernetes_test_v1.0.0"}, mclWithDifferentOrder.Plugins.GetLabels())
	})
}

func TestAgentConfigSpec_MergeConfig(t *testing.T) {
	t.Run("empty is ignored", func(t *testing.T) {
		nsConfig := AgentConfigSpec{
			ServiceAccount:             "porter-agent",
			InstallationServiceAccount: "installation-service-account",
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
			Plugins:                    map[string]Plugin{"test-plugin": {FeedURL: "localhost:5000"}, "kubernetes": {}},
		}

		instConfig := AgentConfigSpec{
			PorterRepository:           "override",
			PorterVersion:              "override",
			ServiceAccount:             "override",
			VolumeSize:                 "2Mi",
			PullPolicy:                 v1.PullAlways,
			InstallationServiceAccount: "override",
			Plugins:                    map[string]Plugin{"azure": {FeedURL: "localhost:6000"}},
		}

		config, err := systemConfig.MergeConfig(nsConfig, instConfig)
		require.NoError(t, err)
		assert.Equal(t, "override", config.PorterRepository)
		assert.Equal(t, "override", config.PorterVersion)
		assert.Equal(t, "override", config.ServiceAccount)
		assert.Equal(t, "2Mi", config.VolumeSize)
		assert.Equal(t, v1.PullAlways, config.PullPolicy)
		assert.Equal(t, "override", config.InstallationServiceAccount)
		assert.Equal(t, map[string]Plugin{"azure": {FeedURL: "localhost:6000"}}, config.Plugins)
	})
}
