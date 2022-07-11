package v1

import (
	"fmt"

	"github.com/mitchellh/mapstructure"
	"github.com/opencontainers/go-digest"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AgentConfigSpec defines the configuration for the Porter agent.
//
// SERIALIZATION NOTE:
//
//		The json serialization is for persisting this to Kubernetes.
//	 The mapstructure tags is used internally for AgentConfigSpec.MergeConfig.
type AgentConfigSpec struct {
	// PorterRepository is the repository for the Porter Agent image.
	// Defaults to ghcr.io/getporter/porter-agent
	PorterRepository string `json:"porterRepository,omitempty" mapstructure:"porterRepository,omitempty"`

	// PorterVersion is the tag for the Porter Agent image.
	// Defaults to a well-known version of the agent that has been tested with the operator.
	// Users SHOULD override this to use more recent versions.
	PorterVersion string `json:"porterVersion,omitempty" mapstructure:"porterVersion,omitempty"`

	// ServiceAccount is the service account to run the Porter Agent under.
	ServiceAccount string `json:"serviceAccount,omitempty" mapstructure:"serviceAccount,omitempty"`

	// VolumeSize is the size of the persistent volume that Porter will
	// request when running the Porter Agent. It is used to share data
	// between the Porter Agent and the bundle invocation image. It must
	// be large enough to store any files used by the bundle including credentials,
	// parameters and outputs.
	VolumeSize string `json:"volumeSize,omitempty" mapstructure:"volumeSize,omitempty"`

	// PullPolicy specifies when to pull the Porter Agent image. The default
	// is to use PullAlways when the tag is canary or latest, and PullIfNotPresent
	// otherwise.
	PullPolicy v1.PullPolicy `json:"pullPolicy,omitempty" mapstructure:"pullPolicy,omitempty"`

	// InstallationServiceAccount specifies a service account to run the Kubernetes pod/job for the installation image.
	// The default is to run without a service account.
	// This can be useful for a bundle which is targeting the kubernetes cluster that the operator is installed in.
	InstallationServiceAccount string `json:"installationServiceAccount,omitempty" mapstructure:"installationServiceAccount,omitempty"`

	Plugins PluginList `json:"plugins,omitempty" mapstructure:"plugins,omitempty"`
}

// GetPorterImage returns the fully qualified image name of the Porter Agent
// image. Defaults the repository and tag when not set.
func (c AgentConfigSpec) GetPorterImage() string {
	version := c.PorterVersion
	if version == "" {
		// We don't use a mutable tag like latest, or canary because it's a bad practice that we don't want to encourage.
		version = DefaultPorterAgentVersion
	}
	repo := c.PorterRepository
	if repo == "" {
		repo = DefaultPorterAgentRepository
	}

	if digest, err := digest.Parse(version); err == nil {
		return fmt.Sprintf("%s@%s", repo, digest)
	}

	return fmt.Sprintf("%s:%s", repo, version)
}

// GetPullPolicy returns the PullPolicy that should be used for the Porter Agent
// (not the bundle). Defaults to PullAlways for latest and canary,
// PullIfNotPresent otherwise.
func (c AgentConfigSpec) GetPullPolicy() v1.PullPolicy {
	if c.PullPolicy != "" {
		return c.PullPolicy
	}

	if c.PorterVersion == "latest" || c.PorterVersion == "canary" || c.PorterVersion == "dev" {
		return v1.PullAlways
	}
	return v1.PullIfNotPresent
}

// GetVolumeSize returns the size of the shared volume to mount between the
// Porter Agent and the bundle's invocation image. Defaults to 64Mi.
func (c AgentConfigSpec) GetVolumeSize() resource.Quantity {
	q, err := resource.ParseQuantity(c.VolumeSize)
	if err != nil || q.IsZero() {
		return resource.MustParse("64Mi")
	}
	return q
}

// MergeConfig from another AgentConfigSpec. The values from the override are applied
// only when they are not empty.
func (c AgentConfigSpec) MergeConfig(overrides ...AgentConfigSpec) (AgentConfigSpec, error) {
	final := c
	var targetRaw map[string]interface{}
	if err := mapstructure.Decode(c, &targetRaw); err != nil {
		return AgentConfigSpec{}, err
	}

	for _, override := range overrides {
		var overrideRaw map[string]interface{}
		if err := mapstructure.Decode(override, &overrideRaw); err != nil {
			return AgentConfigSpec{}, err
		}

		targetRaw = MergeMap(targetRaw, overrideRaw)
	}

	if err := mapstructure.Decode(targetRaw, &final); err != nil {
		return AgentConfigSpec{}, err
	}

	return final, nil
}

// AgentConfigStatus defines the observed state of AgentConfig
type AgentConfigStatus struct {
	PorterResourceStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// AgentConfig is the Schema for the agentconfigs API
type AgentConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentConfigSpec   `json:"spec,omitempty"`
	Status AgentConfigStatus `json:"status,omitempty"`
}

func (ac *AgentConfig) GetStatus() PorterResourceStatus {
	return ac.Status.PorterResourceStatus
}

func (ac *AgentConfig) SetStatus(value PorterResourceStatus) {
	ac.Status.PorterResourceStatus = value
}

// GetRetryLabelValue returns a value that is safe to use
// as a label value and represents the retry annotation used
// to trigger reconciliation.
func (ac *AgentConfig) GetRetryLabelValue() string {
	return getRetryLabelValue(ac.Annotations)
}

// SetRetryAnnotation flags the resource to retry its last operation.
func (ac *AgentConfig) SetRetryAnnotation(retry string) {
	if ac.Annotations == nil {
		ac.Annotations = make(map[string]string, 1)
	}
	ac.Annotations[AnnotationRetry] = retry
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

type PluginList []Plugin
type Plugin struct {
	Name    string `json:"name" yaml:"name" mapstructure:"name"`
	FeedURL string `json:"feedUrl" yaml:"feedUrl" mapstructure:"name"`
	Version string `json:"version" yaml:"version" mapstructure:"version"`
}
