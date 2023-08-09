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
	"k8s.io/utils/ptr"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestShouldInstall(t *testing.T) {
	now := metav1.Now()
	tests := map[string]struct {
		wantTrue     bool
		delTimeStamp *metav1.Time
	}{
		"true":  {wantTrue: true, delTimeStamp: &now},
		"false": {wantTrue: false, delTimeStamp: nil},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			inst := &porterv1.Installation{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "fake-name",
					Namespace:         "fake-ns",
					Finalizers:        []string{porterv1.FinalizerName},
					DeletionTimestamp: test.delTimeStamp,
				},
			}
			rec := setupInstallationController(inst)
			isTrue := rec.shouldUninstall(inst)
			if test.wantTrue {
				assert.True(t, isTrue)
			}
			if !test.wantTrue {
				assert.False(t, isTrue)
			}
		})
	}
}

func TestUninstallInstallation(t *testing.T) {
	ctx := context.Background()
	inst := &porterv1.Installation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fake-install",
			Namespace: "fake-ns",
		},
	}
	rec := setupInstallationController(inst)
	err := rec.uninstallInstallation(ctx, rec.Log, inst)
	assert.NoError(t, err)
	gotInstall := &porterv1.Installation{}
	rec.Get(ctx, types.NamespacedName{Name: "fake-install", Namespace: "fake-ns"}, gotInstall)
	assert.NotEmpty(t, gotInstall.Status)
	assert.Equal(t, porterv1.PhaseUnknown, gotInstall.Status.Phase)
}

func TestRetry(t *testing.T) {
	ctx := context.Background()
	inst := &porterv1.Installation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fake-install",
			Namespace: "fake-ns",
		},
	}
	action := &porterv1.AgentAction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fake-action",
			Namespace: "fake-ns",
		},
	}
	rec := setupInstallationController(inst, action)
	err := rec.retry(ctx, rec.Log, inst, action)
	assert.NoError(t, err)
}

