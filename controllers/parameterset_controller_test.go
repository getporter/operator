package controllers

import (
	"context"
	"testing"
	"time"

	porterv1 "get.porter.sh/operator/api/v1"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/pointer"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestParameterSetReconiler_Reconcile(t *testing.T) {
	ctx := context.Background()

	namespace := "test"
	name := "mybuns"
	testdata := []client.Object{
		&porterv1.ParameterSet{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, Generation: 1},
		},
	}
	controller := setupParameterSetController(testdata...)

	var ps porterv1.ParameterSet
	triggerReconcile := func() {
		fullname := types.NamespacedName{Namespace: namespace, Name: name}
		key := client.ObjectKey{Namespace: namespace, Name: name}
		request := controllerruntime.Request{
			NamespacedName: fullname,
		}
		result, err := controller.Reconcile(ctx, request)
		require.NoError(t, err)
		require.True(t, result.IsZero())

		err = controller.Get(ctx, key, &ps)
		if !apierrors.IsNotFound(err) {
			require.NoError(t, err)
		}
	}
	triggerReconcile()

	// Verify the parameter set was picked up and the status initialized
	assert.Equal(t, porterv1.PhaseUnknown, ps.Status.Phase, "New resources should be initialized to Phase: Unknown")

	triggerReconcile()

	// Verify an AgentAction was created and set on the status
	require.NotNil(t, ps.Status.Action, "expected Action to be set")
	var action porterv1.AgentAction
	require.NoError(t, controller.Get(ctx, client.ObjectKey{Namespace: ps.Namespace, Name: ps.Status.Action.Name}, &action))
	assert.Equal(t, "1", action.Labels[porterv1.LabelResourceGeneration], "The wrong resource generation is set for the agent action")

	// Mark the action as scheduled
	action.Status.Phase = porterv1.PhasePending
	action.Status.Conditions = []metav1.Condition{{Type: string(porterv1.ConditionScheduled), Status: metav1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &action))

	triggerReconcile()

	// Verify the parameter set status was synced with the action
	assert.Equal(t, porterv1.PhasePending, ps.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(ps.Status.Conditions, string(porterv1.ConditionScheduled)))

	// Mark the action as started
	action.Status.Phase = porterv1.PhaseRunning
	action.Status.Conditions = []metav1.Condition{{Type: string(porterv1.ConditionStarted), Status: metav1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &action))

	triggerReconcile()

	// Verify the parameter set status was synced with the action
	assert.Equal(t, porterv1.PhaseRunning, ps.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(ps.Status.Conditions, string(porterv1.ConditionStarted)))

	// Complete the action
	action.Status.Phase = porterv1.PhaseSucceeded
	action.Status.Conditions = []metav1.Condition{{Type: string(porterv1.ConditionComplete), Status: metav1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &action))

	triggerReconcile()

	// Verify the parameter set status was synced with the action
	assert.NotNil(t, ps.Status.Action, "expected Action to still be set")
	assert.Equal(t, porterv1.PhaseSucceeded, ps.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(ps.Status.Conditions, string(porterv1.ConditionComplete)))

	// Fail the action
	action.Status.Phase = porterv1.PhaseFailed
	action.Status.Conditions = []metav1.Condition{{Type: string(porterv1.ConditionFailed), Status: metav1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &action))

	triggerReconcile()

	// Verify that the parameter set status shows the action is failed
	require.NotNil(t, ps.Status.Action, "expected Action to still be set")
	assert.Equal(t, porterv1.PhaseFailed, ps.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue((ps.Status.Conditions), string(porterv1.ConditionFailed)))

	// Edit the generation spec
	ps.Generation = 2
	require.NoError(t, controller.Update(ctx, &ps))

	triggerReconcile()

	// Verify that the parameter set status was re-initialized
	assert.Equal(t, int64(2), ps.Status.ObservedGeneration)
	assert.Equal(t, porterv1.PhaseUnknown, ps.Status.Phase, "New resources should be initialized to Phase: Unknown")
	assert.Empty(t, ps.Status.Conditions, "Conditions should have been reset")

	// Retry the last action
	lastAction := ps.Status.Action.Name
	ps.Annotations = map[string]string{porterv1.AnnotationRetry: "retry-1"}
	require.NoError(t, controller.Update(ctx, &ps))

	triggerReconcile()

	// Verify that action has retry set on it now
	require.NotNil(t, ps.Status.Action, "Expected the action to still be set")
	assert.Equal(t, lastAction, ps.Status.Action.Name, "Expected the action to be the same")
	// get the latest version of the action
	require.NoError(t, controller.Get(ctx, client.ObjectKey{Namespace: ps.Namespace, Name: ps.Status.Action.Name}, &action))
	assert.NotEmpty(t, action.Annotations[porterv1.AnnotationRetry], "Expected the action to have its retry annotation set")

	assert.Equal(t, int64(2), ps.Status.ObservedGeneration)
	assert.NotEmpty(t, ps.Status.Action, "Expected the action to still be set")
	assert.Equal(t, porterv1.PhaseUnknown, ps.Status.Phase, "New resources should be initialized to Phase Unknown")
	assert.Empty(t, ps.Status.Conditions, "Conditions should have been reset")

	// Delete the parameter set (setting the delete timestamp directly instead of client.Delete because otherwise the fake client just removes it immediately)
	// The fake client doesn't really follow finalizer logic
	now := metav1.NewTime(time.Now())
	ps.Generation = 3
	ps.DeletionTimestamp = &now
	require.NoError(t, controller.Update(ctx, &ps))

	triggerReconcile()

	// Verify that an action was created to delete it
	require.NotNil(t, ps.Status.Action, "expected Action to be set")
	require.NoError(t, controller.Get(ctx, client.ObjectKey{Namespace: ps.Namespace, Name: ps.Status.Action.Name}, &action))
	assert.Equal(t, "3", action.Labels[porterv1.LabelResourceGeneration], "The wrong resource generation is set for the agent action")

	// Complete the delete action
	action.Status.Phase = porterv1.PhaseSucceeded
	action.Status.Conditions = []metav1.Condition{{Type: string(porterv1.ConditionComplete), Status: metav1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &action))

	triggerReconcile()

	// Verify that the parameter set was removed
	err := controller.Get(ctx, client.ObjectKeyFromObject(&ps), &ps)
	require.True(t, apierrors.IsNotFound(err), "expected the parameter set was deleted")

	// Verify that the reconcile doesn't error out after its deleted
	triggerReconcile()
}

