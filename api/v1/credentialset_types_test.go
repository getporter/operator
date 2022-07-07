package v1

import (
	"testing"

	"get.porter.sh/porter/pkg/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	portertest "get.porter.sh/porter/pkg/test"
	portertests "get.porter.sh/porter/tests"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCredentialSetSpec_ToPorterDocument(t *testing.T) {
	wantGoldenFile := "testdata/credential-set.yaml"
	type fields struct {
		AgentConfig   *corev1.LocalObjectReference
		PorterConfig  *corev1.LocalObjectReference
		SchemaVersion string
		Name          string
		Namespace     string
		Credentials   []Credential
	}
	tests := []struct {
		name       string
		fields     fields
		wantFile   string
		wantErrMsg string
	}{
		{
			name: "golden file test",
			fields: fields{SchemaVersion: string(storage.CredentialSetSchemaVersion),
				Name:      "porter-test-me",
				Namespace: "dev",
				Credentials: []Credential{{
					Name:   "insecureValue",
					Source: CredentialSource{Secret: "test-secret"},
				},
				},
			},
			wantFile:   wantGoldenFile,
			wantErrMsg: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := CredentialSetSpec{
				AgentConfig:   tt.fields.AgentConfig,
				PorterConfig:  tt.fields.PorterConfig,
				SchemaVersion: tt.fields.SchemaVersion,
				Name:          tt.fields.Name,
				Namespace:     tt.fields.Namespace,
				Credentials:   tt.fields.Credentials,
			}
			got, err := cs.ToPorterDocument()
			if tt.wantErrMsg == "" {
				require.NoError(t, err)
				portertest.CompareGoldenFile(t, "testdata/credential-set.yaml", string(got))
			} else {
				portertests.RequireErrorContains(t, err, tt.wantErrMsg)
			}
		})
	}
}

func TestCredentialSet_SetRetryAnnotation(t *testing.T) {
	type fields struct {
		TypeMeta   metav1.TypeMeta
		ObjectMeta metav1.ObjectMeta
		Spec       CredentialSetSpec
		Status     CredentialSetStatus
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
				Spec:       CredentialSetSpec{},
				Status:     CredentialSetStatus{},
			},
			args: args{retry: "1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := &CredentialSet{
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
