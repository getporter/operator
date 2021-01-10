package v1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// BundleInstallationSpec defines the desired state of BundleInstallation
type BundleInstallationSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Reference to the bundle in an OCI Registry, e.g. getporter/porter-hello:v0.1.1.
	Reference string `json:"reference"`

	// Action defined in the bundle to execute. If unspecified, Porter will run an
	// install if the installation does not exist, or an upgrade otherwise.
	Action string `json:"action"`

	// PorterVersion is the version of the Porter CLI to use when executing the bundle.
	// Defaults to "latest"
	PorterVersion string `json:"porterVersion,omitempty"`
	// TODO: Define credential and parameter sets
}

// BundleInstallationStatus defines the observed state of BundleInstallation
type BundleInstallationStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	ActiveJob v1.LocalObjectReference `json:"activeJob,omitempty"`
	LastJob   v1.LocalObjectReference `json:"lastJob,omitempty"`
	// TODO: Include values from the claim such as success/failure, last action
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// BundleInstallation is the Schema for the bundleinstallations API
type BundleInstallation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BundleInstallationSpec   `json:"spec,omitempty"`
	Status BundleInstallationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// BundleInstallationList contains a list of BundleInstallation
type BundleInstallationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BundleInstallation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BundleInstallation{}, &BundleInstallationList{})
}
