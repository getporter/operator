package controllers

import (
	"context"
	"fmt"
	"testing"

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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPorterResourceStatus_ApplyAgentAction(t *testing.T) {
	tests := []struct {
		name       string
		action     *porterv1.AgentAction
		resource   porterResource
		wantStatus porterv1.PorterResourceStatus
	}{
		{
			name:     "no action",
			resource: &porterv1.Installation{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			wantStatus: porterv1.PorterResourceStatus{
				PorterStatus: porterv1.PorterStatus{
					ObservedGeneration: 1,
					Phase:              porterv1.PhaseUnknown,
				},
			},
		},
		{
			name:     "action created",
			resource: &porterv1.Installation{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			action: &porterv1.AgentAction{
				ObjectMeta: metav1.ObjectMeta{Name: "myaction"},
				Status: porterv1.AgentActionStatus{
					PorterStatus: porterv1.PorterStatus{
						Phase: porterv1.PhasePending,
						Conditions: []metav1.Condition{
							{Type: string(porterv1.ConditionScheduled), Status: metav1.ConditionTrue},
						},
					},
				}},
			wantStatus: porterv1.PorterResourceStatus{
				Action: &corev1.LocalObjectReference{Name: "myaction"},
				PorterStatus: porterv1.PorterStatus{
					ObservedGeneration: 1,
					Phase:              porterv1.PhasePending,
					Conditions: []metav1.Condition{
						{Type: string(porterv1.ConditionScheduled), Status: metav1.ConditionTrue},
					},
				}},
		},
		{name: "action started",
			resource: &porterv1.Installation{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			action: &porterv1.AgentAction{
				ObjectMeta: metav1.ObjectMeta{Name: "myaction"},
				Status: porterv1.AgentActionStatus{
					PorterStatus: porterv1.PorterStatus{
						Phase: porterv1.PhaseRunning,
						Conditions: []metav1.Condition{
							{Type: string(porterv1.ConditionScheduled), Status: metav1.ConditionTrue},
							{Type: string(porterv1.ConditionStarted), Status: metav1.ConditionTrue},
						},
					},
				}},
			wantStatus: porterv1.PorterResourceStatus{
				Action: &corev1.LocalObjectReference{Name: "myaction"},
				PorterStatus: porterv1.PorterStatus{
					ObservedGeneration: 1,
					Phase:              porterv1.PhaseRunning,
					Conditions: []metav1.Condition{
						{Type: string(porterv1.ConditionScheduled), Status: metav1.ConditionTrue},
						{Type: string(porterv1.ConditionStarted), Status: metav1.ConditionTrue},
					},
				}},
		},
		{name: "action succeeded",
			resource: &porterv1.Installation{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			action: &porterv1.AgentAction{
				ObjectMeta: metav1.ObjectMeta{Name: "myaction"},
				Status: porterv1.AgentActionStatus{
					PorterStatus: porterv1.PorterStatus{
						Phase: porterv1.PhaseSucceeded,
						Conditions: []metav1.Condition{
							{Type: string(porterv1.ConditionScheduled), Status: metav1.ConditionTrue},
							{Type: string(porterv1.ConditionStarted), Status: metav1.ConditionTrue},
							{Type: string(porterv1.ConditionComplete), Status: metav1.ConditionTrue},
						},
					},
				}},
			wantStatus: porterv1.PorterResourceStatus{
				Action: &corev1.LocalObjectReference{Name: "myaction"},
				PorterStatus: porterv1.PorterStatus{
					ObservedGeneration: 1,
					Phase:              porterv1.PhaseSucceeded,
					Conditions: []metav1.Condition{
						{Type: string(porterv1.ConditionScheduled), Status: metav1.ConditionTrue},
						{Type: string(porterv1.ConditionStarted), Status: metav1.ConditionTrue},
						{Type: string(porterv1.ConditionComplete), Status: metav1.ConditionTrue},
					},
				}},
		},
		{name: "action failed",
			resource: &porterv1.Installation{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			action: &porterv1.AgentAction{
				ObjectMeta: metav1.ObjectMeta{Name: "myaction"},
				Status: porterv1.AgentActionStatus{
					PorterStatus: porterv1.PorterStatus{
						Phase: porterv1.PhaseFailed,
						Conditions: []metav1.Condition{
							{Type: string(porterv1.ConditionScheduled), Status: metav1.ConditionTrue},
							{Type: string(porterv1.ConditionStarted), Status: metav1.ConditionTrue},
							{Type: string(porterv1.ConditionFailed), Status: metav1.ConditionTrue},
						},
					}}},
			wantStatus: porterv1.PorterResourceStatus{
				Action: &corev1.LocalObjectReference{Name: "myaction"},
				PorterStatus: porterv1.PorterStatus{
					ObservedGeneration: 1,
					Phase:              porterv1.PhaseFailed,
					Conditions: []metav1.Condition{
						{Type: string(porterv1.ConditionScheduled), Status: metav1.ConditionTrue},
						{Type: string(porterv1.ConditionStarted), Status: metav1.ConditionTrue},
						{Type: string(porterv1.ConditionFailed), Status: metav1.ConditionTrue},
					},
				}},
		},
		{name: "update resets status",
			resource: &porterv1.Installation{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
				Status: porterv1.InstallationStatus{PorterResourceStatus: porterv1.PorterResourceStatus{
					Action: nil,
					PorterStatus: porterv1.PorterStatus{
						ObservedGeneration: 1,
						Phase:              porterv1.PhaseFailed,
						Conditions: []metav1.Condition{
							{Type: string(porterv1.ConditionScheduled), Status: metav1.ConditionTrue},
							{Type: string(porterv1.ConditionStarted), Status: metav1.ConditionTrue},
							{Type: string(porterv1.ConditionFailed), Status: metav1.ConditionTrue},
						},
					}}}},
			wantStatus: porterv1.PorterResourceStatus{
				Action: nil,
				PorterStatus: porterv1.PorterStatus{
					ObservedGeneration: 2,
					Phase:              porterv1.PhaseUnknown,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyAgentAction(logr.Discard(), tt.resource, tt.action)

			gotStatus := tt.resource.GetStatus()
			assert.Equal(t, tt.wantStatus.Phase, gotStatus.Phase, "incorrect Phase")
			assert.Equal(t, tt.wantStatus.ObservedGeneration, gotStatus.ObservedGeneration, "incorrect ObservedGeneration")
			assert.Equal(t, tt.wantStatus.Action, gotStatus.Action, "incorrect Action")

			assert.Len(t, gotStatus.Conditions, len(tt.wantStatus.Conditions), "incorrect number of Conditions")
			for _, cond := range tt.wantStatus.Conditions {
				assert.True(t, apimeta.IsStatusConditionPresentAndEqual(gotStatus.Conditions, cond.Type, cond.Status), "expected condition %s to be %s", cond.Type, cond.Status)
			}
		})
	}
}

func TestAgentActionReconciler_Reconcile(t *testing.T) {
	// long test is long
	// Run through a full resource lifecycle: create, update, delete
	ctx := context.Background()

	namespace := "test"
	name := "mybuns-install"
	testdata := []client.Object{
		&porterv1.AgentAction{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, Generation: 1}},
	}
	controller := setupAgentActionController(testdata...)

	var action porterv1.AgentAction
	triggerReconcile := func() {
		fullname := types.NamespacedName{Namespace: namespace, Name: name}
		key := client.ObjectKey{Namespace: namespace, Name: name}

		request := controllerruntime.Request{
			NamespacedName: fullname,
		}
		result, err := controller.Reconcile(ctx, request)
		require.NoError(t, err)
		require.True(t, result.IsZero())

		var updatedAction porterv1.AgentAction
		if err := controller.Get(ctx, key, &updatedAction); err == nil {
			action = updatedAction
		}
	}

	triggerReconcile()

	// Verify the action was picked up and the status initialized
	assert.Equal(t, porterv1.PhaseUnknown, action.Status.Phase, "New resources should be initialized to Phase: Unknown")

	triggerReconcile()

	// Verify a job has been scheduled
	var jobs batchv1.JobList
	require.NoError(t, controller.List(ctx, &jobs))
	require.Len(t, jobs.Items, 1)
	job := jobs.Items[0]

	require.NotNil(t, action.Status.Job, "expected ActiveJob to be set")
	assert.Equal(t, job.Name, action.Status.Job.Name, "expected ActiveJob to contain the job name")
	assert.Equal(t, porterv1.PhasePending, action.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(action.Status.Conditions, string(porterv1.ConditionScheduled)))

	// Start the job
	job.Status.Active = 1
	require.NoError(t, controller.Status().Update(ctx, &job))

	triggerReconcile()

	// Verify that the action status has the job
	require.NotNil(t, action.Status.Job, "expected Job to be set")
	assert.Equal(t, job.Name, action.Status.Job.Name, "expected Job to contain the job name")
	assert.Equal(t, porterv1.PhaseRunning, action.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(action.Status.Conditions, string(porterv1.ConditionStarted)))

	// Complete the job
	job.Status.Active = 0
	job.Status.Succeeded = 1
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &job))

	triggerReconcile()

	// Verify that the action status shows the job is done
	require.NotNil(t, action.Status.Job, "expected Job to still be set")
	assert.Equal(t, porterv1.PhaseSucceeded, action.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(action.Status.Conditions, string(porterv1.ConditionComplete)))

	// Fail the job
	job.Status.Active = 0
	job.Status.Succeeded = 0
	job.Status.Failed = 1
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &job))

	triggerReconcile()

	// Verify that the action status shows the job is failed
	require.NotNil(t, action.Status.Job, "expected Job to still be set")
	assert.Equal(t, porterv1.PhaseFailed, action.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(action.Status.Conditions, string(porterv1.ConditionFailed)))

	// Edit the action spec
	action.Generation = 2
	require.NoError(t, controller.Update(ctx, &action))

	triggerReconcile()

	// Verify that the action status was re-initialized
	assert.Equal(t, int64(2), action.Status.ObservedGeneration)
	assert.Equal(t, porterv1.PhaseUnknown, action.Status.Phase, "New resources should be initialized to Phase: Unknown")
	assert.Empty(t, action.Status.Conditions, "Conditions should have been reset")

	// Delete the action
	controller.Delete(ctx, &action)

	// Verify that reconcile doesn't error out after it's deleted
	triggerReconcile()
}

func TestAgentActionReconciler_createAgentVolume(t *testing.T) {
	tests := []struct {
		name            string
		createNamespace bool
		existingVolume  bool
		matchLabels     bool
		created         bool
	}{
		{
			name:            "No agent volumes exist in cluster creates volume",
			createNamespace: false,
			existingVolume:  false,
			matchLabels:     false,
			created:         true,
		},
		{
			name:            "Existing volume with matching labels in separate namespace creates volume",
			createNamespace: true,
			existingVolume:  true,
			matchLabels:     true,
			created:         true,
		},
		{
			name:            "Existing volume without matching labels in separate namespace creates volume",
			createNamespace: true,
			existingVolume:  true,
			matchLabels:     false,
			created:         true,
		},
		{
			name:            "Existing volume in agent namespace without matching labels creates volume",
			createNamespace: false,
			existingVolume:  true,
			matchLabels:     false,
			created:         true,
		},
		{
			name:            "Existing volume in agent namespace with matching labels does not create volume",
			createNamespace: false,
			existingVolume:  true,
			matchLabels:     true,
			created:         false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controller := setupAgentActionController()
			action := &porterv1.AgentAction{
				TypeMeta: metav1.TypeMeta{
					APIVersion: porterv1.GroupVersion.String(),
					Kind:       "AgentAction",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       "test",
					Name:            "porter-hello",
					Generation:      1,
					ResourceVersion: "123",
					UID:             "random-uid",
					Labels: map[string]string{
						"testLabel": "abc123",
					},
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
			if test.existingVolume {
				namespace := action.Namespace
				if test.createNamespace {
					namespace = "test-existing"
					existingNs := &corev1.Namespace{
						ObjectMeta: metav1.ObjectMeta{
							GenerateName: namespace,
							Labels: map[string]string{
								"porter-test": "true",
							},
						},
					}
					err := controller.Client.Create(context.Background(), existingNs)
					require.NoError(t, err)
				}
				sharedLabels := map[string]string{
					"match": "false",
				}
				// Overwrite the labels with the action labels
				if test.matchLabels {
					sharedLabels = controller.getSharedAgentLabels(action)
				}
				existingPvc := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "existing-",
						Namespace:    namespace,
						Labels:       sharedLabels,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
						Resources: corev1.ResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("64Mi"),
							},
						},
					},
				}
				err := controller.Client.Create(context.Background(), existingPvc)
				require.NoError(t, err)
			}
			pvc, err := controller.createAgentVolume(context.Background(), logr.Discard(), action, agentCfg)
			require.NoError(t, err)

			// Verify the pvc properties
			if test.created {
				assert.Equal(t, "porter-hello-", pvc.GenerateName, "incorrect pvc name")
				assert.Equal(t, []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, pvc.Spec.AccessModes, "incorrect pvc access modes")
				assert.Equal(t, pvc.Spec.Resources.Requests[corev1.ResourceStorage], resource.MustParse("128Mi"))
			} else {
				assert.Equal(t, "existing-", pvc.GenerateName, "incorrect pvc name")
				assert.Equal(t, []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}, pvc.Spec.AccessModes, "incorrect pvc access modes")
				assert.Equal(t, pvc.Spec.Resources.Requests[corev1.ResourceStorage], resource.MustParse("64Mi"))
			}
			assert.Equal(t, action.Namespace, pvc.Namespace, "incorrect pvc namespace")
			assertSharedAgentLabels(t, pvc.Labels)
			assertContains(t, pvc.Labels, "testLabel", "abc123", "incorrect label")
		})
	}
}

