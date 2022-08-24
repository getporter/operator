package controllers

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"reflect"

	porterv1 "get.porter.sh/operator/api/v1"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// AgentConfigReconciler calls porter to execute changes made to an AgentConfig CRD
type AgentConfigReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

type runPorter func(ctx context.Context, log logr.Logger, pvc *corev1.PersistentVolumeClaim, agentCfg *porterv1.AgentConfig) error

//+kubebuilder:rbac:groups=porter.sh,resources=agentconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=porter.sh,resources=agentconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=porter.sh,resources=agentconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=porter.sh,resources=porterconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=persistentvolumes,verbs=get;list;watch;create;update;patch;delete

// SetupWithManager sets up the controller with the Manager.
func (r *AgentConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&porterv1.AgentConfig{}, builder.WithPredicates(resourceChanged{})).
		Owns(&porterv1.AgentAction{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Complete(r)
}

// Reconcile is called when the spec of an agent config is changed
// or a job associated with an agent config is updated.
// Either schedule a job to handle a spec change, or update the agent config status in response to the job's state.
func (r *AgentConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("agent config", req.Name, "namespace", req.Namespace)

	// Retrieve the agent config
	agentCfg := &porterv1.AgentConfig{}
	err := r.Get(ctx, req.NamespacedName, agentCfg)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.V(Log5Trace).Info("Reconciliation skipped: AgentConfig CRD or one of its owned resources was deleted.")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	log = log.WithValues("resourceVersion", agentCfg.ResourceVersion, "generation", agentCfg.Generation, "observedGeneration", agentCfg.Status.ObservedGeneration)
	log.V(Log5Trace).Info("Reconciling agent config")

	// Check if we have requested an agent run yet
	action, handled, err := r.isHandled(ctx, log, agentCfg)
	if err != nil {
		return ctrl.Result{}, err
	}

	if action != nil {
		log = log.WithValues("agentaction", action.Name)
	}

	// Sync the agent config status from the action
	if err = r.syncStatus(ctx, log, agentCfg, action); err != nil {
		return ctrl.Result{}, err
	}

	// Check if we have finished deleting the pvc and pv created by this agentCfg resource
	if r.isDeleteProcessed(ctx, agentCfg) {
		err = removeAgentCfgFinalizer(ctx, log, r.Client, agentCfg)
		log.V(Log4Debug).Info("Reconciliation complete: Finalizer has been removed from the AgentConfig.")
		return ctrl.Result{}, err
	}

	// if there's a volumn ready with the same set of plugins installed, no need to create new ones
	var pvcExists bool
	hashPVC := getPluginHash(agentCfg)
	key := client.ObjectKey{Namespace: agentCfg.Namespace, Name: hashPVC}
	newPVC := &corev1.PersistentVolumeClaim{}
	err = r.Get(ctx, key, newPVC)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	if err == nil {
		if newPVC.Status.Phase != corev1.ClaimBound {
			// wait for the pvc to be bounded
			log.V(Log4Debug).Info("Plugin persistent volume claim waiting to be bounded.", "persistentvolumeclaim", newPVC.Name, "namespace", newPVC.Namespace)
			return ctrl.Result{}, nil
		}
		log.V(Log4Debug).Info("Plugin persistent volume claim already exists.", "persistentvolumeclaim", newPVC.Name, "namespace", newPVC.Namespace)
		pvcExists = true
	}

	// Check if we have already handled any spec changes
	if handled {
		// Check if a retry was requested
		if action.GetRetryLabelValue() != agentCfg.GetRetryLabelValue() {
			err = r.retry(ctx, log, agentCfg, action)
			log.V(Log4Debug).Info("Reconciliation complete: The associated porter agent action was retried.")
			return ctrl.Result{}, err
		}

		// check if it's a tmp pvc action or the hash pvc is ready and we should delete the tmp pvc

		// if the volume is not ready, there's nothing we can do
		if len(action.Spec.Volumes) < 1 {
			log.V(Log4Debug).Info("Volumn is not ready yet.", "persistentvolumeclaim", newPVC.Name, "namespace", newPVC.Namespace)
			return ctrl.Result{}, nil
		}
		// check if we can find the hash pvc that's bounded, if so, we can delete the tmp pvc
		var tmpPVCName string
		var shouldDeleteTmpPVC bool
		if pvcExists {
			oldPVC, ok := agentCfg.Annotations["tmpPVC"]
			if !ok {
				// TODO: this should not be possible, should we retry?
				return ctrl.Result{}, nil
			}

			shouldDeleteTmpPVC = true
			tmpPVCName = oldPVC
		} else {
			// the temporary pvc is created
			if action.Status.Phase == porterv1.PhaseSucceeded {
				// ToDo: i assume this means the porter plugin install is finished by now
				tmpPVCName = action.Spec.Volumes[0].PersistentVolumeClaim.ClaimName
			}
		}

		log.V(Log4Debug).Info("Temporary persistent volume claim found", "persistentvolumeclaim", tmpPVCName, "namespace", action.Namespace)
		// rename the pvc to the hash of plugins metadata
		key := client.ObjectKey{Namespace: agentCfg.Namespace, Name: tmpPVCName}
		tmpPVC := &corev1.PersistentVolumeClaim{}
		err = r.Get(ctx, key, tmpPVC)
		if err != nil {
			// Todo: awkward that it's not found anymore. Should we mark the action to be finished here?
			if apierrors.IsNotFound(err) {
				log.V(Log4Debug).Info("Temporary persistent volume claim already deleted", "persistentvolumeclaim", tmpPVCName, "namespace", action.Namespace)
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}

		if tmpPVC.Status.Phase != corev1.ClaimBound && !pvcExists {
			// wait for it to be bounded
			log.V(Log4Debug).Info("Temporary persistent volume claim waiting to be bounded.", "persistentvolumeclaim", tmpPVC.Name, "namespace", tmpPVC.Namespace)
			return ctrl.Result{}, nil
		}

		if shouldDeleteTmpPVC {
			log.V(Log4Debug).Info("Deleting temporary persistent volume claim.", "persistentvolumeclaim", tmpPVC.Name, "namespace", tmpPVC.Namespace)
			if err := r.Delete(ctx, tmpPVC); err != nil {
				return ctrl.Result{}, err
			}
			log.V(Log4Debug).Info("Deleted temporary persistent volume claim.", "persistentvolumeclaim", tmpPVC.Name, "namespace", tmpPVC.Namespace)
			return ctrl.Result{}, nil
		}

		_, err = r.renamePVC(ctx, log, tmpPVC, hashPVC)
		if err != nil {
			return ctrl.Result{}, err
		}

		log.V(Log4Debug).Info("Renamed temporary persistent volume claim.", "old persistentvolumeclaim", tmpPVC.Name, "new persistentvolumeclaim", hashPVC, "namespace", tmpPVC.Namespace)
		if agentCfg.Annotations == nil {
			agentCfg.Annotations = make(map[string]string)
		}
		agentCfg.Annotations["tmpPVC"] = tmpPVC.Name
		if err := r.Update(ctx, agentCfg); err != nil {
			return ctrl.Result{}, err
		}
		log.V(Log4Debug).Info("Renamed temporary persistent volume claim.", "old persistentvolumeclaim", tmpPVC.Name, "new persistentvolumeclaim", hashPVC, "namespace", tmpPVC.Namespace)

		return ctrl.Result{}, nil
	}

	if r.shouldDelete(agentCfg) {
		// delete the pvc and pc created by this agent config
		err = r.cleanup(ctx, log, agentCfg)
		log.V(Log4Debug).Info("Reconciliation complete: A porter agent has been dispatched to delete the credential set")
		return ctrl.Result{}, err
	} else if isDeleted(agentCfg) {
		log.V(Log4Debug).Info("Reconciliation complete: AgentConfig CRD is ready for deletion.")
		return ctrl.Result{}, nil

	}

	// ensure non-deleted credential sets have finalizers
	updated, err := ensureFinalizerSet(ctx, log, r.Client, agentCfg)
	if err != nil {
		return ctrl.Result{}, err
	}
	if updated {
		// if we added a finalizer, stop processing and we will finish when the updated resource is reconciled
		log.V(Log4Debug).Info("Reconciliation complete: A finalizer has been set on the agent config.")
		return ctrl.Result{}, nil
	}

	pvc, err := r.createAgentVolumeWithPlugins(ctx, log, agentCfg)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Use porter to finish reconciling the agent config
	err = r.applyAgentConfig(ctx, log, pvc, agentCfg, r.runPorterPluginInstall)
	if err != nil {
		return ctrl.Result{}, err
	}

	log.V(Log4Debug).Info("Reconciliation complete: A porter agent has been dispatched to apply changes to the agent config.")
	return ctrl.Result{}, nil
}

// Determines if this AgentConfig has been handled by Porter
func (r *AgentConfigReconciler) isHandled(ctx context.Context, log logr.Logger, agentCfg *porterv1.AgentConfig) (*porterv1.AgentAction, bool, error) {
	labels := getActionLabels(agentCfg)
	results := porterv1.AgentActionList{}
	err := r.List(ctx, &results, client.InNamespace(agentCfg.Namespace), client.MatchingLabels(labels))
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

// Run the porter agent with the command `porter agent config apply`
func (r *AgentConfigReconciler) applyAgentConfig(ctx context.Context, log logr.Logger, pvc *corev1.PersistentVolumeClaim, agentCfg *porterv1.AgentConfig, f runPorter) error {
	log.V(Log5Trace).Info("Initializing agent config status")
	agentCfg.Status.Initialize()
	if err := r.saveStatus(ctx, log, agentCfg); err != nil {
		return err
	}

	return f(ctx, log, pvc, agentCfg)
}

func (r *AgentConfigReconciler) createAgentVolumeWithPlugins(ctx context.Context, log logr.Logger, agentCfg *porterv1.AgentConfig) (*corev1.PersistentVolumeClaim, error) {

	labels := getActionLabels(agentCfg)
	pluginHash := getPluginHash(agentCfg)
	var results corev1.PersistentVolumeClaim
	key := client.ObjectKey{Namespace: agentCfg.Namespace, Name: pluginHash}
	err := r.Get(ctx, key, &results)
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, errors.Wrap(err, "error checking for an existing agent volume (pvc)")
	}

	if results.Name != "" {
		return &results, nil
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: agentCfg.Name + "-",
			Namespace:    agentCfg.Namespace,
			Labels:       labels,
			// this should be the plugin name as the key, url feed and version as the value
			Annotations: map[string]string{},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.ResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: agentCfg.Spec.GetVolumeSize(),
				},
			},
		},
	}

	if err := r.Create(ctx, pvc); err != nil {
		return nil, errors.Wrap(err, "error creating the agent volume (pvc)")
	}

	log.V(Log4Debug).Info("Created PersistentVolumeClaim for the Porter agent", "name", pvc.Name)
	return pvc, nil
}

