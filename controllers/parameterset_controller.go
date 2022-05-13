package controllers

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	porterv1 "get.porter.sh/operator/api/v1"
)

// ParameterSetReconciler reconciles a ParameterSet object
type ParameterSetReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=porter.sh,resources=parametersets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=porter.sh,resources=parametersets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=porter.sh,resources=parametersets/finalizers,verbs=update
//+kubebuilder:rbac:groups=porter.sh,resources=agentconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=porter.sh,resources=porterconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

// SetupWithManager sets up the controller with the Manager.
func (r *ParameterSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&porterv1.ParameterSet{}, builder.WithPredicates(resourceChanged{})).
		Owns(&porterv1.AgentAction{}).
		Complete(r)
}

func (r *ParameterSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	log := r.Log.WithValues("parameterSet", req.Name, "namespace", req.Namespace)

	ps := &porterv1.ParameterSet{}
	err := r.Get(ctx, req.NamespacedName, ps)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.V(Log5Trace).Info("Reconciliation skipped: ParameterSet CRD or one of its owned resources was deleted.")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	log = log.WithValues("resourceVersion", ps.ResourceVersion, "generation", ps.Generation)
	log.V(Log5Trace).Info("Reconciling parameter set")

	// Check if we have requested an agent run yet
	action, handled, err := r.isHandled(ctx, log, ps)
	if err != nil {
		return ctrl.Result{}, err
	}

	if action != nil {
		log = log.WithValues("agentaction", action.Name)
	}

	if err = r.syncStatus(ctx, log, ps, action); err != nil {
		return ctrl.Result{}, err
	}

	if isDeleteProcessed(ps) {
		err = removeParamSetFinalizer(ctx, log, r.Client, ps)
		log.V(Log4Debug).Info("Reconciliation complete: Finalizer has been removed from the ParameterSet.")
		return ctrl.Result{}, err
	}

	if handled {
		// Check if retry was requested
		if action.GetRetryLabelValue() != ps.GetRetryLabelValue() {
			err = r.retry(ctx, log, ps, action)
			log.V(Log4Debug).Info("Reconciliation complete: The associated porter agent action was retried.")
			return ctrl.Result{}, err
		}

		//Nothing to do
		log.V(Log4Debug).Info("Reconciliation complete: A porter agent has already been dispatched.")
		return ctrl.Result{}, nil
	}

	if r.shouldDelete(ps) {
		err = r.runParameterSet(ctx, log, ps)
		log.V(Log4Debug).Info("Reconciliation complete: A porter agent has been dispatched to delete the parameter set")
		return ctrl.Result{}, err

	} else if isDeleted(ps) {
		log.V(Log4Debug).Info("Reconciliation complete: ParameterSet CRD is ready for deletion.")
		return ctrl.Result{}, nil
	}

	// ensure non-deleted parameter sets have finalizers
	updated, err := ensureFinalizerSet(ctx, log, r.Client, ps)
	if err != nil {
		return ctrl.Result{}, err
	}
	if updated {
		// if we added a finalizer, stop processing and we will finish when the updated resource is reconciled
		log.V(Log4Debug).Info("Reconciliation complete: A finalizer has been set on the parameter set.")
		return ctrl.Result{}, nil
	}
	err = r.runParameterSet(ctx, log, ps)
	if err != nil {
		return ctrl.Result{}, err
	}
	log.V(Log4Debug).Info("Reconciliation complete: A porter agent has been dispatched to apply changes to the parameter set.")
	return ctrl.Result{}, nil
}

