package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AgentActionSpec defines the desired state of AgentAction
type AgentActionSpec struct {
	// AgentConfig is the name of an AgentConfig to use instead of the AgentConfig defined at the namespace or system level.
	// +optional
	AgentConfig *corev1.LocalObjectReference `json:"agentConfig,omitempty"`

	// PorterConfig is the name of a PorterConfig to use instead of the PorterConfig defined at the namespace or system level.
	PorterConfig *corev1.LocalObjectReference `json:"porterConfig,omitempty"`

	// Command to run inside the Porter Agent job. Defaults to running the agent.
	Command []string `json:"command,omitempty"`

	// Args to pass to the Porter Agent job. This should be the porter command that you want to run.
	Args []string `json:"args,omitempty"`

	// Files that should be present in the working directory where the command is run.
	Files map[string][]byte `json:"files,omitempty"`

	// Env variables to set on the Porter Agent job.
	Env []corev1.EnvVar `json:"env,omitempty"`

	// EnvFrom allows setting environment variables on the Porter Agent job, using secrets or config maps as the source.
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`

	// VolumeMounts that should be defined on the Porter Agent job.
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`

	// Volumes that should be defined on the Porter Agent job.
	Volumes []corev1.Volume `json:"volumes,omitempty"`
}

// AgentActionStatus defines the observed state of AgentAction
type AgentActionStatus struct {
	// The last generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// The currently active job that is running the Porter Agent.
	Job *corev1.LocalObjectReference `json:"job,omitempty"`

	// The current status of the agent.
	// Possible values are: Unknown, Pending, Running, Succeeded, and Failed.
	// +kubebuilder:validation:Type=string
	Phase AgentPhase `json:"phase,omitempty"`

	// Conditions store a list of states that have been reached.
	// Each condition refers to the status of the Job
	// Possible conditions are: Scheduled, Started, Completed, and Failed
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// AgentAction is the Schema for the agentactions API
type AgentAction struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentActionSpec   `json:"spec,omitempty"`
	Status AgentActionStatus `json:"status,omitempty"`
}

func (a *AgentAction) GetConditions() *[]metav1.Condition {
	return &a.Status.Conditions
}

// GetRetryLabelValue returns a value that is safe to use
// as a label value and represents the retry annotation used
// to trigger reconciliation.
func (a *AgentAction) GetRetryLabelValue() string {
	return getRetryLabelValue(a.Annotations)
}

// SetRetryAnnotation flags the resource to retry its last operation.
func (a *AgentAction) SetRetryAnnotation(retry string) {
	if a.Annotations == nil {
		a.Annotations = make(map[string]string, 1)
	}
	a.Annotations[AnnotationRetry] = retry
}

// +kubebuilder:object:root=true

// AgentActionList contains a list of AgentAction
type AgentActionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentAction `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AgentAction{}, &AgentActionList{})
}
