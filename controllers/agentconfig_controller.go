package controllers

import (
	"context"
	"fmt"
	"reflect"

	porterv1 "get.porter.sh/operator/api/v1"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
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

	if len(agentCfg.Spec.Plugins) == 0 {
		log.V(Log5Trace).Info("No plugins need to be installed")
		agentCfg.Status.Phase = porterv1.PhaseSucceeded
		if err := r.saveStatus(ctx, log, agentCfg); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

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

	// Check if we have finished removing the finalizers from the pvc and pv created by this agentCfg resource
	processed, err := r.isDeleteProcessed(ctx, agentCfg)
	if err != nil {
		return ctrl.Result{}, err
	}
	if processed {
		err = removeAgentCfgFinalizer(ctx, log, r.Client, agentCfg)
		log.V(Log4Debug).Info("Reconciliation complete: Finalizer has been removed from the AgentConfig.")
		return ctrl.Result{}, err
	}

	readyPVC, tempPVC, err := r.GetPersistentVolumeClaims(ctx, agentCfg)
	if err != nil {
		return ctrl.Result{}, err
	}
	if readyPVC != nil && tempPVC == nil {
		log.V(Log4Debug).Info("Plugin persistent volume claim already exits", "persistentvolumeclaim", readyPVC.Name, "namespace", readyPVC.Namespace, "status", readyPVC.Status.Phase)
		return ctrl.Result{}, nil
	}

	// Check if we have already handled any spec changes
	if handled {
		// Check if a retry was requested
		if action.GetRetryLabelValue() != agentCfg.GetRetryLabelValue() {
			err = r.retry(ctx, log, agentCfg, action)
			log.V(Log4Debug).Info("Reconciliation complete: The associated porter agent action was retried.")
			return ctrl.Result{}, err
		}

		// if the plugin install action is not finished, we need to wait for it before acting further
		if !(apimeta.IsStatusConditionTrue(action.Status.Conditions, string(porterv1.ConditionComplete)) && action.Status.Phase == porterv1.PhaseSucceeded) {
			log.V(Log4Debug).Info("Volumn is not ready yet.", "persistentvolumeclaim", tempPVC.Name, "namespace", tempPVC.Namespace)
			return ctrl.Result{}, nil
		}
		// check if we can find the hash pvc that's bounded, if so, we can delete the tmp pvc
		shouldDeleteTmpPVC := readyPVC != nil && tempPVC != nil
		if shouldDeleteTmpPVC {
			err := r.DeleteTemporaryPVC(ctx, log, tempPVC, readyPVC)
			if err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, nil
		}

		// rename the pvc to the hash of plugins metadata
		_, err = r.createHashPVC(ctx, log, tempPVC, agentCfg.GetPVCName())
		if err != nil {
			return ctrl.Result{}, err
		}

		log.V(Log4Debug).Info("Renamed temporary persistent volume claim.", "old persistentvolumeclaim", tempPVC.Name, "new persistentvolumeclaim", readyPVC, "namespace", tempPVC.Namespace)

		return ctrl.Result{}, nil
	}

	if r.shouldDelete(agentCfg) {
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

	pvc, created, err := r.createAgentVolumeWithPlugins(ctx, log, agentCfg)
	if err != nil {
		return ctrl.Result{}, err
	}
	if created {
		log.V(Log4Debug).Info("Created new persistent volume claim.", "name", pvc.Name)
	}

	// Use porter to finish reconciling the agent config
	err = r.applyAgentConfig(ctx, log, pvc, agentCfg)
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
func (r *AgentConfigReconciler) applyAgentConfig(ctx context.Context, log logr.Logger, pvc *corev1.PersistentVolumeClaim, agentCfg *porterv1.AgentConfig) error {
	log.V(Log5Trace).Info("Initializing agent config status")
	agentCfg.Status.Initialize()
	if err := r.saveStatus(ctx, log, agentCfg); err != nil {
		return err
	}

	return r.runPorterPluginInstall(ctx, log, pvc, agentCfg)
}

func (r *AgentConfigReconciler) createAgentVolumeWithPlugins(ctx context.Context, log logr.Logger, agentCfg *porterv1.AgentConfig) (*corev1.PersistentVolumeClaim, bool, error) {

	pvc, exists, err := r.GetPersistentVolumeClaim(ctx, agentCfg.Namespace, agentCfg.GetPVCName())
	if err != nil {
		return nil, false, err
	}

	if exists {
		return pvc, exists, nil
	}

	lables := agentCfg.Spec.GetPluginsLabels(agentCfg.Name)
	pvc = &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: agentCfg.Name + "-",
			Namespace:    agentCfg.Namespace,
			Labels:       lables,
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
					corev1.ResourceStorage: agentCfg.Spec.GetVolumeSize(),
				},
			},
		},
	}

	if err := r.Create(ctx, pvc); err != nil {
		return nil, false, errors.Wrap(err, "error creating the agent volume (pvc)")
	}

	log.V(Log4Debug).Info("Created PersistentVolumeClaim for the Porter agent", "name", pvc.Name)
	return pvc, true, nil
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
func (r *AgentConfigReconciler) createHashPVC(
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

	return newPvc, nil

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

// cleanup remove the finalizers on both pvc and pv so the GC can clean them up after the agentCfg has been deleted
// it first removes the finalizer from the pv and then pvc to make sure the pv is deleted first
func (r *AgentConfigReconciler) cleanup(ctx context.Context, log logr.Logger, agentCfg *porterv1.AgentConfig) error {
	hashPVC := agentCfg.GetPVCName()
	log.V(Log4Debug).Info("Start cleaning up persistent volume claim.", "persistentvolumeclaim", hashPVC, "namespace", agentCfg.Namespace)
	key := client.ObjectKey{Namespace: agentCfg.Namespace, Name: hashPVC}
	newPVC := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, key, newPVC)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	var pvcHasFinalizer bool
	if err == nil {
		pvcHasFinalizer = len(newPVC.GetFinalizers()) > 0
	}

	if pvcHasFinalizer {
		log.V(Log4Debug).Info("Start cleaning up persistent volume.", "persistentvolume", newPVC.Spec.VolumeName, "namespace", agentCfg.Namespace)
		pv := &corev1.PersistentVolume{}
		pvKey := client.ObjectKey{Namespace: newPVC.Namespace, Name: newPVC.Spec.VolumeName}
		err = r.Get(ctx, pvKey, pv)
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		if err == nil {
			finalizers := pv.GetFinalizers()
			if len(finalizers) > 0 {
				pv.SetFinalizers([]string{})
				if err := r.Client.Update(ctx, pv); err != nil {
					return err
				}
				return nil
			}
		}

		newPVC.SetFinalizers([]string{})
		if err := r.Client.Update(ctx, newPVC); err != nil {
			return err
		}

		return nil
	}

	return nil

}

