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
)

// AgentConfigReconciler calls porter to execute changes made to an AgentConfig CRD
type AgentConfigReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

type runPorter func(ctx context.Context, log logr.Logger, pvc *corev1.PersistentVolumeClaim, agentCfg *porterv1.AgentConfig) error

// +kubebuilder:rbac:groups=porter.sh,resources=agentconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=porter.sh,resources=porterconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

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
			log.V(Log4Debug).Info("Reconciliation complete: A porter agent has already been dispatched.")
			return ctrl.Result{}, nil
		}

		// check if we can find the hash pvc that's bounded, if so, we can delete the tmp pvc
		hashPVC := getPluginHash(agentCfg)
		var tmpPVC string
		var shouldDeleteTmpPVC bool
		if action.Spec.Volumes[0].PersistentVolumeClaim.ClaimName == hashPVC {
			oldPVC, ok := agentCfg.Annotations["tmpPVC"]
			if !ok {
				return ctrl.Result{}, nil
			}
			key := client.ObjectKey{Namespace: agentCfg.Namespace, Name: hashPVC}
			newPVC := &corev1.PersistentVolumeClaim{}
			err = r.Get(ctx, key, newPVC)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return ctrl.Result{}, nil
				}
				return ctrl.Result{}, err
			}

			if newPVC.Status.Phase != corev1.ClaimBound {
				return ctrl.Result{}, nil
			}
			shouldDeleteTmpPVC = true
			tmpPVC = oldPVC
		} else {
			tmpPVC = action.Spec.Volumes[0].PersistentVolumeClaim.ClaimName
		}

		// rename the pvc to the hash of plugins metadata
		key := client.ObjectKey{Namespace: agentCfg.Namespace, Name: tmpPVC}
		tmpVolume := &corev1.PersistentVolumeClaim{}
		err = r.Get(ctx, key, tmpVolume)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}

		if tmpVolume.Status.Phase != corev1.ClaimBound {
			return ctrl.Result{}, nil
		}

		if shouldDeleteTmpPVC {
			if err := r.Delete(ctx, tmpVolume); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}

		_, err = r.renamePVC(ctx, tmpVolume, hashPVC)
		if err != nil {
			return ctrl.Result{}, err
		}

		if agentCfg.Annotations == nil {
			agentCfg.Annotations = make(map[string]string)
		}
		agentCfg.Annotations["tmpPVC"] = tmpVolume.Name

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

	switch len(results.Items) {
	case 0:
		log.V(Log4Debug).Info("No existing agent action was found")
		return nil, false, nil
	case 1:
		action := results.Items[0]
		log.V(Log4Debug).Info("Found existing agent action", "agentaction", action.Name, "namespace", action.Namespace)
		return &action, true, nil
	case 2:
		action := results.Items[1]
		log.V(Log4Debug).Info("Found existing agent action", "agentaction", action.Name, "namespace", action.Namespace)
		return &action, true, nil
	default:
		return nil, false, fmt.Errorf("more than 2 existing agent actions found")
	}
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

func (r *AgentConfigReconciler) runPorterPluginList(ctx context.Context, log logr.Logger, pvc *corev1.PersistentVolumeClaim, agentCfg *porterv1.AgentConfig) error {
	action, err := r.createAgentAction(ctx, log, pvc, agentCfg, []string{"plugins", "list"})
	if err != nil {
		return err
	}

	// Update the agent config Status with the agent action
	return r.syncStatus(ctx, log, agentCfg, action)
}

// Trigger an agent
func (r *AgentConfigReconciler) runPorterPluginInstall(ctx context.Context, log logr.Logger, pvc *corev1.PersistentVolumeClaim, agentCfg *porterv1.AgentConfig) error {
	action, err := r.createAgentAction(ctx, log, pvc, agentCfg, []string{"plugins", "install"})
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
	oldPvc *corev1.PersistentVolumeClaim,
	new string,
) (*corev1.PersistentVolumeClaim, error) {
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
		return nil, err
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
