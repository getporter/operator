package controllers

import (
	"context"
	"testing"
	"time"

	porterv1 "get.porter.sh/operator/api/v1"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func Test_getNamePrefix(t *testing.T) {
	testcases := []struct {
		name string
		inst porterv1.Installation
		want string
	}{
		{name: "short name", want: "short-123-9912",
			inst: porterv1.Installation{ObjectMeta: metav1.ObjectMeta{
				Name: "short", Generation: 123, ResourceVersion: "9912"}}},
		{name: "long name", want: "1oF8JkZxyfEojJonxujl9rFvnSgghT1XaP57j3nNirWAA-123-9912",
			inst: porterv1.Installation{ObjectMeta: metav1.ObjectMeta{
				Name: "1oF8JkZxyfEojJonxujl9rFvnSgghT1XaP57j3nNirWAA5YLG8", Generation: 123, ResourceVersion: "9912"}}},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			got := getNamePrefix(&tc.inst)
			assert.Equal(t, tc.want, got)
		})
	}
}

func Test_getJobOwner(t *testing.T) {
	controllerUUID := "9908ddc5-70cb-4425-b0e4-1faed03bae14"
	tests := []struct {
		name string
		obj  client.Object
		want []string
	}{
		{name: "not a job", obj: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{{
				Name:       controllerUUID,
				APIVersion: porterv1.GroupVersion.String(),
				Kind:       "Secret",
				Controller: pointer.BoolPtr(true)}}}}},
		{name: "our job", obj: &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{{
				Name:       controllerUUID,
				APIVersion: porterv1.GroupVersion.String(),
				Kind:       "Installation",
				Controller: pointer.BoolPtr(true)}}}},
			want: []string{controllerUUID}},
		{name: "not our job", obj: &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{{
				Name:       controllerUUID,
				APIVersion: "someone else",
				Kind:       "Installation",
				Controller: pointer.BoolPtr(true)}}}}},
		{name: "not our kind", obj: &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{{
				Name:       controllerUUID,
				APIVersion: porterv1.GroupVersion.String(),
				Kind:       "something else",
				Controller: pointer.BoolPtr(true)}}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getOwner(tt.obj)
			assert.Equal(t, tt.want, got, "incorrect job owner")
		})
	}
}

func Test_getRetryLabelValue(t *testing.T) {
	inst := porterv1.Installation{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				porterv1.AnnotationRetry: "123",
			},
		},
	}

	assert.Equal(t, "202cb962ac59075b964b07152d234b70", inst.GetRetryLabelValue(), "retry label value should be populated when the annotation is set")

	delete(inst.Annotations, porterv1.AnnotationRetry)

	assert.Empty(t, inst.GetRetryLabelValue(), "retry label value should be empty when no annotation is set")

}

