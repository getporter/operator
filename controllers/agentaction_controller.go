package controllers

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	porterv1 "get.porter.sh/operator/api/v1"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:rbac:groups=porter.sh,resources=agentconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=porter.sh,resources=porterconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=porter.sh,resources=agentactions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=porter.sh,resources=agentactions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=porter.sh,resources=agentactions/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

type AgentActionReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *AgentActionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&porterv1.AgentAction{}, builder.WithPredicates(resourceChanged{})).
		Owns(&batchv1.Job{}).
		Complete(r)
}

// Reconcile is called when the spec of an AgentAction is changed
// or a job associated with an agent is updated.
// Either schedule a job to handle a spec change, or update the AgentAction status in response to the job's state.
func (r *AgentActionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("agentaction", req.Name, "namespace", req.Namespace)

	// Retrieve the action
	action := &porterv1.AgentAction{}
	err := r.Get(ctx, req.NamespacedName, action)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{Requeue: false}, err
	}

	log = log.WithValues("resourceVersion", action.ResourceVersion, "generation", action.Generation, "observedGeneration", action.Status.ObservedGeneration)

	if action.Generation != action.Status.ObservedGeneration {
		log.V(Log5Trace).Info("Reconciling agent action because its spec changed")
	} else {
		log.V(Log5Trace).Info("Reconciling agent action")
	}

	// Check if we have scheduled a job for this change yet
	job, handled, err := r.isHandled(ctx, log, action)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Sync the installation status from the job
	if err = r.syncStatus(ctx, log, action, job); err != nil {
		return ctrl.Result{}, err
	}

	// Check if we have already handled any spec changes
	if handled {
		// Nothing for us to do at this point
		log.V(Log4Debug).Info("Reconciliation complete: A porter agent has already been dispatched.")
		return ctrl.Result{}, nil
	}

	// Run a porter agent
	err = r.runPorter(ctx, log, action)
	if err != nil {
		return ctrl.Result{}, err
	}

	log.V(Log4Debug).Info("Reconciliation complete: A porter agent has been dispatched.")
	return ctrl.Result{}, nil
}

// Determines if this generation of the AgentAction has being processed by Porter.
func (r *AgentActionReconciler) isHandled(ctx context.Context, log logr.Logger, action *porterv1.AgentAction) (*batchv1.Job, bool, error) {
	// Retrieve the Job running the porter action
	// Only query by generation, not revision, since rev can be bumped when the status is updated, or a label changed
	jobLabels := r.getAgentJobLabels(action)

	results := batchv1.JobList{}
	err := r.List(ctx, &results, client.InNamespace(action.Namespace), client.MatchingLabels(jobLabels))
	if err != nil {
		return nil, false, errors.Wrapf(err, "could not query for active porter jobs")
	}

	if len(results.Items) == 0 {
		log.V(Log4Debug).Info("No existing job was found")
		return nil, false, nil
	}

	job := results.Items[0]
	log.V(Log4Debug).Info("Found existing job", "job", job.Name)
	return &job, true, nil
}

// Check the status of the porter-agent job and use that to update the AgentAction status
func (r *AgentActionReconciler) syncStatus(ctx context.Context, log logr.Logger, action *porterv1.AgentAction, job *batchv1.Job) error {
	origStatus := action.Status

	r.applyJobToStatus(log, action, job)

	if !reflect.DeepEqual(origStatus, action.Status) {
		return r.saveStatus(ctx, log, action)
	}

	return nil
}

// Only update the status with a PATCH, don't clobber the entire resource
func (r *AgentActionReconciler) saveStatus(ctx context.Context, log logr.Logger, action *porterv1.AgentAction) error {
	log.V(Log5Trace).Info("Patching agent action status")
	return PatchObjectWithRetry(ctx, log, r.Client, r.Client.Status().Patch, action, func() client.Object {
		return &porterv1.AgentAction{}
	})
}

