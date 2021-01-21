package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	porterv1 "get.porter.sh/operator/api/v1"
)

// BundleInstallationReconciler reconciles a BundleInstallation object
type BundleInstallationReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=porter.sh,resources=bundleinstallations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=porter.sh,resources=bundleinstallations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=porter.sh,resources=bundleinstallations/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the BundleInstallation object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
func (r *BundleInstallationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	cfg := &v1.ConfigMap{}
	porterCfgName := types.NamespacedName{Name: "porter", Namespace: "porter-operator-system"}
	err := r.Get(ctx, porterCfgName, cfg)
	if err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "cannot retrieve porter configmap: %s", porterCfgName)
	}

	// Retrieve the BundleInstallation
	inst := &porterv1.BundleInstallation{}
	err = r.Get(ctx, req.NamespacedName, inst)
	if err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "could not find bundle installation %s/%s", req.Namespace, req.Name)
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

	// Update BundleInstallation events with job created
	// Can we do the status to have the deployments running? e.g. like how a deployment says 1/1 available/ready.
	// Can we add last action and result to the bundle installation?
	// how much do we want to replicate of porter's info in k8s? Just the k8s data?

	return ctrl.Result{}, nil
}

func (r *BundleInstallationReconciler) createJobForInstallation(ctx context.Context, name string, inst *porterv1.BundleInstallation) error {
	r.Log.Info(fmt.Sprintf("creating porter job %s/%s for BundleInstallation %s/%s", inst.Namespace, name, inst.Namespace, inst.Name))

	porterVersion, pullPolicy := r.getPorterImageVersion(ctx, inst)

	// porter ACTION INSTALLATION_NAME --tag=REFERENCE --debug
	// TODO: For now require the action, and when porter supports installorupgrade switch
	args := []string{
		inst.Spec.Action,
		inst.Name,
		"--reference=" + inst.Spec.Reference,
		"--debug",
		"--debug-plugins",
		"--driver=kubernetes",
	}
	for _, c := range inst.Spec.Credentials {
		args = append(args, "--cred="+c)
	}
	for _, p := range inst.Spec.Parameters {
		args = append(args, "--param="+p)
	}

	porterJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: inst.Namespace,
			Labels: map[string]string{
				"porter":       "true",
				"installation": inst.Name,
			},
		},
		Spec: batchv1.JobSpec{
			Completions:  Int32Ptr(1),
			BackoffLimit: Int32Ptr(0),
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: name,
					Namespace:    inst.Namespace,
					Labels: map[string]string{
						"porter":       "true",
						"installation": inst.Name,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         inst.APIVersion,
							Kind:               inst.Kind,
							Name:               inst.Name,
							UID:                inst.UID,
							BlockOwnerDeletion: BoolPtr(true),
						},
					},
				},
				Spec: v1.PodSpec{
					Volumes: []v1.Volume{
						{
							Name: "docker-socket",
							VolumeSource: v1.VolumeSource{
								HostPath: &v1.HostPathVolumeSource{
									Path: "/var/run/docker.sock",
								},
							},
						},
						{
							Name: "porter-config",
							VolumeSource: v1.VolumeSource{
								Secret: &v1.SecretVolumeSource{
									SecretName: "porter-config",
								},
							},
						},
					},
					Containers: []v1.Container{
						{
							Name:            name,
							Image:           "ghcr.io/getporter/porter:kubernetes-" + porterVersion,
							ImagePullPolicy: pullPolicy,
							Args:            args,
							Env: []v1.EnvVar{
								// Configuration for the Kubernetes Driver
								{
									Name:  "KUBE_NAMESPACE",
									Value: inst.Namespace,
								},
								{
									Name:  "IN_CLUSTER",
									Value: "true",
								},
							},
							EnvFrom: []v1.EnvFromSource{
								{
									SecretRef: &v1.SecretEnvSource{
										LocalObjectReference: v1.LocalObjectReference{
											Name: "porter-env",
										},
									},
								},
							},
							VolumeMounts: []v1.VolumeMount{
								{
									Name:      "docker-socket",
									MountPath: "/var/run/docker.sock",
								},
								{
									Name:      "porter-config",
									MountPath: "/porter-config/",
								},
							},
						},
					},
					RestartPolicy:      "Never", // TODO: Make the retry policy configurable on the BundleInstallation
					ServiceAccountName: "",      // TODO: Make the service account configurable so that different bundles can use pod identity to read credentials from a secret store
					ImagePullSecrets:   nil,     // TODO: Make pulling from a private registry possible
				},
			},
		},
	}

	err := r.Create(ctx, porterJob, &client.CreateOptions{})
	return errors.Wrapf(err, "error creating job for BundleInstallation %s/%s@%s", inst.Namespace, inst.Name, inst.ResourceVersion)
}

func (r *BundleInstallationReconciler) getPorterImageVersion(ctx context.Context, inst *porterv1.BundleInstallation) (porterVersion string, pullPolicy v1.PullPolicy) {
	porterVersion = "latest"
	if inst.Spec.PorterVersion != "" {
		r.Log.Info("porter image version override")
		// Use the version specified by the instance
		porterVersion = inst.Spec.PorterVersion
	} else {
		// Check if the namespace has a default porter version configured
		cfg := &v1.ConfigMap{}
		err := r.Get(ctx, types.NamespacedName{Name: "porter", Namespace: "porter-operator-system"}, cfg)
		if err != nil {
			r.Log.Info(fmt.Sprintf("WARN: cannot retrieve porter configmap %q, using default configuration", err))
		}
		if v, ok := cfg.Data["porter-version"]; ok {
			r.Log.Info("porter image version defaulted from configmap")
			porterVersion = v
		}
	}

	r.Log.Info("resolved porter image version", "version", porterVersion)

	pullPolicy = v1.PullIfNotPresent
	if porterVersion == "canary" || porterVersion == "latest" {
		pullPolicy = v1.PullAlways
	}

	return porterVersion, pullPolicy
}

// SetupWithManager sets up the controller with the Manager.
func (r *BundleInstallationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&porterv1.BundleInstallation{}).
		Owns(&v1.Pod{}).
		Complete(r)
}
