package controllers

import (
	"context"
	"fmt"
	"reflect"

	porterv1 "get.porter.sh/operator/api/v1"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

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

//+kubebuilder:rbac:groups=getporter.org,resources=agentconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=getporter.org,resources=agentconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=getporter.org,resources=agentconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=getporter.org,resources=porterconfigs,verbs=get;list;watch;create;update;patch;delete
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
	agentCfgData := &porterv1.AgentConfig{}
	err := r.Get(ctx, req.NamespacedName, agentCfgData)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.V(Log5Trace).Info("Reconciliation skipped: AgentConfig CRD or one of its owned resources was deleted.")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if agentCfgData.DeletionTimestamp != nil {
		if controllerutil.ContainsFinalizer(agentCfgData, porterv1.FinalizerName) {
			controllerutil.RemoveFinalizer(agentCfgData, porterv1.FinalizerName)
			if err := r.Update(ctx, agentCfgData); err != nil {
				return ctrl.Result{}, err
			}
		}
	}
	agentCfg := porterv1.NewAgentConfigAdapter(*agentCfgData)

	log = log.WithValues("resourceVersion", agentCfg.ResourceVersion, "generation", agentCfg.Generation, "observedGeneration", agentCfg.Status.ObservedGeneration, "status", agentCfg.Status.Ready)
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

	// Check if we have finished removing the agentCfg from the pvc owner reference
	processed, err := r.isReadyToBeDeleted(ctx, log, agentCfg)
	if err != nil {
		return ctrl.Result{}, err
	}
	if processed {
		err = removeAgentCfgFinalizer(ctx, log, r.Client, agentCfg)
		log.V(Log4Debug).Info("Reconciliation complete: Finalizer has been removed from the AgentConfig.")
		return ctrl.Result{}, err
	}

	updatedStatus, err := r.syncPluginInstallStatus(ctx, log, agentCfg)
	if err != nil {
		return ctrl.Result{}, err
	}
	if updatedStatus {
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

		err := r.renamePluginVolume(ctx, log, action, agentCfg)
		if err != nil {
			return ctrl.Result{}, err
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

	updated, err := ensureFinalizerSet(ctx, log, r.Client, &agentCfg.AgentConfig)
	if err != nil {
		return ctrl.Result{}, err
	}
	if updated {
		// if we added a finalizer, stop processing and we will finish when the updated resource is reconciled
		log.V(Log4Debug).Info("Reconciliation complete: A finalizer has been set on the agent config.")
		return ctrl.Result{}, nil
	}

	pvc, created, err := r.createEmptyPluginVolume(ctx, log, agentCfg)
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
func (r *AgentConfigReconciler) isHandled(ctx context.Context, log logr.Logger, agentCfg *porterv1.AgentConfigAdapter) (*porterv1.AgentAction, bool, error) {
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

// Run the porter agent with the command `porter plugins install <plugin-name>`
func (r *AgentConfigReconciler) applyAgentConfig(ctx context.Context, log logr.Logger, pvc *corev1.PersistentVolumeClaim, agentCfg *porterv1.AgentConfigAdapter) error {
	log.V(Log5Trace).Info("Initializing agent config status")
	agentCfg.Status.Initialize()
	if err := r.saveStatus(ctx, log, agentCfg); err != nil {
		return err
	}

	return r.runPorterPluginInstall(ctx, log, pvc, agentCfg)
}

// createEmptyPluginVolume returns a volume resources that will be used to install plugins on.
// it returns the a volume claim and whether it's newly created.
func (r *AgentConfigReconciler) createEmptyPluginVolume(ctx context.Context, log logr.Logger, agentCfg *porterv1.AgentConfigAdapter) (*corev1.PersistentVolumeClaim, bool, error) {
	pluginPVC, tempPVC, err := r.getExistingPluginPVCs(ctx, log, agentCfg)
	if err != nil {
		return nil, false, err
	}
	// If there is an exising claim then return it. This can be either the pluginPVC or a tempPVC depending on where in the process
	// we are. If the final plugin PVC exists then return that otherwise return the temp one.
	if pluginPVC != nil {
		return pluginPVC, false, nil
	}
	if tempPVC != nil {
		return tempPVC, false, nil
	}

	storageClassName := agentCfg.Spec.GetStorageClassName()
	labels := agentCfg.Spec.Plugins.GetLabels()
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: agentCfg.Name + "-",
			Namespace:    agentCfg.Namespace,
			Labels:       labels,
			Annotations:  agentCfg.GetPluginsPVCNameAnnotation(),
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
	if storageClassName != "" {
		pvc.Spec.StorageClassName = &storageClassName
	}
	if err := controllerutil.SetControllerReference(&agentCfg.AgentConfig, pvc, r.Scheme); err != nil {
		return nil, false, errors.Wrap(err, "error attaching owner reference to agent volume (pvc)")
	}

	if err := r.Create(ctx, pvc); err != nil {
		return nil, false, errors.Wrap(err, "error creating the agent volume (pvc)")
	}

	return pvc, true, nil
}

// runPorterPluginInstall creates an AgentAction that triggers a porter run to install plugins on the passed in volume.
func (r *AgentConfigReconciler) runPorterPluginInstall(ctx context.Context, log logr.Logger, pvc *corev1.PersistentVolumeClaim, agentCfg *porterv1.AgentConfigAdapter) error {
	if agentCfg.Spec.Plugins.IsZero() {
		return nil
	}

	installCmd := []string{"plugins", "install", "-f", "plugins.yaml"}
	action, err := r.createAgentAction(ctx, log, pvc, agentCfg, installCmd)
	if err != nil {
		return err
	}

	// Update the agent config Status with the agent action
	return r.syncStatus(ctx, log, agentCfg, action)
}

// createAgentAction creates an AgentAction with the temporary volumes that's used for plugin installation.
func (r *AgentConfigReconciler) createAgentAction(ctx context.Context, log logr.Logger, pvc *corev1.PersistentVolumeClaim, agentCfg *porterv1.AgentConfigAdapter, args []string) (*porterv1.AgentAction, error) {
	log.V(Log5Trace).Info("Creating porter agent action")
	labels := getActionLabels(agentCfg)
	for k, v := range agentCfg.Labels {
		labels[k] = v
	}

	volumn, volumnMount := definePluginVomeAndMount(pvc)
	agentCfgName := &corev1.LocalObjectReference{Name: agentCfg.Name}
	pluginsCfg, err := agentCfg.Spec.ToPorterDocument()
	if err != nil {
		return nil, err
	}

	action := &porterv1.AgentAction{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    agentCfg.Namespace,
			GenerateName: agentCfg.Name + "-",
			Labels:       labels,
			Annotations:  agentCfg.Annotations,
		},
		Spec: porterv1.AgentActionSpec{
			AgentConfig:  agentCfgName,
			Args:         args,
			Volumes:      []corev1.Volume{volumn},
			VolumeMounts: []corev1.VolumeMount{volumnMount},
			Files: map[string][]byte{
				"plugins.yaml": pluginsCfg,
			},
		},
	}

	if err := r.Create(ctx, action); err != nil {
		return nil, errors.Wrap(err, "error creating the porter agent action")
	}

	if err := controllerutil.SetControllerReference(&agentCfg.AgentConfig, action, r.Scheme); err != nil {
		return nil, errors.Wrap(err, "error attaching owner reference"+
			"while creating porter agent action")
	}

	log.V(Log4Debug).Info("Created porter agent action", "name", action.Name)
	return action, nil
}

// Check the status of the porter-agent job and use that to update the AgentAction status
func (r *AgentConfigReconciler) syncStatus(ctx context.Context, log logr.Logger, agentCfg *porterv1.AgentConfigAdapter, action *porterv1.AgentAction) error {

	origStatus := agentCfg.Status

	applyAgentAction(log, agentCfg, action)

	// if the spec changed, we need to reset the readiness of the agent config
	if origStatus.Ready && origStatus.ObservedGeneration != agentCfg.Generation || agentCfg.Status.Phase != porterv1.PhaseSucceeded {
		agentCfg.Status.Ready = false
	}

	if !reflect.DeepEqual(origStatus, agentCfg.Status) {
		return r.saveStatus(ctx, log, agentCfg)
	}

	return nil
}

// Only update the status with a PATCH, don't clobber the entire agent config
func (r *AgentConfigReconciler) saveStatus(ctx context.Context, log logr.Logger, agentCfg *porterv1.AgentConfigAdapter) error {
	log.V(Log5Trace).Info("Patching agent config status")
	cfg := &agentCfg.AgentConfig
	return PatchStatusWithRetry(ctx, log, r.Client, r.Status().Patch, cfg, func() client.Object {
		return &porterv1.AgentConfig{}
	})
}

// Sync the retry annotation from the agent config to the agent action to trigger another run.
func (r *AgentConfigReconciler) retry(ctx context.Context, log logr.Logger, agentCfg *porterv1.AgentConfigAdapter, action *porterv1.AgentAction) error {
	log.V(Log5Trace).Info("Initializing agent config status")
	agentCfg.Status.Initialize()
	agentCfg.Status.Action = &corev1.LocalObjectReference{Name: action.Name}
	agentCfg.Status.Ready = false
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

func definePluginVomeAndMount(pvc *corev1.PersistentVolumeClaim) (corev1.Volume, corev1.VolumeMount) {
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
// It uses the label selector to make sure we will select the volum that has plugins installed.
func (r *AgentConfigReconciler) createHashPVC(ctx context.Context, log logr.Logger, agentCfg *porterv1.AgentConfigAdapter,
) (*corev1.PersistentVolumeClaim, error) {
	log.V(Log4Debug).Info("Creating new pvc using the hash of all plugins metadata", "new persistentvolumeclaim", agentCfg.GetPluginsPVCName())
	labels := agentCfg.Spec.Plugins.GetLabels()
	labels[porterv1.LabelResourceName] = agentCfg.Name
	storageClassName := agentCfg.Spec.GetStorageClassName()
	selector := &metav1.LabelSelector{
		MatchLabels: labels,
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agentCfg.GetPluginsPVCName(),
			Namespace: agentCfg.Namespace,
			Labels:    labels,
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
	if storageClassName != "" {
		pvc.Spec.StorageClassName = &storageClassName
	}

	if err := controllerutil.SetControllerReference(&agentCfg.AgentConfig, pvc, r.Scheme); err != nil {
		return nil, errors.Wrap(err, "error attaching owner reference to agent volume (pvc)")
	}

	if err := r.Create(ctx, pvc); err != nil {
		return nil, errors.Wrap(err, "error creating the agent volume (pvc)")
	}

	return pvc, nil

}

// removeAgentCfgFinalizer deletes the porter finalizer from the specified resource and saves it.
func removeAgentCfgFinalizer(ctx context.Context, log logr.Logger, client client.Client, agentCfg *porterv1.AgentConfigAdapter) error {
	log.V(Log5Trace).Info("removing finalizer")
	controllerutil.RemoveFinalizer(agentCfg, porterv1.FinalizerName)
	return client.Update(ctx, &agentCfg.AgentConfig)
}

func (r *AgentConfigReconciler) shouldDelete(agentCfg *porterv1.AgentConfigAdapter) bool {
	// ignore a deleted CRD with no finalizers
	return isDeleted(agentCfg) && isFinalizerSet(agentCfg)
}

// cleanup remove the owner references on both pvc and pv so the when no resource is referencing them, GC can clean them up after the agentCfg has been deleted
func (r *AgentConfigReconciler) cleanup(ctx context.Context, log logr.Logger, agentCfg *porterv1.AgentConfigAdapter) error {
	if agentCfg.Status.Ready {
		agentCfg.Status.Ready = false
		return r.saveStatus(ctx, log, agentCfg)
	}

	pvcName := agentCfg.GetPluginsPVCName()
	log.V(Log4Debug).Info("Start cleaning up persistent volume claim.", "persistentvolumeclaim", pvcName, "namespace", agentCfg.Namespace)

	key := client.ObjectKey{Namespace: agentCfg.Namespace, Name: pvcName}
	pvc := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, key, pvc)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	pv := &corev1.PersistentVolume{}
	pvKey := client.ObjectKey{Namespace: pvc.Namespace, Name: pvc.Spec.VolumeName}
	err = r.Get(ctx, pvKey, pv)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	// remove owner reference from the persistent volume claim
	if idx, exist := containsOwner(pvc.GetOwnerReferences(), agentCfg); exist {
		pvc.OwnerReferences = removeOwnerAtIdx(pvc.GetOwnerReferences(), idx)
		err := r.Update(ctx, pvc)
		if err != nil {
			return err
		}
		return nil
	}

	return nil

}

func (r *AgentConfigReconciler) getPersistentVolumeClaim(ctx context.Context, namespace string, name string) (*corev1.PersistentVolumeClaim, bool, error) {
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

func (r *AgentConfigReconciler) getPersistentVolume(ctx context.Context, namespace string, name string) (*corev1.PersistentVolume, error) {
	key := client.ObjectKey{Namespace: namespace, Name: name}
	pv := &corev1.PersistentVolume{}
	err := r.Get(ctx, key, pv)
	if err != nil {
		return nil, err
	}

	return pv, nil

}

// bindPVWithPluginPVC binds the persistent volume to the claim with a name created by the hash of all plugins defined on a AgentConfigSpec.
func (r *AgentConfigReconciler) bindPVWithPluginPVC(ctx context.Context, log logr.Logger, tempPVC *corev1.PersistentVolumeClaim, agentCfg *porterv1.AgentConfigAdapter) (bool, error) {
	pv, err := r.getPersistentVolume(ctx, tempPVC.Namespace, tempPVC.Spec.VolumeName)
	if err != nil {
		return false, err
	}

	if _, exist := pv.Labels[porterv1.LabelPluginsHash]; !exist {
		labels := agentCfg.Spec.Plugins.GetLabels()
		labels[porterv1.LabelResourceName] = agentCfg.Name
		storageClassName := agentCfg.Spec.GetStorageClassName()
		pv.Labels = labels
		pv.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadOnlyMany}
		if storageClassName != "" {
			pv.Spec.StorageClassName = storageClassName
		}
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

// deleteTemporaryPVC deletes the persistent volume claim created by the agent action controller.
func (r *AgentConfigReconciler) deleteTemporaryPVC(ctx context.Context, log logr.Logger, tempPVC *corev1.PersistentVolumeClaim, agentCfg *porterv1.AgentConfigAdapter) error {
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

// isReadyToBeDeleted checks if an AgentConfig is ready to be deleted.
// It checks if any related persistent volume resources are released.
func (r *AgentConfigReconciler) isReadyToBeDeleted(ctx context.Context, log logr.Logger, agentCfg *porterv1.AgentConfigAdapter) (bool, error) {
	if !isDeleted(agentCfg) {
		return false, nil
	}

	pvc, exists, err := r.getPersistentVolumeClaim(ctx, agentCfg.Namespace, agentCfg.GetPluginsPVCName())
	if err != nil {
		return false, err
	}
	if exists {
		ref := pvc.GetOwnerReferences()
		if _, exist := containsOwner(ref, agentCfg); exist {
			return false, nil
		}
	}

	return true, nil
}

func (r *AgentConfigReconciler) getExistingPluginPVCs(ctx context.Context, log logr.Logger, agentCfg *porterv1.AgentConfigAdapter) (readyPVC *corev1.PersistentVolumeClaim, tempPVC *corev1.PersistentVolumeClaim, err error) {
	results := &corev1.PersistentVolumeClaimList{}
	err = r.List(ctx, results, client.InNamespace(agentCfg.Namespace), client.MatchingLabels(agentCfg.Spec.Plugins.GetLabels()))
	if err != nil && !apierrors.IsNotFound(err) || len(results.Items) < 1 {
		return nil, nil, err
	}
	hashedName := agentCfg.Spec.Plugins.GetPVCName(agentCfg.Namespace)
	for _, item := range results.Items {
		item := item
		if item.Name == hashedName {
			readyPVC = &item
			log.V(Log4Debug).Info("Plugin persistent volume claims found", "persistentvolumeclaim", readyPVC.Name, "namespace", readyPVC.Namespace, "status", readyPVC.Status.Phase)
			continue
		}

		if annotation := item.GetAnnotations(); annotation != nil {
			hash, ok := annotation[porterv1.AnnotationAgentCfgPluginsHash]
			if ok && hash == hashedName {
				tempPVC = &item
				log.V(Log4Debug).Info("Temporary plugin persistent volume claims found", "persistentvolumeclaim", tempPVC.Name, "namespace", tempPVC.Namespace, "status", tempPVC.Status.Phase)
			}
		}

		if readyPVC != nil && tempPVC != nil {
			break
		}
	}

	return readyPVC, tempPVC, nil
}

func checkPluginAndAgentReadiness(agentCfg *porterv1.AgentConfigAdapter, hashedPluginsPVC, tempPVC *corev1.PersistentVolumeClaim) (pvcReady bool, cfgReady bool) {
	if hashedPluginsPVC != nil && tempPVC == nil && !isDeleted(agentCfg) {
		cfgReady = agentCfg.Status.Phase == porterv1.PhaseSucceeded
		pvcReady = hashedPluginsPVC.Status.Phase == corev1.ClaimBound
	}

	return pvcReady, cfgReady
}

func (r *AgentConfigReconciler) updateOwnerReference(ctx context.Context, log logr.Logger, agentCfg *porterv1.AgentConfigAdapter, readyPVC *corev1.PersistentVolumeClaim) (bool, error) {
	// update readyPVC to include this agentCfg in its ownerRference so when a delete happens, we know other agentCfg is still using this pvc
	if _, exist := containsOwner(readyPVC.OwnerReferences, agentCfg); !exist {
		err := controllerutil.SetOwnerReference(&agentCfg.AgentConfig, readyPVC, r.Scheme)
		if err != nil {
			return false, fmt.Errorf("failed to set owner reference: %w", err)
		}
		err = r.Update(ctx, readyPVC)
		if err != nil {
			return false, err
		}
		return true, nil

	}

	var shouldUpdateStatus bool
	if agentCfg.Status.Phase == porterv1.PhasePending {
		// update the agentCfg status to be ready
		agentCfg.Status.Phase = porterv1.PhaseSucceeded
		shouldUpdateStatus = true
	}
	if !agentCfg.Status.Ready {
		agentCfg.Status.Ready = true
		shouldUpdateStatus = true
	}
	if shouldUpdateStatus {
		if err := r.saveStatus(ctx, log, agentCfg); err != nil {
			return false, err
		}
		return true, nil
	}

	return false, nil
}

func containsOwner(owners []metav1.OwnerReference, agentCfg *porterv1.AgentConfigAdapter) (int, bool) {
	for i, owner := range owners {
		if owner.APIVersion == agentCfg.APIVersion && owner.Kind == agentCfg.Kind && owner.Name == agentCfg.Name {
			return i, true
		}

	}
	return -1, false
}

func removeOwnerAtIdx(s []metav1.OwnerReference, index int) []metav1.OwnerReference {
	ret := make([]metav1.OwnerReference, 0)
	ret = append(ret, s[:index]...)
	return append(ret, s[index+1:]...)
}

func (r *AgentConfigReconciler) syncPluginInstallStatus(ctx context.Context, log logr.Logger, agentCfg *porterv1.AgentConfigAdapter) (bool, error) {
	if agentCfg.Spec.Plugins.IsZero() && !agentCfg.Status.Ready {
		agentCfg.Status.Ready = true
		err := r.saveStatus(ctx, log, agentCfg)
		return true, err
	}

	readyPVC, tempPVC, err := r.getExistingPluginPVCs(ctx, log, agentCfg)
	if err != nil {
		return false, err
	}
	// Check to see if there is a plugin volume already has all the defined plugins installed
	pluginReady, agentCfgReady := checkPluginAndAgentReadiness(agentCfg, readyPVC, tempPVC)
	log.V(Log4Debug).Info("Existing volume and agent Status", "volume status", pluginReady, "agent status", agentCfgReady)
	if pluginReady && agentCfgReady {

		updated, err := r.updateOwnerReference(ctx, log, agentCfg, readyPVC)
		if err != nil {
			return false, err
		}

		if updated {
			return true, nil
		}
	}

	// if plugin is not ready, we just need to wait for it before we move forward
	if agentCfgReady {
		return true, nil
	}

	return false, nil
}

func (r *AgentConfigReconciler) renamePluginVolume(ctx context.Context, log logr.Logger, action *porterv1.AgentAction, agentCfg *porterv1.AgentConfigAdapter) error {
	// if the plugin install action is not finished, we need to wait for it before acting further
	if !apimeta.IsStatusConditionTrue(action.Status.Conditions, string(porterv1.ConditionComplete)) && action.Status.Phase != porterv1.PhaseSucceeded {
		log.V(Log4Debug).Info("Plugins is not ready yet.", "action status", action.Status)
		return nil
	}

	log.V(Log4Debug).Info("Renaming temporary persistent volume claim.", "new persistentvolumeclaim", agentCfg.GetPluginsPVCName())

	readyPVC, tempPVC, err := r.getExistingPluginPVCs(ctx, log, agentCfg)
	if err != nil {
		return err
	}
	// delete the temporary pvc first once the plugin install action has been completed
	shouldRenameTmpPVC := tempPVC != nil && readyPVC == nil
	if shouldRenameTmpPVC {
		updated, err := r.bindPVWithPluginPVC(ctx, log, tempPVC, agentCfg)
		if err != nil {
			return err
		}
		if updated {
			return nil
		}
		return r.deleteTemporaryPVC(ctx, log, tempPVC, agentCfg)
	}

	if tempPVC == nil {
		hashedPVCName := agentCfg.GetPluginsPVCName()
		// create the pvc with the hash of plugins metadata
		_, err = r.createHashPVC(ctx, log, agentCfg)
		if err != nil {
			return err
		}

		log.V(Log4Debug).Info("Created the new PVC with plugins hash as its name.", "new persistentvolumeclaim", hashedPVCName)
	}

	return nil
}
