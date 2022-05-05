package v1

import (
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SERIALIZATION NOTE:
// * json tags are required for Kubernetes.  Any new fields you add must have json tags for the fields to be serialized.
// * yaml tags are required for Porter.  Any new fields you add must have yaml tags for the fields to be serialized.

// Credential defines a element in a CredentialSet
type Credential struct {
	//Name is the bundle credential name
	Name string `json:"name" yaml:"name"`

	//Source is the bundle credential source
	//supported: secret
	//unsupported: file path(via configMap), specific value, env var, shell cmd
	Source CredentialSource `json:"source" yaml:"source"`
}

// CredentialSource defines a element in a CredentialSet
type CredentialSource struct {
	//Secret is a credential source using a secret plugin
	Secret string `json:"secret,omitempty" yaml:"secret,omitempty"`
}

// CredentialSetSpec defines the desired state of CredentialSet
type CredentialSetSpec struct {
	// AgentConfig is the name of an AgentConfig to use instead of the AgentConfig defined at the namespace or system level.
	// +optional
	AgentConfig *corev1.LocalObjectReference `json:"agentConfig,omitempty" yaml:"-"`

	// PorterConfig is the name of a PorterConfig to use instead of the PorterConfig defined at the namespace or system level.
	PorterConfig *corev1.LocalObjectReference `json:"porterConfig,omitempty" yaml:"-"`
	//
	// These are fields from the Porter credential set resource.
	// Your goal is that someone can copy/paste a resource from Porter into the
	// spec and have it work. So be consistent!
	//

	// SchemaVersion is the version of the credential set state schema.
	SchemaVersion string `json:"schemaVersion" yaml:"schemaVersion"`

	// Name is the name of the credential set in Porter. Immutable.
	Name string `json:"name" yaml:"name"`

	// Namespace (in Porter) where the credential set is defined.
	Namespace string `json:"namespace" yaml:"namespace"`

	//Credentials list of bundle credentials in the credential set.
	Credentials []Credential `json:"credentials" yaml:"credentials"`
}

func (cs CredentialSetSpec) ToPorterDocument() ([]byte, error) {
	b, err := yaml.Marshal(cs)
	return b, errors.Wrap(err, "error converting the CredentialSet spec into its Porter resource representation")
}

// CredentialSetStatus defines the observed state of CredentialSet
type CredentialSetStatus struct {
	PorterResourceStatus `json:",inline"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// CredentialSet is the Schema for the credentialsets API
type CredentialSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CredentialSetSpec   `json:"spec,omitempty"`
	Status CredentialSetStatus `json:"status,omitempty"`
}

func (cs *CredentialSet) GetStatus() PorterResourceStatus {
	return cs.Status.PorterResourceStatus
}

func (cs *CredentialSet) SetStatus(value PorterResourceStatus) {
	cs.Status.PorterResourceStatus = value
}

// GetRetryLabelValue returns a value that is safe to use
// as a label value and represents the retry annotation used
// to trigger reconciliation.
func (cs *CredentialSet) GetRetryLabelValue() string {
	return getRetryLabelValue(cs.Annotations)
}

// SetRetryAnnotation flags the resource to retry its last operation.
func (cs *CredentialSet) SetRetryAnnotation(retry string) {
	if cs.Annotations == nil {
		cs.Annotations = make(map[string]string, 1)
	}
	cs.Annotations[AnnotationRetry] = retry
}

//+kubebuilder:object:root=true

// CredentialSetList contains a list of CredentialSet
type CredentialSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CredentialSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CredentialSet{}, &CredentialSetList{})
}
