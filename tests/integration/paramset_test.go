//go:build integration

package integration_test

import (
	"context"
	"fmt"

	"get.porter.sh/operator/controllers"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	"github.com/tidwall/gjson"
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
			Expect(waitForPorter(ctx, ps, 1, "waiting for parameter set to apply")).Should(Succeed())
			validateResourceConditions(ps)

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
			inst.Spec.SchemaVersion = "1.0.1"
			Expect(k8sClient.Create(ctx, inst)).Should(Succeed())
			Expect(waitForPorter(ctx, inst, 1, "waiting for porter-test-me to install")).Should(Succeed())
			validateResourceConditions(inst)

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
				// Wait for the patch to apply, this can cause race conditions
			}
			patchPS(ps)
			Expect(waitForPorter(ctx, ps, 2, "waiting for parameters update to apply")).Should(Succeed())
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
			Expect(waitForResourceDeleted(ctx, ps)).Should(Succeed())

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
