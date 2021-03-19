package v1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InstallationSpec defines the desired state of Installation
type InstallationSpec struct {
	// Reference to the bundle in an OCI Registry, e.g. getporter/porter-hello:v0.1.1.
	Reference string `json:"reference"`

	// Action defined in the bundle to execute. If unspecified, Porter will run an
	// install if the installation does not exist, or an upgrade otherwise.
	Action string `json:"action"`

	// AgentConfig overrides the Porter Agent configuration defined at the namespace or system level.
	// +optional
	AgentConfig AgentConfigSpec `json:"agentConfig,omitempty"` // TODO: Make this a reference

	// TODO: Add reference to a porter config.toml secret

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
