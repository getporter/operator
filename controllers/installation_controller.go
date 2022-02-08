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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	labelJobType            = porterv1.Prefix + "jobType"
	jobTypeAgent            = "porter-agent"
	jobTypeInstaller        = "bundle-installer"
	labelManaged            = porterv1.Prefix + "managed"
	labelResourceKind       = porterv1.Prefix + "resourceKind"
	labelResourceName       = porterv1.Prefix + "resourceName"
	labelResourceVersion    = porterv1.Prefix + "resourceVersion"
	labelResourceGeneration = porterv1.Prefix + "resourceGeneration"
	labelRetry              = porterv1.Prefix + "retry"
	operatorNamespace       = "porter-operator-system"
	finalizerName           = porterv1.Prefix + "finalizer"
)

// InstallationReconciler calls porter to execute changes made to an Installation CRD
type InstallationReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=porter.sh,resources=agentconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=porter.sh,resources=porterconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=porter.sh,resources=installations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=porter.sh,resources=installations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=porter.sh,resources=installations/finalizers,verbs=update;patch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

// SetupWithManager sets up the controller with the Manager.
func (r *InstallationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// We want reconcile called on an installation when the job that it spawned finishes
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &batchv1.Job{}, ".metadata.controller", getOwner); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&porterv1.Installation{}).
		WithEventFilter(installationChanged{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}

// For an object extracts the owner id as long as it's managed by this controller
func getOwner(rawObj client.Object) []string {
	owner := metav1.GetControllerOf(rawObj)
	if owner == nil {
		return nil
	}

	if owner.APIVersion != porterv1.GroupVersion.String() || owner.Kind != "Installation" {
		return nil
	}

	return []string{owner.Name}
}

type installationChanged struct {
	predicate.Funcs
}

// Determine if the spec or the finalizer was changed
// Allow forcing porter to run with the retry annotation
func (installationChanged) Update(e event.UpdateEvent) bool {
	if e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration() {
		return true
	}

	if !reflect.DeepEqual(e.ObjectNew.GetFinalizers(), e.ObjectOld.GetFinalizers()) {
		return true
	}

	if e.ObjectNew.GetAnnotations()[porterv1.AnnotationRetry] != e.ObjectOld.GetAnnotations()[porterv1.AnnotationRetry] {
		return true
	}

	return false
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
			log.V(Log4Debug).Info("Reconciliation complete: Installation CRD is deleted.")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{Requeue: false}, err
	}

	log.WithValues("resourceVersion", inst.ResourceVersion, "generation", inst.Generation)
	log.V(Log5Trace).Info("Reconciling installation")

	// Check if we have scheduled a job for this change yet
	job, handled, err := r.isHandled(ctx, log, inst)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Sync the installation status from the job
	if err = r.syncStatus(ctx, log, inst, job); err != nil {
		return ctrl.Result{}, err
	}

	// Check if we have finished uninstalling
	if isDeleted(inst) && apimeta.IsStatusConditionTrue(inst.Status.Conditions, string(porterv1.ConditionComplete)) {
		err = r.removeFinalizer(ctx, log, inst)
		log.V(Log4Debug).Info("Reconciliation complete: Finalizer has been removed from the Installation.")
		return ctrl.Result{}, err
	}

	// Check if we have already handled any spec changes
	if handled {
		// Nothing for us to do at this point
		log.V(Log4Debug).Info("Reconciliation complete: A porter agent has already been dispatched.")
		return ctrl.Result{}, nil
	}

	// Should we uninstall the bundle?
	if shouldUninstall(inst) {
		err = r.uninstallInstallation(ctx, log, inst)
		log.V(Log4Debug).Info("Reconciliation complete: A porter agent has been dispatched to uninstall the installation.")
		return ctrl.Result{}, err
	} else if isDeleted(inst) {
		// This is installation without a finalizer that was deleted
		// We remove the finalizer after we successfully uninstall (or someone is manually cleaning things up)
		// Just let it go
		log.V(Log4Debug).Info("Reconciliation complete: Installation CRD is ready for deletion.")
		return ctrl.Result{}, nil
	}

	// Ensure non-deleted installations have finalizers
	updated, err := r.ensureFinalizerSet(ctx, inst)
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
func (r *InstallationReconciler) isHandled(ctx context.Context, log logr.Logger, inst *porterv1.Installation) (*batchv1.Job, bool, error) {
	// Retrieve the Job running the porter action
	// Only query by generation, not revision, since rev can be bumped when the status is updated, or a label changed
	jobLabels := getAgentJobLabels(inst)
	delete(jobLabels, labelResourceVersion) // resource version will vary betwen reconcile runs, don't use it to match jobs. We may want to stop using that label entirely

	results := batchv1.JobList{}
	err := r.List(ctx, &results, client.InNamespace(inst.Namespace), client.MatchingLabels(jobLabels))
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

// Create a job that runs `porter installation apply`
func (r *InstallationReconciler) applyInstallation(ctx context.Context, log logr.Logger, inst *porterv1.Installation) error {
	log.V(Log5Trace).Info("Initializing installation status")
	inst.Status.Initialize()
	if err := r.saveStatus(ctx, log, inst); err != nil {
		return err
	}

	return r.runPorter(ctx, log, inst, "installation", "apply", "/porter-config/installation.yaml")
}

// Create a job that runs `porter uninstall`
func (r *InstallationReconciler) uninstallInstallation(ctx context.Context, log logr.Logger, inst *porterv1.Installation) error {
	log.V(Log5Trace).Info("Initializing installation status")
	inst.Status.Initialize()
	if err := r.saveStatus(ctx, log, inst); err != nil {
		return err
	}

	// Mark the document for deletion before giving it to Porter
	log.V(Log5Trace).Info("Setting uninstalled=true to uninstall the bundle")
	inst.Spec.Uninstalled = true

	return r.runPorter(ctx, log, inst, "installation", "apply", "/porter-config/installation.yaml")
}

// Create a job that runs the specified porter command in a job
func (r *InstallationReconciler) runPorter(ctx context.Context, log logr.Logger, inst *porterv1.Installation, porterCommand ...string) error {
	log.V(Log5Trace).Info("Porter agent requested", "command", strings.Join(porterCommand, " "))

	agentCfg, err := r.resolveAgentConfig(ctx, log, inst)
	if err != nil {
		return err
	}

	porterCfg, err := r.resolvePorterConfig(ctx, log, inst)
	if err != nil {
		return err
	}

	pvc, err := r.createAgentVolume(ctx, log, inst, agentCfg)
	if err != nil {
		return err
	}

	secret, err := r.createAgentSecret(ctx, log, inst, porterCfg)
	if err != nil {
		return err
	}

	job, err := r.createAgentJob(ctx, log, porterCommand, inst, agentCfg, pvc, secret)
	if err != nil {
		return err
	}

	return r.syncStatus(ctx, log, inst, &job)
}

func getSharedAgentLabels(inst *porterv1.Installation) map[string]string {
	return map[string]string{
		labelManaged:            "true",
		labelResourceKind:       "Installation",
		labelResourceName:       inst.Name,
		labelResourceVersion:    inst.ResourceVersion,
		labelResourceGeneration: fmt.Sprintf("%d", inst.Generation),
		labelRetry:              inst.GetRetryLabelValue(),
	}
}

// get the labels that should be applied to the porter agent job
func getAgentJobLabels(inst *porterv1.Installation) map[string]string {
	labels := getSharedAgentLabels(inst)
	labels[labelJobType] = jobTypeAgent
	return labels
}

// get the labels that should be applied to the installer (invocation image)
func getInstallerJobLabels(inst *porterv1.Installation) map[string]string {
	labels := getSharedAgentLabels(inst)
	labels[labelJobType] = jobTypeInstaller
	return labels
}

func (r *InstallationReconciler) createAgentVolume(ctx context.Context, log logr.Logger, inst *porterv1.Installation, agentCfg porterv1.AgentConfigSpec) (corev1.PersistentVolumeClaim, error) {
	sharedLabels := getSharedAgentLabels(inst)

	var results corev1.PersistentVolumeClaimList
	if err := r.List(ctx, &results, client.MatchingLabels(sharedLabels)); err != nil {
		return corev1.PersistentVolumeClaim{}, errors.Wrap(err, "error checking for an existing agent volume (pvc)")
	}
	if len(results.Items) > 0 {
		return results.Items[0], nil
	}

	// Create a volume to share data between porter and the invocation image
	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: getNamePrefix(inst),
			Namespace:    inst.Namespace,
			Labels:       sharedLabels,
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

	if err := r.Create(ctx, &pvc); err != nil {
		return corev1.PersistentVolumeClaim{}, errors.Wrap(err, "error creating the agent volume (pvc)")
	}

	log.V(Log4Debug).Info("Created PersistentVolumeClaim for the Porter agent", "name", pvc.Name)
	return pvc, nil
}

func (r *InstallationReconciler) createAgentSecret(ctx context.Context, log logr.Logger, inst *porterv1.Installation, porterCfg porterv1.PorterConfigSpec) (corev1.Secret, error) {
	sharedLabels := getSharedAgentLabels(inst)

	var results corev1.SecretList
	if err := r.List(ctx, &results, client.MatchingLabels(sharedLabels)); err != nil {
		return corev1.Secret{}, errors.Wrap(err, "error checking for a existing agent secret")
	}
	if len(results.Items) > 0 {
		return results.Items[0], nil
	}

	// Create a secret with all the files that should be copied into the agent
	// * porter config file (~/.porter/config.json)
	// * installation.yaml that we will pass to the command
	porterCfgB, err := porterCfg.ToPorterDocument()
	if err != nil {
		return corev1.Secret{}, errors.Wrap(err, "error marshaling the porter config.json file")
	}

	installationResourceB, err := inst.Spec.ToPorterDocument()
	if err != nil {
		return corev1.Secret{}, err
	}
	log.V(Log4Debug).Info("installation document", "installation.yaml", string(installationResourceB))

	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: getNamePrefix(inst),
			Namespace:    inst.Namespace,
			Labels:       sharedLabels,
		},
		Type:      corev1.SecretTypeOpaque,
		Immutable: pointer.BoolPtr(true),
		Data: map[string][]byte{
			"config.yaml":       porterCfgB,
			"installation.yaml": installationResourceB,
		},
	}

	if err = r.Create(ctx, &secret); err != nil {
		return corev1.Secret{}, errors.Wrap(err, "error creating the agent secret")
	}

	log.V(Log4Debug).Info("Created Secret for the Porter agent", "name", secret.Name)
	return secret, nil
}

