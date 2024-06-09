package controllers

import (
	"context"
	"fmt"
	"testing"
	"time"

	v1 "get.porter.sh/operator/api/v1"
	mocks "get.porter.sh/operator/mocks/grpc"
	installationv1 "get.porter.sh/porter/gen/proto/go/porterapis/installation/v1alpha1"
	porterv1alpha1 "get.porter.sh/porter/gen/proto/go/porterapis/porter/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/manager"
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
			inst := &v1.Installation{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "fake-name",
					Namespace:         "fake-ns",
					Finalizers:        []string{v1.FinalizerName},
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
	inst := &v1.Installation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fake-install",
			Namespace: "fake-ns",
		},
	}
	rec := setupInstallationController(inst)
	err := rec.uninstallInstallation(ctx, rec.Log, inst)
	assert.NoError(t, err)
	gotInstall := &v1.Installation{}
	rec.Get(ctx, types.NamespacedName{Name: "fake-install", Namespace: "fake-ns"}, gotInstall)
	assert.NotEmpty(t, gotInstall.Status)
	assert.Equal(t, v1.PhaseUnknown, gotInstall.Status.Phase)
}

func TestRetry(t *testing.T) {
	ctx := context.Background()
	inst := &v1.Installation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fake-install",
			Namespace: "fake-ns",
		},
	}
	action := &v1.AgentAction{
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
	testdata := &v1.Installation{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, Generation: 1}}

	controller := setupInstallationController(testdata)
	clientConn := &mocks.ClientConn{}
	clientConn.On("Close").Return(nil)
	controller.CreateGRPCClient = func(ctx context.Context) (porterv1alpha1.PorterClient, ClientConn, error) {
		return nil, clientConn, fmt.Errorf("this is not needed for this test")
	}

	var inst v1.Installation
	triggerReconcile := func() {
		fullname := types.NamespacedName{Namespace: namespace, Name: name}
		key := client.ObjectKey{Namespace: namespace, Name: name}

		request := ctrl.Request{
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
	assert.Equal(t, v1.PhaseUnknown, inst.Status.Phase, "New resources should be initialized to Phase: Unknown")

	triggerReconcile()

	// Verify an AgentAction was created and set on the status
	require.NotNil(t, inst.Status.Action, "expected Action to be set")
	var action v1.AgentAction
	require.NoError(t, controller.Get(ctx, client.ObjectKey{Namespace: inst.Namespace, Name: inst.Status.Action.Name}, &action))
	assert.Equal(t, "1", action.Labels[v1.LabelResourceGeneration], "The wrong action is set on the status")

	// Mark the action as scheduled
	action.Status.Phase = v1.PhasePending
	action.Status.Conditions = []metav1.Condition{{Type: string(v1.ConditionScheduled), Status: metav1.ConditionTrue}}
	action.ResourceVersion = ""
	controller = setupInstallationController(testdata, &action)
	assert.NoError(t, controller.Client.Status().Update(ctx, &action))
	triggerReconcile()

	// Verify the installation status was synced with the action
	assert.Equal(t, v1.PhasePending, inst.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(inst.Status.Conditions, string(v1.ConditionScheduled)))

	// Mark the action as started
	action.Status.Phase = v1.PhaseRunning
	action.Status.Conditions = []metav1.Condition{{Type: string(v1.ConditionStarted), Status: metav1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &action))

	triggerReconcile()

	// Verify that the installation status was synced with the action
	assert.Equal(t, v1.PhaseRunning, inst.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(inst.Status.Conditions, string(v1.ConditionStarted)))

	// Complete the action
	action.Status.Phase = v1.PhaseSucceeded
	action.Status.Conditions = []metav1.Condition{{Type: string(v1.ConditionComplete), Status: metav1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &action))

	triggerReconcile()

	// Verify that the installation status was synced with the action
	require.NotNil(t, inst.Status.Action, "expected Action to still be set")
	assert.Equal(t, v1.PhaseSucceeded, inst.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(inst.Status.Conditions, string(v1.ConditionComplete)))

	// Fail the action
	action.Status.Phase = v1.PhaseFailed
	action.Status.Conditions = []metav1.Condition{{Type: string(v1.ConditionFailed), Status: metav1.ConditionTrue}}
	require.NoError(t, controller.Status().Update(ctx, &action))

	triggerReconcile()

	actionName := inst.Status.Action.Name
	// Verify that the installation status shows the action is failed
	require.NotNil(t, inst.Status.Action, "expected Action to still be set")
	assert.Equal(t, v1.PhaseFailed, inst.Status.Phase, "incorrect Phase")
	assert.True(t, apimeta.IsStatusConditionTrue(inst.Status.Conditions, string(v1.ConditionFailed)))

	// Edit the installation spec
	inst.Generation = 2
	require.NoError(t, controller.Update(ctx, &inst))

	triggerReconcile()

	// Verify that the installation status was re-initialized
	assert.Equal(t, int64(2), inst.Status.ObservedGeneration)
	assert.Equal(t, v1.PhaseUnknown, inst.Status.Phase, "New resources should be initialized to Phase: Unknown")
	assert.Empty(t, inst.Status.Conditions, "Conditions should have been reset")

	// Retry the last action
	lastAction := actionName
	inst.Annotations = map[string]string{v1.AnnotationRetry: "retry-1"}
	require.NoError(t, controller.Update(ctx, &inst))

	triggerReconcile()

	// Verify that action has retry set on it now
	require.NotNil(t, inst.Status.Action, "Expected the action to still be set")
	assert.Equal(t, lastAction, actionName, "Expected the action to be the same")
	// get the latest version of the action
	require.NoError(t, controller.Get(ctx, client.ObjectKey{Namespace: inst.Namespace, Name: inst.Status.Action.Name}, &action))
	assert.NotEmpty(t, action.Annotations[v1.AnnotationRetry], "Expected the action to have its retry annotation set")

	assert.Equal(t, int64(2), inst.Status.ObservedGeneration)
	assert.NotEmpty(t, inst.Status.Action, "Expected the action to still be set")
	assert.Equal(t, v1.PhaseUnknown, inst.Status.Phase, "New resources should be initialized to Phase: Unknown")
	assert.Empty(t, inst.Status.Conditions, "Conditions should have been reset")

	// Delete the installation (setting the delete timestamp directly instead of client.Delete because otherwise the fake client just removes it immediately)
	// The fake client doesn't really follow finalizer logic
	// metadata.Timestamp is immutable and not allowed to be set  by the client
	now := metav1.NewTime(time.Now())
	inst.Generation = 3
	inst.DeletionTimestamp = &now
	require.NoError(t, controller.Delete(ctx, &inst))
	//end of the lifecycle
}

func TestInstallationReconciler_createAgentAction(t *testing.T) {
	controller := setupInstallationController()

	inst := &v1.Installation{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1.GroupVersion.String(),
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
				v1.AnnotationRetry: "2021-2-2 12:00:00",
			},
		},
		Spec: v1.InstallationSpec{
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
		APIVersion:         v1.GroupVersion.String(),
		Kind:               "Installation",
		Name:               "myblog",
		UID:                "random-uid",
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}
	assert.Equal(t, wantOwnerRef, action.OwnerReferences[0], "incorrect owner reference")

	assertContains(t, action.Annotations, v1.AnnotationRetry, inst.Annotations[v1.AnnotationRetry], "incorrect annotation")
	assertContains(t, action.Labels, v1.LabelManaged, "true", "incorrect label")
	assertContains(t, action.Labels, v1.LabelResourceKind, "Installation", "incorrect label")
	assertContains(t, action.Labels, v1.LabelResourceName, "myblog", "incorrect label")
	assertContains(t, action.Labels, v1.LabelResourceGeneration, "1", "incorrect label")
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

func TestDeletionTimeStampInstallation(t *testing.T) {
	action := &v1.Installation{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "fake-name",
			Namespace:         "fake-ns",
			DeletionTimestamp: ptr.To(metav1.Now()),
			Finalizers:        []string{v1.FinalizerName},
		},
	}
	ctx := context.Background()
	r := setupInstallationController(action)
	_, err := r.Reconcile(ctx, ctrl.Request{})
	assert.NoError(t, err)
}

