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
		Active:        true,
		Bundle: OCIReferenceParts{
			Repository: "getporter/porter-hello",
			Version:    "0.1.0",
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
		ObservedGeneration: 2,
		ActiveJob:          &corev1.LocalObjectReference{Name: "somejob"},
		Phase:              PhaseSucceeded,
		Conditions: []metav1.Condition{
			{Type: string(ConditionComplete), Status: metav1.ConditionTrue},
		},
	}

	s.Initialize()

	assert.Equal(t, int64(2), s.ObservedGeneration, "ObservedGeneration should not be reset")
	assert.Empty(t, s.Conditions, "Conditions should be empty")
	assert.Nil(t, s.ActiveJob, "ActiveJob should be nil")
	assert.Equal(t, PhaseUnknown, s.Phase, "Phase should reset to Unknown")
}
