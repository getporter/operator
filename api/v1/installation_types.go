package v1

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// InstallationSpec defines the desired state of Installation
type InstallationSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Reference to the bundle in an OCI Registry, e.g. getporter/porter-hello:v0.1.1.
	Reference string `json:"reference"`

	// Action defined in the bundle to execute. If unspecified, Porter will run an
	// install if the installation does not exist, or an upgrade otherwise.
	Action string `json:"action"`

	// AgentConfig overrides the Porter Agent configuration defined at the namespace or system level.
	AgentConfig AgentConfigSpec `json:"agentConfig,omitEmpty"`

	// TODO: Force pull, debug and other flags

	// CredentialSets is a list of credential set names.
	CredentialSets []string `json:"credentialSets,omitempty"`

	// ParameterSets is a list of parameter set names.
	ParameterSets []string `json:"parameterSets,omitempty"`

	// Parameters is a list of parameter set names.
	Parameters map[string]string `json:"parameters,omitempty"`
}

// InstallationStatus defines the observed state of Installation
type InstallationStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	ActiveJob v1.LocalObjectReference `json:"activeJob,omitempty"`
	LastJob   v1.LocalObjectReference `json:"lastJob,omitempty"`
	// TODO: Include values from the claim such as success/failure, last action
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Installation is the Schema for the installations API
type Installation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InstallationSpec   `json:"spec,omitempty"`
	Status InstallationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// InstallationList contains a list of Installation
type InstallationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Installation `json:"items"`
}

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
	SchemeBuilder.Register(&Installation{}, &InstallationList{})
	SchemeBuilder.Register(&AgentConfig{}, &AgentConfigList{})
}