func TestCreateInstallationOutputsCR(t *testing.T) {
	ctx := context.Background()
	install := &v1.Installation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fake-install",
			Namespace: "fake-ns",
		},
		Spec: v1.InstallationSpec{
			Name:      "install-name",
			Namespace: "install-ns",
		},
	}
	in := &installationv1.ListInstallationLatestOutputResponse{
		Outputs: []*installationv1.PorterValue{
			{
				Name:      "fake-output",
				Type:      "string",
				Sensitive: false,
				Value:     structpb.NewStringValue("this is an output"),
			},
		},
	}
	tests := map[string]struct {
		wantError bool
		outputs   *installationv1.ListInstallationLatestOutputResponse
	}{
		"success": {wantError: false, outputs: in},
		"failure": {wantError: true, outputs: &installationv1.ListInstallationLatestOutputResponse{}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			rec := setupInstallationController()
			cr, err := rec.CreateInstallationOutputsCR(ctx, install, test.outputs)
			if test.wantError {
				assert.Error(t, err)
			}
			if !test.wantError {
				assert.NoError(t, err)
				assert.IsType(t, &v1.InstallationOutput{}, cr)
			}
		})
	}
}

func TestCreateStatusOutputs(t *testing.T) {
	ctx := context.Background()
	rec := setupInstallationController()
	install := &v1.InstallationOutput{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fake-name",
			Namespace: "fake-ns",
		},
		Spec: v1.InstallationOutputSpec{
			Name:      "fake-porterName",
			Namespace: "fake-porter-namespace",
		},
	}
	in := &installationv1.ListInstallationLatestOutputResponse{
		Outputs: []*installationv1.PorterValue{
			{
				Name:      "fake-output",
				Type:      "string",
				Sensitive: false,
				Value:     structpb.NewStringValue("this is an output"),
			},
		},
	}
	installOut, err := rec.CreateStatusOutputs(ctx, install, in)
	assert.NoError(t, err)
	assert.IsType(t, v1.InstallationOutputStatus{}, installOut.Status)
}