func (r *InstallationReconciler) createAgentJob(ctx context.Context, log logr.Logger, porterCommand []string, inst *porterv1.Installation, agentCfg porterv1.AgentConfigSpec, pvc corev1.PersistentVolumeClaim, secret corev1.Secret) (batchv1.Job, error) {
	sharedLabels := getSharedAgentLabels(inst)

	// not checking for a job because that happens earlier during reconcile

	// represent the shared labels that we are applying to all the things in a way that porter can accept on the command line
	// These labels are added to the invocation image and should be sorted consistently
	installerLabels := getInstallerJobLabels(inst)
	sortedInstallerLabels := make([]string, 0, len(installerLabels))
	for k, v := range installerLabels {
		sortedInstallerLabels = append(sortedInstallerLabels, fmt.Sprintf("%s=%s", k, v))
	}
	sort.Strings(sortedInstallerLabels)

	porterJob := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: getNamePrefix(inst),
			Namespace:    inst.Namespace,
			Labels:       getAgentJobLabels(inst),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         inst.APIVersion,
					Kind:               inst.Kind,
					Name:               inst.Name,
					UID:                inst.UID,
					BlockOwnerDeletion: pointer.BoolPtr(true),
				},
			},
		},
		Spec: batchv1.JobSpec{
			Completions:  pointer.Int32Ptr(1),
			BackoffLimit: pointer.Int32Ptr(0),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: getNamePrefix(inst),
					Namespace:    inst.Namespace,
					Labels:       sharedLabels,
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
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
									SecretName: secret.Name,
									Optional:   pointer.BoolPtr(false),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "porter-agent",
							Image:           agentCfg.GetPorterImage(),
							ImagePullPolicy: agentCfg.GetPullPolicy(),
							Args:            porterCommand,
							Env: []corev1.EnvVar{
								// Configuration for Porter
								{
									Name:  "PORTER_RUNTIME_DRIVER",
									Value: "kubernetes",
								},
								// Configuration for the Kubernetes Driver
								{
									Name:  "KUBE_NAMESPACE",
									Value: inst.Namespace,
								},
								{
									Name:  "IN_CLUSTER",
									Value: "true",
								},
								{
									Name:  "LABELS",
									Value: strings.Join(sortedInstallerLabels, " "),
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
									Name: "AFFINITY_MATCH_LABELS",
									Value: fmt.Sprintf("%s=Installation %s=%s %s=%d %s=%s",
										labelResourceKind, labelResourceName, inst.Name, labelResourceGeneration, inst.Generation, labelRetry, inst.GetRetryLabelValue()),
								},
							},
							EnvFrom: []corev1.EnvFromSource{
								// Environment variables for the plugins
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "porter-env",
										},
										Optional: pointer.BoolPtr(true),
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "porter-shared",
									MountPath: "/porter-shared",
								},
								{
									Name:      "porter-config",
									MountPath: "/porter-config",
								},
							},
						},
					},
					RestartPolicy:      "Never", // TODO: Make the retry policy configurable on the Installation
					ServiceAccountName: agentCfg.ServiceAccount,
					ImagePullSecrets:   nil, // TODO: Make pulling from a private registry possible
					// Mount the volumes used by this pod as the nonroot user
					// Porter's agent doesn't run as root and won't have access to files on the volume
					// otherwise.
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup: pointer.Int64Ptr(65532),
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

