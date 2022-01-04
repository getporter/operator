package v1

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
	v1 "k8s.io/api/core/v1"
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
	AgentConfig v1.LocalObjectReference `json:"agentConfig,omitempty" yaml:"-"`

	// PorterConfig is the name of a PorterConfig to use instead of the PorterConfig defined at the namespace or system level.
	PorterConfig v1.LocalObjectReference `json:"porterConfig,omitempty" yaml:"-"`

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

	// Active specifies if the bundle should be installed.
	Active bool `json:"active" yaml:"active"`

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
	// The last generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// The currently active job that is running Porter.
	ActiveJob *v1.LocalObjectReference `json:"activeJob,omitempty"`

	// The current status of the installation
	// Possible values are: Unknown, Pending, Running, Succeeded, and Failed.
	// +kubebuilder:validation:Type=string
	Phase InstallationPhase `json:"phase,omitempty"`

	// Conditions store a list of states that have been reached.
	// Each condition refers to the status of the ActiveJob
	// Possible conditions are: Scheduled, Started, Completed, and Failed
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// Reset the installation status before Porter is run.
// This wipes out the status from any previous runs.
func (s *InstallationStatus) Initialize() {
	s.Conditions = []metav1.Condition{}
	s.Phase = PhaseUnknown
	s.ActiveJob = nil
}

// These are valid statuses for an Installation.
type InstallationPhase string

const (
	// PhaseUnknown means that we don't know what porter is doing yet.
	PhaseUnknown InstallationPhase = "Unknown"

	// PhasePending means that Porter's execution is pending.
	PhasePending InstallationPhase = "Pending"

	// PhasePending indicates that Porter is running.
	PhaseRunning InstallationPhase = "Running"

	// PhaseSucceeded means that calling Porter succeeded.
	PhaseSucceeded InstallationPhase = "Succeeded"

	// PhaseFailed means that calling Porter failed.
	PhaseFailed InstallationPhase = "Failed"
)

// These are valid conditions of an Installation.
type InstallationConditionType string

const (
	// RunScheduled means that the Porter run has been scheduled.
	ConditionScheduled InstallationConditionType = "Scheduled"

	// RunStarted means that the Porter run has started.
	ConditionStarted InstallationConditionType = "Started"

	// RunComplete means the Porter run has completed successfully.
	ConditionComplete InstallationConditionType = "Completed"

	// RunFailed means the Porter run failed.
	ConditionFailed InstallationConditionType = "Failed"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Installation is the Schema for the installations API
type Installation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InstallationSpec   `json:"spec,omitempty"`
	Status InstallationStatus `json:"status,omitempty"`
}

// BuidRetryLabel returns a value that is safe to use
// as a label value and represents the retry annotation used
// to trigger reconciliation. Annotations don't have limits on
// the value, but labels are restricted to alphanumeric and .-_
// I am just hashing the annotation value here to avoid problems
// using it directly as a label value.
func (i Installation) GetRetryLabelValue() string {
	retry := i.Annotations[AnnotationRetry]
	if retry == "" {
		return ""
	}
	sum := md5.Sum([]byte(retry))
	return hex.EncodeToString(sum[:])
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
