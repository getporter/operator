//go:build integration
// +build integration

package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"get.porter.sh/operator/controllers"

	portershv1 "get.porter.sh/operator/api/v1"
	porterv1 "get.porter.sh/operator/api/v1"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var testNamespace string

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Controller Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func(done Done) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		UseExistingCluster: pointer.BoolPtr(true),
		CRDDirectoryPaths:  []string{filepath.Join("..", "config", "crd", "bases")},
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	Expect(clientgoscheme.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(porterv1.AddToScheme(scheme.Scheme)).To(Succeed())

	err = portershv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = portershv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	err = (&controllers.InstallationReconciler{
		Client: k8sManager.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("Installation"),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&controllers.AgentActionReconciler{
		Client: k8sManager.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("AgentAction"),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		err = k8sManager.Start(ctrl.SetupSignalHandler())
		Expect(err).ToNot(HaveOccurred())
	}()

	k8sClient = k8sManager.GetClient()
	Expect(k8sClient).ToNot(BeNil())

	close(done)
}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

var _ = BeforeEach(func() {
	testNamespace = createTestNamespace(context.Background())
}, 5)

var _ = AfterEach(func() {
	if _, ok := os.LookupEnv("KEEP_TESTS"); ok {
		return
	}
	deleteNamespace(testNamespace)
}, 5)

func createTestNamespace(ctx context.Context) string {
	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "ginkgo-tests",
			Labels: map[string]string{
				"porter.sh/testdata": "true",
			},
		},
	}
	Expect(k8sClient.Create(ctx, ns)).To(Succeed())

	// porter-agent service account
	svc := &v1.ServiceAccount{
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
	instsa := &v1.ServiceAccount{
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
		agentVersion = porterv1.DefaultPorterAgentVersion
	}
	// Tweak porter agent config for testing
	agentCfg := &porterv1.AgentConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: ns.Name,
		},
		Spec: porterv1.AgentConfigSpec{
			PorterRepository:           agentRepo,
			PorterVersion:              agentVersion,
			ServiceAccount:             svc.Name,
			InstallationServiceAccount: "installation-agent",
		},
	}
	Expect(k8sClient.Create(ctx, agentCfg)).To(Succeed())

	return ns.Name
}

func deleteNamespace(name string) {
	// Delete the test namespace
	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	var background = metav1.DeletePropagationBackground
	err := k8sClient.Delete(context.Background(), ns, &client.DeleteOptions{
		GracePeriodSeconds: pointer.Int64Ptr(0),
		PropagationPolicy:  &background,
	})
	if apierrors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
}
