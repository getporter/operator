package controllers

import (
	"testing"

	porterv1 "get.porter.sh/operator/api/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func Test_resourceChanged_Update(t *testing.T) {
	predicate := resourceChanged{}

	t.Run("spec changed", func(t *testing.T) {
		e := event.UpdateEvent{
			ObjectOld: &porterv1.Installation{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
			},
			ObjectNew: &porterv1.Installation{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 2,
				},
			},
		}
		assert.True(t, predicate.Update(e), "expected changing the generation to trigger reconciliation")
	})

	t.Run("finalizer added", func(t *testing.T) {
		e := event.UpdateEvent{
			ObjectOld: &porterv1.Installation{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
			},
			ObjectNew: &porterv1.Installation{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
					Finalizers: []string{porterv1.FinalizerName},
				},
			},
		}
		assert.True(t, predicate.Update(e), "expected setting a finalizer to trigger reconciliation")
	})

	t.Run("retry annotation changed", func(t *testing.T) {
		e := event.UpdateEvent{
			ObjectOld: &porterv1.Installation{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
			},
			ObjectNew: &porterv1.Installation{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
					Annotations: map[string]string{
						porterv1.AnnotationRetry: "1",
					},
				},
			},
		}
		assert.True(t, predicate.Update(e), "expected setting changing the retry annotation to trigger reconciliation")
	})

	t.Run("status changed", func(t *testing.T) {
		e := event.UpdateEvent{
			ObjectOld: &porterv1.Installation{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 2,
				},
				Status: porterv1.InstallationStatus{PorterResourceStatus: porterv1.PorterResourceStatus{
					ObservedGeneration: 1,
				}},
			},
			ObjectNew: &porterv1.Installation{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 2,
				},
				Status: porterv1.InstallationStatus{PorterResourceStatus: porterv1.PorterResourceStatus{
					ObservedGeneration: 2,
				}},
			},
		}
		assert.False(t, predicate.Update(e), "expected status changes to be ignored")
	})

	t.Run("label added", func(t *testing.T) {
		e := event.UpdateEvent{
			ObjectOld: &porterv1.Installation{
				ObjectMeta: metav1.ObjectMeta{
					Generation:      1,
					ResourceVersion: "1",
				},
			},
			ObjectNew: &porterv1.Installation{
				ObjectMeta: metav1.ObjectMeta{
					Generation:      1,
					ResourceVersion: "2",
					Labels: map[string]string{
						"myLabel": "super useful",
					},
				},
			},
		}
		assert.False(t, predicate.Update(e), "expected metadata changes to be ignored")
	})
}

func Test_isFinalizerSet(t *testing.T) {
	inst := &porterv1.Installation{
		ObjectMeta: metav1.ObjectMeta{},
	}
	assert.False(t, isFinalizerSet(inst))

	inst.Finalizers = append(inst.Finalizers, "something-else")
	assert.False(t, isFinalizerSet(inst))

	inst.Finalizers = append(inst.Finalizers, porterv1.FinalizerName)
	assert.True(t, isFinalizerSet(inst))
}
