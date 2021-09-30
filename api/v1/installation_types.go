package v1

import (
	"encoding/json"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InstallationSpec defines the desired state of Installation
type InstallationSpec struct {

	/* SERIALIZATION NOTE:
		The json serialization is for persisting this to Kubernetes.
	    The yaml serialization is for creating a Porter representation of the resource.
	*/

	// AgentConfig is the name of an AgentConfig to use instead of the AgentConfig defined at the namespace or system level.
	// +optional
	AgentConfig v1.LocalObjectReference `json:"agentConfig,omitempty" yaml:"-"`

	// PorterConfig is the name of a PorterConfig to use instead of the PorterConfig defined at the namespace or system level.
	PorterConfig v1.LocalObjectReference `json:"porterConfig,omitempty" yaml:"-"`

	//
	// These are fields from the Porter installation resource
	//

	// SchemaVersion is the version of the installation state schema.
	SchemaVersion string `json:"schemaVersion" yaml:"schemaVersion"`

	// InstallationName is the name of the installation in Porter. Immutable.
	InstallationName string `json:"installationName" yaml:"name"`

	// TargetNamespace (in Porter) where the installation is defined.
	TargetNamespace string `json:"targetNamespace" yaml:"namespace"`

	// BundleRepository is the OCI repository of the current bundle definition.
	BundleRepository string `json:"bundleRepository" yaml:"bundleRepository"`

	// BundleVersion is the current version of the bundle.
	BundleVersion string `json:"bundleVersion,omitempty" yaml:"bundleVersion,omitempty"`

	// BundleDigest is the current digest of the bundle.
	BundleDigest string `json:"bundleDigest,omitempty" yaml:"bundleDigest,omitempty"`

	// BundleTag is the OCI tag of the current bundle definition.
	BundleTag string `json:"bundleTag,omitempty" yaml:"bundleTag,omitempty"`

	// Labels applied to the installation.
	InstallationLabels map[string]string `json:"installationLabels,omitempty" yaml:"labels,omitempty"`

	// Parameters specified by the user through overrides.
	// Does not include defaults, or values resolved from parameter sources.
	Parameters map[string]json.RawMessage `json:"parameters,omitempty" yaml:"parameters,omitempty"`

	// CredentialSets that should be included when the bundle is reconciled.
	CredentialSets []string `json:"credentialSets,omitempty" yaml:"credentialSets,omitempty"`

	// ParameterSets that should be included when the bundle is reconciled.
	ParameterSets []string `json:"parameterSets,omitempty" yaml:"parameterSets,omitempty"`
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