// Takes a job and uses it to calculate the new status for the agent action
// Returns whether or not any changes were made
func (r *AgentActionReconciler) applyJobToStatus(log logr.Logger, action *porterv1.AgentAction, job *batchv1.Job) {
	// Recalculate all conditions based on what we currently observe
	action.Status.ObservedGeneration = action.Generation
	action.Status.Phase = porterv1.PhaseUnknown

	if job == nil {
		action.Status.Job = nil
		action.Status.Conditions = nil
		log.V(Log5Trace).Info("Cleared status because there is no current job")
	} else {
		action.Status.Job = &corev1.LocalObjectReference{Name: job.Name}
		setCondition(log, action, porterv1.ConditionScheduled, "JobCreated")
		action.Status.Phase = porterv1.PhasePending

		if job.Status.Active+job.Status.Failed+job.Status.Succeeded > 0 {
			action.Status.Phase = porterv1.PhaseRunning
			setCondition(log, action, porterv1.ConditionStarted, "JobStarted")
		}

		for _, condition := range job.Status.Conditions {
			switch condition.Type {
			case batchv1.JobComplete:
				action.Status.Phase = porterv1.PhaseSucceeded
				setCondition(log, action, porterv1.ConditionComplete, "JobCompleted")
				break
			case batchv1.JobFailed:
				action.Status.Phase = porterv1.PhaseFailed
				setCondition(log, action, porterv1.ConditionFailed, "JobFailed")
				break
			}
		}
	}
}

// Create a job that runs the specified porter command in a job
func (r *AgentActionReconciler) runPorter(ctx context.Context, log logr.Logger, action *porterv1.AgentAction) error {
	log.V(Log5Trace).Info("Porter agent requested", "namespace", action.Namespace, "action", action.Name)

	agentCfg, err := r.resolveAgentConfig(ctx, log, action)
	if err != nil {
		return err
	}

	porterCfg, err := r.resolvePorterConfig(ctx, log, action)
	if err != nil {
		return err
	}

	pvc, err := r.createAgentVolume(ctx, log, action, agentCfg)
	if err != nil {
		return err
	}

	configSecret, err := r.createConfigSecret(ctx, log, action, porterCfg)
	if err != nil {
		return err
	}

	workdirSecret, err := r.createWorkdirSecret(ctx, log, action)
	if err != nil {
		return err
	}

	_, err = r.createAgentJob(ctx, log, action, agentCfg, pvc, configSecret, workdirSecret)
	if err != nil {
		return err
	}

	return nil
}

// get the labels that are used to match agent resources, merging custom labels defined on the action.
func (r *AgentActionReconciler) getSharedAgentLabels(action *porterv1.AgentAction) map[string]string {
	labels := map[string]string{
		porterv1.LabelManaged:            "true",
		porterv1.LabelResourceKind:       action.TypeMeta.Kind,
		porterv1.LabelResourceName:       action.Name,
		porterv1.LabelResourceGeneration: fmt.Sprintf("%d", action.Generation),
		porterv1.LabelRetry:              action.GetRetryLabelValue(),
	}
	for k, v := range action.Labels {
		// if the action has labels that conflict with existing labels, ignore them
		if _, ok := labels[k]; ok {
			continue
		}
		labels[k] = v
	}
	return labels
}