// Trigger an agent
func (r *AgentConfigReconciler) runPorterPluginInstall(ctx context.Context, log logr.Logger, pvc *corev1.PersistentVolumeClaim, agentCfg *porterv1.AgentConfig) error {
	if len(agentCfg.Spec.Plugins) == 0 {
		return nil
	}
	installCmd := []string{"plugins", "install"}
	for _, p := range agentCfg.Spec.Plugins {
		installCmd = append(installCmd, p.Name)
		if p.FeedURL != "" {
			installCmd = append(installCmd, "--feed-url", p.FeedURL)
		}
		if p.Version != "" {
			installCmd = append(installCmd, "--version", p.Version)
		}
	}
	action, err := r.createAgentAction(ctx, log, pvc, agentCfg, installCmd)
	if err != nil {
		return err
	}

	// Update the agent config Status with the agent action
	return r.syncStatus(ctx, log, agentCfg, action)
}

// create an AgentAction that will trigger running porter
func (r *AgentConfigReconciler) createAgentAction(ctx context.Context, log logr.Logger, pvc *corev1.PersistentVolumeClaim, agentCfg *porterv1.AgentConfig, args []string) (*porterv1.AgentAction, error) {
	log.V(Log5Trace).Info("Creating porter agent action")

	labels := getActionLabels(agentCfg)
	for k, v := range agentCfg.Labels {
		labels[k] = v
	}

	volumn, volumnMount := getPluginVolumn(pvc)
	agentCfgName := &corev1.LocalObjectReference{Name: agentCfg.Name}

	action := &porterv1.AgentAction{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    agentCfg.Namespace,
			GenerateName: agentCfg.Name + "-",
			Labels:       labels,
			Annotations:  agentCfg.Annotations,
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
		Spec: porterv1.AgentActionSpec{
			AgentConfig:  agentCfgName,
			Args:         args,
			Volumes:      []v1.Volume{volumn},
			VolumeMounts: []v1.VolumeMount{volumnMount},
		},
	}

	if err := r.Create(ctx, action); err != nil {
		return nil, errors.Wrap(err, "error creating the porter agent action")
	}

	log.V(Log4Debug).Info("Created porter agent action", "name", action.Name)
	return action, nil
}

