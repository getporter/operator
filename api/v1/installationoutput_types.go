package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	InstallationOutputSucceeded  = "InstallationOutputSucceeded"
	AnnotationInstallationOutput = Prefix + "installationoutput"
)

type Output struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Sensitive bool   `json:"sensitive"`
	Value     string `json:"value"`
}

// InstallationOutputSpec defines the desired state of InstallationOutput
type InstallationOutputSpec struct {
	Name string `json:"name,omitempty"`

	Namespace string `json:"namespace,omitempty"`
}

// InstallationOutputStatus defines the observed state of InstallationOutput
type InstallationOutputStatus struct {
	Phase AgentPhase `json:"phase,omitempty"`

	Conditions []metav1.Condition `json:"conditions,omitempty"`

	Outputs []Output `json:"outputs,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// +kubebuilder:printcolumn:name="Porter Name",type="string",JSONPath=".spec.name"
// +kubebuilder:printcolumn:name="Porter Namespace",type="string",JSONPath=".spec.namespace"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// InstallationOutput is the Schema for the installationoutputs API
type InstallationOutput struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InstallationOutputSpec   `json:"spec,omitempty"`
	Status InstallationOutputStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// InstallationOutputList contains a list of InstallationOutput
type InstallationOutputList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InstallationOutput `json:"items"`
}

func init() {
	SchemeBuilder.Register(&InstallationOutput{}, &InstallationOutputList{})
}