func setCondition(log logr.Logger, inst *porterv1.Installation, condType porterv1.InstallationConditionType, reason string) bool {
	if apimeta.IsStatusConditionTrue(inst.Status.Conditions, string(condType)) {
		return false
	}

	log.V(Log4Debug).Info("Setting condition", "condition", condType, "reason", reason)
	apimeta.SetStatusCondition(&inst.Status.Conditions, metav1.Condition{
		Type:               string(condType),
		Reason:             reason,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: inst.Status.ObservedGeneration,
	})
	return true
}

func (r *InstallationReconciler) resolveAgentConfig(ctx context.Context, log logr.Logger, inst *porterv1.Installation) (porterv1.AgentConfigSpec, error) {
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
	err = r.Get(ctx, types.NamespacedName{Name: "default", Namespace: inst.Namespace}, nsCfg)
	if err != nil && !apierrors.IsNotFound(err) {
		return porterv1.AgentConfigSpec{}, errors.Wrap(err, "cannot retrieve namespace level porter agent configuration")
	}
	logConfig("namespace", nsCfg)

	// Read agent configuration defines on the installation
	instCfg := &porterv1.AgentConfig{}
	err = r.Get(ctx, types.NamespacedName{Name: inst.Spec.AgentConfig.Name, Namespace: inst.Namespace}, instCfg)
	if err != nil && !apierrors.IsNotFound(err) {
		return porterv1.AgentConfigSpec{}, errors.Wrapf(err, "cannot retrieve agent configuration %s specified by the installation", inst.Spec.AgentConfig.Name)
	}
	logConfig("instance", instCfg)

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

func (r *InstallationReconciler) resolvePorterConfig(ctx context.Context, log logr.Logger, inst *porterv1.Installation) (porterv1.PorterConfigSpec, error) {
	log.V(Log5Trace).Info(fmt.Sprintf("Resolving porter configuration file for %s", inst.Name))
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
	err = r.Get(ctx, types.NamespacedName{Name: "default", Namespace: inst.Namespace}, nsCfg)
	if err != nil && !apierrors.IsNotFound(err) {
		return porterv1.PorterConfigSpec{}, errors.Wrap(err, "cannot retrieve namespace level porter agent configuration")
	}
	logConfig("namespace", nsCfg)

	// Read agent configuration defines on the installation
	instCfg := &porterv1.PorterConfig{}
	err = r.Get(ctx, types.NamespacedName{Name: inst.Spec.PorterConfig.Name, Namespace: inst.Namespace}, instCfg)
	if err != nil && !apierrors.IsNotFound(err) {
		return porterv1.PorterConfigSpec{}, errors.Wrapf(err, "cannot retrieve agent configuration %s specified by the installation", inst.Spec.AgentConfig.Name)
	}
	logConfig("instance", instCfg)

	// Resolve final configuration
	// We don't log the final config because we haven't yet added the feature to enable not having sensitive data in porter's config files
	base := &defaultCfg
	cfg, err := base.MergeConfig(systemCfg.Spec, nsCfg.Spec, instCfg.Spec)
	if err != nil {
		return porterv1.PorterConfigSpec{}, err
	}

	return cfg, nil
}

// make sure that all CRDs, even ones made with old versions of the operator,
// have a finalizer set so that we can uninstall when the CRD is deleted.
func (r *InstallationReconciler) ensureFinalizerSet(ctx context.Context, inst *porterv1.Installation) (updated bool, err error) {
	// Ensure all Installations have a finalizer to we can uninstall when they are deleted
	if inst.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !isFinalizerSet(inst) {
			controllerutil.AddFinalizer(inst, finalizerName)
			return true, r.Update(ctx, inst)
		}
	}
	return false, nil
}

