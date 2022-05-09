package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	portertest "get.porter.sh/porter/pkg/test"
	portertests "get.porter.sh/porter/tests"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestParameterSetSpec_ToPorterDocument(t *testing.T) {
	wantGoldenFile := "testdata/parameter-set.yaml"
	type fields struct {
		AgentConfig   *corev1.LocalObjectReference
		PorterConfig  *corev1.LocalObjectReference
		SchemaVersion string
		Name          string
		Namespace     string
		Parameters    []Parameter
	}
	tests := []struct {
		name       string
		fields     fields
		wantFile   string
		wantErrMsg string
	}{
		{
			name: "golden file test",
			fields: fields{SchemaVersion: "1.0.1",
				Name:      "porter-test-me",
				Namespace: "dev",
				Parameters: []Parameter{{
					Name:   "param1",
					Source: ParameterSource{Value: "test-param"},
				},
				},
			},
			wantFile:   wantGoldenFile,
			wantErrMsg: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := ParameterSetSpec{
				AgentConfig:   tt.fields.AgentConfig,
				PorterConfig:  tt.fields.PorterConfig,
				SchemaVersion: tt.fields.SchemaVersion,
				Name:          tt.fields.Name,
				Namespace:     tt.fields.Namespace,
				Parameters:    tt.fields.Parameters,
			}
			got, err := cs.ToPorterDocument()
			if tt.wantErrMsg == "" {
				require.NoError(t, err)
				portertest.CompareGoldenFile(t, "testdata/parameter-set.yaml", string(got))
			} else {
				portertests.RequireErrorContains(t, err, tt.wantErrMsg)
			}
		})
	}
}

func TestParameterSet_SetRetryAnnotation(t *testing.T) {
	type fields struct {
		TypeMeta   metav1.TypeMeta
		ObjectMeta metav1.ObjectMeta
		Spec       ParameterSetSpec
		Status     ParameterSetStatus
	}
	type args struct {
		retry string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		{
			name: "set retry 1",
			fields: fields{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       ParameterSetSpec{},
				Status:     ParameterSetStatus{},
			},
			args: args{retry: "1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := &ParameterSet{
				TypeMeta:   tt.fields.TypeMeta,
				ObjectMeta: tt.fields.ObjectMeta,
				Spec:       tt.fields.Spec,
				Status:     tt.fields.Status,
			}
			cs.SetRetryAnnotation(tt.args.retry)
			assert.Equal(t, tt.args.retry, cs.Annotations[AnnotationRetry])
		})
	}
}
