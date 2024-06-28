package controllers

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	v1 "get.porter.sh/operator/api/v1"
	installationv1 "get.porter.sh/porter/gen/proto/go/porterapis/installation/v1alpha1"
	porterv1alpha1 "get.porter.sh/porter/gen/proto/go/porterapis/porter/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	operatorNamespace = "porter-operator-system"
)

// InstallationReconciler calls porter to execute changes made to an Installation CRD
type InstallationReconciler struct {
	client.Client
	Log              logr.Logger
	Recorder         record.EventRecorder
	Scheme           *runtime.Scheme
	CreateGRPCClient func(ctx context.Context) (porterv1alpha1.PorterClient, ClientConn, error)
}

// +kubebuilder:rbac:groups=getporter.org,resources=agentconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=getporter.org,resources=porterconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=getporter.org,resources=installations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=getporter.org,resources=installationoutputs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=getporter.org,resources=installations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=getporter.org,resources=installationoutputs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=getporter.org,resources=installations/finalizers,verbs=update;patch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

// SetupWithManager sets up the controller with the Manager.
func (r *InstallationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Installation{}, builder.WithPredicates(resourceChanged{})).
		Owns(&v1.AgentAction{}).
		Owns(&v1.InstallationOutput{}, builder.MatchEveryOwner).
		Complete(r)
}

