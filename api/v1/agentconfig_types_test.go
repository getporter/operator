package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestAgentConfigSpec_GetPorterImage(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		c := AgentConfigSpec{}
		assert.Equal(t, "ghcr.io/getporter/porter-agent:v1.0.0-alpha.12", c.GetPorterImage())
	})

	t.Run("porter version set", func(t *testing.T) {
		c := AgentConfigSpec{PorterVersion: "canary"}
		assert.Equal(t, "ghcr.io/getporter/porter-agent:canary", c.GetPorterImage())
	})

	t.Run("porter repository set", func(t *testing.T) {
		// Test if someone has mirrored porter's agent to another registry
		c := AgentConfigSpec{PorterRepository: "localhost:5000/myporter"}
		assert.Equal(t, "localhost:5000/myporter:v1.0.0-alpha.12", c.GetPorterImage())
	})

	t.Run("porter repository and version set", func(t *testing.T) {
		c := AgentConfigSpec{PorterRepository: "localhost:5000/myporter", PorterVersion: "v1.2.3"}
		assert.Equal(t, "localhost:5000/myporter:v1.2.3", c.GetPorterImage())
	})

	t.Run("digest set", func(t *testing.T) {
		c := AgentConfigSpec{
			PorterVersion: "sha256:ea7d328dc6b65e4b62a971ba8436f89d5857c2878c211312aaa5e2db2e47a2da",
		}
		assert.Equal(t, "ghcr.io/getporter/porter-agent@sha256:ea7d328dc6b65e4b62a971ba8436f89d5857c2878c211312aaa5e2db2e47a2da", c.GetPorterImage())
	})
}

func TestAgentConfigSpec_GetPullPolicy(t *testing.T) {
	testcases := map[string]v1.PullPolicy{
		"":       v1.PullIfNotPresent,
		"latest": v1.PullAlways,
		"canary": v1.PullAlways,
		"v1.2.3": v1.PullIfNotPresent,
	}

	for version, wantPullPolicy := range testcases {
		t.Run("version "+version, func(t *testing.T) {
			c := AgentConfigSpec{PorterVersion: version}
			assert.Equal(t, wantPullPolicy, c.GetPullPolicy())
		})

	}
}

func TestAgentConfigSpec_GetVolumeSize(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		c := AgentConfigSpec{}
		assert.Equal(t, resource.MustParse("64Mi"), c.GetVolumeSize())
	})

	t.Run("quantity set", func(t *testing.T) {
		qty := resource.MustParse("128Mi")
		c := AgentConfigSpec{VolumeSize: "128Mi"}
		assert.Equal(t, qty, c.GetVolumeSize())
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
		}

		instConfig := AgentConfigSpec{
			PorterRepository:           "override",
			PorterVersion:              "override",
			ServiceAccount:             "override",
			VolumeSize:                 "2Mi",
			PullPolicy:                 v1.PullAlways,
			InstallationServiceAccount: "override",
		}

		config, err := systemConfig.MergeConfig(nsConfig, instConfig)
		require.NoError(t, err)
		assert.Equal(t, "override", config.PorterRepository)
		assert.Equal(t, "override", config.PorterVersion)
		assert.Equal(t, "override", config.ServiceAccount)
		assert.Equal(t, "2Mi", config.VolumeSize)
		assert.Equal(t, v1.PullAlways, config.PullPolicy)
		assert.Equal(t, "override", config.InstallationServiceAccount)
	})
}