func Test_applyJobToStatus(t *testing.T) {
	tests := []struct {
		name       string
		job        *batchv1.Job
		inst       porterv1.Installation
		wantStatus porterv1.InstallationStatus
	}{
		{name: "no job",
			inst: porterv1.Installation{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			wantStatus: porterv1.InstallationStatus{
				ObservedGeneration: 1,
				Phase:              porterv1.PhaseUnknown,
			}},
		{name: "job created",
			inst: porterv1.Installation{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "myjob"}},
			wantStatus: porterv1.InstallationStatus{
				ObservedGeneration: 1,
				ActiveJob:          &corev1.LocalObjectReference{Name: "myjob"},
				Phase:              porterv1.PhasePending,
				Conditions: []metav1.Condition{
					{Type: string(porterv1.ConditionScheduled), Status: metav1.ConditionTrue},
				},
			}},
		{name: "job started",
			inst: porterv1.Installation{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "myjob"},
				Status:     batchv1.JobStatus{Active: 1}},
			wantStatus: porterv1.InstallationStatus{
				ObservedGeneration: 1,
				ActiveJob:          &corev1.LocalObjectReference{Name: "myjob"},
				Phase:              porterv1.PhaseRunning,
				Conditions: []metav1.Condition{
					{Type: string(porterv1.ConditionScheduled), Status: metav1.ConditionTrue},
					{Type: string(porterv1.ConditionStarted), Status: metav1.ConditionTrue},
				},
			}},
		{name: "job succeeded",
			inst: porterv1.Installation{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "myjob"},
				Status:     batchv1.JobStatus{Succeeded: 1, Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}}},
			wantStatus: porterv1.InstallationStatus{
				ObservedGeneration: 1,
				ActiveJob:          nil,
				Phase:              porterv1.PhaseSucceeded,
				Conditions: []metav1.Condition{
					{Type: string(porterv1.ConditionScheduled), Status: metav1.ConditionTrue},
					{Type: string(porterv1.ConditionStarted), Status: metav1.ConditionTrue},
					{Type: string(porterv1.ConditionComplete), Status: metav1.ConditionTrue},
				},
			}},
		{name: "job failed",
			inst: porterv1.Installation{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "myjob"},
				Status:     batchv1.JobStatus{Failed: 1, Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}}},
			wantStatus: porterv1.InstallationStatus{
				ObservedGeneration: 1,
				ActiveJob:          nil,
				Phase:              porterv1.PhaseFailed,
				Conditions: []metav1.Condition{
					{Type: string(porterv1.ConditionScheduled), Status: metav1.ConditionTrue},
					{Type: string(porterv1.ConditionStarted), Status: metav1.ConditionTrue},
					{Type: string(porterv1.ConditionFailed), Status: metav1.ConditionTrue},
				},
			}},
		{name: "update resets status",
			inst: porterv1.Installation{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
				Status: porterv1.InstallationStatus{
					ObservedGeneration: 1,
					ActiveJob:          nil,
					Phase:              porterv1.PhaseFailed,
					Conditions: []metav1.Condition{
						{Type: string(porterv1.ConditionScheduled), Status: metav1.ConditionTrue},
						{Type: string(porterv1.ConditionStarted), Status: metav1.ConditionTrue},
						{Type: string(porterv1.ConditionFailed), Status: metav1.ConditionTrue},
					},
				}},
			wantStatus: porterv1.InstallationStatus{
				ObservedGeneration: 2,
				ActiveJob:          nil,
				Phase:              porterv1.PhaseUnknown,
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := &tt.inst
			applyJobToStatus(logr.Discard(), inst, tt.job)

			assert.Equal(t, tt.wantStatus.Phase, inst.Status.Phase, "incorrect Phase")
			assert.Equal(t, tt.wantStatus.ObservedGeneration, inst.Status.ObservedGeneration, "incorrect ObservedGeneration")
			assert.Equal(t, tt.wantStatus.ActiveJob, inst.Status.ActiveJob, "incorrect ActiveJob")

			assert.Len(t, inst.Status.Conditions, len(tt.wantStatus.Conditions), "incorrect number of Conditions")
			for _, cond := range tt.wantStatus.Conditions {
				assert.True(t, apimeta.IsStatusConditionPresentAndEqual(inst.Status.Conditions, cond.Type, cond.Status), "expected condition %s to be %s", cond.Type, cond.Status)
			}
		})
	}
}

func Test_installationChanged_Update(t *testing.T) {
	predicate := installationChanged{}

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
					Finalizers: []string{finalizerName},
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
				Status: porterv1.InstallationStatus{
					ObservedGeneration: 1,
				},
			},
			ObjectNew: &porterv1.Installation{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 2,
				},
				Status: porterv1.InstallationStatus{
					ObservedGeneration: 2,
				},
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

func setupTestController(objs ...client.Object) InstallationReconciler {
	scheme := runtime.NewScheme()
	porterv1.AddToScheme(scheme)
	batchv1.AddToScheme(scheme)
	corev1.AddToScheme(scheme)

	fakeBuilder := fake.NewClientBuilder()
	fakeBuilder.WithScheme(scheme)
	fakeBuilder.WithObjects(objs...)
	fakeClient := fakeBuilder.Build()

	return InstallationReconciler{
		Log:    logr.Discard(),
		Client: fakeClient,
	}
}