// Reconcile is called when the spec of an installation is changed
// or a job associated with an installation is updated.
// Either schedule a job to handle a spec change, or update the installation status in response to the job's state.
func (r *InstallationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("installation", req.Name, "namespace", req.Namespace)

	// Retrieve the Installation
	inst := &v1.Installation{}
	err := r.Get(ctx, req.NamespacedName, inst)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.V(Log5Trace).Info("Reconciliation skipped: Installation CRD or one of its owned resources was deleted.")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if inst.DeletionTimestamp != nil {
		if controllerutil.ContainsFinalizer(inst, v1.FinalizerName) {
			controllerutil.RemoveFinalizer(inst, v1.FinalizerName)
			if err := r.Update(ctx, inst); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	log = log.WithValues("resourceVersion", inst.ResourceVersion, "generation", inst.Generation, "observedGeneration", inst.Status.ObservedGeneration)
	log.V(Log5Trace).Info("Reconciling installation")
	// Check if we have requested an agent run yet
	// TODO Look for annoation/label for outputs generation CR
	// TODO Get installationoutput CR if annotation exists

	action, handled, err := r.isHandled(ctx, log, inst)
	if err != nil {
		return ctrl.Result{}, err
	}

	if action != nil {
		log = log.WithValues("agentaction", action.Name)
	}

	// Sync the installation status from the action
	if err = r.syncStatus(ctx, log, inst, action); err != nil {
		return ctrl.Result{}, err
	}

	// Check if we have finished uninstalling
	if isDeleteProcessed(inst) {
		err = removeFinalizer(ctx, log, r.Client, inst)
		log.V(Log4Debug).Info("Reconciliation complete: Finalizer has been removed from the Installation.")
		return ctrl.Result{}, err
	}

	// Check if we have already handled any spec changes
	if handled {
		// Check if a retry was requested
		if action.GetRetryLabelValue() != inst.GetRetryLabelValue() {
			err = r.retry(ctx, log, inst, action)
			log.V(Log4Debug).Info("Reconciliation complete: The associated porter agent action was retried.")
			return ctrl.Result{}, err
		}

		// Nothing for us to do at this point
		log.V(Log4Debug).Info("Reconciliation complete: A porter agent has already been dispatched.")
		log.V(Log4Debug).Info(fmt.Sprintf("performing installation outputs for %s", inst.Name))
		return r.CheckOrCreateInstallationOutputsCR(ctx, log, inst)
	}

	// Should we uninstall the bundle?
	if r.shouldUninstall(inst) {
		err = r.uninstallInstallation(ctx, log, inst)
		log.V(Log4Debug).Info("Reconciliation complete: A porter agent has been dispatched to uninstall the installation.")
		return ctrl.Result{}, err
	} else if r.shouldOrphan(inst) {
		log.V(Log4Debug).Info("Reconciliation complete: Your installation is being deleted. Please clean up installation resources")
		err = removeFinalizer(ctx, log, r.Client, inst)
		return ctrl.Result{}, err
	} else if isDeleted(inst) {
		// This is installation without a finalizer that was deleted We remove the
		// finalizer after we successfully uninstall (or someone is manually cleaning
		// things up) Just let it go
		log.V(Log4Debug).Info("Reconciliation complete: Installation CRD is ready for deletion.")
		return ctrl.Result{}, nil
	}

	// Ensure non-deleted installations have finalizers
	updated, err := ensureFinalizerSet(ctx, log, r.Client, inst)
	if err != nil {
		return ctrl.Result{}, err
	}
	if updated {
		// if we added a finalizer, stop processing and we will finish when the updated resource is reconciled
		log.V(Log4Debug).Info("Reconciliation complete: A finalizer has been set on the installation.")
		return ctrl.Result{}, nil
	}

	// Use porter to finish reconciling the installation
	err = r.applyInstallation(ctx, log, inst)
	if err != nil {
		return ctrl.Result{}, err
	}

	log.V(Log4Debug).Info("Reconciliation complete: A porter agent has been dispatched to apply changes to the installation.")
	// NOTE:  If this is nil, it will use the default policy of Delete
	err = r.applyDeletionPolicy(ctx, log, inst, inst.GetAnnotations()[v1.PorterDeletePolicyAnnotation])
	if err != nil {
		return ctrl.Result{}, err
	}

	log.V(Log4Debug).Info("Reconciliation complete: A porter agent has been dispatched to apply changes to the installation.")
	log.V(Log4Debug).Info(fmt.Sprintf("performing installation outputs for %s", inst.Name))
	return r.CheckOrCreateInstallationOutputsCR(ctx, log, inst)
}

func (r *InstallationReconciler) CheckOrCreateInstallationOutputsCR(ctx context.Context, log logr.Logger, inst *v1.Installation) (ctrl.Result, error) {
	// NOTE: May not want to requeue if this fails
	if r.CreateGRPCClient == nil {
		log.V(Log4Debug).Info("no grpc client function set on controller")
		r.Recorder.Event(inst, "Warning", "CreateInstallationOutputs", "not creating installation outputs")
		return ctrl.Result{}, nil
	}

	porterGRPCClient, conn, err := r.CreateGRPCClient(ctx)
	if err != nil {
		log.V(Log4Debug).Info("no grpc client... Not performing installation outputs")
		r.Recorder.Event(inst, "Warning", "CreateInstallationOutputs", "no grpc client not creating installation outputs")
		return ctrl.Result{}, nil
	}
	defer conn.Close()
	installCr := &v1.InstallationOutput{}
	err = r.Get(ctx, types.NamespacedName{Name: inst.Spec.Name, Namespace: inst.Spec.Namespace}, installCr)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.V(Log4Debug).Info("installation output cr doesn't exist, seeing if we should create")
			in := &installationv1.ListInstallationLatestOutputRequest{Name: inst.Spec.Name, Namespace: ptr.To(inst.Spec.Namespace)}
			resp, err := porterGRPCClient.ListInstallationLatestOutputs(ctx, in)
			if err != nil {
				log.V(Log4Debug).Info(fmt.Sprintf("failed to get output from grpc server for: %s:%s installation error: %s", inst.Spec.Name, inst.Spec.Namespace, err.Error()))
				// NOTE: Stop installation output cr creation
				r.Recorder.Event(inst, "Warning", "CreatingInstallationOutputs", fmt.Sprintf("created installation outputs failed for %s", inst.Name))
				return ctrl.Result{}, nil
			}
			// TODO: Separate this into it's own func to test and extract what you
			// can
			log.V(Log5Trace).Info("creating installation outputs cr")
			outputs, err := r.CreateInstallationOutputsCR(ctx, inst, resp)
			if err != nil {
				log.V(Log4Debug).Error(err, "error creating installation outputs resource")
				return ctrl.Result{}, err
			}
			// TODO: Wrap in a retry? Try to reduce the errors
			log.V(Log5Trace).Info("setting owner references on outputs cr")
			controllerutil.SetOwnerReference(inst, outputs, r.Scheme)
			err = r.Create(ctx, outputs, &client.CreateOptions{})
			if err != nil {
				return ctrl.Result{}, err
			}
			r.Recorder.Event(inst, "Normal", "CreatingInstallationOutputs", fmt.Sprintf("created installation outputs for %s", inst.Name))
			installOutputs, err := r.CreateStatusOutputs(ctx, outputs, resp)
			if err != nil {
				return ctrl.Result{}, err
			}

			err = r.Status().Update(ctx, installOutputs)
			if err != nil {
				return ctrl.Result{}, err
			}
			log.V(Log5Trace).Info("successfully created outputs cr")
			patchInstall := client.MergeFrom(inst.DeepCopy())
			inst.SetAnnotations(map[string]string{v1.AnnotationInstallationOutput: "true"})
			log.V(Log5Trace).Info("patching installation cr")
			return ctrl.Result{}, r.Patch(ctx, inst, patchInstall)
		}
	}
	patchInstallCR := client.MergeFrom(installCr.DeepCopy())
	return ctrl.Result{}, r.Patch(ctx, installCr, patchInstallCR)
}
func (r *InstallationReconciler) CreateStatusOutputs(ctx context.Context, install *v1.InstallationOutput, in *installationv1.ListInstallationLatestOutputResponse) (*v1.InstallationOutput, error) {
	install.Status = v1.InstallationOutputStatus{
		Phase: v1.PhaseSucceeded,
		Conditions: []metav1.Condition{
			{
				Type:               v1.InstallationOutputSucceeded,
				Status:             metav1.ConditionTrue,
				Reason:             "InstallationOutputCreatedSuccess",
				LastTransitionTime: metav1.NewTime(time.Now()),
				Message:            "outputs custom resource generated succeeded",
			},
		},
	}

	outputs := []v1.Output{}
	outputNames := []string{}
	for _, output := range in.Outputs {
		tmpOutput := v1.Output{
			Name:      output.Name,
			Type:      output.Type,
			Sensitive: output.Sensitive,
			Value:     output.GetValue().GetStringValue(),
		}
		outputNames = append(outputNames, output.Name)
		outputs = append(outputs, tmpOutput)
	}
	install.Status.Outputs = outputs
	install.Status.OutputNames = strings.Join(outputNames, ",")

	return install, nil
}