func (r *AgentActionReconciler) createAgentVolume(ctx context.Context, log logr.Logger, action *porterv1.AgentAction, agentCfg porterv1.AgentConfigSpec) (*corev1.PersistentVolumeClaim, error) {
	labels := r.getSharedAgentLabels(action)

	var results corev1.PersistentVolumeClaimList
	if err := r.List(ctx, &results, client.MatchingLabels(labels)); err != nil {
		return nil, errors.Wrap(err, "error checking for an existing agent volume (pvc)")
	}
	if len(results.Items) > 0 {
		return &results.Items[0], nil
	}

	// Create a volume to share data between porter and the invocation image
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: action.Name + "-",
			Namespace:    action.Namespace,
			Labels:       labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.ResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: agentCfg.GetVolumeSize(),
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

// creates a secret for the porter configuration directory
func (r *AgentActionReconciler) createConfigSecret(ctx context.Context, log logr.Logger, action *porterv1.AgentAction, porterCfg porterv1.PorterConfigSpec) (*corev1.Secret, error) {
	labels := r.getSharedAgentLabels(action)
	labels[porterv1.LabelSecretType] = porterv1.SecretTypeConfig

	var results corev1.SecretList
	if err := r.List(ctx, &results, client.MatchingLabels(labels)); err != nil {
		return nil, errors.Wrap(err, "error checking for a existing config secret")
	}

	if len(results.Items) > 0 {
		return &results.Items[0], nil
	}

	// Create a secret with all the files that should be copied into the porter config directory
	// * porter config file (~/.porter/config.json)
	porterCfgB, err := porterCfg.ToPorterDocument()
	if err != nil {
		return nil, errors.Wrap(err, "error marshaling the porter config.json file")
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: action.Name + "-",
			Namespace:    action.Namespace,
			Labels:       labels,
		},
		Type:      corev1.SecretTypeOpaque,
		Immutable: pointer.BoolPtr(true),
		Data: map[string][]byte{
			"config.yaml": porterCfgB,
		},
	}

	if err = r.Create(ctx, secret); err != nil {
		return nil, errors.Wrap(err, "error creating the porter config secret")
	}

	log.V(Log4Debug).Info("Created secret for the porter config", "name", secret.Name)
	return secret, nil
}

// creates a secret for the porter configuration directory
func (r *AgentActionReconciler) createWorkdirSecret(ctx context.Context, log logr.Logger, action *porterv1.AgentAction) (*corev1.Secret, error) {
	labels := r.getSharedAgentLabels(action)
	labels[porterv1.LabelSecretType] = porterv1.SecretTypeWorkdir

	var results corev1.SecretList
	if err := r.List(ctx, &results, client.MatchingLabels(labels)); err != nil {
		return nil, errors.Wrap(err, "error checking for a existing workdir secret")
	}

	if len(results.Items) > 0 {
		return &results.Items[0], nil
	}

	// Create a secret with all the files that should be copied into the agent's working directory
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: action.Name + "-",
			Namespace:    action.Namespace,
			Labels:       labels,
		},
		Type:      corev1.SecretTypeOpaque,
		Immutable: pointer.BoolPtr(true),
		Data:      action.Spec.Files,
	}

	if err := r.Create(ctx, secret); err != nil {
		return nil, errors.Wrap(err, "error creating the porter workdir secret")
	}

	log.V(Log4Debug).Info("Created secret for the porter workdir", "name", secret.Name)
	return secret, nil
}

func (r *AgentActionReconciler) getAgentJobLabels(action *porterv1.AgentAction) map[string]string {
	labels := r.getSharedAgentLabels(action)
	labels[porterv1.LabelJobType] = porterv1.JobTypeAgent
	return labels
}

