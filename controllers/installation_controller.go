package controllers

import (
	"context"
	"fmt"
	"strings"

	porterv1 "get.porter.sh/operator/api/v1"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	operatorNamespace = "porter-operator-system"
)

// InstallationReconciler reconciles a Installation object
type InstallationReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=porter.sh,resources=agentconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=porter.sh,resources=porterconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=porter.sh,resources=installations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=porter.sh,resources=installations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=porter.sh,resources=installations/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=create;delete

// Reconcile is responsible for calling porter when an Installation resource changes.
// Porter itself handles reconciling the resource with the current state of the installation.
func (r *InstallationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Retrieve the Installation
	inst := &porterv1.Installation{}
	err := r.Get(ctx, req.NamespacedName, inst)
	if err != nil {
		// TODO: Register a finalizer and then either delete or uninstall the resource from Porter
		r.Log.Info("Installation has been deleted", "installation", fmt.Sprintf("%s/%s", req.Namespace, req.Name))
		return ctrl.Result{Requeue: false}, nil
	}

	// Retrieve the Job running the porter action
	//  - job metadata contains a reference to the bundle installation and CRD revision
	porterJob := &batchv1.Job{}
	jobName := req.Name + "-" + inst.ResourceVersion
	err = r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: req.Namespace}, porterJob)
	if err != nil {
		// Create the Job if not found
		if apierrors.IsNotFound(err) {
			err = r.createJobForInstallation(ctx, jobName, inst)
			if err != nil {
				return ctrl.Result{}, err
			}
		} else {
			return ctrl.Result{}, errors.Wrapf(err, "could not query for the bundle installation job %s/%s", req.Namespace, porterJob.Name)
		}
	}

	// How to prevent concurrent jobs?
	//  1. Have porter itself wait for pending actions to complete, i.e. storage locks (added to backlog)
	//  1. * Requeue with backoff until we can run it (May run into problematic backoff behavior)
	//  1. Use job dependencies with either init container (problematic because of init container timeouts)

	// Update Installation events with job created
	// Can we do the status to have the deployments running? e.g. like how a deployment says 1/1 available/ready.
	// Can we add last action and result to the bundle installation?
	// how much do we want to replicate of porter's info in k8s? Just the k8s data?

	return ctrl.Result{}, nil
}

const labelPrefix = "porter.sh/"

func (r *InstallationReconciler) createJobForInstallation(ctx context.Context, jobName string, inst *porterv1.Installation) error {
	r.Log.Info(fmt.Sprintf("creating porter job %s/%s for Installation %s/%s", inst.Namespace, jobName, inst.Namespace, inst.Name))

	installationResourceB, err := inst.Spec.ToPorterDocument()
	if err != nil {
		return err
	}

	agentCfg, err := r.resolveAgentConfig(ctx, inst)
	if err != nil {
		return err
	}

	porterCfg, err := r.resolvePorterConfig(ctx, inst)
	if err != nil {
		return err
	}

	r.Log.Info("Porter Config Struct", "dump", fmt.Sprintf("%#v", porterCfg))
	porterCfgB, err := porterCfg.ToPorterDocument()
	if err != nil {
		return errors.Wrap(err, "error marshaling the porter config.json file")
	}
	r.Log.Info("Porter Config File", "config.yaml", string(porterCfgB))

	sharedLabels := map[string]string{
		labelPrefix + "managed":         "true",
		labelPrefix + "resourceKind":    "Installation",
		labelPrefix + "resourceName":    inst.Name,
		labelPrefix + "resourceVersion": inst.ResourceVersion,
		labelPrefix + "job":             jobName,
	}

	// Create a volume to share data between porter and the invocation image
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: inst.Namespace,
			Labels:    sharedLabels,
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
	err = r.Create(ctx, pvc)
	if err != nil {
		return errors.Wrapf(err, "error creating the the shared volume (pvc) for Installation %s/%s@%s", inst.Namespace, inst.Name, inst.ResourceVersion)
	}

	log := r.Log.WithValues("resourceKind", "Installation", "resourceName", inst.Name, "resourceNamespace", inst.Namespace, "resourceVersion", inst.ResourceVersion)
	log.Info("Using " + agentCfg.GetPorterImage())

	// Create a secret with all the files that should be copied into the agent
	// * porter config file (~/.porter/config.json)
	// * installation.yaml that we will pass to the command
	agentInput := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: inst.Namespace,
			Labels:    sharedLabels,
		},
		Type:      corev1.SecretTypeOpaque,
		Immutable: pointer.BoolPtr(true),
		Data: map[string][]byte{
			"config.yaml":       porterCfgB,
			"installation.yaml": installationResourceB,
		},
	}

	err = r.Create(ctx, agentInput)
	if err != nil {
		return errors.Wrapf(err, "error creating the agent input secret for Installation %s/%s@%s", inst.Namespace, inst.Name, inst.ResourceVersion)
	}

	// porter installation apply installation.yaml
	porterCommand := []string{"installation", "apply", "/porter-config/installation.yaml"}

	// represent the shared labels that we are applying to all the things in a way that porter can accept on teh command line
	var sharedLabelsBlob strings.Builder
	for k, v := range sharedLabels {
		sharedLabelsBlob.WriteString(fmt.Sprintf("%s=%s ", k, v))
	}

	porterJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: inst.Namespace,
			Labels:    sharedLabels,
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
					GenerateName: jobName,
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
									SecretName: agentInput.Name,
									Optional:   pointer.BoolPtr(false),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:            jobName,
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
									Value: sharedLabelsBlob.String(),
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
									Value: fmt.Sprintf("porter.sh/resourceKind=Installation porter.sh/resourceName=%s porter.sh/resourceVersion=%s", inst.Name, inst.ResourceVersion),
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
				},
			},
		},
	}

	err = r.Create(ctx, porterJob, &client.CreateOptions{})
	return errors.Wrapf(err, "error creating job for Installation %s/%s@%s", inst.Namespace, inst.Name, inst.ResourceVersion)
}

