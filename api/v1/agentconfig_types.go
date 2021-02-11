package v1

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AgentConfigSpec defines the configuration for the Porter agent.
type AgentConfigSpec struct {
	// PorterRepository is the repository for the Porter Agent image.
	// Defaults to ghcr.io/getporter/porter
	PorterRepository string `json:"porterRepository,omitempty"`

	// PorterVersion is the tag for the Porter Agent image.
	// Defaults to latest.
	PorterVersion string `json:"porterVersion,omitempty"`

	// ServiceAccount is the service account to run the Porter Agent under.
	ServiceAccount string `json:"serviceAccount,omitempty"`

	// VolumeSize is the size of the persistent volume that Porter will
	// request when running the Porter Agent. It is used to share data
	// between the Porter Agent and the bundle invocation image. It must
	// be large enough to store any files used by the bundle including credentials,
	// parameters and outputs.
	VolumeSize resource.Quantity `json:"volumeSize,omitempty"`

	// PullPolicy specifies when to pull the Porter Agent image. The default
	// is to use PullAlways when the tag is canary or latest, and PullIfNotPresent
	// otherwise.
	PullPolicy v1.PullPolicy `json:"pullPolicy,omitempty"`
}

// GetPorterImage returns the fully qualified image name of the Porter Agent
// image. Defaults the repository and tag when not set.
func (c AgentConfigSpec) GetPorterImage() string {
	version := c.PorterVersion
	if version == "" {
		version = "latest"
	}
	repo := c.PorterRepository
	if repo == "" {
		repo = "ghcr.io/getporter/porter"
	}
	return fmt.Sprintf("%s:kubernetes-%s", repo, version)
}

// GetPullPolicy returns the PullPolicy that should be used for the Porter Agent
// (not the bundle). Defaults to PullAlways for latest and canary,
// PullIfNotPresent otherwise.
func (c AgentConfigSpec) GetPullPolicy() v1.PullPolicy {
	if c.PullPolicy != "" {
		return c.PullPolicy
	}

	if c.PorterVersion == "latest" || c.PorterVersion == "canary" {
		return v1.PullAlways
	}
	return v1.PullIfNotPresent
}

// GetVolumeSize returns the size of the shared volume to mount between the
// Porter Agent and the bundle's invocation image. Defaults to 64Mi.
func (c AgentConfigSpec) GetVolumeSize() resource.Quantity {
	if c.VolumeSize.IsZero() {
		return resource.MustParse("64Mi")
	}
	return c.VolumeSize
}

// MergeConfig from another AgentConfigSpec. The values from the override are applied
// only when they are not empty.
func (c AgentConfigSpec) MergeConfig(override AgentConfigSpec) AgentConfigSpec {
	if override.PorterRepository != "" {
		c.PorterRepository = override.PorterRepository
	}

	if override.PorterVersion != "" {
		c.PorterVersion = override.PorterVersion
	}

	if override.ServiceAccount != "" {
		c.ServiceAccount = override.ServiceAccount
	}

	if !override.VolumeSize.IsZero() {
		c.VolumeSize = override.VolumeSize
	}

	return c
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// AgentConfig is the Schema for the agentconfigs API
type AgentConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              AgentConfigSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// AgentConfigList contains a list of AgentConfig values.
type AgentConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AgentConfig{}, &AgentConfigList{})
}