func (r *AgentActionReconciler) createAgentJob(ctx context.Context, log logr.Logger,
	action *porterv1.AgentAction, agentCfg porterv1.AgentConfigSpec,
	pvc *corev1.PersistentVolumeClaim, configSecret *corev1.Secret, workdirSecret *corev1.Secret) (batchv1.Job, error) {

	// not checking for an existing job because that happens earlier during reconcile

	labels := r.getAgentJobLabels(action)
	env, envFrom := r.getAgentEnv(action, agentCfg, pvc)
	volumes, volumeMounts := r.getAgentVolumes(action, pvc, configSecret, workdirSecret)

	porterJob := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: action.Name + "-",
			Namespace:    action.Namespace,
			Labels:       labels,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         action.APIVersion,
					Kind:               action.Kind,
					Name:               action.Name,
					UID:                action.UID,
					Controller:         pointer.BoolPtr(true),
					BlockOwnerDeletion: pointer.BoolPtr(true),
				},
			},
		},
		Spec: batchv1.JobSpec{
			Completions:  pointer.Int32Ptr(1),
			BackoffLimit: pointer.Int32Ptr(0),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: action.Name + "-",
					Namespace:    action.Namespace,
					Labels:       labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "porter-agent",
							Image:           agentCfg.GetPorterImage(),
							ImagePullPolicy: agentCfg.GetPullPolicy(),
							Command:         action.Spec.Command,
							Args:            action.Spec.Args,
							Env:             env,
							EnvFrom:         envFrom,
							VolumeMounts:    volumeMounts,
							WorkingDir:      "/porter-workdir",
						},
					},
					Volumes:            volumes,
					RestartPolicy:      "Never", // TODO: Make the retry policy configurable on the Installation
					ServiceAccountName: agentCfg.ServiceAccount,
					ImagePullSecrets:   nil, // TODO: Make pulling from a private registry possible
					SecurityContext: &corev1.PodSecurityContext{
						// Run as the well-known nonroot user that Porter uses for the invocation image and the agent
						RunAsUser: pointer.Int64Ptr(65532),
						// Porter builds the bundles with the root group having the same permissions as the owner
						// So make sure that we are running as the root group
						RunAsGroup: pointer.Int64Ptr(0),
						FSGroup:    pointer.Int64Ptr(0),
					},
				},
			},
		},
	}

	if err := r.Create(ctx, &porterJob); err != nil {
		return batchv1.Job{}, errors.Wrap(err, "error creating Porter agent job")
	}

	log.V(Log4Debug).Info("Created Job for the Porter agent", "name", porterJob.Name)
	return porterJob, nil
}

func (r *AgentActionReconciler) resolveAgentConfig(ctx context.Context, log logr.Logger, action *porterv1.AgentAction) (porterv1.AgentConfigSpec, error) {
	log.V(Log5Trace).Info("Resolving porter agent configuration")

	logConfig := func(level string, config *porterv1.AgentConfig) {
		if config == nil || config.Name == "" {
			return
		}

		log.V(Log4Debug).Info("Found porter agent configuration",
			"level", level,
			"namespace", config.Namespace,
			"name", config.Name)
	}

	// Read agent configuration defined at the system level
	systemCfg := &porterv1.AgentConfig{}
	err := r.Get(ctx, types.NamespacedName{Name: "default", Namespace: operatorNamespace}, systemCfg)
	if err != nil && !apierrors.IsNotFound(err) {
		return porterv1.AgentConfigSpec{}, errors.Wrap(err, "cannot retrieve system level porter agent configuration")
	}
	logConfig("system", systemCfg)

	// Read agent configuration defined at the namespace level
	nsCfg := &porterv1.AgentConfig{}
	err = r.Get(ctx, types.NamespacedName{Name: "default", Namespace: action.Namespace}, nsCfg)
	if err != nil && !apierrors.IsNotFound(err) {
		return porterv1.AgentConfigSpec{}, errors.Wrap(err, "cannot retrieve namespace level porter agent configuration")
	}
	logConfig("namespace", nsCfg)

	// Read agent configuration override
	instCfg := &porterv1.AgentConfig{}
	if action.Spec.AgentConfig != nil {
		err = r.Get(ctx, types.NamespacedName{Name: action.Spec.AgentConfig.Name, Namespace: action.Namespace}, instCfg)
		if err != nil && !apierrors.IsNotFound(err) {
			return porterv1.AgentConfigSpec{}, errors.Wrapf(err, "cannot retrieve agent configuration %s specified by the agent action", action.Spec.AgentConfig.Name)
		}
		logConfig("instance", instCfg)
	}

	// Apply overrides
	base := &systemCfg.Spec
	cfg, err := base.MergeConfig(nsCfg.Spec, instCfg.Spec)
	if err != nil {
		return porterv1.AgentConfigSpec{}, err
	}

	log.V(Log4Debug).Info("resolved porter agent configuration",
		"porterImage", cfg.GetPorterImage(),
		"pullPolicy", cfg.GetPullPolicy(),
		"serviceAccount", cfg.ServiceAccount,
		"volumeSize", cfg.GetVolumeSize(),
		"installationServiceAccount", cfg.InstallationServiceAccount,
	)
	return cfg, nil
}