func (r *AgentConfigReconciler) GetPersistentVolumeClaim(ctx context.Context, namespace string, name string) (*corev1.PersistentVolumeClaim, bool, error) {
	key := client.ObjectKey{Namespace: namespace, Name: name}
	newPVC := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, key, newPVC)
	if apierrors.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	return newPVC, true, nil

}

func (r *AgentConfigReconciler) GetPersistentVolume(ctx context.Context, namespace string, name string) (*corev1.PersistentVolume, error) {
	key := client.ObjectKey{Namespace: namespace, Name: name}
	pv := &corev1.PersistentVolume{}
	err := r.Get(ctx, key, pv)
	if err != nil {
		return nil, err
	}

	return pv, nil

}

func (r *AgentConfigReconciler) DeleteTemporaryPVC(ctx context.Context, log logr.Logger, tempPVC, newPVC *corev1.PersistentVolumeClaim) error {
	finalizers := tempPVC.GetFinalizers()
	if tempPVC.Status.Phase == corev1.ClaimLost && len(finalizers) > 0 {
		tempPVC.SetFinalizers([]string{})
		if err := r.Client.Update(ctx, tempPVC); err != nil {
			return err
		}
		log.V(Log4Debug).Info("Removed finalizers from temporary pvc.", "persistentvolumeclaim", tempPVC.Name, "namespace", tempPVC.Namespace)
		return nil
	}
	// release old pvc from agentCfg
	if len(finalizers) > 0 {
		//update the pv with the new pvc
		pv, err := r.GetPersistentVolume(ctx, tempPVC.Namespace, tempPVC.Spec.VolumeName)
		if err != nil {
			return err
		}
		pv.Spec.ClaimRef = &corev1.ObjectReference{
			Kind:            newPVC.Kind,
			Namespace:       newPVC.Namespace,
			Name:            newPVC.Name,
			UID:             newPVC.UID,
			APIVersion:      newPVC.APIVersion,
			ResourceVersion: newPVC.ResourceVersion,
		}

		if err := r.Update(ctx, pv); err != nil {
			return err
		}

		return nil
	}

	log.V(Log4Debug).Info("Deleting temporary persistent volume claim.", "persistentvolumeclaim", tempPVC.Name, "namespace", tempPVC.Namespace)
	if err := r.Delete(ctx, tempPVC); err != nil {
		return err
	}
	log.V(Log4Debug).Info("Deleted temporary persistent volume claim.", "persistentvolumeclaim", tempPVC.Name, "namespace", tempPVC.Namespace)
	return nil
}

func (r *AgentConfigReconciler) isDeleteProcessed(ctx context.Context, agentCfg *porterv1.AgentConfig) (bool, error) {
	if !isDeleted(agentCfg) {
		return false, nil
	}
	// check if the finalizers for the pvc and pv has been removed
	pvc, exists, err := r.GetPersistentVolumeClaim(ctx, agentCfg.Namespace, agentCfg.GetPVCName())
	if err != nil {
		return false, err
	}
	if exists {
		if len(pvc.GetFinalizers()) == 0 {
			return true, nil
		}

		pv, err := r.GetPersistentVolume(ctx, agentCfg.Namespace, pvc.Spec.VolumeName)
		if err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}
		if pv != nil {
			if len(pv.GetFinalizers()) == 0 {
				return true, nil
			}

			return false, nil
		}
	}

	return true, nil
}

func (r *AgentConfigReconciler) GetPersistentVolumeClaims(ctx context.Context, agentCfg *porterv1.AgentConfig) (readyPVC *corev1.PersistentVolumeClaim, tempPVC *corev1.PersistentVolumeClaim, err error) {
	results := &corev1.PersistentVolumeClaimList{}
	err = r.List(ctx, results, client.InNamespace(agentCfg.Namespace), client.MatchingLabels(agentCfg.Spec.GetPluginsLabels(agentCfg.Name)))
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, nil, err
	}

	hashedName := agentCfg.GetPVCName()
	switch len(results.Items) {
	case 2:
		// find the pvc with a name that's the hash of all plugins
		// the random name should ne the temp pvc
		for _, item := range results.Items {
			if item.Name == hashedName {
				readyPVC = &item
				continue
			}
			tempPVC = &item
		}

		return readyPVC, tempPVC, nil
	case 1:
		// if the name matches with the hash of all plugsin, it's the ready PVC
		pvc := results.Items[0]
		if pvc.Name == hashedName {
			readyPVC = &pvc
		} else {
			tempPVC = &pvc
		}
		return readyPVC, tempPVC, nil
	case 0:
		return nil, nil, nil
	}
	return nil, nil, errors.New("unexpected number of persistent volume claims found")
}