func TestAgentActionReconciler_createConfigSecret(t *testing.T) {
	tests := []struct {
		name            string
		createNamespace bool
		existingSecret  bool
		matchLabels     bool
		created         bool
	}{
		{
			name:            "No config secret exist in cluster creates secret",
			createNamespace: false,
			existingSecret:  false,
			matchLabels:     false,
			created:         true,
		},
		{
			name:            "Existing config secret with matching labels in separate namespace creates secret",
			createNamespace: true,
			existingSecret:  true,
			matchLabels:     true,
			created:         true,
		},
		{
			name:            "Existing config secret without matching labels in separate namespace creates secret",
			createNamespace: true,
			existingSecret:  true,
			matchLabels:     false,
			created:         true,
		},
		{
			name:            "Existing config secret in agent namespace without matching labels creates secret",
			createNamespace: false,
			existingSecret:  true,
			matchLabels:     false,
			created:         true,
		},
		{
			name:            "Existing config secret in agent namespace with matching labels does not create secret",
			createNamespace: false,
			existingSecret:  true,
			matchLabels:     true,
			created:         false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controller := setupAgentActionController()
			action := &porterv1.AgentAction{
				TypeMeta: metav1.TypeMeta{
					APIVersion: porterv1.GroupVersion.String(),
					Kind:       "AgentAction",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       "test",
					Name:            "porter-hello",
					Generation:      1,
					ResourceVersion: "123",
					UID:             "random-uid",
					Labels: map[string]string{
						"testLabel": "abc123",
					},
				},
			}
			if test.existingSecret {
				namespace := action.Namespace
				if test.createNamespace {
					namespace = "test-existing"
					existingNs := &corev1.Namespace{
						ObjectMeta: metav1.ObjectMeta{
							GenerateName: namespace,
							Labels: map[string]string{
								"porter-test": "true",
							},
						},
					}
					err := controller.Client.Create(context.Background(), existingNs)
					require.NoError(t, err)
				}
				sharedLabels := map[string]string{
					"match": "false",
				}
				// Overwrite the labels with the action labels
				if test.matchLabels {
					sharedLabels = controller.getSharedAgentLabels(action)
					sharedLabels[porterv1.LabelSecretType] = porterv1.SecretTypeConfig
				}
				porterCfg := porterv1.PorterConfigSpec{}
				porterCfgB, _ := porterCfg.ToPorterDocument()
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "existing-",
						Namespace:    namespace,
						Labels:       sharedLabels,
					},
					Type:      corev1.SecretTypeBasicAuth,
					Immutable: pointer.BoolPtr(false),
					Data: map[string][]byte{
						"config.yaml": porterCfgB,
					},
				}
				err := controller.Client.Create(context.Background(), secret)
				require.NoError(t, err)
			}
			porterCfg := porterv1.PorterConfigSpec{}
			secret, err := controller.createConfigSecret(context.Background(), logr.Discard(), action, porterCfg)
			require.NoError(t, err)

			// Verify the secret properties
			if test.created {
				assert.Equal(t, "porter-hello-", secret.GenerateName, "incorrect secret name")
				assert.Equal(t, corev1.SecretTypeOpaque, secret.Type, "expected the secret to be of type Opaque")
				assert.Equal(t, pointer.BoolPtr(true), secret.Immutable, "expected the secret to be immutable")
				assert.Contains(t, secret.Data, "config.yaml", "expected the secret to have config.yaml")
			} else {
				assert.Equal(t, "existing-", secret.GenerateName, "incorrect secret name")
				assert.Equal(t, corev1.SecretTypeBasicAuth, secret.Type, "expected the secret to be of type Opaque")
				assert.Equal(t, pointer.BoolPtr(false), secret.Immutable, "expected the secret to be immutable")
				assert.Contains(t, secret.Data, "config.yaml", "expected the secret to have config.yaml")

			}
			assert.Equal(t, action.Namespace, secret.Namespace, "incorrect secret namespace")
			assertSharedAgentLabels(t, secret.Labels)
			assertContains(t, secret.Labels, porterv1.LabelSecretType, porterv1.SecretTypeConfig, "incorrect label")
			assertContains(t, secret.Labels, "testLabel", "abc123", "incorrect label")
		})
	}
}

