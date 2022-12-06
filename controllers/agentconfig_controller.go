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

	// Check if we have finished removing the agentCfg from the pvc and pv's owner reference
	processed, err := r.isDeleteProcessed(ctx, agentCfg)
	if err != nil {
		return ctrl.Result{}, err
	}
	if processed {
		err = removeAgentCfgFinalizer(ctx, log, r.Client, agentCfg)
		log.V(Log4Debug).Info("Reconciliation complete: Finalizer has been removed from the AgentConfig.")
		return ctrl.Result{}, err
	}

	// TODO: once porter has ability to install multiple plugins with one command, we will allow users
	// to install plugins other than the default one
	updatedCfg := setDefaultPlugins(agentCfg)
	if updatedCfg {
		err := r.Update(ctx, agentCfg)
		return ctrl.Result{}, err
	}

	readyPVC, tempPVC, err := r.GetPersistentVolumeClaims(ctx, log, agentCfg)
	if err != nil {
		return ctrl.Result{}, err
	}
	if readyPVC != nil && tempPVC == nil && !isDeleted(agentCfg) {
		if readyPVC.Status.Phase != corev1.ClaimBound || agentCfg.Status.Phase == porterv1.PhaseSucceeded {
			return ctrl.Result{}, nil
		}
		log.V(Log4Debug).Info("Plugin persistent volume claim already exits", "persistentvolumeclaim", readyPVC.Name, "status", readyPVC.Status.Phase)
		// update readyPVC to include this agentCfg in its ownerRference so when a delete happens, we know other agentCfg is still using this pvc
		if _, exist := containOwner(readyPVC.OwnerReferences, agentCfg); !exist {
			err := controllerutil.SetOwnerReference(agentCfg, readyPVC, r.Scheme)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to set owner reference: %w", err)
			}
			err = r.Update(ctx, readyPVC)
			if err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil

		}

		if agentCfg.Status.Phase == porterv1.PhasePending {
			// update the agentCfg status to be ready
			agentCfg.Status.Phase = porterv1.PhaseSucceeded
			if err := r.saveStatus(ctx, log, agentCfg); err != nil {
				return ctrl.Result{}, err
			}
		}
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
			log.V(Log4Debug).Info("Plugins is not ready yet.", "action status", action.Status)
			return ctrl.Result{}, nil
		}

		// delete the temporary pvc first once the plugin install action has been completed
		shouldDeleteTmpPVC := tempPVC != nil && readyPVC == nil
		if shouldDeleteTmpPVC {
			updated, err := r.bindPVWithPluginPVC(ctx, log, tempPVC, agentCfg)
			if err != nil {
				return ctrl.Result{}, err
			}
			if updated {
				return ctrl.Result{}, nil
			}
			err = r.DeleteTemporaryPVC(ctx, log, tempPVC, agentCfg)
			if err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, nil
		}

		if tempPVC == nil {
			hashedPVCName := agentCfg.GetPluginsPVCName()
			// create the pvc with the hash of plugins metadata
			_, err = r.createHashPVC(ctx, log, agentCfg)
			if err != nil {
				return ctrl.Result{}, err
			}

			log.V(Log4Debug).Info("Created the new PVC with plugins hash as its name.", "new persistentvolumeclaim", hashedPVCName)
		}

		return ctrl.Result{}, nil
	}

	if r.shouldDelete(agentCfg) {
		log.V(Log4Debug).Info("Reconciliation complete: cleaning up pvc and pv created by this agent config", "agentCfg", agentCfg.Name)
		err = r.cleanup(ctx, log, agentCfg)
		return ctrl.Result{}, err
	} else if isDeleted(agentCfg) {
		log.V(Log4Debug).Info("Reconciliation complete: AgentConfig CRD is ready for deletion.")
		return ctrl.Result{}, nil

	}

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
		log.V(Log4Debug).Info("Created new temporary persistent volume claim.", "name", pvc.Name)
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
	log.V(Log4Debug).Info("Found existing agent action", "agentaction", action.Name)
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

	pvc, exists, err := r.GetPersistentVolumeClaim(ctx, agentCfg.Namespace, agentCfg.GetPluginsPVCName())
	if err != nil {
		return nil, false, err
	}

	if exists {
		return pvc, exists, nil
	}

	lables := agentCfg.Spec.GetPluginsLabels()
	pvc = &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: agentCfg.Name + "-",
			Namespace:    agentCfg.Namespace,
			Labels:       lables,
			Annotations:  agentCfg.GetPVCNameAnnotation(),
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

