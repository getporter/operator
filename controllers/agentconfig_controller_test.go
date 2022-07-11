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
)

func TestAgentConfigReconciler_Reconcile(t *testing.T) {
	// long test is long
	// Run through a full resource lifecycle: create, update, delete
	ctx := context.Background()

	namespace := "test"
	name := "mybuns"
	testdata := []client.Object{
		&porterv1.AgentConfig{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, Generation: 1}},
	}
	controller := setupAgentConfigController(testdata...)

	var agentCfg porterv1.AgentConfig
	triggerReconcile := func() {
		fullname := types.NamespacedName{Namespace: namespace, Name: name}
		key := client.ObjectKey{Namespace: namespace, Name: name}

		request := controllerruntime.Request{
			NamespacedName: fullname,
		}
		result, err := controller.Reconcile(ctx, request)
		require.NoError(t, err)
		require.True(t, result.IsZero())

		err = controller.Get(ctx, key, &agentCfg)
		if !apierrors.IsNotFound(err) {
			require.NoError(t, err)
		}
	}

	triggerReconcile()

	// Verify the agent config was picked up and the status initialized
	assert.Equal(t, porterv1.PhaseUnknown, agentCfg.Status.Phase, "New resources should be initialized to Phase: Unknown")

	triggerReconcile()

	// Verify an AgentAction was created and set on the status
	require.NotNil(t, agentCfg.Status.Action, "expected Action to be set")
	var action porterv1.AgentAction
	require.NoError(t, controller.Get(ctx, client.ObjectKey{Namespace: agentCfg.Namespace, Name: agentCfg.Status.Action.Name}, &action))
	assert.Equal(t, "1", action.Labels[porterv1.LabelResourceGeneration], "The wrong action is set on the status")

	// Mark the action as scheduled
	action.Status.Phase = porterv1.PhasePending
	action.Status.Conditions = []metav1.Condition{{Type: string(porterv1.ConditionScheduled), Status: metav1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &action))

	triggerReconcile()

	// Verify the agent config status was synced with the action
	assert.Equal(t, porterv1.PhasePending, agentCfg.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(agentCfg.Status.Conditions, string(porterv1.ConditionScheduled)))

	// Mark the action as started
	action.Status.Phase = porterv1.PhaseRunning
	action.Status.Conditions = []metav1.Condition{{Type: string(porterv1.ConditionStarted), Status: metav1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &action))

	triggerReconcile()

	// Verify that the agent config status was synced with the action
	assert.Equal(t, porterv1.PhaseRunning, agentCfg.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(agentCfg.Status.Conditions, string(porterv1.ConditionStarted)))

	// Complete the action
	action.Status.Phase = porterv1.PhaseSucceeded
	action.Status.Conditions = []metav1.Condition{{Type: string(porterv1.ConditionComplete), Status: metav1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &action))

	// once the agent action is completed, the PVC should have been bound to a PV created by kubernetes
	pvc := &corev1.PersistentVolumeClaim{}
	key := client.ObjectKey{Namespace: agentCfg.Namespace, Name: action.Spec.Volumes[0].VolumeSource.PersistentVolumeClaim.ClaimName}
	require.NoError(t, controller.Get(ctx, key, pvc))
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pv-agent-config",
			Namespace: agentCfg.Namespace,
		},
		Spec: corev1.PersistentVolumeSpec{
			ClaimRef: &corev1.ObjectReference{
				Kind:            pvc.Kind,
				Namespace:       pvc.Namespace,
				Name:            pvc.Name,
				UID:             pvc.UID,
				APIVersion:      pvc.APIVersion,
				ResourceVersion: pvc.ResourceVersion,
			},
		},
	}
	require.NoError(t, controller.Create(ctx, pv))
	pvc.Spec.VolumeName = pv.Name
	pvc.Status.Phase = corev1.ClaimBound
	require.NoError(t, controller.Update(ctx, pvc))

	triggerReconcile()

	// Verify that the agent config status was synced with the action
	var actionList porterv1.AgentAction
	require.NoError(t, controller.Get(ctx, client.ObjectKey{Namespace: agentCfg.Namespace, Name: agentCfg.Status.Action.Name}, &actionList))
	assert.Equal(t, "1", actionList.Labels[porterv1.LabelResourceGeneration], "The wrong action is set on the status")
	require.NotNil(t, agentCfg.Status.Action, "expected Action to still be set")
	assert.Equal(t, porterv1.PhaseUnknown, agentCfg.Status.Phase, "incorrect Phase")

	// Mark the action as scheduled
	actionList.Status.Phase = porterv1.PhasePending
	actionList.Status.Conditions = []metav1.Condition{{Type: string(porterv1.ConditionScheduled), Status: metav1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &actionList))

	triggerReconcile()

	assert.Equal(t, porterv1.PhasePending, agentCfg.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(agentCfg.Status.Conditions, string(porterv1.ConditionScheduled)), agentCfg.Status.Conditions)

	// Complete the action
	actionList.Status.Phase = porterv1.PhaseSucceeded
	actionList.Status.Conditions = []metav1.Condition{{Type: string(porterv1.ConditionComplete), Status: metav1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &actionList))

	triggerReconcile()

	// verify that pvc is renamed to the actual name and the tmp pvc is deleted
	tmpPVCName, ok := agentCfg.Annotations["tmpPVC"]
	require.True(t, ok)
	tmpPVC := &corev1.PersistentVolumeClaim{}
	require.Error(t, controller.Get(ctx, client.ObjectKey{Namespace: agentCfg.Namespace, Name: tmpPVCName}, tmpPVC), tmpPVCName)

	// Fail the action
	action.Status.Phase = porterv1.PhaseFailed
	action.Status.Conditions = []metav1.Condition{{Type: string(porterv1.ConditionFailed), Status: metav1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &action))

	triggerReconcile()

	// Verify that the agent config status shows the action is failed
	require.NotNil(t, agentCfg.Status.Action, "expected Action to still be set")
	assert.Equal(t, porterv1.PhaseFailed, agentCfg.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(agentCfg.Status.Conditions, string(porterv1.ConditionFailed)))

	// Edit the agent config spec
	agentCfg.Generation = 2
	require.NoError(t, controller.Update(ctx, &agentCfg))

	triggerReconcile()

	// Verify that the agent config status was re-initialized
	assert.Equal(t, int64(2), agentCfg.Status.ObservedGeneration)
	assert.Equal(t, porterv1.PhaseUnknown, agentCfg.Status.Phase, "New resources should be initialized to Phase: Unknown")
	assert.Empty(t, agentCfg.Status.Conditions, "Conditions should have been reset")

	// Retry the last action
	lastAction := agentCfg.Status.Action.Name
	agentCfg.Annotations = map[string]string{porterv1.AnnotationRetry: "retry-1"}
	require.NoError(t, controller.Update(ctx, &agentCfg))

	triggerReconcile()

	// Verify that action has retry set on it now
	require.NotNil(t, agentCfg.Status.Action, "Expected the action to still be set")
	assert.Equal(t, lastAction, agentCfg.Status.Action.Name, "Expected the action to be the same")
	// get the latest version of the action
	require.NoError(t, controller.Get(ctx, client.ObjectKey{Namespace: agentCfg.Namespace, Name: agentCfg.Status.Action.Name}, &action))
	assert.NotEmpty(t, action.Annotations[porterv1.AnnotationRetry], "Expected the action to have its retry annotation set")

	assert.Equal(t, int64(2), agentCfg.Status.ObservedGeneration)
	assert.NotEmpty(t, agentCfg.Status.Action, "Expected the action to still be set")
	assert.Equal(t, porterv1.PhaseUnknown, agentCfg.Status.Phase, "New resources should be initialized to Phase: Unknown")
	assert.Empty(t, agentCfg.Status.Conditions, "Conditions should have been reset")

	// Delete the agent config (setting the delete timestamp directly instead of client.Delete because otherwise the fake client just removes it immediately)
	// The fake client doesn't really follow finalizer logic
	now := metav1.NewTime(time.Now())
	agentCfg.Generation = 3
	agentCfg.DeletionTimestamp = &now
	require.NoError(t, controller.Update(ctx, &agentCfg))

	triggerReconcile()

	// Verify that an action was created to uninstall it
	require.NotNil(t, agentCfg.Status.Action, "expected Action to be set")
	require.NoError(t, controller.Get(ctx, client.ObjectKey{Namespace: agentCfg.Namespace, Name: agentCfg.Status.Action.Name}, &action))
	assert.Equal(t, "3", action.Labels[porterv1.LabelResourceGeneration], "The wrong action is set on the status")

	// Complete the uninstall action
	action.Status.Phase = porterv1.PhaseSucceeded
	action.Status.Conditions = []metav1.Condition{{Type: string(porterv1.ConditionComplete), Status: metav1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &action))

	triggerReconcile()

	// Verify that the agent config was removed
	err := controller.Get(ctx, client.ObjectKeyFromObject(&agentCfg), &agentCfg)
	require.True(t, apierrors.IsNotFound(err), "expected the installation was deleted")

	// Verify that reconcile doesn't error out after it's deleted
	triggerReconcile()
}