func (r *AgentActionReconciler) resolvePorterConfig(ctx context.Context, log logr.Logger, action *porterv1.AgentAction) (porterv1.PorterConfigSpec, error) {
	log.V(Log5Trace).Info("Resolving porter configuration file")

	logConfig := func(level string, config *porterv1.PorterConfig) {
		if config == nil || config.Name == "" {
			return
		}
		log.V(Log4Debug).Info("Found porter config",
			"level", level,
			"namespace", config.Namespace,
			"name", config.Name)
	}

	// Provide a safe default config in case nothing is defined anywhere
	defaultCfg := porterv1.PorterConfigSpec{
		DefaultStorage:       pointer.StringPtr("in-cluster-mongodb"),
		DefaultSecretsPlugin: pointer.StringPtr("kubernetes.secrets"),
		Storage: []porterv1.StorageConfig{
			{PluginConfig: porterv1.PluginConfig{
				Name:         "in-cluster-mongodb",
				PluginSubKey: "mongodb",
				Config:       runtime.RawExtension{Raw: []byte(`{"url":"mongodb://mongodb.porter-operator-system.svc.cluster.local"}`)},
			}},
		},
	}

	// Read agent configuration defined at the system level
	systemCfg := &porterv1.PorterConfig{}
	err := r.Get(ctx, types.NamespacedName{Name: "default", Namespace: operatorNamespace}, systemCfg)
	if err != nil && !apierrors.IsNotFound(err) {
		return porterv1.PorterConfigSpec{}, errors.Wrap(err, "cannot retrieve system level porter agent configuration")
	}
	logConfig("system", systemCfg)

	// Read agent configuration defined at the namespace level
	nsCfg := &porterv1.PorterConfig{}
	err = r.Get(ctx, types.NamespacedName{Name: "default", Namespace: action.Namespace}, nsCfg)
	if err != nil && !apierrors.IsNotFound(err) {
		return porterv1.PorterConfigSpec{}, errors.Wrap(err, "cannot retrieve namespace level porter agent configuration")
	}
	logConfig("namespace", nsCfg)

	// Read agent configuration defines on the installation
	instCfg := &porterv1.PorterConfig{}
	if action.Spec.PorterConfig != nil {
		err = r.Get(ctx, types.NamespacedName{Name: action.Spec.PorterConfig.Name, Namespace: action.Namespace}, instCfg)
		if err != nil && !apierrors.IsNotFound(err) {
			return porterv1.PorterConfigSpec{}, errors.Wrapf(err, "cannot retrieve agent configuration %s specified by the agent action", action.Spec.AgentConfig.Name)
		}
		logConfig("instance", instCfg)
	}

	// Resolve final configuration
	// We don't log the final config because we haven't yet added the feature to enable not having sensitive data in porter's config files
	base := &defaultCfg
	cfg, err := base.MergeConfig(systemCfg.Spec, nsCfg.Spec, instCfg.Spec)
	if err != nil {
		return porterv1.PorterConfigSpec{}, err
	}

	return cfg, nil
}

