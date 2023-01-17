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
	"k8s.io/apimachinery/pkg/api/resource"
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
	testAgentCfg := &porterv1.AgentConfig{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, Generation: 1},
		Spec: porterv1.AgentConfigSpec{
			Plugins: map[string]porterv1.Plugin{"kubernetes": {}},
		},
	}
	testdata := []client.Object{
		testAgentCfg,
	}
	controller := setupAgentConfigController(testdata...)

	var (
		agentCfg     *porterv1.AgentConfigAdapter
		agentCfgData porterv1.AgentConfig
	)

	triggerReconcile := func() {
		fullname := types.NamespacedName{Namespace: namespace, Name: testAgentCfg.Name}
		key := client.ObjectKey{Namespace: namespace, Name: testAgentCfg.Name}

		request := controllerruntime.Request{
			NamespacedName: fullname,
		}
		result, err := controller.Reconcile(ctx, request)
		require.NoError(t, err)
		require.True(t, result.IsZero())

		err = controller.Get(ctx, key, &agentCfgData)
		if !apierrors.IsNotFound(err) {
			require.NoError(t, err)
		}
		agentCfg = porterv1.NewAgentConfigAdapter(agentCfgData)
	}

	triggerReconcile()

	// Verify the agent config was picked up and the status initialized
	assert.Equal(t, porterv1.PhaseUnknown, agentCfg.Status.Phase, "New resources should be initialized to Phase: Unknown")

	triggerReconcile()
	_, ok := agentCfg.Spec.Plugins.GetByName("kubernetes")
	require.True(t, ok)

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
	require.False(t, agentCfg.Status.Ready)

	// once the agent action is completed, the PVC should have been bound to a PV created by kubernetes
	pvc := &corev1.PersistentVolumeClaim{}
	key := client.ObjectKey{Namespace: agentCfg.Namespace, Name: action.Spec.Volumes[0].VolumeSource.PersistentVolumeClaim.ClaimName}
	require.NoError(t, controller.Get(ctx, key, pvc))
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-pv-agent-config",
			Namespace:       agentCfg.Namespace,
			OwnerReferences: pvc.OwnerReferences,
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
	// the pvc controller should have updated the pvc with the pvc-protection finalizer
	pvc.Finalizers = append(pvc.Finalizers, "kubernetes.io/pvc-protection")
	require.NoError(t, controller.Update(ctx, pvc))

	triggerReconcile()

	// Verify that the agent config status was synced with the action
	var actionList porterv1.AgentAction
	require.NoError(t, controller.Get(ctx, client.ObjectKey{Namespace: agentCfg.Namespace, Name: agentCfg.Status.Action.Name}, &actionList))
	assert.Equal(t, "1", actionList.Labels[porterv1.LabelResourceGeneration], "The wrong action is set on the status")
	require.NotNil(t, agentCfg.Status.Action, "expected Action to still be set")
	assert.Equal(t, porterv1.PhaseSucceeded, agentCfg.Status.Phase, "incorrect Phase")
	require.False(t, agentCfg.Status.Ready)

	require.NotEmpty(t, actionList.Spec.Volumes)

	// verify that the pv that has plugins installed has been updated with the expected labels and claim reference
	pluginsPV := &corev1.PersistentVolume{}
	require.NoError(t, controller.Get(ctx, client.ObjectKey{Namespace: agentCfg.Namespace, Name: pv.Name}, pluginsPV))
	pluginLabels, exists := pluginsPV.Labels[porterv1.LabelPluginsHash]
	require.True(t, exists)
	require.Equal(t, agentCfg.Spec.Plugins.GetLabels()[porterv1.LabelPluginsHash], pluginLabels)
	rn, exists := pluginsPV.Labels[porterv1.LabelResourceName]
	require.True(t, exists)
	require.Equal(t, agentCfg.Name, rn)
	require.Equal(t, agentCfg.GetPluginsPVCName(), pluginsPV.Spec.ClaimRef.Name)

	triggerReconcile()

	// verify that the tmp pvc's finalizer is deleted
	tmpPVC := &corev1.PersistentVolumeClaim{}
	require.NoError(t, controller.Get(ctx, client.ObjectKey{Namespace: agentCfg.Namespace, Name: pvc.Name}, tmpPVC))
	require.Empty(t, tmpPVC.GetFinalizers())

	triggerReconcile()

	tmpPVC = &corev1.PersistentVolumeClaim{}
	require.True(t, apierrors.IsNotFound(controller.Get(ctx, client.ObjectKey{Namespace: agentCfg.Namespace, Name: pvc.Name}, tmpPVC)))

	triggerReconcile()
	require.False(t, agentCfg.Status.Ready)

	// the renamed pvc should be created with label selector set and correct access mode
	renamedPVC := &corev1.PersistentVolumeClaim{}
	require.NoError(t, controller.Get(ctx, client.ObjectKey{Namespace: agentCfg.Namespace, Name: agentCfg.GetPluginsPVCName()}, renamedPVC))
	readonlyMany := []corev1.PersistentVolumeAccessMode{corev1.ReadOnlyMany}
	require.Equal(t, readonlyMany, renamedPVC.Spec.AccessModes)
	matchLables := agentCfg.Spec.Plugins.GetLabels()
	matchLables[porterv1.LabelResourceName] = agentCfg.Name
	require.Equal(t, matchLables, renamedPVC.Spec.Selector.MatchLabels)

	// the renamed pvc should eventually be bounded the to pv
	renamedPVC.Spec.VolumeName = pv.Name
	renamedPVC.Status.Phase = corev1.ClaimBound
	require.NoError(t, controller.Update(ctx, renamedPVC))

	triggerReconcile()
	require.True(t, agentCfg.Status.Ready)

	// Fail the action
	action.Status.Phase = porterv1.PhaseFailed
	action.Status.Conditions = []metav1.Condition{{Type: string(porterv1.ConditionFailed), Status: metav1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &action))

	triggerReconcile()

	// Verify that the agent config status shows the action is failed
	require.NotNil(t, agentCfg.Status.Action, "expected Action to still be set")
	require.False(t, agentCfg.Status.Ready, "agent config should not be ready if the agent action has failed")
	assert.Equal(t, porterv1.PhaseFailed, agentCfg.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(agentCfg.Status.Conditions, string(porterv1.ConditionFailed)))

	// Edit the agent config spec
	agentCfgData.Generation = 2
	agentCfgData.Spec.Plugins = map[string]porterv1.Plugin{"azure": {}}
	require.NoError(t, controller.Update(ctx, &agentCfgData))

	triggerReconcile()

	// Verify that the agent config status was re-initialized
	assert.Equal(t, int64(2), agentCfg.Status.ObservedGeneration)
	assert.Equal(t, porterv1.PhaseUnknown, agentCfg.Status.Phase, "New resources should be initialized to Phase: Unknown")
	assert.Empty(t, agentCfg.Status.Conditions, "Conditions should have been reset")
	assert.False(t, agentCfg.Status.Ready)

	triggerReconcile()

	// Retry the last action
	lastAction := agentCfg.Status.Action.Name
	agentCfgData.Annotations = map[string]string{porterv1.AnnotationRetry: "retry-1"}
	require.NoError(t, controller.Update(ctx, &agentCfgData))

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
	agentCfgData.Generation = 3
	agentCfgData.Spec.Plugins = map[string]porterv1.Plugin{"kubernetes": {}}
	agentCfgData.DeletionTimestamp = &now
	require.NoError(t, controller.Update(ctx, &agentCfgData))
	triggerReconcile()

	// remove the agent config from the pvc's owner reference list
	triggerReconcile()
	// Verify that pvc and pv no longer has the agent config in their owner reference list
	require.NoError(t, controller.Get(ctx, client.ObjectKey{Namespace: agentCfg.Namespace, Name: agentCfg.GetPluginsPVCName()}, renamedPVC))
	for _, owner := range renamedPVC.OwnerReferences {
		require.NotEqual(t, "AgentConfig", owner.Kind, "failed to remove agent config from pv's owner reference list after deletion")
	}

	// this trigger will then remove the agent config's finalizer
	triggerReconcile()
	// Verify that the agent config was removed
	err := controller.Get(ctx, client.ObjectKeyFromObject(&agentCfg.AgentConfig), &agentCfg.AgentConfig)
	require.True(t, apierrors.IsNotFound(err), "expected the agent config was deleted")

	// Verify that reconcile doesn't error out after it's deleted
	triggerReconcile()
}

