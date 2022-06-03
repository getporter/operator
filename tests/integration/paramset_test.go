//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"time"

	"get.porter.sh/operator/controllers"
	"github.com/carolynvs/magex/shx"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	"github.com/pkg/errors"
	"github.com/tidwall/gjson"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	porterv1 "get.porter.sh/operator/api/v1"
	. "github.com/onsi/gomega"
)

var _ = Describe("ParameterSet lifecycle", func() {
	It("should run porter apply", func() {
		By("creating an agent action", func() {
			ctx := context.Background()
			ns := createTestNamespace(ctx)
			delayValue := "1"
			resourceName := "ps-lifecycle-" + ns
			pSetName := "ps-lifecycle-test"
			createTestSecret(ctx, resourceName, delayValue, ns)

			Log(fmt.Sprintf("create parameter set '%s'", resourceName))
			paramName := "delay"
			ps := NewTestParamSet(resourceName)
			ps.Spec.Name = pSetName
			ps.ObjectMeta.Namespace = ns
			param := porterv1.Parameter{
				Name: paramName,
				Source: porterv1.ParameterSource{
					Secret: resourceName,
				},
			}
			ps.Spec.Parameters = append(ps.Spec.Parameters, param)
			ps.Spec.Namespace = ns

			Expect(k8sClient.Create(ctx, ps)).Should(Succeed())
			Expect(waitForPorterPS(ctx, ps, "waiting for parameter set to apply")).Should(Succeed())
			validateParamSetConditions(ps)

			Log("verify it's created")
			jsonOut := runAgentAction(ctx, "create-check-parameters-list", ns, []string{"parameters", "list", "-n", ns, "-o", "json"})
			firstName := gjson.Get(jsonOut, "0.name").String()
			numParamSets := gjson.Get(jsonOut, "#").Int()
			numParams := gjson.Get(jsonOut, "0.parameters.#").Int()
			firstParamName := gjson.Get(jsonOut, "0.parameters.0.name").String()
			Expect(int64(1)).To(Equal(numParamSets))
			Expect(int64(1)).To(Equal(numParams))
			Expect(ps.Spec.Name).To(Equal(firstName))
			Expect(paramName).To(Equal(firstParamName))

			Log("install porter-test-me bundle with new param set")
			inst := NewTestInstallation("ps-with-secret")
			inst.ObjectMeta.Namespace = ns
			inst.Spec.Namespace = ns
			inst.Spec.ParameterSets = append(inst.Spec.ParameterSets, pSetName)
			inst.Spec.SchemaVersion = "1.0.0"
			Expect(k8sClient.Create(ctx, inst)).Should(Succeed())
			Expect(waitForPorter(ctx, inst, "waiting for porter-test-me to install")).Should(Succeed())
			validateInstallationConditions(inst)

			// Validate that the correct parameter set was used by the installation
			instJsonOut := runAgentAction(ctx, "show-outputs", ns, []string{"installation", "outputs", "list", "-n", ns, "-i", inst.Spec.Name, "-o", "json"})
			outDelayValue := gjson.Get(instJsonOut, `#(name=="outDelay").value`).String()
			Expect(outDelayValue).To(Equal(delayValue))

			Log("update a parameter set")
			updateParamName := "update-paramset"
			updateParamValue := "update-value"
			newParam := porterv1.Parameter{
				Name: updateParamName,
				Source: porterv1.ParameterSource{
					Value: updateParamValue,
				},
			}
			ps.Spec.Parameters = append(ps.Spec.Parameters, newParam)
			patchPS := func(ps *porterv1.ParameterSet) {
				controllers.PatchObjectWithRetry(ctx, logr.Discard(), k8sClient, k8sClient.Patch, ps, func() client.Object {
					return &porterv1.ParameterSet{}
				})
			}
			patchPS(ps)
			Expect(waitForPorterPS(ctx, ps, "waiting for parameters update to apply")).Should(Succeed())
			Log("verify it's updated")
			jsonOut = runAgentAction(ctx, "update-check-parameters-list", ns, []string{"parameters", "list", "-n", ns, "-o", "json"})
			updatedFirstName := gjson.Get(jsonOut, "0.name").String()
			updatedNumParamSets := gjson.Get(jsonOut, "#").Int()
			updatedNumParams := gjson.Get(jsonOut, "0.parameters.#").Int()
			updatedFirstParamName := gjson.Get(jsonOut, "0.parameters.0.name").String()
			updatedSecondParamName := gjson.Get(jsonOut, "0.parameters.1.name").String()
			Expect(int64(1)).To(Equal(updatedNumParamSets))
			Expect(int64(2)).To(Equal(updatedNumParams))
			Expect(ps.Spec.Name).To(Equal(updatedFirstName))
			Expect(paramName).To(Equal(updatedFirstParamName))
			Expect(updateParamName).To(Equal(updatedSecondParamName))

			Log("delete a parameter set")
			Expect(k8sClient.Delete(ctx, ps)).Should(Succeed())
			Expect(waitForParamSetDeleted(ctx, ps)).Should(Succeed())

			Log("verify parameter set is gone from porter data store")
			delJsonOut := runAgentAction(ctx, "delete-check-parameters-list", ns, []string{"parameters", "list", "-n", ns, "-o", "json"})
			delNumParams := gjson.Get(delJsonOut, "#").Int()
			Expect(int64(0)).To(Equal(delNumParams))

			Log("verify parameter set CRD is gone from k8s cluster")
			nsName := apitypes.NamespacedName{Namespace: ps.Namespace, Name: ps.Name}
			getPS := &porterv1.ParameterSet{}
			Expect(k8sClient.Get(ctx, nsName, getPS)).ShouldNot(Succeed())

		})
	})
})

