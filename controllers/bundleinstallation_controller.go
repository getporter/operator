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
	log := r.Log.WithValues("bundleinstallation", req.NamespacedName)

	// Retrieve the BundleInstallation
	inst := &porterv1.BundleInstallation{}
	err := r.Get(ctx, req.NamespacedName, inst)
	if err != nil {
		err = errors.Wrapf(err, "could not find bundle installation %s/%s", req.Namespace, req.Name)
		log.Error(err, "")
		return ctrl.Result{}, err
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
			err = errors.Wrapf(err, "could not query for the bundle installation job %s/%s", req.Namespace, porterJob.Name)
			log.Error(err, "")
			return ctrl.Result{}, err
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

	porterVersion := "latest"
	if inst.Spec.PorterVersion != "" {
		porterVersion = inst.Spec.PorterVersion
	}

	// TODO: For now require the action, and when porter supports installorupgrade switch
	porterAction := inst.Spec.Action

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
					},
					Containers: []v1.Container{
						{
							Name:  name,
							Image: "getporter/porter:" + porterVersion,
							// porter ACTION INSTALLATION_NAME --tag=REFERENCE --debug
							Args: []string{
								porterAction,
								inst.Name,
								"--tag=" + inst.Spec.Reference,
								"--debug",
							},
							VolumeMounts: []v1.VolumeMount{
								{
									Name:      "docker-socket",
									MountPath: "/var/run/docker.sock",
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

// SetupWithManager sets up the controller with the Manager.
func (r *BundleInstallationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&porterv1.BundleInstallation{}).
		Owns(&v1.Pod{}).
		Complete(r)
}
