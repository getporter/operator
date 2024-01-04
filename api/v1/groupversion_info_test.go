package v1

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
)

func TestAddKnownTypes(t *testing.T) {
	scheme := runtime.NewScheme()
	AddToScheme(scheme)
	err := addKnownTypes(scheme)
	if err != nil {
		t.Fatalf("failure to add known types %v", err)
	}
}