func TestAgentConfigReconciler_createAgentAction(t *testing.T) {
	controller := setupInstallationController()

	inst := &porterv1.Installation{
		TypeMeta: metav1.TypeMeta{
			APIVersion: porterv1.GroupVersion.String(),
			Kind:       "Installation",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  "test",
			Name:       "myblog",
			UID:        "random-uid",
			Generation: 1,
			Labels: map[string]string{
				"testLabel": "abc123",
			},
			Annotations: map[string]string{
				porterv1.AnnotationRetry: "2021-2-2 12:00:00",
			},
		},
		Spec: porterv1.InstallationSpec{
			Namespace:    "dev",
			Name:         "wordpress",
			AgentConfig:  &corev1.LocalObjectReference{Name: "myAgentConfig"},
			PorterConfig: &corev1.LocalObjectReference{Name: "myPorterConfig"},
		},
	}
	action, err := controller.createAgentAction(context.Background(), logr.Discard(), inst)
	require.NoError(t, err)
	assert.Equal(t, "test", action.Namespace)
	assert.Contains(t, action.Name, "myblog-")
	assert.Len(t, action.OwnerReferences, 1, "expected an owner reference")
	wantOwnerRef := metav1.OwnerReference{
		APIVersion:         porterv1.GroupVersion.String(),
		Kind:               "Installation",
		Name:               "myblog",
		UID:                "random-uid",
		Controller:         pointer.BoolPtr(true),
		BlockOwnerDeletion: pointer.BoolPtr(true),
	}
	assert.Equal(t, wantOwnerRef, action.OwnerReferences[0], "incorrect owner reference")

	assertContains(t, action.Annotations, porterv1.AnnotationRetry, inst.Annotations[porterv1.AnnotationRetry], "incorrect annotation")
	assertContains(t, action.Labels, porterv1.LabelManaged, "true", "incorrect label")
	assertContains(t, action.Labels, porterv1.LabelResourceKind, "Installation", "incorrect label")
	assertContains(t, action.Labels, porterv1.LabelResourceName, "myblog", "incorrect label")
	assertContains(t, action.Labels, porterv1.LabelResourceGeneration, "1", "incorrect label")
	assertContains(t, action.Labels, "testLabel", "abc123", "incorrect label")

	assert.Equal(t, inst.Spec.AgentConfig, action.Spec.AgentConfig, "incorrect AgentConfig reference")
	assert.Equal(t, inst.Spec.AgentConfig, action.Spec.AgentConfig, "incorrect PorterConfig reference")
	assert.Nilf(t, action.Spec.Command, "should use the default command for the agent")
	assert.Equal(t, []string{"installation", "apply", "installation.yaml"}, action.Spec.Args, "incorrect agent arguments")
	assert.Contains(t, action.Spec.Files, "installation.yaml")
	assert.NotEmpty(t, action.Spec.Files["installation.yaml"], "expected installation.yaml to get set on the action")

	assert.Empty(t, action.Spec.Env, "incorrect Env")
	assert.Empty(t, action.Spec.EnvFrom, "incorrect EnvFrom")
	assert.Empty(t, action.Spec.Volumes, "incorrect Volumes")
	assert.Empty(t, action.Spec.VolumeMounts, "incorrect VolumeMounts")
}

func setupAgentConfigController(objs ...client.Object) AgentConfigReconciler {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(porterv1.AddToScheme(scheme))

	fakeBuilder := fake.NewClientBuilder()
	fakeBuilder.WithScheme(scheme)
	fakeBuilder.WithObjects(objs...)
	fakeClient := fakeBuilder.Build()

	return AgentConfigReconciler{
		Log:    logr.Discard(),
		Client: fakeClient,
		Scheme: scheme,
	}
}