// Check the status of the porter-agent job and use that to update the AgentAction status
func (r *AgentConfigReconciler) syncStatus(ctx context.Context, log logr.Logger, agentCfg *porterv1.AgentConfig, action *porterv1.AgentAction) error {
	origStatus := agentCfg.Status

	applyAgentAction(log, agentCfg, action)

	if !reflect.DeepEqual(origStatus, agentCfg.Status) {
		return r.saveStatus(ctx, log, agentCfg)
	}

	return nil
}

// Only update the status with a PATCH, don't clobber the entire agent config
func (r *AgentConfigReconciler) saveStatus(ctx context.Context, log logr.Logger, agentCfg *porterv1.AgentConfig) error {
	log.V(Log5Trace).Info("Patching agent config status")
	return PatchObjectWithRetry(ctx, log, r.Client, r.Client.Status().Patch, agentCfg, func() client.Object {
		return &porterv1.AgentConfig{}
	})
}

// Sync the retry annotation from the agent config to the agent action to trigger another run.
func (r *AgentConfigReconciler) retry(ctx context.Context, log logr.Logger, agentCfg *porterv1.AgentConfig, action *porterv1.AgentAction) error {
	log.V(Log5Trace).Info("Initializing agent config status")
	agentCfg.Status.Initialize()
	agentCfg.Status.Action = &corev1.LocalObjectReference{Name: action.Name}
	if err := r.saveStatus(ctx, log, agentCfg); err != nil {
		return err
	}
	log.V(Log5Trace).Info("Retrying associated porter agent action")
	retry := agentCfg.GetRetryLabelValue()
	action.SetRetryAnnotation(retry)
	if err := r.Update(ctx, action); err != nil {
		return errors.Wrap(err, "error updating the associated porter agent action")
	}

	log.V(Log4Debug).Info("Retried associated porter agent action", "name", "retry", action.Name, retry)
	return nil
}

