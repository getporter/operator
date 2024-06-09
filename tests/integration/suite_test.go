//go:build integration

package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"get.porter.sh/operator/controllers"

	v1 "get.porter.sh/operator/api/v1"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func(done Done) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		UseExistingCluster: ptr.To(true),
		CRDDirectoryPaths:  []string{filepath.Join("..", "config", "crd", "bases")},
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	Expect(clientgoscheme.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(v1.AddToScheme(scheme.Scheme)).To(Succeed())

	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	err = (&controllers.InstallationReconciler{
		Client:           k8sManager.GetClient(),
		Scheme:           scheme.Scheme,
		Recorder:         k8sManager.GetEventRecorderFor("installation"),
		Log:              ctrl.Log.WithName("controllers").WithName("Installation"),
		CreateGRPCClient: controllers.CreatePorterGRPCClient,
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&controllers.CredentialSetReconciler{
		Client: k8sManager.GetClient(),
		Scheme: scheme.Scheme,
		Log:    ctrl.Log.WithName("controllers").WithName("CredentialSet"),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&controllers.ParameterSetReconciler{
		Client: k8sManager.GetClient(),
		Scheme: scheme.Scheme,
		Log:    ctrl.Log.WithName("controllers").WithName("ParameterSet"),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&controllers.AgentActionReconciler{
		Client: k8sManager.GetClient(),
		Scheme: scheme.Scheme,
		Log:    ctrl.Log.WithName("controllers").WithName("AgentAction"),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&controllers.AgentConfigReconciler{
		Client: k8sManager.GetClient(),
		Scheme: scheme.Scheme,
		Log:    ctrl.Log.WithName("controllers").WithName("AgentConfig"),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		err = k8sManager.Start(ctrl.SetupSignalHandler())
		Expect(err).ToNot(HaveOccurred())
	}()

	k8sClient = k8sManager.GetClient()
	Expect(k8sClient).ToNot(BeNil())

	close(done)
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

func createTestNamespace(ctx context.Context) string {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "ginkgo-tests-",
			Labels: map[string]string{
				"getporter.org/testdata": "true",
			},
		},
	}
	Expect(k8sClient.Create(ctx, ns)).To(Succeed())

	// porter-agent service account
	svc := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "porter-agent",
			Namespace: ns.Name,
		},
	}
	Expect(k8sClient.Create(ctx, svc)).To(Succeed())

	// Configure rbac for porter-agent
	svcRole := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "porter-agent-rolebinding",
			Namespace: ns.Name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      svc.Name,
				Namespace: svc.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "porter-operator-agent-role",
		},
	}
	Expect(k8sClient.Create(ctx, svcRole)).To(Succeed())

	// installation image service account
	instsa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "installation-agent",
			Namespace: ns.Name,
		},
	}
	Expect(k8sClient.Create(ctx, instsa)).To(Succeed())

	agentRepo := os.Getenv("PORTER_AGENT_REPOSITORY")
	if agentRepo == "" {
		agentRepo = "ghcr.io/getporter/porter-agent"
	}
	agentVersion := os.Getenv("PORTER_AGENT_VERSION")
	if agentVersion == "" {
		// We can switch this back to latest when 1.0.0 of porter releases
		agentVersion = v1.DefaultPorterAgentVersion
	}
	var retryLimit int32 = 2
	// Tweak porter agent config for testing
	agentCfg := &v1.AgentConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: ns.Name,
		},
		Spec: v1.AgentConfigSpec{
			PullPolicy:                 corev1.PullAlways,
			PorterRepository:           agentRepo,
			PorterVersion:              agentVersion,
			RetryLimit:                 &retryLimit,
			ServiceAccount:             svc.Name,
			InstallationServiceAccount: "installation-agent",
			PluginConfigFile:           &v1.PluginFileSpec{SchemaVersion: "1.0.0", Plugins: map[string]v1.Plugin{"kubernetes": {FeedURL: "https://cdn.porter.sh/plugins/atom.xml", Version: "v1.0.1"}}},
		},
	}
	Expect(k8sClient.Create(ctx, agentCfg)).To(Succeed())

	return ns.Name
}
