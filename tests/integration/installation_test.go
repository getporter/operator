//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"os"
	"time"

	porterv1 "get.porter.sh/operator/api/v1"
	"get.porter.sh/operator/controllers"
	"github.com/carolynvs/magex/shx"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"github.com/tidwall/pretty"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
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
					SchemaVersion: "1.0.0",
					Name:          "hello",
					Namespace:     "operator-tests",
					Bundle: porterv1.OCIReferenceParts{
						Repository: "carolynvs/porter-hello-nonroot",
						Version:    "0.1.0",
					},
				},
			}
			Expect(k8sClient.Create(ctx, inst)).Should(Succeed())
			Expect(waitForPorter(ctx, inst, "waiting for the bundle to install")).Should(Succeed())
			validateInstallationConditions(inst)

			patchInstallation := func(inst *porterv1.Installation) {
				controllers.PatchObjectWithRetry(ctx, logr.Discard(), k8sClient, k8sClient.Patch, inst, func() client.Object {
					return &porterv1.Installation{}
				})
			}

			Log("upgrade the installation")
			inst.Spec.Parameters = runtime.RawExtension{Raw: []byte(`{"name": "operator"}`)}
			patchInstallation(inst)
			Expect(waitForPorter(ctx, inst, "waiting for the bundle to upgrade")).Should(Succeed())
			validateInstallationConditions(inst)

			Log("uninstall the installation")
			inst.Spec.Uninstalled = true
			patchInstallation(inst)
			Expect(waitForPorter(ctx, inst, "waiting for the bundle to uninstall")).Should(Succeed())
			validateInstallationConditions(inst)

			Log("delete the installation")
			Expect(k8sClient.Delete(ctx, inst)).Should(Succeed())
			Expect(waitForInstallationDeleted(ctx, inst)).Should(Succeed())
		})
	})
})

func waitForPorter(ctx context.Context, inst *porterv1.Installation, msg string) error {
	Log("%s: %s/%s", msg, inst.Namespace, inst.Name)
	key := client.ObjectKey{Namespace: inst.Namespace, Name: inst.Name}
	ctx, cancel := context.WithTimeout(ctx, getWaitTimeout())
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			Fail(errors.Wrapf(ctx.Err(), "timeout %s", msg).Error())
		default:
			err := k8sClient.Get(ctx, key, inst)
			if err != nil {
				// There is lag between creating and being able to retrieve, I don't understand why
				if apierrors.IsNotFound(err) {
					time.Sleep(time.Second)
					continue
				}
				return err
			}

			// Check if the latest change has been processed
			if inst.Generation == inst.Status.ObservedGeneration {
				if apimeta.IsStatusConditionTrue(inst.Status.Conditions, string(porterv1.ConditionComplete)) {
					return nil
				}

				if apimeta.IsStatusConditionTrue(inst.Status.Conditions, string(porterv1.ConditionFailed)) {
					// Grab some extra info to help with debugging
					debugFailedInstallation(ctx, inst)
					return errors.New("porter did not run successfully")
				}
			}

			time.Sleep(time.Second)
			continue
		}
	}
}

func debugFailedInstallation(ctx context.Context, inst *porterv1.Installation) {
	Log("DEBUG: ----------------------------------------------------")
	actionKey := client.ObjectKey{Name: inst.Status.Action.Name, Namespace: inst.Namespace}
	action := &porterv1.AgentAction{}
	if err := k8sClient.Get(ctx, actionKey, action); err != nil {
		Log(errors.Wrap(err, "could not retrieve the Installation's AgentAction to troubleshoot").Error())
		return
	}

	jobKey := client.ObjectKey{Name: action.Status.Job.Name, Namespace: action.Namespace}
	job := &batchv1.Job{}
	if err := k8sClient.Get(ctx, jobKey, job); err != nil {
		Log(errors.Wrap(err, "could not retrieve the Installation's Job to troubleshoot").Error())
		return
	}

	shx.Command("kubectl", "logs", "-n="+job.Namespace, "job/"+job.Name).
		Env("KUBECONFIG=" + "../../kind.config").RunV()
	Log("DEBUG: ----------------------------------------------------")
}

// Get the amount of time that we should wait for a test action to be processed.
func getWaitTimeout() time.Duration {
	if value := os.Getenv("PORTER_TEST_WAIT_TIMEOUT"); value != "" {
		timeout, err := time.ParseDuration(value)
		if err != nil {
			fmt.Printf("WARNING: An invalid value, %q, was set for PORTER_TEST_WAIT_TIMEOUT environment variable. The format should be a Go time duration such as 30s or 1m. Ignoring and using the default instead", value)
			return defaultWaitTimeout
		}

		return timeout
	}
	return defaultWaitTimeout
}

func waitForInstallationDeleted(ctx context.Context, inst *porterv1.Installation) error {
	Log("Waiting for installation to finish deleting: %s/%s", inst.Namespace, inst.Name)
	key := client.ObjectKey{Namespace: inst.Namespace, Name: inst.Name}
	waitCtx, cancel := context.WithTimeout(ctx, getWaitTimeout())
	defer cancel()
	for {
		select {
		case <-waitCtx.Done():
			Fail(errors.Wrap(waitCtx.Err(), "timeout waiting for installation to delete").Error())
		default:
			err := k8sClient.Get(ctx, key, inst)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return nil
				}
				return err
			}

			time.Sleep(time.Second)
			continue
		}
	}
}

func validateInstallationConditions(inst *porterv1.Installation) {
	// Checks that all expected conditions are set
	Expect(apimeta.IsStatusConditionTrue(inst.Status.Conditions, string(porterv1.ConditionScheduled)))
	Expect(apimeta.IsStatusConditionTrue(inst.Status.Conditions, string(porterv1.ConditionStarted)))
	Expect(apimeta.IsStatusConditionTrue(inst.Status.Conditions, string(porterv1.ConditionComplete)))
}

func Log(value string, args ...interface{}) {
	GinkgoWriter.Write([]byte(fmt.Sprintf(value+"\n", args...)))
}

func LogJson(value string) {
	GinkgoWriter.Write(pretty.Pretty([]byte(value + "\n")))
}