func getPluginHash(agentCfg *porterv1.AgentConfig) string {
	var input []byte

	for _, p := range agentCfg.Spec.Plugins {
		input = append(input, []byte(p.Name+p.FeedURL+p.Version)...)
	}
	pluginHash := md5.Sum(input)
	return hex.EncodeToString(pluginHash[:])
}

func getPluginVolumn(pvc *corev1.PersistentVolumeClaim) (corev1.Volume, corev1.VolumeMount) {
	volume := corev1.Volume{
		Name: porterv1.VolumePorterPluginsName,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: pvc.Name,
			},
		},
	}

	volumeMount := corev1.VolumeMount{
		Name:      porterv1.VolumePorterPluginsName,
		MountPath: porterv1.VolumePorterPluginsPath,
	}

	return volume, volumeMount
}

// rename the oldPvc to newName
func (r *AgentConfigReconciler) renamePVC(
	ctx context.Context,
	log logr.Logger,
	oldPvc *corev1.PersistentVolumeClaim,
	new string,
) (*corev1.PersistentVolumeClaim, error) {
	log.V(Log4Debug).Info("Renaming temporary persistent volume claim.", "old persistentvolumeclaim", oldPvc.Name, "new persistentvolumeclaim", new, "namespace", oldPvc.Namespace)
	// get new pvc with old PVC inputs
	newPvc := oldPvc.DeepCopy()
	newPvc.Status = corev1.PersistentVolumeClaimStatus{}
	newPvc.Name = new
	newPvc.UID = ""
	newPvc.CreationTimestamp = metav1.Now()
	newPvc.SelfLink = "" // nolint: staticcheck // to keep compatibility with older versions
	newPvc.ResourceVersion = ""

	// get the persistent volumn created with the temporary pvc
	pv := &corev1.PersistentVolume{}
	key := client.ObjectKey{Namespace: oldPvc.Namespace, Name: oldPvc.Spec.VolumeName}
	err := r.Get(ctx, key, pv)
	if err != nil {
		return nil, err
	}

	err = r.Create(ctx, newPvc)
	if err != nil {
		return nil, fmt.Errorf("failed to rename pvc: %w", err)
	}

	pv.Spec.ClaimRef = &corev1.ObjectReference{
		Kind:            newPvc.Kind,
		Namespace:       newPvc.Namespace,
		Name:            newPvc.Name,
		UID:             newPvc.UID,
		APIVersion:      newPvc.APIVersion,
		ResourceVersion: newPvc.ResourceVersion,
	}

	return newPvc, r.Update(ctx, pv)
}