func TestAgentActionReconciler_createWorkdirSecret(t *testing.T) {
	tests := []struct {
		name            string
		createNamespace bool
		existingSecret  bool
		matchLabels     bool
		created         bool
	}{
		{
			name:            "No workdir secret exist in cluster creates secret",
			createNamespace: false,
			existingSecret:  false,
			matchLabels:     false,
			created:         true,
		},
		{
			name:            "Existing workdir secret with matching labels in separate namespace creates secret",
			createNamespace: true,
			existingSecret:  true,
			matchLabels:     true,
			created:         true,
		},
		{
			name:            "Existing workdir secret without matching labels in separate namespace creates secret",
			createNamespace: true,
			existingSecret:  true,
			matchLabels:     false,
			created:         true,
		},
		{
			name:            "Existing workdir secret in agent namespace without matching labels creates secret",
			createNamespace: false,
			existingSecret:  true,
			matchLabels:     false,
			created:         true,
		},
		{
			name:            "Existing workdir secret in agent namespace with matching labels does not create secret",
			createNamespace: false,
			existingSecret:  true,
			matchLabels:     true,
			created:         false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controller := setupAgentActionController()

			action := &porterv1.AgentAction{
				TypeMeta: metav1.TypeMeta{
					APIVersion: porterv1.GroupVersion.String(),
					Kind:       "AgentAction",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       "test",
					Name:            "porter-hello",
					Generation:      1,
					ResourceVersion: "123",
					UID:             "random-uid",
					Labels: map[string]string{
						"testLabel": "abc123",
					},
				},
				Spec: porterv1.AgentActionSpec{
					Files: map[string][]byte{
						"installation.yaml": []byte(`{}`),
					},
				},
			}
			if test.existingSecret {
				namespace := action.Namespace
				if test.createNamespace {
					namespace = "test-existing"
					existingNs := &corev1.Namespace{
						ObjectMeta: metav1.ObjectMeta{
							GenerateName: namespace,
							Labels: map[string]string{
								"porter-test": "true",
							},
						},
					}
					err := controller.Client.Create(context.Background(), existingNs)
					require.NoError(t, err)
				}
				sharedLabels := map[string]string{
					"match": "false",
				}
				// Overwrite the labels with the action labels
				if test.matchLabels {
					sharedLabels = controller.getSharedAgentLabels(action)
					sharedLabels[porterv1.LabelSecretType] = porterv1.SecretTypeWorkdir
				}
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "existing-",
						Namespace:    namespace,
						Labels:       sharedLabels,
					},
					Type:      corev1.SecretTypeBasicAuth,
					Immutable: pointer.BoolPtr(false),
					Data: map[string][]byte{
						"existing.yaml": []byte(`{}`),
					},
				}
				err := controller.Client.Create(context.Background(), secret)
				require.NoError(t, err)
			}
			secret, err := controller.createWorkdirSecret(context.Background(), logr.Discard(), action)
			require.NoError(t, err)

			// Verify the secret properties
			if test.created {
				assert.Equal(t, "porter-hello-", secret.GenerateName, "incorrect secret name")
				assert.Equal(t, corev1.SecretTypeOpaque, secret.Type, "expected the secret to be of type Opaque")
				assert.Equal(t, pointer.BoolPtr(true), secret.Immutable, "expected the secret to be immutable")
				assert.Contains(t, secret.Data, "installation.yaml", "expected the secret to have config.yaml")
			} else {
				assert.Equal(t, "existing-", secret.GenerateName, "incorrect secret name")
				assert.Equal(t, corev1.SecretTypeBasicAuth, secret.Type, "expected the secret to be of type Opaque")
				assert.Equal(t, pointer.BoolPtr(false), secret.Immutable, "expected the secret to be immutable")
				assert.Contains(t, secret.Data, "existing.yaml", "expected the secret to have config.yaml")
			}
			assert.Equal(t, action.Namespace, secret.Namespace, "incorrect secret namespace")
			assertSharedAgentLabels(t, secret.Labels)
			assertContains(t, secret.Labels, porterv1.LabelSecretType, porterv1.SecretTypeWorkdir, "incorrect label")
			assertContains(t, secret.Labels, "testLabel", "abc123", "incorrect label")
		})
	}
}