func (r *InstallationReconciler) CreateInstallationOutputsCR(ctx context.Context, install *v1.Installation, in *installationv1.ListInstallationLatestOutputResponse) (*v1.InstallationOutput, error) {
	if len(in.Outputs) < 1 {
		return nil, fmt.Errorf("no outputs for the installation %s", install.Name)
	}
	installOutputs := &v1.InstallationOutput{
		ObjectMeta: metav1.ObjectMeta{
			Name:      install.Spec.Name,
			Namespace: install.Namespace,
		},
		Spec: v1.InstallationOutputSpec{
			Name:      install.Spec.Name,
			Namespace: install.Spec.Namespace,
		},
	}
	return installOutputs, nil
}

// Determines if this generation of the Installation has being processed by Porter.
func (r *InstallationReconciler) isHandled(ctx context.Context, log logr.Logger, inst *v1.Installation) (*v1.AgentAction, bool, error) {
	labels := getActionLabels(inst)
	results := v1.AgentActionList{}
	err := r.List(ctx, &results, client.InNamespace(inst.Namespace), client.MatchingLabels(labels))
	if err != nil {
		return nil, false, errors.Wrapf(err, "could not query for the current agent action")
	}
	if len(results.Items) == 0 {
		log.V(Log4Debug).Info("No existing agent action was found")
		return nil, false, nil
	}

	action := results.Items[0]
	log.V(Log4Debug).Info("Found existing agent action", "agentaction", action.Name, "namespace", action.Namespace)
	return &action, true, nil
}

// Run the porter agent with the command `porter installation apply`
func (r *InstallationReconciler) applyInstallation(ctx context.Context, log logr.Logger, inst *v1.Installation) error {
	log.V(Log5Trace).Info("Initializing installation status")
	inst.Status.Initialize()
	if err := r.saveStatus(ctx, log, inst); err != nil {
		return err
	}

	return r.runPorter(ctx, log, inst)
}

// Flag the bundle as uninstalled, and then run the porter agent with the command `porter installation apply`
func (r *InstallationReconciler) uninstallInstallation(ctx context.Context, log logr.Logger, inst *v1.Installation) error {
	log.V(Log5Trace).Info("Initializing installation status")
	inst.Status.Initialize()
	if err := r.saveStatus(ctx, log, inst); err != nil {
		return err
	}

	// Mark the document for deletion before giving it to Porter
	log.V(Log5Trace).Info("Setting uninstalled=true to uninstall the bundle")
	inst.Spec.Uninstalled = true

	return r.runPorter(ctx, log, inst)
}

// Trigger an agent
func (r *InstallationReconciler) runPorter(ctx context.Context, log logr.Logger, inst *v1.Installation) error {
	action, err := r.createAgentAction(ctx, log, inst)
	if err != nil {
		return err
	}

	// Update the Installation Status with the agent action
	return r.syncStatus(ctx, log, inst, action)
}

// create an AgentAction that will trigger running porter
func (r *InstallationReconciler) createAgentAction(ctx context.Context, log logr.Logger, inst *v1.Installation) (*v1.AgentAction, error) {
	log.V(Log5Trace).Info("Creating porter agent action")

	installationResourceB, err := inst.Spec.ToPorterDocument()
	if err != nil {
		return nil, err
	}

	labels := getActionLabels(inst)
	for k, v := range inst.Labels {
		labels[k] = v
	}

	action := &v1.AgentAction{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    inst.Namespace,
			GenerateName: inst.Name + "-",
			Labels:       labels,
			Annotations:  inst.Annotations,
		},
		Spec: v1.AgentActionSpec{
			AgentConfig: inst.Spec.AgentConfig,
			Args:        []string{"installation", "apply", "installation.yaml"},
			Files: map[string][]byte{
				"installation.yaml": installationResourceB,
			},
		},
	}
	if err := controllerutil.SetControllerReference(inst, action, r.Scheme); err != nil {
		return nil, err
	}
	if err := r.Create(ctx, action); err != nil {
		return nil, errors.Wrap(err, "error creating the porter agent action")
	}

	r.Recorder.Event(inst, "Normal", "CreateAgentAction", fmt.Sprintf("created installation agent action for %s", inst.Name))
	log.V(Log4Debug).Info("Created porter agent action", "name", action.Name)
	return action, nil
}

