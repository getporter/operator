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

// Parameter defines an element in a ParameterSet
type Parameter struct {
	// Name is the bundle parameter name
	Name string `json:"name" yaml:"name"`

	//Source is the bundle parameter source
	//supported: secret, value
	//unsupported: file path(via configMap), env var, shell cmd
	Source ParameterSource `json:"source" yaml:"source"`
}

type ParameterSource struct {
	// Secret is a parameter source using a secret plugin
	Secret string `json:"secret,omitempty" yaml:"secret,omitempty"`
	// Value is a paremeter source using plaintext value
	Value string `json:"value,omitempty" yaml:"value,omitempty"`
}

// ParameterSetSpec defines the desired state of ParameterSet
type ParameterSetSpec struct {
	// AgentConfig is the name of an AgentConfig to use instead of the AgentConfig defined at the namespace or system level.
	// +optional
	AgentConfig *corev1.LocalObjectReference `json:"agentConfig,omitempty" yaml:"-"`

	// PorterConfig is the name of a PorterConfig to use instead of the PorterConfig defined at the namespace or system level.
	PorterConfig *corev1.LocalObjectReference `json:"porterConfig,omitempty" yaml:"-"`
	//
	// These are fields from the Porter parameter set resource.
	// Your goal is that someone can copy/paste a resource from Porter into the
	// spec and have it work. So be consistent!
	//

	// SchemaVersion is the version of the parameter set state schema.
	SchemaVersion string `json:"schemaVersion" yaml:"schemaVersion"`

	// Name is the name of the parameter set in Porter. Immutable.
	Name string `json:"name" yaml:"name"`

	// Namespace (in Porter) where the parameter set is defined.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Parameters list of bundle parameters in the parameter set.
	Parameters []Parameter `json:"parameters" yaml:"parameters"`
}

func (ps ParameterSetSpec) ToPorterDocument() ([]byte, error) {
	b, err := yaml.Marshal(ps)
	return b, errors.Wrap(err, "error converting the ParameterSet spec into its Porter resource representation")
}

// ParameterSetStatus defines the observed state of ParameterSet
type ParameterSetStatus struct {
	PorterResourceStatus `json:",inline"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ParameterSet is the Schema for the parametersets API
type ParameterSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ParameterSetSpec   `json:"spec,omitempty"`
	Status ParameterSetStatus `json:"status,omitempty"`
}

func (ps *ParameterSet) GetStatus() PorterResourceStatus {
	return ps.Status.PorterResourceStatus
}

func (ps *ParameterSet) SetStatus(value PorterResourceStatus) {
	ps.Status.PorterResourceStatus = value
}

// GetRetryLabelValue returns a value that is safe to use
// as a label value and represents the retry annotation used
// to trigger reconciliation.
func (ps *ParameterSet) GetRetryLabelValue() string {
	return getRetryLabelValue(ps.Annotations)
}

// SetRetryAnnotation flags the resource to retry its last operation.
func (ps *ParameterSet) SetRetryAnnotation(retry string) {
	if ps.Annotations == nil {
		ps.Annotations = make(map[string]string, 1)
	}
	ps.Annotations[AnnotationRetry] = retry
}

//+kubebuilder:object:root=true

// ParameterSetList contains a list of ParameterSet
type ParameterSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ParameterSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ParameterSet{}, &ParameterSetList{})
}
