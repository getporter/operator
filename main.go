package main

import (
	"context"
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	v1 "get.porter.sh/operator/api/v1"
	"get.porter.sh/operator/controllers"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	ctx := context.Background()
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var createGrpc bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&createGrpc, "create-grpc", true, "create grpc deployment for use in operator")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "c58eb551.getporter.org",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	if err = (&controllers.InstallationReconciler{
		Client:           mgr.GetClient(),
		Recorder:         mgr.GetEventRecorderFor("installation"),
		Log:              ctrl.Log.WithName("controllers").WithName("Installation"),
		Scheme:           mgr.GetScheme(),
		CreateGRPCClient: controllers.CreatePorterGRPCClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Installation")
		os.Exit(1)
	}
	if err = (&controllers.AgentActionReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("AgentAction"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AgentAction")
		os.Exit(1)
	}
	if err = (&controllers.CredentialSetReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("CredentialSet"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CredentialSet")
		os.Exit(1)
	}
	if err = (&controllers.ParameterSetReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("ParameterSet"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ParameterSet")
		os.Exit(1)
	}
	if err = (&controllers.AgentConfigReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("AgentConfig"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AgentConfig")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("health", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("check", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	if createGrpc {
		g, ctx := errgroup.WithContext(ctx)
		g.Go(func() error {
			k8sClient := mgr.GetClient()
			// GET deployment to see if it exists first before creating it
			deployment := &appsv1.Deployment{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: controllers.GrpcDeployment.Name, Namespace: controllers.GrpcDeployment.Namespace}, deployment)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return k8sClient.Create(ctx, controllers.GrpcDeployment, &client.CreateOptions{})
				}
			}
			// NOTE: Don't crash, just don't deploy if Get fails for any other reason than not found
			return nil
		})

		g.Go(func() error {
			k8sClient := mgr.GetClient()
			service := &corev1.Service{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: controllers.GrpcService.Name, Namespace: controllers.GrpcService.Namespace}, service)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return k8sClient.Create(ctx, controllers.GrpcService, &client.CreateOptions{})
				}
			}
			// NOTE: Don't crash, just don't deploy if Get fails for any other reason than not found
			return nil
		})

		go func() {
			if err := g.Wait(); err != nil {
				setupLog.Error(err, "error with async operation of creating grpc deployment")
				os.Exit(1)
			}
		}()
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