func (r *InstallationReconciler) removeFinalizer(ctx context.Context, log logr.Logger, inst *porterv1.Installation) error {
	log.V(Log5Trace).Info("removing finalizer")
	controllerutil.RemoveFinalizer(inst, finalizerName)
	return r.Update(ctx, inst)
}

// Check the status of the porter-agent job and use that to update the installation status
func (r *InstallationReconciler) syncStatus(ctx context.Context, log logr.Logger, inst *porterv1.Installation, job *batchv1.Job) error {
	origStatus := inst.Status

	applyJobToStatus(log, inst, job)

	if !reflect.DeepEqual(origStatus, inst.Status) {
		return r.saveStatus(ctx, log, inst)
	}

	return nil
}

// Takes a job and uses it to calculate the new status for an installation
// Returns whether or not any changes were made
func applyJobToStatus(log logr.Logger, inst *porterv1.Installation, job *batchv1.Job) {
	// Recalculate all conditions based on what we currently observe
	inst.Status.ObservedGeneration = inst.Generation
	inst.Status.Conditions = make([]metav1.Condition, 0, 4)

	if job == nil {
		inst.Status.Phase = porterv1.PhaseUnknown
		inst.Status.ActiveJob = nil
	}
	if job != nil {
		inst.Status.ActiveJob = &corev1.LocalObjectReference{Name: job.Name}
		setCondition(log, inst, porterv1.ConditionScheduled, "JobCreated")
		inst.Status.Phase = porterv1.PhasePending

		if job.Status.Active+job.Status.Failed+job.Status.Succeeded > 0 {
			inst.Status.Phase = porterv1.PhaseRunning
			setCondition(log, inst, porterv1.ConditionStarted, "JobStarted")
		}

		for _, condition := range job.Status.Conditions {
			switch condition.Type {
			case batchv1.JobComplete:
				inst.Status.Phase = porterv1.PhaseSucceeded
				inst.Status.ActiveJob = nil
				setCondition(log, inst, porterv1.ConditionComplete, "JobCompleted")
				break
			case batchv1.JobFailed:
				inst.Status.Phase = porterv1.PhaseFailed
				inst.Status.ActiveJob = nil
				setCondition(log, inst, porterv1.ConditionFailed, "JobFailed")
				break
			}
		}
	}
}

