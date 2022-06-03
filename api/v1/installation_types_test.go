package v1

import (
	"testing"

	"get.porter.sh/porter/pkg/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestInstallationSpec_ToPorterDocument(t *testing.T) {
	// Validate the special handling for the arbitrary parameters
	// which the CRD can't directly represent as map[string]interface{}
	spec := InstallationSpec{
		SchemaVersion: "1.0.0",
		Name:          "mybuns",
		Namespace:     "dev",
		Bundle: OCIReferenceParts{
			Repository: "ghcr.io/getporter/porter-hello",
			Version:    "0.1.1",
		},
		Parameters: runtime.RawExtension{
			Raw: []byte(`{"name":"Porter Operator"}`),
		},
	}

	b, err := spec.ToPorterDocument()
	require.NoError(t, err)

	test.CompareGoldenFile(t, "testdata/installation.yaml", string(b))
}

func TestInstallationStatus_Initialize(t *testing.T) {
	s := &InstallationStatus{
		PorterResourceStatus: PorterResourceStatus{
			PorterStatus: PorterStatus{
				ObservedGeneration: 2,
				Phase:              PhaseSucceeded,
				Conditions: []metav1.Condition{
					{Type: string(ConditionComplete), Status: metav1.ConditionTrue},
				},
			},
			Action: &corev1.LocalObjectReference{Name: "something"},
		},
	}

	s.Initialize()

	assert.Equal(t, int64(2), s.ObservedGeneration, "ObservedGeneration should not be reset")
	assert.Empty(t, s.Conditions, "Conditions should be empty")
	assert.Nil(t, s.Action, "Active should be nil")
	assert.Equal(t, PhaseUnknown, s.Phase, "Phase should reset to Unknown")
}

func TestInstallation_SetRetryAnnotation(t *testing.T) {
	inst := Installation{}
	inst.SetRetryAnnotation("retry-1")
	assert.Equal(t, "retry-1", inst.Annotations[AnnotationRetry])
}
