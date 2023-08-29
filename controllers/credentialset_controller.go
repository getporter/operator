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
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	porterv1 "get.porter.sh/operator/api/v1"
)

// CredentialSetReconciler reconciles a CredentialSet object
type CredentialSetReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=getporter.org,resources=credentialsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=getporter.org,resources=credentialsets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=getporter.org,resources=credentialsets/finalizers,verbs=update
//+kubebuilder:rbac:groups=getporter.org,resources=agentconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=getporter.org,resources=porterconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

// SetupWithManager sets up the controller with the Manager.
func (r *CredentialSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&porterv1.CredentialSet{}, builder.WithPredicates(resourceChanged{})).
		Owns(&porterv1.AgentAction{}).
		Complete(r)
}

// Reconcile is called when the spec of a credential set is changed
func (r *CredentialSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("credentialSet", req.Name, "namespace", req.Namespace)

	cs := &porterv1.CredentialSet{}
	err := r.Get(ctx, req.NamespacedName, cs)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.V(Log5Trace).Info("Reconciliation skipped: CredentialSet CRD or one of its owned resources was deleted.")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	log = log.WithValues("resourceVersion", cs.ResourceVersion, "generation", cs.Generation)
	log.V(Log5Trace).Info("Reconciling credential set")

	// Check if we have requested an agent run yet
	action, handled, err := r.isHandled(ctx, log, cs)
	if err != nil {
		return ctrl.Result{}, err
	}

	if action != nil {
		log = log.WithValues("agentaction", action.Name)
	}

	if err = r.syncStatus(ctx, log, cs, action); err != nil {
		return ctrl.Result{}, err
	}

	if isDeleteProcessed(cs) {
		err = removeCredSetFinalizer(ctx, log, r.Client, cs)
		log.V(Log4Debug).Info("Reconciliation complete: Finalizer has been removed from the CredentialSet.")
		return ctrl.Result{}, err
	}

	if handled {
		// Check if retry was requested
		if action.GetRetryLabelValue() != cs.GetRetryLabelValue() {
			err = r.retry(ctx, log, cs, action)
			log.V(Log4Debug).Info("Reconciliation complete: The associated porter agent action was retried.")
			return ctrl.Result{}, err
		}

		//Nothing to do
		log.V(Log4Debug).Info("Reconciliation complete: A porter agent has already been dispatched.")
		return ctrl.Result{}, nil
	}

	if r.shouldDelete(cs) {
		err = r.runCredentialSet(ctx, log, cs)
		log.V(Log4Debug).Info("Reconciliation complete: A porter agent has been dispatched to delete the credential set")
		return ctrl.Result{}, err

	} else if isDeleted(cs) {
		log.V(Log4Debug).Info("Reconciliation complete: CredentialSet CRD is ready for deletion.")
		return ctrl.Result{}, nil
	}

	// ensure non-deleted credential sets have finalizers
	updated, err := ensureFinalizerSet(ctx, log, r.Client, cs)
	if err != nil {
		return ctrl.Result{}, err
	}
	if updated {
		// if we added a finalizer, stop processing and we will finish when the updated resource is reconciled
		log.V(Log4Debug).Info("Reconciliation complete: A finalizer has been set on the credential set.")
		return ctrl.Result{}, nil
	}
	err = r.runCredentialSet(ctx, log, cs)
	if err != nil {
		return ctrl.Result{}, err
	}
	log.V(Log4Debug).Info("Reconciliation complete: A porter agent has been dispatched to apply changes to the credential set.")
	return ctrl.Result{}, nil
}