func Test_Reconcile(t *testing.T) {
	// long test is long
	// Run through a full installation lifecycle: create, update, delete
	ctx := context.Background()

	testdata := []client.Object{
		&porterv1.Installation{
			ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "mybuns", Generation: 1},
			Spec:       porterv1.InstallationSpec{Active: true}},
	}
	controller := setupTestController(testdata...)

	var inst porterv1.Installation
	triggerReconcile := func() {
		instRef := types.NamespacedName{Namespace: "test", Name: "mybuns"}
		instKey := client.ObjectKey{Namespace: "test", Name: "mybuns"}

		request := controllerruntime.Request{
			NamespacedName: instRef,
		}
		result, err := controller.Reconcile(ctx, request)
		require.NoError(t, err)
		require.True(t, result.IsZero())

		var updatedInst porterv1.Installation
		if err := controller.Get(ctx, instKey, &updatedInst); err == nil {
			inst = updatedInst
		}
	}

	triggerReconcile()

	// Verify the installation was picked up and now has finalizers
	assert.Contains(t, inst.Finalizers, finalizerName, "Finalizer should be set on new resources")
	assert.Equal(t, inst.Status.Phase, porterv1.PhaseUnknown, "New resources should be initialized to Phase: Unknown")

	triggerReconcile()

	// Verify a job has been scheduled
	var jobs batchv1.JobList
	require.NoError(t, controller.List(ctx, &jobs))
	require.Len(t, jobs.Items, 1)
	job := jobs.Items[0]

	require.NotNil(t, inst.Status.ActiveJob, "expected ActiveJob to be set")
	assert.Equal(t, job.Name, inst.Status.ActiveJob.Name, "expected ActiveJob to contain the job name")
	assert.Equal(t, porterv1.PhasePending, inst.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(inst.Status.Conditions, string(porterv1.ConditionScheduled)))

	// Start the job
	job.Status.Active = 1
	require.NoError(t, controller.Status().Update(ctx, &job))

	triggerReconcile()

	// Verify that the installation status has the job
	require.NotNil(t, inst.Status.ActiveJob, "expected ActiveJob to be set")
	assert.Equal(t, job.Name, inst.Status.ActiveJob.Name, "expected ActiveJob to contain the job name")
	assert.Equal(t, porterv1.PhaseRunning, inst.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(inst.Status.Conditions, string(porterv1.ConditionStarted)))

	// Complete the job
	job.Status.Active = 0
	job.Status.Succeeded = 1
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &job))

	triggerReconcile()

	// Verify that the installation status shows the job is done
	require.Nil(t, inst.Status.ActiveJob, "expected ActiveJob to be nil")
	assert.Equal(t, porterv1.PhaseSucceeded, inst.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(inst.Status.Conditions, string(porterv1.ConditionComplete)))

	// Fail the job
	job.Status.Active = 0
	job.Status.Succeeded = 0
	job.Status.Failed = 1
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &job))

	triggerReconcile()

	// Verify that the installation status shows the job is failed
	require.Nil(t, inst.Status.ActiveJob, "expected ActiveJob to be nil")
	assert.Equal(t, porterv1.PhaseFailed, inst.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(inst.Status.Conditions, string(porterv1.ConditionFailed)))

	// Edit the installation spec
	inst.Generation = 2
	require.NoError(t, controller.Update(ctx, &inst))

	triggerReconcile()

	// Verify that the installation status was re-initialized
	assert.Equal(t, int64(2), inst.Status.ObservedGeneration)
	assert.Equal(t, porterv1.PhasePending, inst.Status.Phase, "New resources should be initialized to Phase: Unknown and then immediately transition to Pending")
	assert.True(t, apimeta.IsStatusConditionTrue(inst.Status.Conditions, string(porterv1.ConditionScheduled)))

	// Delete the installation (setting the delete timestamp directly instead of client.Delete because otherwise the fake client just removes it immediately)
	// The fake client doesn't really follow finalizer logic
	now := metav1.NewTime(time.Now())
	inst.Generation = 3
	inst.DeletionTimestamp = &now
	require.NoError(t, controller.Update(ctx, &inst))

	triggerReconcile()

	// Verify that a job was spawned to uninstall it
	require.NoError(t, controller.List(ctx, &jobs))
	require.Len(t, jobs.Items, 3)
	job = jobs.Items[2]
	require.NotNil(t, inst.Status.ActiveJob, "expected ActiveJob to be set")
	assert.Equal(t, job.Name, inst.Status.ActiveJob.Name, "expected ActiveJob to contain the job name")
	assert.Equal(t, porterv1.PhasePending, inst.Status.Phase, "An uninstall job should have been kicked off")
	assert.True(t, apimeta.IsStatusConditionTrue(inst.Status.Conditions, string(porterv1.ConditionScheduled)))

	// Complete the uninstall job
	job.Status.Active = 0
	job.Status.Succeeded = 1
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &job))

	triggerReconcile()

	// Verify that the finalizer was removed (which will allow the resource to be deleted)
	assert.Empty(t, inst.Finalizers, "expected the finalizer to be removed")

	// The installation is deleted after the finalizer is removed
	controller.Delete(ctx, &inst)

	// Verify that reconcile doesn't error out after it's deleted
	triggerReconcile()
}

