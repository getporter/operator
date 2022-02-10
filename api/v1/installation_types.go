package v1

import (
	"encoding/json"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	Prefix          = "porter.sh/"
	AnnotationRetry = Prefix + "retry"
)

// We marshal installation spec to yaml when converting to a porter object
var _ yaml.Marshaler = InstallationSpec{}

// InstallationSpec defines the desired state of Installation
//
// SERIALIZATION NOTE:
// * The json serialization is for persisting this to Kubernetes.
// * The yaml serialization is for creating a Porter representation of the resource.
type InstallationSpec struct {
	// AgentConfig is the name of an AgentConfig to use instead of the AgentConfig defined at the namespace or system level.
	// +optional
	AgentConfig *corev1.LocalObjectReference `json:"agentConfig,omitempty" yaml:"-"`

	// PorterConfig is the name of a PorterConfig to use instead of the PorterConfig defined at the namespace or system level.
	PorterConfig *corev1.LocalObjectReference `json:"porterConfig,omitempty" yaml:"-"`

	//
	// These are fields from the Porter installation resource.
	// Your goal is that someone can copy/paste a resource from Porter into the
	// spec and have it work. So be consistent!
	//

	// SchemaVersion is the version of the installation state schema.
	SchemaVersion string `json:"schemaVersion" yaml:"schemaVersion"`

	// Name is the name of the installation in Porter. Immutable.
	Name string `json:"name" yaml:"name"`

	// Namespace (in Porter) where the installation is defined.
	Namespace string `json:"namespace" yaml:"namespace"`

	// Uninstalled specifies if the installation should be uninstalled.
	Uninstalled bool `json:"uninstalled,omitempty" yaml:"uninstalled,omitempty"`

	// Bundle definition for the installation.
	Bundle OCIReferenceParts `json:"bundle" yaml:"bundle"`

	// Labels applied to the installation.
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`

	// Parameters specified by the user through overrides.
	// Does not include defaults, or values resolved from parameter sources.
	// +kubebuilder:pruning:PreserveUnknownFields
	Parameters runtime.RawExtension `json:"parameters,omitempty" yaml:"-"` // See custom marshaler below

	// CredentialSets that should be included when the bundle is reconciled.
	CredentialSets []string `json:"credentialSets,omitempty" yaml:"credentialSets,omitempty"`

	// ParameterSets that should be included when the bundle is reconciled.
	ParameterSets []string `json:"parameterSets,omitempty" yaml:"parameterSets,omitempty"`
}

type OCIReferenceParts struct {
	// Repository is the OCI repository of the current bundle definition.
	Repository string `json:"repository" yaml:"repository"`

	// Version is the current version of the bundle.
	Version string `json:"version,omitempty" yaml:"version,omitempty"`

	// Digest is the current digest of the bundle.
	Digest string `json:"digest,omitempty" yaml:"digest,omitempty"`

	// Tag is the OCI tag of the current bundle definition.
	Tag string `json:"tag,omitempty" yaml:"tag,omitempty"`
}

// ToPorterDocument converts from the Kubernetes representation of the Installation into Porter's resource format.
func (in InstallationSpec) ToPorterDocument() ([]byte, error) {
	b, err := yaml.Marshal(in)
	return b, errors.Wrap(err, "error converting the Installation spec into its Porter resource representation")
}

func (in InstallationSpec) MarshalYAML() (interface{}, error) {
	type Alias InstallationSpec

	raw := struct {
		Alias      `yaml:",inline"`
		Parameters map[string]interface{} `yaml:"parameters,omitempty"`
	}{
		Alias: Alias(in),
	}

	if in.Parameters.Raw != nil {
		err := json.Unmarshal(in.Parameters.Raw, &raw.Parameters)
		if err != nil {
			return nil, errors.Wrapf(err, "error unmarshaling raw parameters\n%s", string(in.Parameters.Raw))
		}
	}

	return raw, nil
}

// InstallationStatus defines the observed state of Installation
type InstallationStatus struct {
	PorterResourceStatus `json:",inline"`
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

func (i *Installation) GetStatus() PorterResourceStatus {
	return i.Status.PorterResourceStatus
}

func (i *Installation) SetStatus(value PorterResourceStatus) {
	i.Status.PorterResourceStatus = value
}

// GetRetryLabelValue returns a value that is safe to use
// as a label value and represents the retry annotation used
// to trigger reconciliation.
func (i *Installation) GetRetryLabelValue() string {
	return getRetryLabelValue(i.Annotations)
}

// SetRetryAnnotation flags the resource to retry its last operation.
func (i *Installation) SetRetryAnnotation(retry string) {
	if i.Annotations == nil {
		i.Annotations = make(map[string]string, 1)
	}
	i.Annotations[AnnotationRetry] = retry
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