func TestAgentActionReconciler_createAgentJob(t *testing.T) {
	controller := setupAgentActionController()

	action := &porterv1.AgentAction{
		TypeMeta: metav1.TypeMeta{
			APIVersion: porterv1.GroupVersion.String(),
			Kind:       "AgentAction",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "test",
			Name:            "porter-hello",
			Generation:      1,
			ResourceVersion: "123",
			UID:             "random-uid",
			Labels: map[string]string{
				"testLabel": "abc123",
			},
		},
		Spec: porterv1.AgentActionSpec{
			Args: []string{"installation", "apply", "installation.yaml"},
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
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "mypvc"}}
	configSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "mysecret"}}
	workDirSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "mysecret"}}
	job, err := controller.createAgentJob(context.Background(), logr.Discard(), action, agentCfg, pvc, configSecret, workDirSecret)
	require.NoError(t, err)

	// Verify the job properties
	wantName := "porter-hello-"
	assert.Equal(t, wantName, job.GenerateName, "incorrect job name")
	assert.Equal(t, action.Namespace, job.Namespace, "incorrect job namespace")
	assert.Len(t, job.OwnerReferences, 1, "expected the job to have an owner reference")
	wantOwnerRef := metav1.OwnerReference{
		APIVersion:         porterv1.GroupVersion.String(),
		Kind:               "AgentAction",
		Name:               "porter-hello",
		UID:                "random-uid",
		Controller:         pointer.BoolPtr(true),
		BlockOwnerDeletion: pointer.BoolPtr(true),
	}
	assert.Equal(t, wantOwnerRef, job.OwnerReferences[0], "incorrect owner reference")
	assertSharedAgentLabels(t, job.Labels)
	assertContains(t, job.Labels, porterv1.LabelJobType, porterv1.JobTypeAgent, "incorrect label")
	assertContains(t, job.Labels, "testLabel", "abc123", "incorrect label")
	assert.Equal(t, pointer.Int32Ptr(1), job.Spec.Completions, "incorrect job completions")
	assert.Equal(t, pointer.Int32Ptr(0), job.Spec.BackoffLimit, "incorrect job back off limit")

	// Verify the job pod template
	podTemplate := job.Spec.Template
	assert.Equal(t, wantName, podTemplate.GenerateName, "incorrect pod generate name")
	assert.Equal(t, "test", podTemplate.Namespace, "incorrect pod namespace")
	assertSharedAgentLabels(t, podTemplate.Labels)
	assertContains(t, podTemplate.Labels, "testLabel", "abc123", "incorrect label")
	assert.Len(t, podTemplate.Spec.Volumes, 3, "incorrect pod volumes")
	assert.Len(t, podTemplate.Spec.Volumes, 3)
	assert.Equal(t, porterv1.VolumePorterSharedName, podTemplate.Spec.Volumes[0].Name, "expected the porter-shared volume")
	assert.Equal(t, porterv1.VolumePorterConfigName, podTemplate.Spec.Volumes[1].Name, "expected the porter-config volume")
	assert.Equal(t, porterv1.VolumePorterWorkDirName, podTemplate.Spec.Volumes[2].Name, "expected the porter-workdir volume")
	assert.Equal(t, "porteraccount", podTemplate.Spec.ServiceAccountName, "incorrect service account for the pod")
	assert.Equal(t, pointer.Int64Ptr(65532), podTemplate.Spec.SecurityContext.RunAsUser, "incorrect RunAsUser")
	assert.Equal(t, pointer.Int64Ptr(0), podTemplate.Spec.SecurityContext.RunAsGroup, "incorrect RunAsGroup")
	assert.Equal(t, pointer.Int64Ptr(0), podTemplate.Spec.SecurityContext.FSGroup, "incorrect FSGroup")

	// Verify the agent container
	agentContainer := podTemplate.Spec.Containers[0]
	assert.Equal(t, "porter-agent", agentContainer.Name, "incorrect agent container name")
	assert.Equal(t, "getporter/custom-agent:v1.0.0", agentContainer.Image, "incorrect agent image")
	assert.Equal(t, corev1.PullPolicy("Always"), agentContainer.ImagePullPolicy, "incorrect agent pull policy")
	assert.Equal(t, []string{"installation", "apply", "installation.yaml"}, agentContainer.Args, "incorrect agent command arguments")
	assertEnvVar(t, agentContainer.Env, "PORTER_RUNTIME_DRIVER", "kubernetes")
	assertEnvVar(t, agentContainer.Env, "KUBE_NAMESPACE", "test")
	assertEnvVar(t, agentContainer.Env, "IN_CLUSTER", "true")
	assertEnvVar(t, agentContainer.Env, "JOB_VOLUME_NAME", pvc.Name)
	assertEnvVar(t, agentContainer.Env, "JOB_VOLUME_PATH", porterv1.VolumePorterSharedPath)
	assertEnvVar(t, agentContainer.Env, "CLEANUP_JOBS", "false") // this will be configurable in the future
	assertEnvVar(t, agentContainer.Env, "SERVICE_ACCOUNT", "installeraccount")
	assertEnvVar(t, agentContainer.Env, "LABELS", "porter.sh/jobType=bundle-installer porter.sh/managed=true porter.sh/resourceGeneration=1 porter.sh/resourceKind=AgentAction porter.sh/resourceName=porter-hello porter.sh/retry= testLabel=abc123")
	assertEnvVar(t, agentContainer.Env, "AFFINITY_MATCH_LABELS", "porter.sh/resourceKind=AgentAction porter.sh/resourceName=porter-hello porter.sh/resourceGeneration=1 porter.sh/retry=")
	assertEnvFrom(t, agentContainer.EnvFrom, "porter-env", pointer.BoolPtr(true))
	assert.Len(t, agentContainer.VolumeMounts, 3)
	assertVolumeMount(t, agentContainer.VolumeMounts, porterv1.VolumePorterConfigName, porterv1.VolumePorterConfigPath)
	assertVolumeMount(t, agentContainer.VolumeMounts, porterv1.VolumePorterSharedName, porterv1.VolumePorterSharedPath)
	assertVolumeMount(t, agentContainer.VolumeMounts, porterv1.VolumePorterWorkDirName, porterv1.VolumePorterWorkDirPath)
}