func (r *InstallationReconciler) resolveAgentConfig(ctx context.Context, inst *porterv1.Installation) (porterv1.AgentConfigSpec, error) {
	logConfig := func(name string, config *porterv1.AgentConfig) {
		r.Log.Info(fmt.Sprintf("Found %s porter agent configuration %#v", name, config),
			"porterRepository", config.Spec.PorterRepository,
			"porterVersion", config.Spec.PorterVersion,
			"pullPolicy", config.Spec.PullPolicy,
			"serviceAccount", config.Spec.ServiceAccount,
			"volumeSize", config.Spec.VolumeSize,
			"installationServiceAccount", config.Spec.InstallationServiceAccount,
		)
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

	r.Log.Info(fmt.Sprintf("resolved porter agent configuration %#v", cfg),
		"porterImage", cfg.GetPorterImage(),
		"pullPolicy", cfg.GetPullPolicy(),
		"serviceAccount", cfg.ServiceAccount,
		"volumeSize", cfg.GetVolumeSize(),
		"installationServiceAccount", cfg.InstallationServiceAccount,
	)
	return cfg, nil
}

func (r *InstallationReconciler) resolvePorterConfig(ctx context.Context, inst *porterv1.Installation) (porterv1.PorterConfigSpec, error) {
	r.Log.Info(fmt.Sprintf("Resolving porter configuration file for %s", inst.Name))
	logConfig := func(name string, config *porterv1.PorterConfig) {
		if config.Name == "" {
			return
		}
		r.Log.Info(fmt.Sprintf("Found %s level porter config file %s", name, config.Name))
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
	r.Log.Info("Instance Config", "name", inst.Spec.PorterConfig.Name)
	err = r.Get(ctx, types.NamespacedName{Name: inst.Spec.PorterConfig.Name, Namespace: inst.Namespace}, instCfg)
	if err != nil && !apierrors.IsNotFound(err) {
		return porterv1.PorterConfigSpec{}, errors.Wrapf(err, "cannot retrieve agent configuration %s specified by the installation", inst.Spec.AgentConfig.Name)
	}
	logConfig("instance", instCfg)

	// Resolve final configuration
	base := &defaultCfg
	cfg, err := base.MergeConfig(systemCfg.Spec, nsCfg.Spec, instCfg.Spec)
	if err != nil {
		return porterv1.PorterConfigSpec{}, err
	}

	return cfg, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *InstallationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// We want reconcile called on an installation when the job that it spawned finishes
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &batchv1.Job{}, ".metadata.controller", func(rawObj client.Object) []string {
		// grab the job object, extract the owner...
		job := rawObj.(*batchv1.Job)
		owner := metav1.GetControllerOf(job)
		if owner == nil {
			return nil
		}

		if owner.APIVersion != porterv1.GroupVersion.String() || owner.Kind != "Installation" {
			return nil
		}

		return []string{owner.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&porterv1.Installation{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