func (r *AgentActionReconciler) getAgentEnv(action *porterv1.AgentAction, agentCfg porterv1.AgentConfigSpec, pvc *corev1.PersistentVolumeClaim) ([]corev1.EnvVar, []corev1.EnvFromSource) {
	sharedLabels := r.getSharedAgentLabels(action)

	env := []corev1.EnvVar{
		{
			Name:  "PORTER_RUNTIME_DRIVER",
			Value: "kubernetes",
		},
		// Configuration for the Kubernetes Driver
		{
			Name:  "KUBE_NAMESPACE",
			Value: action.Namespace,
		},
		{
			Name:  "IN_CLUSTER",
			Value: "true",
		},
		{
			Name:  "LABELS",
			Value: r.getFormattedInstallerLabels(sharedLabels),
		},
		{
			Name:  "JOB_VOLUME_NAME",
			Value: pvc.Name,
		},
		{
			Name:  "JOB_VOLUME_PATH",
			Value: "/porter-shared",
		},
		{
			Name:  "CLEANUP_JOBS",
			Value: "false",
		},
		{
			Name:  "SERVICE_ACCOUNT",
			Value: agentCfg.InstallationServiceAccount,
		},
		{
			Name:  "AFFINITY_MATCH_LABELS",
			Value: r.getFormattedAffinityLabels(action),
		},
	}

	for _, e := range action.Spec.Env {
		env = append(env, e)
	}

	envFrom := []corev1.EnvFromSource{
		// Environment variables for the plugins
		{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "porter-env",
				},
				Optional: pointer.BoolPtr(true),
			},
		},
	}

	for _, e := range action.Spec.EnvFrom {
		envFrom = append(envFrom, e)
	}

	return env, envFrom
}

func (r *AgentActionReconciler) getAgentVolumes(action *porterv1.AgentAction, pvc *corev1.PersistentVolumeClaim, configSecret *corev1.Secret, workdirSecret *corev1.Secret) ([]corev1.Volume, []corev1.VolumeMount) {
	volumes := []corev1.Volume{
		{
			Name: "porter-shared",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvc.Name,
				},
			},
		},
		{
			Name: "porter-config",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: configSecret.Name,
					Optional:   pointer.BoolPtr(false),
				},
			},
		},
		{
			Name: "porter-workdir",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: workdirSecret.Name,
					Optional:   pointer.BoolPtr(false),
				},
			},
		},
	}
	for _, volume := range action.Spec.Volumes {
		volumes = append(volumes, volume)
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "porter-shared",
			MountPath: "/porter-shared",
		},
		{
			Name:      "porter-config",
			MountPath: "/porter-config",
		},
		{
			Name:      "porter-workdir",
			MountPath: "/porter-workdir",
		},
	}
	for _, mount := range action.Spec.VolumeMounts {
		volumeMounts = append(volumeMounts, mount)
	}

	return volumes, volumeMounts
}

func (r *AgentActionReconciler) getFormattedInstallerLabels(labels map[string]string) string {
	// represent the shared labels that we are applying to all the things in a way that porter can accept on the command line
	// These labels are added to the invocation image and should be sorted consistently
	labels[porterv1.LabelJobType] = porterv1.JobTypeInstaller
	formattedLabels := make([]string, 0, len(labels))
	for k, v := range labels {
		formattedLabels = append(formattedLabels, fmt.Sprintf("%s=%s", k, v))
	}
	sort.Strings(formattedLabels)
	return strings.Join(formattedLabels, " ")
}

func (r *AgentActionReconciler) getFormattedAffinityLabels(action *porterv1.AgentAction) string {
	// These labels are used by the kubernetes driver to ensure that the invocation image is scheduled
	// on the same node as the agent
	return fmt.Sprintf("%s=%s %s=%s %s=%d %s=%s",
		porterv1.LabelResourceKind, action.Kind,
		porterv1.LabelResourceName, action.Name,
		porterv1.LabelResourceGeneration, action.Generation,
		porterv1.LabelRetry, action.GetRetryLabelValue())
}

func setCondition(log logr.Logger, action *porterv1.AgentAction, condType porterv1.AgentConditionType, reason string) bool {
	if apimeta.IsStatusConditionTrue(action.Status.Conditions, string(condType)) {
		return false
	}

	log.V(Log4Debug).Info("Setting condition", "condition", condType, "reason", reason)
	apimeta.SetStatusCondition(&action.Status.Conditions, metav1.Condition{
		Type:               string(condType),
		Reason:             reason,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: action.Generation,
	})
	return true
}