func TestAgentConfigReconciler_createAgentAction(t *testing.T) {
	ctx := context.Background()

	agentCfg := &porterv1.AgentConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: porterv1.GroupVersion.String(),
			Kind:       "AgentConfig",
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
		Spec: porterv1.AgentConfigSpec{
			Plugins:    map[string]porterv1.Plugin{"test": {Version: "v1.2.3"}},
			VolumeSize: "64Mi",
		},
	}
	wrapper := porterv1.NewAgentConfigAdapter(*agentCfg)
	// once the agent action is completed, the PVC should have been bound to a PV created by kubernetes
	labels := wrapper.Spec.Plugins.GetLabels()
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: agentCfg.Name + "-",
			Namespace:    agentCfg.Namespace,
			Labels:       labels,
			Annotations:  wrapper.GetPluginsPVCNameAnnotation(),
			OwnerReferences: []metav1.OwnerReference{
				{ // I'm not using controllerutil.SetControllerReference because I can't track down why that throws a panic when running our tests
					APIVersion:         agentCfg.APIVersion,
					Kind:               agentCfg.Kind,
					Name:               agentCfg.Name,
					UID:                agentCfg.UID,
					Controller:         pointer.BoolPtr(true),
					BlockOwnerDeletion: pointer.BoolPtr(true),
				},
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.ResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: wrapper.Spec.GetVolumeSize(),
				},
			},
		},
	}
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-pv-agent-config",
			Namespace:       agentCfg.Namespace,
			OwnerReferences: pvc.OwnerReferences,
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
	pvc.Spec.VolumeName = pv.Name
	pvc.Status.Phase = corev1.ClaimBound
	// the pvc controller should have updated the pvc with the pvc-protection finalizer
	pvc.Finalizers = append(pvc.Finalizers, "kubernetes.io/pvc-protection")
	controller := setupAgentConfigController(pvc, pv)
	action, err := controller.createAgentAction(ctx, logr.Discard(), pvc, wrapper, nil)
	require.NoError(t, err)
	assert.Equal(t, "test", action.Namespace)
	assert.Contains(t, action.Name, "myblog-")
	assert.Len(t, action.OwnerReferences, 1, "expected an owner reference")
	wantOwnerRef := metav1.OwnerReference{
		APIVersion:         porterv1.GroupVersion.String(),
		Kind:               "AgentConfig",
		Name:               "myblog",
		UID:                "random-uid",
		Controller:         pointer.BoolPtr(true),
		BlockOwnerDeletion: pointer.BoolPtr(true),
	}
	assert.Equal(t, wantOwnerRef, action.OwnerReferences[0], "incorrect owner reference")

	assertContains(t, action.Annotations, porterv1.AnnotationRetry, agentCfg.Annotations[porterv1.AnnotationRetry], "incorrect annotation")
	assertContains(t, action.Labels, porterv1.LabelManaged, "true", "incorrect label")
	assertContains(t, action.Labels, porterv1.LabelResourceKind, "AgentConfig", "incorrect label")
	assertContains(t, action.Labels, porterv1.LabelResourceName, "myblog", "incorrect label")
	assertContains(t, action.Labels, porterv1.LabelResourceGeneration, "1", "incorrect label")
	assertContains(t, action.Labels, "testLabel", "abc123", "incorrect label")

	assert.NotEmpty(t, action.Spec.Volumes, "incorrect Volumes")
	assert.Equal(t, action.Spec.Volumes[0].Name, porterv1.VolumePorterPluginsName, "incorrect Volumes")
	assert.Equal(t, action.Spec.Volumes[0].VolumeSource.PersistentVolumeClaim.ClaimName, pvc.Name, "incorrect Volumes")
	assert.NotEmpty(t, action.Spec.VolumeMounts, "incorrect Volumes mounts")
	assert.Equal(t, action.Spec.VolumeMounts[0].Name, porterv1.VolumePorterPluginsName, "incorrect VolumeMounts")
	assert.Equal(t, action.Spec.VolumeMounts[0].MountPath, porterv1.VolumePorterPluginsPath, "incorrect VolumeMounts")
	assert.Equal(t, action.Spec.VolumeMounts[0].SubPath, "plugins", "incorrect VolumeMounts")
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