func TestCheckOrCreateInstallationOutputsCR(t *testing.T) {
	ctx := context.Background()
	output := &v1.InstallationOutput{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fake-install",
			Namespace: "fake-ns",
		},
	}
	install := &v1.Installation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fake-install",
			Namespace: "fake-ns",
		},
		Spec: v1.InstallationSpec{
			Name:      "fake-install",
			Namespace: "fake-ns",
		},
	}
	rec := setupInstallationController(output)
	_, err := rec.CheckOrCreateInstallationOutputsCR(ctx, logr.Discard(), install)
	assert.NoError(t, err)
}

func TestCheckOrCreateInstallationOutputsCRCreate(t *testing.T) {
	ctx := context.Background()
	grpcClient := &mocks.PorterClient{}
	outputs := &installationv1.ListInstallationLatestOutputResponse{
		Outputs: []*installationv1.PorterValue{
			{
				Name:      "fake-output",
				Type:      "string",
				Sensitive: false,
				Value:     structpb.NewStringValue("output that is fake"),
			},
		},
	}
	listInstallationRequest := &installationv1.ListInstallationLatestOutputRequest{Name: "fake-install", Namespace: ptr.To("fake-ns")}
	grpcClient.On("ListInstallationLatestOutputs", ctx, listInstallationRequest).Return(outputs, nil)
	rec := setupInstallationController()
	install := &v1.Installation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fake-install",
			Namespace: "fake-ns",
		},
		Spec: v1.InstallationSpec{
			Name:      "fake-install",
			Namespace: "fake-ns",
		},
	}
	clientConn := &mocks.ClientConn{}
	clientConn.On("Close").Return(nil)
	rec.CreateGRPCClient = func(ctx context.Context) (porterv1alpha1.PorterClient, ClientConn, error) {
		return grpcClient, clientConn, nil
	}
	_, err := rec.CheckOrCreateInstallationOutputsCR(ctx, logr.Discard(), install)
	// NOTE: This errors because of the limitation we have with fake in
	// controller-runtime. https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/client/fake
	// There is some support for sub resources which
	// can cause issues with tests if you're trying to update e.g.
	// metadata and status in the same reconcile. We update the status in the
	// same reconcile when creating the object but it can't find it after it
	// creates it. This limitation isn't an issue when running live.
	assert.Error(t, err)
}

func TestCheckOrCreateInstallationOutputsCRCreateFail(t *testing.T) {
	ctx := context.Background()
	grpcClient := &mocks.PorterClient{}
	listInstallationRequest := &installationv1.ListInstallationLatestOutputRequest{Name: "fake-install", Namespace: ptr.To("fake-ns")}
	grpcClient.On("ListInstallationLatestOutputs", ctx, listInstallationRequest).Return(nil, fmt.Errorf("this is an error"))
	rec := setupInstallationController()
	install := &v1.Installation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fake-install",
			Namespace: "fake-ns",
		},
		Spec: v1.InstallationSpec{
			Name:      "fake-install",
			Namespace: "fake-ns",
		},
	}
	_, err := rec.CheckOrCreateInstallationOutputsCR(ctx, logr.Discard(), install)
	// NOTE: This will return nil if the output of the grpc call fails.  We do not
	// want to requeue if this fails.  We will not include outputs of
	// installations that do not have it stored in the grpc server.
	assert.NoError(t, err)
}

