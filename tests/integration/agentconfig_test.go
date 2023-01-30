//go:build integration

package integration_test

import (
	"context"

	. "github.com/onsi/ginkgo"
	"github.com/tidwall/gjson"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	porterv1 "get.porter.sh/operator/api/v1"
	. "github.com/onsi/gomega"
)

var _ = Describe("AgentConfig delete", func() {
	Context("when an existing AgentConfig is deleted", func() {
		It("should delete AgentConfig and remove owner reference from all volumes it's associated with", func() {
			By("creating an agent action", func() {
				ctx := context.Background()
				ns := createTestNamespace(ctx)

				agentCfg := NewTestAgentCfg()
				agentCfg.Namespace = ns

				Expect(k8sClient.Create(ctx, &agentCfg.AgentConfig)).Should(Succeed())
				Expect(waitForPorter(ctx, &agentCfg.AgentConfig, 1, "waiting for plugins to be installed")).Should(Succeed())
				validateResourceConditions(agentCfg)
				Expect(len(agentCfg.Spec.Plugins.GetNames())).To(Equal(1))

				Log("verify it's created")
				jsonOut := runAgentAction(ctx, "create-check-plugins-list", ns, []string{"plugins", "list", "-o", "json"})
				firstName := gjson.Get(jsonOut, "0.name").String()
				numPluginsInstalled := gjson.Get(jsonOut, "#").Int()
				Expect(int64(1)).To(Equal(numPluginsInstalled))
				_, ok := agentCfg.Spec.Plugins.GetByName(firstName)
				Expect(ok).To(BeTrue())

				Log("delete a agent config")
				Expect(k8sClient.Delete(ctx, &agentCfg.AgentConfig)).Should(Succeed())
				Expect(waitForResourceDeleted(ctx, &agentCfg.AgentConfig)).Should(Succeed())

				Log("verify persistent volume and claim no longer has the agent config in their owner reference")
				results := &corev1.PersistentVolumeClaimList{}
				Expect(k8sClient.List(ctx, results, client.InNamespace(agentCfg.Namespace), client.MatchingLabels(agentCfg.Spec.Plugins.GetLabels()))).Should(Succeed())
				for _, pvc := range results.Items {
					for _, ow := range pvc.OwnerReferences {
						if ow.Kind == "AgentConfig" {

							Expect(ow.Name).ShouldNot(Equal(agentCfg.Name))
						}
					}
					key := client.ObjectKey{Namespace: agentCfg.Namespace, Name: pvc.Spec.VolumeName}
					pv := &corev1.PersistentVolume{}
					Expect(k8sClient.Get(ctx, key, pv)).Should(Succeed())
					for _, ow := range pv.OwnerReferences {
						if ow.Kind == "AgentConfig" {

							Expect(ow.Name).ShouldNot(Equal(agentCfg.Name))
						}
					}
				}
			})
		})
	})
})

// NewTestCredSet minimal CredentialSet CRD for tests
func NewTestAgentCfg() *porterv1.AgentConfigAdapter {
	cs := porterv1.AgentConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "getporter.org/v1",
			Kind:       "AgentConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "porter-test-me-",
		},
		Spec: porterv1.AgentConfigSpec{
			PluginConfigFile: &porterv1.PluginFileSpec{
				SchemaVersion: "1.0.0",
				Plugins: map[string]porterv1.Plugin{
					"kubernetes": {},
				},
			},
		},
	}
	return porterv1.NewAgentConfigAdapter(cs)
}
