package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAgentAction_SetRetryAnnotation(t *testing.T) {
	action := AgentAction{}
	action.SetRetryAnnotation("retry-1")
	assert.Equal(t, "retry-1", action.Annotations[AnnotationRetry])
}
