// +build !integration

package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestAgentConfigSpec_GetPorterImage(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		c := AgentConfigSpec{}
		assert.Equal(t, "ghcr.io/getporter/porter:kubernetes-latest", c.GetPorterImage())
	})

	t.Run("porter version set", func(t *testing.T) {
		c := AgentConfigSpec{PorterVersion: "canary"}
		assert.Equal(t, "ghcr.io/getporter/porter:kubernetes-canary", c.GetPorterImage())
	})

	t.Run("porter repository set", func(t *testing.T) {
		c := AgentConfigSpec{PorterRepository: "localhost:5000/myporter"}
		assert.Equal(t, "localhost:5000/myporter:kubernetes-latest", c.GetPorterImage())
	})

	t.Run("porter repository and version set", func(t *testing.T) {
		c := AgentConfigSpec{PorterRepository: "localhost:5000/myporter", PorterVersion: "v1.2.3"}
		assert.Equal(t, "localhost:5000/myporter:kubernetes-v1.2.3", c.GetPorterImage())
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
		c := AgentConfigSpec{VolumeSize: qty}
		assert.Equal(t, qty, c.GetVolumeSize())
	})
}