// Only update the status with a PATCH, don't clobber the entire installation
func (r *InstallationReconciler) saveStatus(ctx context.Context, log logr.Logger, inst *porterv1.Installation) error {
	key := client.ObjectKeyFromObject(inst)
	latest := &porterv1.Installation{}
	if err := r.Client.Get(ctx, key, latest); err != nil {
		return errors.Wrap(err, "could not get the latest installation definition")
	}

	log.V(Log5Trace).Info("Patching installation status")
	err := r.Client.Status().Patch(ctx, inst, client.MergeFrom(latest))
	return errors.Wrap(err, "failed to update the installation status")
}

func isFinalizerSet(inst *porterv1.Installation) bool {
	for _, finalizer := range inst.Finalizers {
		if finalizer == finalizerName {
			return true
		}
	}
	return false
}

func shouldUninstall(inst *porterv1.Installation) bool {
	// ignore a deleted CRD with no finalizers
	return isDeleted(inst) && isFinalizerSet(inst)
}

func isDeleted(inst *porterv1.Installation) bool {
	return inst.ObjectMeta.DeletionTimestamp.IsZero() == false
}

func getNamePrefix(inst *porterv1.Installation) string {
	// Limit how much of the name we use so that we have space for the
	// additional characters appended "-generation-resourceversion-random"
	maxNameLength := 45
	name := inst.Name
	if len(name) > maxNameLength {
		name = name[:maxNameLength]
	}
	return fmt.Sprintf("%s-%d-%s", name, inst.Generation, inst.ResourceVersion)
}
