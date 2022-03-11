package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetRetryLabelValue(t *testing.T) {
	annotations := map[string]string{
		AnnotationRetry: "123",
	}

	assert.Equal(t, "202cb962ac59075b964b07152d234b70", getRetryLabelValue(annotations), "retry label value should be populated when the annotation is set")

	delete(annotations, AnnotationRetry)

	assert.Empty(t, getRetryLabelValue(annotations), "retry label value should be empty when no annotation is set")
}