func (r *AgentConfigReconciler) isDeleteProcessed(ctx context.Context, agentCfg *porterv1.AgentConfig) bool {
	if !isDeleted(agentCfg) {
		return false
	}

	var isPVCDeleted, isPVDeleted bool
	hashPVC := getPluginHash(agentCfg)
	key := client.ObjectKey{Namespace: agentCfg.Namespace, Name: hashPVC}
	newPVC := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, key, newPVC); err != nil && apierrors.IsNotFound(err) {
		isPVCDeleted = true
	}

	pv := &corev1.PersistentVolume{}
	pvKey := client.ObjectKey{Namespace: newPVC.Namespace, Name: newPVC.Spec.VolumeName}
	if err := r.Get(ctx, pvKey, pv); err != nil && apierrors.IsNotFound(err) {
		isPVDeleted = true
	}

	return isPVCDeleted && isPVDeleted

}

// removeAgentCfgFinalizer deletes the porter finalizer from the specified resource and saves it.
func removeAgentCfgFinalizer(ctx context.Context, log logr.Logger, client client.Client, agentCfg *porterv1.AgentConfig) error {
	log.V(Log5Trace).Info("removing finalizer")
	controllerutil.RemoveFinalizer(agentCfg, porterv1.FinalizerName)
	return client.Update(ctx, agentCfg)
}

func (r *AgentConfigReconciler) shouldDelete(agentCfg *porterv1.AgentConfig) bool {
	// ignore a deleted CRD with no finalizers
	return isDeleted(agentCfg) && isFinalizerSet(agentCfg)
}

func (r *AgentConfigReconciler) cleanup(ctx context.Context, log logr.Logger, agentCfg *porterv1.AgentConfig) error {
	hashPVC := getPluginHash(agentCfg)
	log.V(Log4Debug).Info("Start cleaning up persistent volume claim.", "persistentvolumeclaim", hashPVC, "namespace", agentCfg.Namespace)
	key := client.ObjectKey{Namespace: agentCfg.Namespace, Name: hashPVC}
	newPVC := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, key, newPVC)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if err == nil {
		if err := r.Delete(ctx, newPVC); err != nil {
			return err
		}
		log.V(Log4Debug).Info("Cleaned up persistent volume claim.", "persistentvolumeclaim", hashPVC, "namespace", agentCfg.Namespace)
	}

	log.V(Log4Debug).Info("Start cleaning up persistent volume.", "persistentvolume", newPVC.Spec.VolumeName, "namespace", agentCfg.Namespace)
	pv := &corev1.PersistentVolume{}
	pvKey := client.ObjectKey{Namespace: newPVC.Namespace, Name: newPVC.Spec.VolumeName}
	err = r.Get(ctx, pvKey, pv)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if err == nil {
		if err := r.Delete(ctx, pv); err != nil {
			return err
		}
		log.V(Log4Debug).Info("Cleaned up persistent volume.", "persistentvolume", pvKey.Name, "namespace", agentCfg.Namespace)
	}

	return nil

}