func Test_createAgentVolume(t *testing.T) {
	controller := setupTestController()

	inst := &porterv1.Installation{
		TypeMeta: metav1.TypeMeta{
			APIVersion: porterv1.GroupVersion.String(),
			Kind:       "Installation",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "test",
			Name:            "porter-hello",
			Generation:      1,
			ResourceVersion: "123",
			UID:             "random-uid",
		},
		Spec: porterv1.InstallationSpec{
			Name:   "mybuns",
			Active: true,
			Bundle: porterv1.OCIReferenceParts{Repository: "getporter/porter-hello", Version: "0.1.1"},
		},
	}
	agentCfg := porterv1.AgentConfigSpec{
		VolumeSize:                 "128Mi",
		PorterRepository:           "getporter/custom-agent",
		PorterVersion:              "v1.0.0",
		PullPolicy:                 "Always",
		ServiceAccount:             "porteraccount",
		InstallationServiceAccount: "installeraccount",
	}
	pvc, err := controller.createAgentVolume(context.Background(), logr.Discard(), inst, agentCfg)
	require.NoError(t, err)

	// Verify the pvc properties
	assert.Equal(t, "porter-hello-1-123", pvc.GenerateName, "incorrect pvc name")
	assert.Equal(t, inst.Namespace, pvc.Namespace, "incorrect pvc namespace")
	assert.Equal(t, []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, pvc.Spec.AccessModes, "incorrect pvc access modes")
	assert.Equal(t, pvc.Spec.Resources.Requests[corev1.ResourceStorage], resource.MustParse("128Mi"))
	assertSharedAgentLabels(t, pvc.Labels)
}