func TestSetupWithManager(t *testing.T) {
	r := &InstallationReconciler{}
	scheme := runtime.NewScheme()
	v1.AddToScheme(scheme)
	var restConfig *rest.Config
	mgr, err := manager.New(restConfig, manager.Options{})
	assert.Error(t, err)
	err = r.SetupWithManager(mgr)
	assert.Error(t, err)
}

func setupInstallationController(objs ...client.Object) *InstallationReconciler {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1.AddToScheme(scheme))

	fakeBuilder := fake.NewClientBuilder()
	fakeBuilder.WithScheme(scheme)
	fakeBuilder.WithObjects(objs...).WithStatusSubresource(objs...)
	fakeClient := fakeBuilder.Build()

	clientConn := &mocks.ClientConn{}
	clientConn.On("Close").Return(nil)
	return &InstallationReconciler{
		Log:      logr.Discard(),
		Client:   fakeClient,
		Recorder: record.NewFakeRecorder(42),
		Scheme:   scheme,
		CreateGRPCClient: func(ctx context.Context) (porterv1alpha1.PorterClient, ClientConn, error) {
			return &mocks.PorterClient{}, clientConn, fmt.Errorf("error with grpc")
		},
	}
}

func TestIsHandled(t *testing.T) {
	ctx := context.Background()
	inst := &v1.Installation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fake-install",
			Namespace: "fake-ns",
		},
		Spec: v1.InstallationSpec{
			Name:      "fake-install",
			Namespace: "fake-ns",
		},
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(v1.AddToScheme(scheme))
	fakeBuilder := fake.NewClientBuilder()
	fakeBuilder.WithScheme(scheme)
	fakeBuilder.WithObjects(inst).WithStatusSubresource(inst)
	fakeClient := fakeBuilder.Build()

	client := interceptor.NewClient(fakeClient, interceptor.Funcs{
		List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			return fmt.Errorf("this is an error")
		},
	})
	r := &InstallationReconciler{
		Client: client,
	}

	_, _, err := r.isHandled(ctx, logr.Discard(), inst)
	assert.Error(t, err)
}

func TestApplyDeletionPolicyWithNoAnnotation(t *testing.T) {
	tests := map[string]struct {
		wantPolicy string
		policy     string
	}{
		"empty-string":  {policy: "", wantPolicy: v1.PorterDeletePolicyDelete},
		"policy-orphan": {policy: v1.PorterDeletePolicyOrphan, wantPolicy: v1.PorterDeletePolicyOrphan},
		"policy-delete": {policy: v1.PorterDeletePolicyDelete, wantPolicy: v1.PorterDeletePolicyDelete},
	}
	ctx := context.Background()
	inst := &v1.Installation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fake-install",
			Namespace: "fake-ns",
		},
		Spec: v1.InstallationSpec{
			Name:      "fake-install",
			Namespace: "fake-ns",
		},
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(v1.AddToScheme(scheme))
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(inst).WithStatusSubresource(inst).Build()
	rc := &InstallationReconciler{
		Client: fakeClient,
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := rc.applyDeletionPolicy(ctx, logr.Discard(), inst, test.policy)
			assert.NoError(t, err)
			assert.Equal(t, inst.GetAnnotations()[v1.PorterDeletePolicyAnnotation], test.wantPolicy)
		})
	}
}

func TestApplyDeletionPolicyWithAnnotation(t *testing.T) {
	ctx := context.Background()
	inst := &v1.Installation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fake-install",
			Namespace: "fake-ns",
			Annotations: map[string]string{
				v1.PorterDeletePolicyAnnotation: v1.PorterDeletePolicyDelete,
			},
		},
		Spec: v1.InstallationSpec{
			Name:      "fake-install",
			Namespace: "fake-ns",
		},
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(v1.AddToScheme(scheme))
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(inst).WithStatusSubresource(inst).Build()
	rc := &InstallationReconciler{
		Client: fakeClient,
	}

	tests := map[string]struct {
		policy     string
		wantPolicy string
	}{
		"no-policy":               {policy: "", wantPolicy: v1.PorterDeletePolicyDelete},
		"delete-update-to-orphan": {policy: v1.PorterDeletePolicyOrphan, wantPolicy: v1.PorterDeletePolicyOrphan},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := rc.applyDeletionPolicy(ctx, logr.Discard(), inst, test.policy)
			assert.NoError(t, err)
			assert.Equal(t, inst.GetAnnotations()[v1.PorterDeletePolicyAnnotation], test.wantPolicy)
		})
	}
}