func TestInstallationReconciler_Reconcile(t *testing.T) {
	// long test is long
	// Run through a full resource lifecycle: create, update, delete
	ctx := context.Background()

	namespace := "test"
	name := "mybuns"
	testdata := &porterv1.Installation{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, Generation: 1}}

	controller := setupInstallationController(testdata)

	var inst porterv1.Installation
	triggerReconcile := func() {
		fullname := types.NamespacedName{Namespace: namespace, Name: name}
		key := client.ObjectKey{Namespace: namespace, Name: name}

		request := controllerruntime.Request{
			NamespacedName: fullname,
		}
		result, err := controller.Reconcile(ctx, request)
		require.NoError(t, err)
		require.True(t, result.IsZero())

		err = controller.Get(ctx, key, &inst)
		if !apierrors.IsNotFound(err) {
			require.NoError(t, err)
		}
	}

	triggerReconcile()

	// Verify the installation was picked up and the status initialized
	assert.Equal(t, porterv1.PhaseUnknown, inst.Status.Phase, "New resources should be initialized to Phase: Unknown")

	triggerReconcile()

	// Verify an AgentAction was created and set on the status
	require.NotNil(t, inst.Status.Action, "expected Action to be set")
	var action porterv1.AgentAction
	require.NoError(t, controller.Get(ctx, client.ObjectKey{Namespace: inst.Namespace, Name: inst.Status.Action.Name}, &action))
	assert.Equal(t, "1", action.Labels[porterv1.LabelResourceGeneration], "The wrong action is set on the status")

	// Mark the action as scheduled
	action.Status.Phase = porterv1.PhasePending
	action.Status.Conditions = []metav1.Condition{{Type: string(porterv1.ConditionScheduled), Status: metav1.ConditionTrue}}
	action.ResourceVersion = ""
	controller = setupInstallationController(testdata, &action)
	assert.NoError(t, controller.Client.Status().Update(ctx, &action))
	triggerReconcile()

	// Verify the installation status was synced with the action
	assert.Equal(t, porterv1.PhasePending, inst.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(inst.Status.Conditions, string(porterv1.ConditionScheduled)))

	// Mark the action as started
	action.Status.Phase = porterv1.PhaseRunning
	action.Status.Conditions = []metav1.Condition{{Type: string(porterv1.ConditionStarted), Status: metav1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &action))

	triggerReconcile()

	// Verify that the installation status was synced with the action
	assert.Equal(t, porterv1.PhaseRunning, inst.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(inst.Status.Conditions, string(porterv1.ConditionStarted)))

	// Complete the action
	action.Status.Phase = porterv1.PhaseSucceeded
	action.Status.Conditions = []metav1.Condition{{Type: string(porterv1.ConditionComplete), Status: metav1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &action))

	triggerReconcile()

	// Verify that the installation status was synced with the action
	require.NotNil(t, inst.Status.Action, "expected Action to still be set")
	assert.Equal(t, porterv1.PhaseSucceeded, inst.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(inst.Status.Conditions, string(porterv1.ConditionComplete)))

	// Fail the action
	action.Status.Phase = porterv1.PhaseFailed
	action.Status.Conditions = []metav1.Condition{{Type: string(porterv1.ConditionFailed), Status: metav1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &action))

	triggerReconcile()

	actionName := inst.Status.Action.Name
	// Verify that the installation status shows the action is failed
	require.NotNil(t, inst.Status.Action, "expected Action to still be set")
	assert.Equal(t, porterv1.PhaseFailed, inst.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(inst.Status.Conditions, string(porterv1.ConditionFailed)))

	// Edit the installation spec
	inst.Generation = 2
	require.NoError(t, controller.Update(ctx, &inst))

	triggerReconcile()

	// Verify that the installation status was re-initialized
	assert.Equal(t, int64(2), inst.Status.ObservedGeneration)
	assert.Equal(t, porterv1.PhaseUnknown, inst.Status.Phase, "New resources should be initialized to Phase: Unknown")
	assert.Empty(t, inst.Status.Conditions, "Conditions should have been reset")

	// Retry the last action
	lastAction := actionName
	inst.Annotations = map[string]string{porterv1.AnnotationRetry: "retry-1"}
	require.NoError(t, controller.Update(ctx, &inst))

	triggerReconcile()

	// Verify that action has retry set on it now
	require.NotNil(t, inst.Status.Action, "Expected the action to still be set")
	assert.Equal(t, lastAction, actionName, "Expected the action to be the same")
	// get the latest version of the action
	require.NoError(t, controller.Get(ctx, client.ObjectKey{Namespace: inst.Namespace, Name: inst.Status.Action.Name}, &action))
	assert.NotEmpty(t, action.Annotations[porterv1.AnnotationRetry], "Expected the action to have its retry annotation set")

	assert.Equal(t, int64(2), inst.Status.ObservedGeneration)
	assert.NotEmpty(t, inst.Status.Action, "Expected the action to still be set")
	assert.Equal(t, porterv1.PhaseUnknown, inst.Status.Phase, "New resources should be initialized to Phase: Unknown")
	assert.Empty(t, inst.Status.Conditions, "Conditions should have been reset")

	// Delete the installation (setting the delete timestamp directly instead of client.Delete because otherwise the fake client just removes it immediately)
	// The fake client doesn't really follow finalizer logic
	// metadata.Timestamp is immutable and not allowed to be set  by the client
	now := metav1.NewTime(time.Now())
	inst.Generation = 3
	inst.DeletionTimestamp = &now
	require.NoError(t, controller.Delete(ctx, &inst))

	triggerReconcile()

	// Verify that an action was created to uninstall it
	require.NotNil(t, inst.Status.Action, "expected Action to be set")
	require.NoError(t, controller.Get(ctx, client.ObjectKey{Namespace: inst.Namespace, Name: inst.Status.Action.Name}, &action))
	assert.Equal(t, "2", action.Labels[porterv1.LabelResourceGeneration], "The wrong action is set on the status")

	// Complete the uninstall action
	action.Status.Phase = porterv1.PhaseSucceeded
	action.Status.Conditions = []metav1.Condition{{Type: string(porterv1.ConditionComplete), Status: metav1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &action))

	triggerReconcile()

	// Verify that the installation was removed
	err := controller.Get(ctx, client.ObjectKeyFromObject(&inst), &inst)
	require.True(t, apierrors.IsNotFound(err), "expected the installation was deleted")

	// Verify that reconcile doesn't error out after it's deleted
	triggerReconcile()
}

func TestInstallationReconciler_createAgentAction(t *testing.T) {
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
			Namespace:   "dev",
			Name:        "wordpress",
			AgentConfig: &corev1.LocalObjectReference{Name: "myAgentConfig"},
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
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
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

func setupInstallationController(objs ...client.Object) *InstallationReconciler {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(porterv1.AddToScheme(scheme))

	fakeBuilder := fake.NewClientBuilder()
	fakeBuilder.WithScheme(scheme)
	fakeBuilder.WithObjects(objs...).WithStatusSubresource(objs...)
	fakeClient := fakeBuilder.Build()

	return &InstallationReconciler{
		Log:    logr.Discard(),
		Client: fakeClient,
		Scheme: scheme,
	}
}