// isHandled determines if this generation of the parameter set resource has been processed by Porter
func (r *ParameterSetReconciler) isHandled(ctx context.Context, log logr.Logger, ps *porterv1.ParameterSet) (*porterv1.AgentAction, bool, error) {
	labels := getActionLabels(ps)
	results := porterv1.AgentActionList{}
	err := r.List(ctx, &results, client.InNamespace(ps.Namespace), client.MatchingLabels(labels))
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

//Check the status of the porter-agent job and use that to update the AgentAction status
func (r *ParameterSetReconciler) syncStatus(ctx context.Context, log logr.Logger, ps *porterv1.ParameterSet, action *porterv1.AgentAction) error {
	origStatus := ps.Status

	applyAgentAction(log, ps, action)

	if !reflect.DeepEqual(origStatus, ps.Status) {
		return r.saveStatus(ctx, log, ps)
	}

	return nil
}

func (r *ParameterSetReconciler) runParameterSet(ctx context.Context, log logr.Logger, ps *porterv1.ParameterSet) error {
	log.V(Log5Trace).Info("Initializing parameter set status")
	ps.Status.Initialize()
	if err := r.saveStatus(ctx, log, ps); err != nil {
		return err
	}

	return r.runPorter(ctx, log, ps)
}

// This could be the main "runFunction for each controller"
// Trigger an agent
func (r *ParameterSetReconciler) runPorter(ctx context.Context, log logr.Logger, ps *porterv1.ParameterSet) error {
	action, err := r.createAgentAction(ctx, log, ps)
	if err != nil {
		return err
	}

	// Update the ParameterSet Status with the agent action
	return r.syncStatus(ctx, log, ps, action)
}

// Only update the status with a PATCH, don't clobber the entire installation
func (r *ParameterSetReconciler) saveStatus(ctx context.Context, log logr.Logger, ps *porterv1.ParameterSet) error {
	log.V(Log5Trace).Info("Patching parameter set status")
	return PatchObjectWithRetry(ctx, log, r.Client, r.Client.Status().Patch, ps, func() client.Object {
		return &porterv1.ParameterSet{}
	})
}

// create a porter parameters AgentAction for applying or deleting parameter sets
func (r *ParameterSetReconciler) createAgentAction(ctx context.Context, log logr.Logger, ps *porterv1.ParameterSet) (*porterv1.AgentAction, error) {
	paramSetResourceB, err := ps.Spec.ToPorterDocument()
	if err != nil {
		return nil, err
	}

	action := newPSAgentAction(ps)
	log.WithValues("action name", action.Name)
	if r.shouldDelete(ps) {
		log.V(Log5Trace).Info("Deleting porter parameter set")
		action.Spec.Args = []string{"parameters", "delete", "-n", ps.Spec.Namespace, ps.Spec.Name}
	} else {
		log.V(Log5Trace).Info(fmt.Sprintf("Creating porter parameter set %s", action.Name))
		action.Spec.Args = []string{"parameters", "apply", "parameters.yaml"}
		action.Spec.Files = map[string][]byte{"parameters.yaml": paramSetResourceB}
	}

	if err := r.Create(ctx, action); err != nil {
		return nil, errors.Wrap(err, "error creating the porter parameter set agent action")
	}

	log.V(Log4Debug).Info("Created porter parameter set agent action")
	return action, nil
}

func (r *ParameterSetReconciler) shouldDelete(ps *porterv1.ParameterSet) bool {
	// ignore a deleted CRD with no finalizers
	return isDeleted(ps) && isFinalizerSet(ps)
}

// Sync the retry annotation from the parameter set to the agent action to trigger another run.
func (r *ParameterSetReconciler) retry(ctx context.Context, log logr.Logger, ps *porterv1.ParameterSet, action *porterv1.AgentAction) error {
	log.V(Log5Trace).Info("Initializing installation status")
	ps.Status.Initialize()
	ps.Status.Action = &corev1.LocalObjectReference{Name: action.Name}
	if err := r.saveStatus(ctx, log, ps); err != nil {
		return err
	}

	log.V(Log5Trace).Info("Retrying associated porter agent action")
	retry := ps.GetRetryLabelValue()
	action.SetRetryAnnotation(retry)
	if err := r.Update(ctx, action); err != nil {
		return errors.Wrap(err, "error updating the associated porter agent action")
	}

	log.V(Log4Debug).Info("Retried associated porter agent action", "name", "retry", action.Name, retry)
	return nil
}

// removeFinalizer deletes the porter finalizer from the specified resource and saves it.
func removeParamSetFinalizer(ctx context.Context, log logr.Logger, client client.Client, ps *porterv1.ParameterSet) error {
	log.V(Log5Trace).Info("removing finalizer")
	controllerutil.RemoveFinalizer(ps, porterv1.FinalizerName)
	return client.Update(ctx, ps)
}

func newPSAgentAction(ps *porterv1.ParameterSet) *porterv1.AgentAction {
	labels := getActionLabels(ps)
	for k, v := range ps.Labels {
		labels[k] = v
	}

	action := &porterv1.AgentAction{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    ps.Namespace,
			GenerateName: ps.Name + "-",
			Labels:       labels,
			Annotations:  ps.Annotations,
			OwnerReferences: []metav1.OwnerReference{
				{ // I'm not using controllerutil.SetControllerReference because I can't track down why that throws a panic when running our tests
					APIVersion:         ps.APIVersion,
					Kind:               ps.Kind,
					Name:               ps.Name,
					UID:                ps.UID,
					Controller:         pointer.BoolPtr(true),
					BlockOwnerDeletion: pointer.BoolPtr(true),
				},
			},
		},
		Spec: porterv1.AgentActionSpec{
			AgentConfig:  ps.Spec.AgentConfig,
			PorterConfig: ps.Spec.PorterConfig,
		},
	}
	return action
}