func assertSharedAgentLabels(t *testing.T, labels map[string]string) {
	assertContains(t, labels, porterv1.LabelManaged, "true", "incorrect label")
	assertContains(t, labels, porterv1.LabelResourceKind, "AgentAction", "incorrect label")
	assertContains(t, labels, porterv1.LabelResourceName, "porter-hello", "incorrect label")
	assertContains(t, labels, porterv1.LabelResourceGeneration, "1", "incorrect label")
	assertContains(t, labels, porterv1.LabelRetry, "", "incorrect label")
}

func assertContains(t *testing.T, labels map[string]string, key string, value string, msg string) {
	assert.Contains(t, labels, key, "%s: expected the %s key to be set", msg, key)
	assert.Equal(t, value, labels[key], "%s: incorrect value for key %s", msg, key)
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

	assert.Fail(t, fmt.Sprintf("expected the %s VolumeMount to be set", name))
}

func setupAgentActionController(objs ...client.Object) AgentActionReconciler {
	scheme := runtime.NewScheme()
	porterv1.AddToScheme(scheme)
	batchv1.AddToScheme(scheme)
	corev1.AddToScheme(scheme)

	fakeBuilder := fake.NewClientBuilder()
	fakeBuilder.WithScheme(scheme)
	fakeBuilder.WithObjects(objs...)
	fakeClient := fakeBuilder.Build()

	return AgentActionReconciler{
		Log:    logr.Discard(),
		Client: fakeClient,
		Scheme: scheme,
	}
}