func Test_createAgentSecret(t *testing.T) {
	controller := setupTestController()

	inst := &porterv1.Installation{
		TypeMeta: metav1.TypeMeta{
			APIVersion: porterv1.GroupVersion.String(),
			Kind:       "Installation",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "test",
			Name:            "porter-hello",
			Generation:      1,
			ResourceVersion: "123",
			UID:             "random-uid",
		},
		Spec: porterv1.InstallationSpec{
			Name:   "mybuns",
			Active: true,
			Bundle: porterv1.OCIReferenceParts{Repository: "getporter/porter-hello", Version: "0.1.1"},
		},
	}
	porterCfg := porterv1.PorterConfigSpec{}
	secret, err := controller.createAgentSecret(context.Background(), logr.Discard(), inst, porterCfg)
	require.NoError(t, err)

	// Verify the secret properties
	assert.Equal(t, "porter-hello-1-123", secret.GenerateName, "incorrect secret name")
	assert.Equal(t, inst.Namespace, secret.Namespace, "incorrect secret namespace")
	assert.Equal(t, corev1.SecretTypeOpaque, secret.Type, "expected the secret to be of type Opaque")
	assert.Equal(t, pointer.BoolPtr(true), secret.Immutable, "expected the secret to be immutable")
	assert.Contains(t, secret.Data, "config.yaml", "expected the secret to have config.yaml")
	assert.Contains(t, secret.Data, "installation.yaml", "expected the secret to have installation.yaml")
	assertSharedAgentLabels(t, secret.Labels)
}

func Test_createAgentJob(t *testing.T) {
	controller := setupTestController()

	cmd := []string{"porter", "installation", "apply", "-f=installation.yaml"}
	inst := &porterv1.Installation{
		TypeMeta: metav1.TypeMeta{
			APIVersion: porterv1.GroupVersion.String(),
			Kind:       "Installation",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "test",
			Name:            "porter-hello",
			Generation:      1,
			ResourceVersion: "123",
			UID:             "random-uid",
		},
		Spec: porterv1.InstallationSpec{
			Name:   "mybuns",
			Active: true,
			Bundle: porterv1.OCIReferenceParts{Repository: "getporter/porter-hello", Version: "0.1.1"},
		},
	}
	agentCfg := porterv1.AgentConfigSpec{
		VolumeSize:                 "128Mi",
		PorterRepository:           "getporter/custom-agent",
		PorterVersion:              "v1.0.0",
		PullPolicy:                 "Always",
		ServiceAccount:             "porteraccount",
		InstallationServiceAccount: "installeraccount",
	}
	pvc := corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "mypvc"}}
	secret := corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "mysecret"}}
	job, err := controller.createAgentJob(context.Background(), logr.Discard(), cmd, inst, agentCfg, pvc, secret)
	require.NoError(t, err)

	// Verify the job properties
	wantName := "porter-hello-1-123"
	assert.Equal(t, wantName, job.GenerateName, "incorrect job name")
	assert.Equal(t, inst.Namespace, job.Namespace, "incorrect job namespace")
	assert.Len(t, job.OwnerReferences, 1, "expected the job to have an owner reference")
	wantOwnerRef := metav1.OwnerReference{
		APIVersion:         porterv1.GroupVersion.String(),
		Kind:               "Installation",
		Name:               "porter-hello",
		UID:                "random-uid",
		BlockOwnerDeletion: pointer.BoolPtr(true),
	}
	assert.Equal(t, wantOwnerRef, job.OwnerReferences[0], "incorrect owner reference")
	assertSharedAgentLabels(t, job.Labels)
	assertContains(t, job.Labels, labelJobType, jobTypeAgent)
	assert.Equal(t, pointer.Int32Ptr(1), job.Spec.Completions, "incorrect job completions")
	assert.Equal(t, pointer.Int32Ptr(0), job.Spec.BackoffLimit, "incorrect job back off limit")

	// Verify the job pod template
	podTemplate := job.Spec.Template
	assert.Equal(t, wantName, podTemplate.GenerateName, "incorrect pod generate name")
	assert.Equal(t, "test", podTemplate.Namespace, "incorrect pod namespace")
	assertSharedAgentLabels(t, podTemplate.Labels)
	assert.Len(t, podTemplate.Spec.Volumes, 2, "incorrect pod volumes")
	assert.Equal(t, "porter-shared", podTemplate.Spec.Volumes[0].Name, "expected the porter-shared volume")
	assert.Equal(t, "porter-config", podTemplate.Spec.Volumes[1].Name, "expected the porter-config volume")
	assert.Equal(t, "porteraccount", podTemplate.Spec.ServiceAccountName, "incorrect service account for the pod")

	// Verify the agent container
	agentContainer := podTemplate.Spec.Containers[0]
	assert.Equal(t, "porter-agent", agentContainer.Name, "incorrect agent container name")
	assert.Equal(t, "getporter/custom-agent:v1.0.0", agentContainer.Image, "incorrect agent image")
	assert.Equal(t, corev1.PullPolicy("Always"), agentContainer.ImagePullPolicy, "incorrect agent pull policy")
	assert.Equal(t, cmd, agentContainer.Args, "incorrect agent command arguments")
	assertEnvVar(t, agentContainer.Env, "PORTER_RUNTIME_DRIVER", "kubernetes")
	assertEnvVar(t, agentContainer.Env, "KUBE_NAMESPACE", "test")
	assertEnvVar(t, agentContainer.Env, "IN_CLUSTER", "true")
	assertEnvVar(t, agentContainer.Env, "JOB_VOLUME_NAME", pvc.Name)
	assertEnvVar(t, agentContainer.Env, "JOB_VOLUME_PATH", "/porter-shared")
	assertEnvVar(t, agentContainer.Env, "CLEANUP_JOBS", "false") // this will be configurable in the future
	assertEnvVar(t, agentContainer.Env, "SERVICE_ACCOUNT", "installeraccount")
	assertEnvVar(t, agentContainer.Env, "LABELS", "porter.sh/jobType=bundle-installer porter.sh/managed=true porter.sh/resourceGeneration=1 porter.sh/resourceKind=Installation porter.sh/resourceName=porter-hello porter.sh/resourceVersion=123 porter.sh/retry=")
	assertEnvVar(t, agentContainer.Env, "AFFINITY_MATCH_LABELS", "porter.sh/resourceKind=Installation porter.sh/resourceName=porter-hello porter.sh/resourceGeneration=1 porter.sh/retry=")
	assertEnvFrom(t, agentContainer.EnvFrom, "porter-env", pointer.BoolPtr(true))
	assertVolumeMount(t, agentContainer.VolumeMounts, "porter-config", "/porter-config")
	assertVolumeMount(t, agentContainer.VolumeMounts, "porter-shared", "/porter-shared")
}