// isHandled determines if this generation of the credential set resource has been processed by Porter
func (r *CredentialSetReconciler) isHandled(ctx context.Context, log logr.Logger, cs *porterv1.CredentialSet) (*porterv1.AgentAction, bool, error) {
	labels := getActionLabels(cs)
	results := porterv1.AgentActionList{}
	err := r.List(ctx, &results, client.InNamespace(cs.Namespace), client.MatchingLabels(labels))
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

// Check the status of the porter-agent job and use that to update the AgentAction status
func (r *CredentialSetReconciler) syncStatus(ctx context.Context, log logr.Logger, cs *porterv1.CredentialSet, action *porterv1.AgentAction) error {
	origStatus := cs.Status

	applyAgentAction(log, cs, action)

	if !reflect.DeepEqual(origStatus, cs.Status) {
		return r.saveStatus(ctx, log, cs)
	}

	return nil
}

// Only update the status with a PATCH, don't clobber the entire installation
func (r *CredentialSetReconciler) saveStatus(ctx context.Context, log logr.Logger, cs *porterv1.CredentialSet) error {
	log.V(Log5Trace).Info("Patching credential set status")
	return PatchStatusWithRetry(ctx, log, r.Client, r.Status().Patch, cs, func() client.Object {
		return &porterv1.CredentialSet{}
	})
}

func (r *CredentialSetReconciler) shouldDelete(cs *porterv1.CredentialSet) bool {
	// ignore a deleted CRD with no finalizers
	return isDeleted(cs) && isFinalizerSet(cs)
}

func (r *CredentialSetReconciler) runCredentialSet(ctx context.Context, log logr.Logger, cs *porterv1.CredentialSet) error {
	log.V(Log5Trace).Info("Initializing credential set status")
	cs.Status.Initialize()
	if err := r.saveStatus(ctx, log, cs); err != nil {
		return err
	}

	return r.runPorter(ctx, log, cs)
}

// This could be the main "runFunction for each controller"
// Trigger an agent
func (r *CredentialSetReconciler) runPorter(ctx context.Context, log logr.Logger, cs *porterv1.CredentialSet) error {
	action, err := r.createAgentAction(ctx, log, cs)
	if err != nil {
		return err
	}

	// Update the CredentialSet Status with the agent action
	return r.syncStatus(ctx, log, cs, action)
}

func newAgentAction(cs *porterv1.CredentialSet) *porterv1.AgentAction {
	labels := getActionLabels(cs)
	for k, v := range cs.Labels {
		labels[k] = v
	}

	action := &porterv1.AgentAction{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    cs.Namespace,
			GenerateName: cs.Name + "-",
			Labels:       labels,
			Annotations:  cs.Annotations,
			OwnerReferences: []metav1.OwnerReference{
				{ // I'm not using controllerutil.SetControllerReference because I can't track down why that throws a panic when running our tests
					APIVersion:         cs.APIVersion,
					Kind:               cs.Kind,
					Name:               cs.Name,
					UID:                cs.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		},
		Spec: porterv1.AgentActionSpec{
			AgentConfig: cs.Spec.AgentConfig,
		},
	}
	return action
}

// create a porter credentials AgentAction for applying or deleting credential sets
func (r *CredentialSetReconciler) createAgentAction(ctx context.Context, log logr.Logger, cs *porterv1.CredentialSet) (*porterv1.AgentAction, error) {
	credSetResourceB, err := cs.Spec.ToPorterDocument()
	if err != nil {
		return nil, err
	}

	action := newAgentAction(cs)

	log.WithValues("action name", action.Name)
	if r.shouldDelete(cs) {
		log.V(Log5Trace).Info("Deleting porter credential set")
		action.Spec.Args = []string{"credentials", "delete", "-n", cs.Spec.Namespace, cs.Spec.Name}
	} else {
		log.V(Log5Trace).Info(fmt.Sprintf("Creating porter credential set %s", action.Name))
		action.Spec.Args = []string{"credentials", "apply", "credentials.yaml"}
		action.Spec.Files = map[string][]byte{"credentials.yaml": credSetResourceB}
	}

	if err := r.Create(ctx, action); err != nil {
		return nil, errors.Wrap(err, "error creating the porter credential set agent action")
	}

	log.V(Log4Debug).Info("Created porter credential set agent action")
	return action, nil
}

// Sync the retry annotation from the credential set to the agent action to trigger another run.
func (r *CredentialSetReconciler) retry(ctx context.Context, log logr.Logger, cs *porterv1.CredentialSet, action *porterv1.AgentAction) error {
	log.V(Log5Trace).Info("Initializing installation status")
	cs.Status.Initialize()
	cs.Status.Action = &corev1.LocalObjectReference{Name: action.Name}
	if err := r.saveStatus(ctx, log, cs); err != nil {
		return err
	}

	log.V(Log5Trace).Info("Retrying associated porter agent action")
	retry := cs.GetRetryLabelValue()
	action.SetRetryAnnotation(retry)
	if err := r.Update(ctx, action); err != nil {
		return errors.Wrap(err, "error updating the associated porter agent action")
	}

	log.V(Log4Debug).Info("Retried associated porter agent action", "name", "retry", action.Name, retry)
	return nil
}

// removeFinalizer deletes the porter finalizer from the specified resource and saves it.
func removeCredSetFinalizer(ctx context.Context, log logr.Logger, client client.Client, cs *porterv1.CredentialSet) error {
	log.V(Log5Trace).Info("removing finalizer")
	controllerutil.RemoveFinalizer(cs, porterv1.FinalizerName)
	return client.Update(ctx, cs)
}