// Check the status of the porter-agent job and use that to update the AgentAction status
func (r *InstallationReconciler) syncStatus(ctx context.Context, log logr.Logger, inst *v1.Installation, action *v1.AgentAction) error {
	origStatus := inst.Status

	applyAgentAction(log, inst, action)

	if !reflect.DeepEqual(origStatus, inst.Status) {
		return r.saveStatus(ctx, log, inst)
	}

	return nil
}

// Only update the status with a PATCH, don't clobber the entire installation
func (r *InstallationReconciler) saveStatus(ctx context.Context, log logr.Logger, inst *v1.Installation) error {
	log.V(Log5Trace).Info("Patching installation status")
	return PatchStatusWithRetry(ctx, log, r.Client, r.Status().Patch, inst, func() client.Object {
		return &v1.Installation{}
	})
}

func (r *InstallationReconciler) shouldUninstall(inst *v1.Installation) bool {
	// ignore a deleted CRD with no finalizers
	return isDeleted(inst) && isFinalizerSet(inst) && inst.GetAnnotations()[v1.PorterDeletePolicyAnnotation] == v1.PorterDeletePolicyDelete
}

func (r *InstallationReconciler) shouldOrphan(inst *v1.Installation) bool {
	return isDeleted(inst) && isFinalizerSet(inst) && inst.GetAnnotations()[v1.PorterDeletePolicyAnnotation] == v1.PorterDeletePolicyOrphan
}

// Sync the retry annotation from the installation to the agent action to trigger another run.
func (r *InstallationReconciler) retry(ctx context.Context, log logr.Logger, inst *v1.Installation, action *v1.AgentAction) error {
	log.V(Log5Trace).Info("Initializing installation status")
	inst.Status.Initialize()
	inst.Status.Action = &corev1.LocalObjectReference{Name: action.Name}
	if err := r.saveStatus(ctx, log, inst); err != nil {
		return err
	}

	log.V(Log5Trace).Info("Retrying associated porter agent action")
	retry := inst.GetRetryLabelValue()
	action.SetRetryAnnotation(retry)
	if err := r.Update(ctx, action); err != nil {
		return errors.Wrap(err, "error updating the associated porter agent action")
	}

	log.V(Log4Debug).Info("Retried associated porter agent action", "name", "retry", action.Name, retry)
	return nil
}

func (r *InstallationReconciler) applyDeletionPolicy(ctx context.Context, log logr.Logger, inst *v1.Installation, policy string) error {
	log.V(Log5Trace).Info("updating deletion policy")
	annotations := inst.GetAnnotations()
	if len(annotations) < 1 {
		annotations = map[string]string{}
	}

	if _, ok := annotations[v1.PorterDeletePolicyAnnotation]; !ok {
		annotations[v1.PorterDeletePolicyAnnotation] = v1.PorterDeletePolicyDelete
		inst.SetAnnotations(annotations)
		return r.Update(ctx, inst)
	}

	if strings.ToLower(policy) != v1.PorterDeletePolicyOrphan && strings.ToLower(policy) != v1.PorterDeletePolicyDelete {
		log.V(Log4Debug).Info("this policy doesn't exist: ", policy)
		annotations[v1.PorterDeletePolicyAnnotation] = v1.PorterDeletePolicyDelete
		inst.SetAnnotations(annotations)
		return r.Update(ctx, inst)
	}

	annotations[v1.PorterDeletePolicyAnnotation] = policy
	inst.SetAnnotations(annotations)
	return r.Update(ctx, inst)
}

func CreatePorterGRPCClient(ctx context.Context) (porterv1alpha1.PorterClient, ClientConn, error) {
	// TODO: Make this not a hard coded value of grpc deployment/service.
	// Have a controller create deployment of grpc server and service
	conn, err := grpc.NewClient("porter-grpc-service:3001", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, fmt.Errorf("error setting up listener for porter grpc client")
	}
	if conn != nil {
		return porterv1alpha1.NewPorterClient(conn), conn, nil
	}
	return nil, nil, fmt.Errorf("error creating porter grpc client")
}