func assertSharedAgentLabels(t *testing.T, labels map[string]string) {
	assertContains(t, labels, labelManaged, "true")
	assertContains(t, labels, labelResourceKind, "Installation")
	assertContains(t, labels, labelResourceName, "porter-hello")
	assertContains(t, labels, labelResourceGeneration, "1")
	assertContains(t, labels, labelResourceVersion, "123")
	assertContains(t, labels, labelRetry, "")
}

func assertContains(t *testing.T, labels map[string]string, key string, value string) {
	assert.Contains(t, labels, key, "expected the %s key to be set", key)
	assert.Equal(t, value, labels[key], "incorrect value for key %s", key)
}

func assertEnvVar(t *testing.T, envVars []corev1.EnvVar, name string, value string) {
	for _, envVar := range envVars {
		if envVar.Name == name {
			assert.Equal(t, value, envVar.Value, "incorrect value for EnvVar %s", name)
			return
		}
	}

	assert.Failf(t, "expected the %s EnvVar to be set", name)
}

func assertEnvFrom(t *testing.T, envFrom []corev1.EnvFromSource, name string, optional *bool) {
	for _, source := range envFrom {
		if source.SecretRef.Name == name {
			assert.Equal(t, optional, source.SecretRef.Optional, "incorrect optional flag for EnvFrom %s", name)
			return
		}
	}

	assert.Failf(t, "expected the %s EnvFrom to be set", name)
}

func assertVolumeMount(t *testing.T, mounts []corev1.VolumeMount, name string, path string) {
	for _, mount := range mounts {
		if mount.Name == name {
			assert.Equal(t, path, mount.MountPath, "incorrect mount path for VolumeMount %s", name)
			return
		}
	}

	assert.Failf(t, "expected the %s VolumeMount to be set", name)
}