//NewTestParamSet minimal ParameterSet CRD for tests
func NewTestParamSet(psName string) *porterv1.ParameterSet {
	ps := &porterv1.ParameterSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "porter.sh/v1",
			Kind:       "ParameterSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "porter-test-me-",
		},
		Spec: porterv1.ParameterSetSpec{
			//TODO: get schema version from porter version
			// https://github.com/getporter/porter/pull/2052
			SchemaVersion: "1.0.1",
			Name:          psName,
		},
	}
	return ps
}

func waitForPorterPS(ctx context.Context, ps *porterv1.ParameterSet, msg string) error {
	Log("%s: %s/%s", msg, ps.Namespace, ps.Name)
	key := client.ObjectKey{Namespace: ps.Namespace, Name: ps.Name}
	ctx, cancel := context.WithTimeout(ctx, getWaitTimeout())
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			Fail(errors.Wrapf(ctx.Err(), "timeout %s", msg).Error())
		default:
			err := k8sClient.Get(ctx, key, ps)
			if err != nil {
				// There is lag between creating and being able to retrieve, I don't understand why
				if apierrors.IsNotFound(err) {
					time.Sleep(time.Second)
					continue
				}
				return err
			}

			// Check if the latest change has been processed
			if ps.Generation == ps.Status.ObservedGeneration {
				if apimeta.IsStatusConditionTrue(ps.Status.Conditions, string(porterv1.ConditionComplete)) {
					time.Sleep(time.Second)
					return nil
				}

				if apimeta.IsStatusConditionTrue(ps.Status.Conditions, string(porterv1.ConditionFailed)) {
					// Grab some extra info to help with debugging
					debugFailedPSCreate(ctx, ps)
					return errors.New("porter did not run successfully")
				}
			}

			time.Sleep(time.Second)
			continue
		}
	}
}

func waitForParamSetDeleted(ctx context.Context, ps *porterv1.ParameterSet) error {
	Log("Waiting for ParameterSet to finish deleting: %s/%s", ps.Namespace, ps.Name)
	key := client.ObjectKey{Namespace: ps.Namespace, Name: ps.Name}
	waitCtx, cancel := context.WithTimeout(ctx, getWaitTimeout())
	defer cancel()
	for {
		select {
		case <-waitCtx.Done():
			Fail(errors.Wrap(waitCtx.Err(), "timeout waiting for ParameterSet to delete").Error())
		default:
			err := k8sClient.Get(ctx, key, ps)
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

func debugFailedPSCreate(ctx context.Context, ps *porterv1.ParameterSet) {
	Log("DEBUG: ----------------------------------------------------")
	actionKey := client.ObjectKey{Name: ps.Status.Action.Name, Namespace: ps.Namespace}
	action := &porterv1.AgentAction{}
	if err := k8sClient.Get(ctx, actionKey, action); err != nil {
		Log(errors.Wrap(err, "could not retrieve the ParameterSet's AgentAction to troubleshoot").Error())
		return
	}

	jobKey := client.ObjectKey{Name: action.Status.Job.Name, Namespace: action.Namespace}
	job := &batchv1.Job{}
	if err := k8sClient.Get(ctx, jobKey, job); err != nil {
		Log(errors.Wrap(err, "could not retrieve the ParameterSet's Job to troubleshoot").Error())
		return
	}

	shx.Command("kubectl", "logs", "-n="+job.Namespace, "job/"+job.Name).
		Env("KUBECONFIG=" + "../../kind.config").RunV()
	Log("DEBUG: ----------------------------------------------------")
}

func validateParamSetConditions(ps *porterv1.ParameterSet) {
	// Checks that all expected conditions are set
	Expect(apimeta.IsStatusConditionTrue(ps.Status.Conditions, string(porterv1.ConditionScheduled)))
	Expect(apimeta.IsStatusConditionTrue(ps.Status.Conditions, string(porterv1.ConditionStarted)))
	Expect(apimeta.IsStatusConditionTrue(ps.Status.Conditions, string(porterv1.ConditionComplete)))
}
