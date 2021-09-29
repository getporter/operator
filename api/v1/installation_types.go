package v1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InstallationSpec defines the desired state of Installation
type InstallationSpec struct {
	// AgentConfig is the name of an AgentConfig to use instead of the Porter Agent configuration defined at the namespace or system level.
	// +optional
	AgentConfig v1.LocalObjectReference `json:"agentConfig,omitempty"`

	// TODO: Add reference to a porter config.toml secret

	// TODO: Force pull, debug and other flags

	//
	// These are fields from the Porter installation resource
	//

	// SchemaVersion is the version of the installation state schema.
	SchemaVersion string `json:"schemaVersion"`

	// InstallationName is the name of the installation in Porter. Immutable.
	InstallationName string `json:"installationName"`

	// TargetNamespace (in Porter) where the installation is defined.
	TargetNamespace string `json:"targetNamespace"`

	// BundleRepository is the OCI repository of the current bundle definition.
	BundleRepository string `json:"bundleRepository,omitempty"`

	// BundleVersion is the current version of the bundle.
	BundleVersion string `json:"bundleVersion,omitempty"`

	// BundleDigest is the current digest of the bundle.
	BundleDigest string `json:"bundleDigest,omitempty"`

	// BundleTag is the OCI tag of the current bundle definition.
	BundleTag string `json:"bundleTag,omitempty"`

	// Labels applied to the installation.
	InstallationLabels map[string]string `json:"installationLabels,omitempty"`

	// Parameters specified by the user through overrides.
	// Does not include defaults, or values resolved from parameter sources.
	Parameters map[string]interface{} `json:"parameters,omitempty" yaml:"parameters,omitempty" toml:"parameters,omitempty"`

	// CredentialSets that should be included when the bundle is reconciled.
	CredentialSets []string `json:"credentialSets,omitempty" yaml:"credentialSets,omitempty" toml:"credentialSets,omitempty"`

	// ParameterSets that should be included when the bundle is reconciled.
	ParameterSets []string `json:"parameterSets,omitempty" yaml:"parameterSets,omitempty" toml:"parameterSets,omitempty"`
}

// InstallationStatus defines the observed state of Installation
type InstallationStatus struct {
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

func init() {
	SchemeBuilder.Register(&Installation{}, &InstallationList{})
}
