//go:build integration

package integration_test

import (
	"context"
	"time"

	porterv1 "get.porter.sh/operator/api/v1"
	"get.porter.sh/operator/controllers"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The default amount of time to wait while a test action is processed.
const defaultWaitTimeout = 120 * time.Second

var _ = Describe("Installation Lifecycle", func() {
	Context("When an installation is changed", func() {
		It("Should run porter", func() {
			By("By creating an agent action")
			ctx := context.Background()
			ns := createTestNamespace(ctx)

			Log("create an installation")
			inst := &porterv1.Installation{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "porter.sh/v1",
					Kind:       "Installation",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "porter-hello",
					Namespace: ns,
				},
				Spec: porterv1.InstallationSpec{
					SchemaVersion: "1.0.1",
					Name:          "hello",
					Namespace:     "operator-tests",
					Bundle: porterv1.OCIReferenceParts{
						Repository: "carolynvs/porter-hello-nonroot",
						Version:    "0.1.0",
					},
				},
			}
			Expect(k8sClient.Create(ctx, inst)).Should(Succeed())
			Expect(waitForPorter(ctx, inst, 1, "waiting for the bundle to install")).Should(Succeed())
			validateResourceConditions(inst.Status.Conditions)

			patchInstallation := func(inst *porterv1.Installation) {
				controllers.PatchObjectWithRetry(ctx, logr.Discard(), k8sClient, k8sClient.Patch, inst, func() client.Object {
					return &porterv1.Installation{}
				})
				// Wait for patch to apply, this can cause race conditions
			}

			Log("upgrade the installation")
			inst.Spec.Parameters = runtime.RawExtension{Raw: []byte(`{"name": "operator"}`)}
			patchInstallation(inst)
			Expect(waitForPorter(ctx, inst, 2, "waiting for the bundle to upgrade")).Should(Succeed())
			validateResourceConditions(inst.Status.Conditions)

			Log("uninstall the installation")
			inst.Spec.Uninstalled = true
			patchInstallation(inst)
			Expect(waitForPorter(ctx, inst, 3, "waiting for the bundle to uninstall")).Should(Succeed())
			validateResourceConditions(inst.Status.Conditions)

			Log("delete the installation")
			Expect(k8sClient.Delete(ctx, inst)).Should(Succeed())
			Expect(waitForResourceDeleted(ctx, inst, inst.Namespace, inst.Name)).Should(Succeed())
		})
	})
})
