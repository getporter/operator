package v1

import (
	"io/ioutil"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestCredentialSetSpec_ToPorterDocument(t *testing.T) {
	wantGoldenFile, err := ioutil.ReadFile("testdata/credential-set.yaml")
	require.NoError(t, err)
	type fields struct {
		AgentConfig   *corev1.LocalObjectReference
		PorterConfig  *corev1.LocalObjectReference
		SchemaVersion string
		Name          string
		Namespace     string
		Credentials   []Credential
	}
	tests := []struct {
		name    string
		fields  fields
		want    []byte
		wantErr bool
	}{
		{
			name: "golden file test",
			fields: fields{SchemaVersion: "1.0.1",
				Name:      "porter-test-me",
				Namespace: "dev",
				Credentials: []Credential{{
					Name:   "insecureValue",
					Source: CredentialSource{Secret: "test-secret"},
				},
				},
			},
			want:    wantGoldenFile,
			wantErr: false,
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
			if (err != nil) != tt.wantErr {
				t.Errorf("CredentialSetSpec.ToPorterDocument() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CredentialSetSpec.ToPorterDocument() = \n%v, want \n%v", string(got), string(tt.want))
			}
		})
	}
}