func TestParameterSetReconciler_createAgentAction(t *testing.T) {
	controller := setupParameterSetController()
	tests := []struct {
		name   string
		delete bool
	}{
		{
			name:   "Parameter Set create agent action",
			delete: false,
		},
		{
			name:   "Parameter Set delete agent action",
			delete: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cs := &porterv1.ParameterSet{
				TypeMeta: metav1.TypeMeta{
					APIVersion: porterv1.GroupVersion.String(),
					Kind:       "ParameterSet",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  "test",
					Name:       "myParams",
					UID:        "random-uid",
					Generation: 1,
					Labels: map[string]string{
						"testLabel": "abc123",
					},
					Annotations: map[string]string{
						porterv1.AnnotationRetry: "2021-2-2 12:00:00",
					},
				},
				Spec: porterv1.ParameterSetSpec{
					Namespace:   "dev",
					Name:        "paramset",
					AgentConfig: &corev1.LocalObjectReference{Name: "myAgentConfig"},
				},
			}
			controllerutil.AddFinalizer(cs, porterv1.FinalizerName)
			if test.delete {
				now := metav1.NewTime(time.Now())
				cs.DeletionTimestamp = &now
			}
			action, err := controller.createAgentAction(context.Background(), logr.Discard(), cs)
			require.NoError(t, err)
			assert.Equal(t, "test", action.Namespace)
			assert.Contains(t, action.Name, "myParams-")
			assert.Len(t, action.OwnerReferences, 1, "expected an owner reference")
			wantOwnerRef := metav1.OwnerReference{
				APIVersion:         porterv1.GroupVersion.String(),
				Kind:               "ParameterSet",
				Name:               "myParams",
				UID:                "random-uid",
				Controller:         pointer.Bool(true),
				BlockOwnerDeletion: pointer.Bool(true),
			}
			assert.Equal(t, wantOwnerRef, action.OwnerReferences[0], "incorrect owner reference")
			assertContains(t, action.Annotations, porterv1.AnnotationRetry, cs.Annotations[porterv1.AnnotationRetry], "incorrect annotation")
			assertContains(t, action.Labels, porterv1.LabelManaged, "true", "incorrect label")
			assertContains(t, action.Labels, porterv1.LabelResourceKind, "ParameterSet", "incorrect label")
			assertContains(t, action.Labels, porterv1.LabelResourceName, "myParams", "incorrect label")
			assertContains(t, action.Labels, porterv1.LabelResourceGeneration, "1", "incorrect label")
			assertContains(t, action.Labels, "testLabel", "abc123", "incorrect label")

			assert.Equal(t, cs.Spec.AgentConfig, action.Spec.AgentConfig, "incorrect AgentConfig reference")
			assert.Equal(t, cs.Spec.AgentConfig, action.Spec.AgentConfig, "incorrect PorterConfig reference")
			assert.Nilf(t, action.Spec.Command, "should use the default command for the agent")
			if test.delete {
				assert.Equal(t, []string{"parameters", "delete", "-n", cs.Spec.Namespace, cs.Spec.Name}, action.Spec.Args, "incorrect agent arguments")
				assert.Empty(t, action.Spec.Files["parameters.yaml"], "expected parameters.yaml to be empty")

			} else {
				assert.Equal(t, []string{"parameters", "apply", "parameters.yaml"}, action.Spec.Args, "incorrect agent arguments")
				assert.Contains(t, action.Spec.Files, "parameters.yaml")
				assert.NotEmpty(t, action.Spec.Files["parameters.yaml"], "expected parameters.yaml to get set on the action")
				paramSetYaml, err := cs.Spec.ToPorterDocument()
				assert.NoError(t, err)
				assert.Equal(t, action.Spec.Files["parameters.yaml"], paramSetYaml)
			}

		})
	}
}

func setupParameterSetController(objs ...client.Object) ParameterSetReconciler {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(porterv1.AddToScheme(scheme))

	fakeBuilder := fake.NewClientBuilder()
	fakeBuilder.WithScheme(scheme)
	fakeBuilder.WithObjects(objs...)
	fakeClient := fakeBuilder.Build()

	return ParameterSetReconciler{
		Log:    logr.Discard(),
		Client: fakeClient,
		Scheme: scheme,
	}
}
