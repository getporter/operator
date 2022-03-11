package v1

import (
	"crypto/md5"
	"encoding/hex"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PorterResourceStatus struct {
	// The last generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// The most recent action executed for the resource
	Action *corev1.LocalObjectReference `json:"action,omitempty"`

	// The current status of the agent.
	// Possible values are: Unknown, Pending, Running, Succeeded, and Failed.
	// +kubebuilder:validation:Type=string
	Phase AgentPhase `json:"phase,omitempty"`

	// Conditions store a list of states that have been reached.
	// Each condition refers to the status of the ActiveJob
	// Possible conditions are: Scheduled, Started, Completed, and Failed
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// Initialize resets the resource status before Porter is run.
// This wipes out the status from any previous runs.
func (s *PorterResourceStatus) Initialize() {
	s.Conditions = []metav1.Condition{}
	s.Phase = PhaseUnknown
	s.Action = nil
}

// GetRetryLabelValue returns a value that is safe to use
// as a label value and represents the retry annotation used
// to trigger reconciliation. Annotations don't have limits on
// the value, but labels are restricted to alphanumeric and .-_
// I am just hashing the annotation value here to avoid problems
// using it directly as a label value.
func getRetryLabelValue(annotations map[string]string) string {
	retry := annotations[AnnotationRetry]
	if retry == "" {
		return ""
	}
	sum := md5.Sum([]byte(retry))
	return hex.EncodeToString(sum[:])
}
