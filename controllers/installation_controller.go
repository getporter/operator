package controllers

import (
	"context"
	"reflect"

	porterv1 "get.porter.sh/operator/api/v1"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=getporter.org,resources=agentconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=getporter.org,resources=porterconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=getporter.org,resources=installations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=getporter.org,resources=installations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=getporter.org,resources=installations/finalizers,verbs=update;patch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

// SetupWithManager sets up the controller with the Manager.
func (r *InstallationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&porterv1.Installation{}, builder.WithPredicates(resourceChanged{})).
		Owns(&porterv1.AgentAction{}).
		Complete(r)
}

// Reconcile is called when the spec of an installation is changed
// or a job associated with an installation is updated.
// Either schedule a job to handle a spec change, or update the installation status in response to the job's state.
func (r *InstallationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("installation", req.Name, "namespace", req.Namespace)

	// Retrieve the Installation
	inst := &porterv1.Installation{}
	err := r.Get(ctx, req.NamespacedName, inst)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.V(Log5Trace).Info("Reconciliation skipped: Installation CRD or one of its owned resources was deleted.")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	log = log.WithValues("resourceVersion", inst.ResourceVersion, "generation", inst.Generation, "observedGeneration", inst.Status.ObservedGeneration)
	log.V(Log5Trace).Info("Reconciling installation")

	// Check if we have requested an agent run yet
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
		return ctrl.Result{}, nil
	}

	// Should we uninstall the bundle?
	if r.shouldUninstall(inst) {
		err = r.uninstallInstallation(ctx, log, inst)
		log.V(Log4Debug).Info("Reconciliation complete: A porter agent has been dispatched to uninstall the installation.")
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
	return ctrl.Result{}, nil
}

// Determines if this generation of the Installation has being processed by Porter.
func (r *InstallationReconciler) isHandled(ctx context.Context, log logr.Logger, inst *porterv1.Installation) (*porterv1.AgentAction, bool, error) {
	labels := getActionLabels(inst)
	results := porterv1.AgentActionList{}
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
func (r *InstallationReconciler) applyInstallation(ctx context.Context, log logr.Logger, inst *porterv1.Installation) error {
	log.V(Log5Trace).Info("Initializing installation status")
	inst.Status.Initialize()
	if err := r.saveStatus(ctx, log, inst); err != nil {
		return err
	}

	return r.runPorter(ctx, log, inst)
}

// Flag the bundle as uninstalled, and then run the porter agent with the command `porter installation apply`
func (r *InstallationReconciler) uninstallInstallation(ctx context.Context, log logr.Logger, inst *porterv1.Installation) error {
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
func (r *InstallationReconciler) runPorter(ctx context.Context, log logr.Logger, inst *porterv1.Installation) error {
	action, err := r.createAgentAction(ctx, log, inst)
	if err != nil {
		return err
	}

	// Update the Installation Status with the agent action
	return r.syncStatus(ctx, log, inst, action)
}

// create an AgentAction that will trigger running porter
func (r *InstallationReconciler) createAgentAction(ctx context.Context, log logr.Logger, inst *porterv1.Installation) (*porterv1.AgentAction, error) {
	log.V(Log5Trace).Info("Creating porter agent action")

	installationResourceB, err := inst.Spec.ToPorterDocument()
	if err != nil {
		return nil, err
	}

	labels := getActionLabels(inst)
	for k, v := range inst.Labels {
		labels[k] = v
	}

	action := &porterv1.AgentAction{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    inst.Namespace,
			GenerateName: inst.Name + "-",
			Labels:       labels,
			Annotations:  inst.Annotations,
		},
		Spec: porterv1.AgentActionSpec{
			AgentConfig: inst.Spec.AgentConfig,
			Args:        []string{"installation", "apply", "installation.yaml"},
			Files: map[string][]byte{
				"installation.yaml": installationResourceB,
			},
		},
	}

	if err := controllerutil.SetControllerReference(inst, action, r.Scheme); err != nil {
		return nil, errors.Wrap(err, "error attaching owner reference to porter agent action")
	}

	if err := r.Create(ctx, action); err != nil {
		return nil, errors.Wrap(err, "error creating the porter agent action")
	}

	log.V(Log4Debug).Info("Created porter agent action", "name", action.Name)
	return action, nil
}

// Check the status of the porter-agent job and use that to update the AgentAction status
func (r *InstallationReconciler) syncStatus(ctx context.Context, log logr.Logger, inst *porterv1.Installation, action *porterv1.AgentAction) error {
	origStatus := inst.Status

	applyAgentAction(log, inst, action)

	if !reflect.DeepEqual(origStatus, inst.Status) {
		return r.saveStatus(ctx, log, inst)
	}

	return nil
}

// Only update the status with a PATCH, don't clobber the entire installation
func (r *InstallationReconciler) saveStatus(ctx context.Context, log logr.Logger, inst *porterv1.Installation) error {
	log.V(Log5Trace).Info("Patching installation status")
	return PatchStatusWithRetry(ctx, log, r.Client, r.Status().Patch, inst, func() client.Object {
		return &porterv1.Installation{}
	})
}

func (r *InstallationReconciler) shouldUninstall(inst *porterv1.Installation) bool {
	// ignore a deleted CRD with no finalizers
	return isDeleted(inst) && isFinalizerSet(inst)
}

// Sync the retry annotation from the installation to the agent action to trigger another run.
func (r *InstallationReconciler) retry(ctx context.Context, log logr.Logger, inst *porterv1.Installation, action *porterv1.AgentAction) error {
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
