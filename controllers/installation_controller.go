package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	porterv1 "get.porter.sh/operator/api/v1"
)

// InstallationReconciler reconciles a Installation object
type InstallationReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=porter.sh,resources=installations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=porter.sh,resources=installations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=porter.sh,resources=installations/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Installation object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
func (r *InstallationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Retrieve the Installation
	inst := &porterv1.Installation{}
	err := r.Get(ctx, req.NamespacedName, inst)
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

	// Update Installation events with job created
	// Can we do the status to have the deployments running? e.g. like how a deployment says 1/1 available/ready.
	// Can we add last action and result to the bundle installation?
	// how much do we want to replicate of porter's info in k8s? Just the k8s data?

	return ctrl.Result{}, nil
}

func (r *InstallationReconciler) createJobForInstallation(ctx context.Context, name string, inst *porterv1.Installation) error {
	r.Log.Info(fmt.Sprintf("creating porter job %s/%s for Installation %s/%s", inst.Namespace, name, inst.Namespace, inst.Name))

	porterVersion, pullPolicy := r.getPorterImageVersion(ctx, inst)
	serviceAccount := r.getPorterAgentServiceAccount(ctx, inst)

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
			Completions:  pointer.Int32Ptr(1),
			BackoffLimit: pointer.Int32Ptr(0),
			Template: corev1.PodTemplateSpec{
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
							BlockOwnerDeletion: pointer.BoolPtr(true),
						},
					},
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "porter-config",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "porter-config",
									Optional:   pointer.BoolPtr(true),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:            name,
							Image:           "ghcr.io/getporter/porter:kubernetes-" + porterVersion,
							ImagePullPolicy: pullPolicy,
							Args:            args,
							Env: []corev1.EnvVar{
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
							EnvFrom: []corev1.EnvFromSource{
								// Environtment variables for the plugins
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
									Name:      "porter-config",
									MountPath: "/porter-config/",
								},
							},
						},
					},
					RestartPolicy:      "Never", // TODO: Make the retry policy configurable on the Installation
					ServiceAccountName: serviceAccount,
					ImagePullSecrets:   nil, // TODO: Make pulling from a private registry possible
				},
			},
		},
	}

	err := r.Create(ctx, porterJob, &client.CreateOptions{})
	return errors.Wrapf(err, "error creating job for Installation %s/%s@%s", inst.Namespace, inst.Name, inst.ResourceVersion)
}

func (r *InstallationReconciler) getPorterImageVersion(ctx context.Context, inst *porterv1.Installation) (porterVersion string, pullPolicy corev1.PullPolicy) {
	porterVersion = "latest"
	if inst.Spec.PorterVersion != "" {
		r.Log.Info(fmt.Sprintf("porter image version override: %s", inst.Spec.PorterVersion))
		// Use the version specified by the instance
		porterVersion = inst.Spec.PorterVersion
	} else {
		// Check if the namespace has a default porter version configured
		cfg := &corev1.ConfigMap{}
		err := r.Get(ctx, types.NamespacedName{Name: "porter", Namespace: inst.Namespace}, cfg)
		if err != nil {
			r.Log.Info(fmt.Sprintf("WARN: cannot retrieve porter configmap %q, using default configuration", err))
		}
		if v, ok := cfg.Data["porterVersion"]; ok {
			r.Log.Info(fmt.Sprintf("porter image version defaulted from configmap to %s", v))
			porterVersion = v
		}
	}

	r.Log.Info("resolved porter image version", "version", porterVersion)

	pullPolicy = corev1.PullIfNotPresent
	if porterVersion == "canary" || porterVersion == "latest" {
		pullPolicy = corev1.PullAlways
	}

	return porterVersion, pullPolicy
}

func (r *InstallationReconciler) getPorterAgentServiceAccount(ctx context.Context, inst *porterv1.Installation) string {
	serviceAccount := ""
	if inst.Spec.ServiceAccount != "" {
		r.Log.Info(fmt.Sprintf("porter agent service account override: %s", inst.Spec.ServiceAccount))
		// Use the version specified by the instance
		serviceAccount = inst.Spec.ServiceAccount
	} else {
		// Check if the namespace has a default service account configured
		cfg := &corev1.ConfigMap{}
		err := r.Get(ctx, types.NamespacedName{Name: "porter", Namespace: inst.Namespace}, cfg)
		if err != nil {
			r.Log.Info(fmt.Sprintf("WARN: cannot retrieve porter configmap %q, using default configuration", err))
		}
		if v, ok := cfg.Data["serviceAccount"]; ok {
			r.Log.Info(fmt.Sprintf("porter agent service account defaulted from configmap to %s", v))
			serviceAccount = v
		}
	}

	r.Log.Info("resolved porter agent service account", "serviceAccount", serviceAccount)

	return serviceAccount
}

// SetupWithManager sets up the controller with the Manager.
func (r *InstallationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&porterv1.Installation{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}