// runPorterPluginInstall creates an AgentAction that triggers a porter run to install plugins on the passed in volume.
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

// createAgentAction creates an AgentAction with the temporary volumes that's used for plugin installation.
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
		SubPath:   "plugins",
	}

	return volume, volumeMount
}

// createHashPVC creates a new pvc using the hash of all plugins metadata.
// It uses the lable selector to make sure we will select the volum that has plugins installed.
func (r *AgentConfigReconciler) createHashPVC(
	ctx context.Context,
	log logr.Logger,
	agentCfg *porterv1.AgentConfig,
) (*corev1.PersistentVolumeClaim, error) {
	log.V(Log4Debug).Info("Renaming temporary persistent volume claim.", "new persistentvolumeclaim", agentCfg.GetPluginsPVCName())
	lables := agentCfg.Spec.GetPluginsLabels()
	lables[porterv1.LabelResourceName] = agentCfg.Name

	selector := &metav1.LabelSelector{
		MatchLabels: lables,
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agentCfg.GetPluginsPVCName(),
			Namespace: agentCfg.Namespace,
			Labels:    agentCfg.Spec.GetPluginsLabels(),
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
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadOnlyMany},
			Selector:    selector,
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

	return pvc, nil

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

// cleanup remove the owner references on both pvc and pv so the when no resource is referencing them, GC can clean them up after the agentCfg has been deleted
func (r *AgentConfigReconciler) cleanup(ctx context.Context, log logr.Logger, agentCfg *porterv1.AgentConfig) error {
	hashPVC := agentCfg.GetPluginsPVCName()
	log.V(Log4Debug).Info("Start cleaning up persistent volume claim.", "persistentvolumeclaim", hashPVC, "namespace", agentCfg.Namespace)
	key := client.ObjectKey{Namespace: agentCfg.Namespace, Name: hashPVC}
	newPVC := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, key, newPVC)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	pv := &corev1.PersistentVolume{}
	pvKey := client.ObjectKey{Namespace: newPVC.Namespace, Name: newPVC.Spec.VolumeName}
	err = r.Get(ctx, pvKey, pv)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if idx, exist := containOwner(pv.GetOwnerReferences(), agentCfg); exist {
		pv.OwnerReferences = removeOwnerByIdx(pv.GetOwnerReferences(), idx)
		err := r.Update(ctx, pv)
		if err != nil {
			return err
		}
		return nil
	}

	// remove owner reference
	if idx, exist := containOwner(newPVC.GetOwnerReferences(), agentCfg); exist {
		newPVC.OwnerReferences = removeOwnerByIdx(newPVC.GetOwnerReferences(), idx)
		err := r.Update(ctx, newPVC)
		if err != nil {
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

// bindPVWithPluginPVC binds the persistent volume to the claim with a name created by the hash of all plugins defined on a AgentConfigSpec.
func (r *AgentConfigReconciler) bindPVWithPluginPVC(ctx context.Context, log logr.Logger, tempPVC *corev1.PersistentVolumeClaim, agentCfg *porterv1.AgentConfig) (bool, error) {
	pv, err := r.GetPersistentVolume(ctx, tempPVC.Namespace, tempPVC.Spec.VolumeName)
	if err != nil {
		return false, err
	}

	if _, exist := pv.Labels[porterv1.LablePlugins]; !exist {
		labels := agentCfg.Spec.GetPluginsLabels()
		labels[porterv1.LabelResourceName] = agentCfg.Name
		pv.Labels = labels
		pv.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadOnlyMany}
		pv.Spec.ClaimRef = &corev1.ObjectReference{
			Kind:       tempPVC.Kind,
			Namespace:  tempPVC.Namespace,
			Name:       agentCfg.GetPluginsPVCName(),
			APIVersion: tempPVC.APIVersion,
		}
		if err := r.Update(ctx, pv); err != nil {
			return false, err
		}

		return true, nil
	}

	return false, nil
}

// DeleteTemporaryPVC deletes the persistent volume claim created by the agent action controller.
func (r *AgentConfigReconciler) DeleteTemporaryPVC(ctx context.Context, log logr.Logger, tempPVC *corev1.PersistentVolumeClaim, agentCfg *porterv1.AgentConfig) error {
	hasFinalizer := controllerutil.ContainsFinalizer(tempPVC, "kubernetes.io/pvc-protection")
	if hasFinalizer {
		log.V(Log4Debug).Info("Starting to remove finalizers from temporary pvc.", "persistentvolumeclaim", tempPVC.Name, "namespace", tempPVC.Namespace)
		controllerutil.RemoveFinalizer(tempPVC, "kubernetes.io/pvc-protection")
		if err := r.Client.Update(ctx, tempPVC); err != nil {
			return err
		}
		log.V(Log4Debug).Info("Removed finalizers from temporary pvc.", "persistentvolumeclaim", tempPVC.Name, "namespace", tempPVC.Namespace)

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
	pvc, exists, err := r.GetPersistentVolumeClaim(ctx, agentCfg.Namespace, agentCfg.GetPluginsPVCName())
	if err != nil {
		return false, err
	}
	if exists {
		ref := pvc.GetOwnerReferences()
		if _, exist := containOwner(ref, agentCfg); exist {
			return false, nil
		}

		pv, err := r.GetPersistentVolume(ctx, agentCfg.Namespace, pvc.Spec.VolumeName)
		if err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}
		if pv != nil {
			if _, exist := containOwner(pv.GetOwnerReferences(), agentCfg); exist {
				return false, nil
			}
		}
	}

	return true, nil
}

func (r *AgentConfigReconciler) GetPersistentVolumeClaims(ctx context.Context, log logr.Logger, agentCfg *porterv1.AgentConfig) (readyPVC *corev1.PersistentVolumeClaim, tempPVC *corev1.PersistentVolumeClaim, err error) {
	results := &corev1.PersistentVolumeClaimList{}
	err = r.List(ctx, results, client.InNamespace(agentCfg.Namespace), client.MatchingLabels(agentCfg.Spec.GetPluginsLabels()))
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, nil, err
	}

	hashedName := agentCfg.GetPluginsPVCName()
	for _, item := range results.Items {
		item := item
		if item.Name == hashedName {
			readyPVC = &item
			log.V(Log4Debug).Info("Plugin persistent volume claims found", "persistentvolumeclaim", readyPVC.Name, "namespace", readyPVC.Namespace, "status", readyPVC.Status.Phase)
			continue
		}

		if annotation := item.GetAnnotations(); annotation != nil {
			hash, ok := annotation[porterv1.AnnotationAgentCfgPluginsHash]
			if ok && hash == agentCfg.GetPluginsPVCName() {
				tempPVC = &item
				log.V(Log4Debug).Info("Temporary plugin persistent volume claims found", "persistentvolumeclaim", tempPVC.Name, "namespace", tempPVC.Namespace, "status", tempPVC.Status.Phase)
			}
		}

		if readyPVC != nil && tempPVC != nil {
			return readyPVC, tempPVC, nil
		}
	}

	return readyPVC, tempPVC, nil
}

func setDefaultPlugins(agentCfg *porterv1.AgentConfig) bool {
	var shouldUpdate bool
	numOfPlugins := len(agentCfg.Spec.Plugins)
	if numOfPlugins == 0 {
		plugins := porterv1.DefaultPlugins
		agentCfg.Spec.Plugins = plugins
		shouldUpdate = true
	}
	if numOfPlugins > 1 {
		agentCfg.Spec.Plugins = agentCfg.Spec.Plugins[:1]
		shouldUpdate = true
	}
	return shouldUpdate
}

func containOwner(owners []metav1.OwnerReference, agentCfg *porterv1.AgentConfig) (int, bool) {
	for i, owner := range owners {
		if owner.APIVersion == agentCfg.APIVersion && owner.Kind == agentCfg.Kind && owner.Name == agentCfg.Name {
			return i, true
		}

	}
	return -1, false
}

func removeOwnerByIdx(s []metav1.OwnerReference, index int) []metav1.OwnerReference {
	ret := make([]metav1.OwnerReference, 0)
	ret = append(ret, s[:index]...)
	return append(ret, s[index+1:]...)
}
