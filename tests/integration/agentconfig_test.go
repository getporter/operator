//go:build integration

package integration_test

import (
	"context"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	"github.com/tidwall/gjson"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	porterv1 "get.porter.sh/operator/api/v1"
	"get.porter.sh/operator/controllers"
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
				verifyAgentConfigPlugins(ctx, agentCfg, "create-check-plugins-list")

				Log("verify retry limit is correctly set")
				job, err := getAgentActionJob(ctx, "create-check-plugins-list", ns)
				Expect(err).Should(BeNil())
				Expect(*job.Spec.BackoffLimit).To(Equal(int32(2)))

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

var _ = Describe("AgentConfig update", func() {
	Context("when an existing AgentConfig is updated", func() {
		It("should update the plugin volumes associated with the AgentConfig", func() {
			By("creating an agent action each time the config is updated", func() {
				ctx := context.Background()
				ns := createTestNamespace(ctx)

				agentCfg := NewTestAgentCfg()
				agentCfg.Namespace = ns

				Expect(k8sClient.Create(ctx, &agentCfg.AgentConfig)).Should(Succeed())
				Expect(waitForPorter(ctx, &agentCfg.AgentConfig, 1, "waiting for plugins to be installed")).Should(Succeed())
				validateResourceConditions(agentCfg)
				Expect(len(agentCfg.Spec.Plugins.GetNames())).To(Equal(1))

				Log("verify it's created")
				verifyAgentConfigPlugins(ctx, agentCfg, "create-check-plugins-list")

				// Update the AgentConfig and patch it
				patchAC := func(ac *porterv1.AgentConfig) error {
					return controllers.PatchObjectWithRetry(ctx, logr.Discard(), k8sClient, k8sClient.Patch, ac, func() client.Object {
						return &porterv1.AgentConfig{}
					})
				}
				k8sClient.Get(ctx, client.ObjectKeyFromObject(&agentCfg.AgentConfig), &agentCfg.AgentConfig)
				// Update the plugins
				agentCfg.AgentConfig.Spec.PluginConfigFile = &porterv1.PluginFileSpec{
					SchemaVersion: "1.0.0",
					Plugins: map[string]porterv1.Plugin{
						"azure": {},
					},
				}
				agentCfg.Spec = porterv1.NewAgentConfigSpecAdapter(agentCfg.AgentConfig.Spec)
				Expect(patchAC(&agentCfg.AgentConfig)).Should(Succeed())
				// Verify that the new job is created
				Expect(waitForPorter(ctx, &agentCfg.AgentConfig, 2, "waiting for agent config to update")).Should(Succeed())
				validateResourceConditions(agentCfg)
				Expect(len(agentCfg.Spec.Plugins.GetNames())).To(Equal(1))
				// Verify its been updated
				Log("verify it's updated")
				verifyAgentConfigPlugins(ctx, agentCfg, "update-check-plugins-list")
				// Verify that the AgentConfig is ready
				k8sClient.Get(ctx, client.ObjectKeyFromObject(&agentCfg.AgentConfig), &agentCfg.AgentConfig)
				Expect(agentCfg.AgentConfig.Status.Ready).To(BeTrue())

				// Update a spec value outside of the plugins
				agentCfg.AgentConfig.Spec.VolumeSize = "50M"
				agentCfg.Spec = porterv1.NewAgentConfigSpecAdapter(agentCfg.AgentConfig.Spec)
				Expect(patchAC(&agentCfg.AgentConfig)).Should(Succeed())
				// Verify that the new job is created
				Expect(waitForPorter(ctx, &agentCfg.AgentConfig, 3, "waiting for agent config to update")).Should(Succeed())
				validateResourceConditions(agentCfg)
				Expect(len(agentCfg.Spec.Plugins.GetNames())).To(Equal(1))
				// Verify the plugins are the same
				Log("verify it's updated")
				verifyAgentConfigPlugins(ctx, agentCfg, "update-check-plugins-list-2")
				// Verify that the AgentConfig is ready
				k8sClient.Get(ctx, client.ObjectKeyFromObject(&agentCfg.AgentConfig), &agentCfg.AgentConfig)
				Expect(agentCfg.AgentConfig.Status.Ready).To(BeTrue())
				Expect(agentCfg.AgentConfig.Spec.VolumeSize).To(Equal("50M"))

				// Revert back to a previously installed plugin
				agentCfg.AgentConfig.Spec.PluginConfigFile = &porterv1.PluginFileSpec{
					SchemaVersion: "1.0.0",
					Plugins: map[string]porterv1.Plugin{
						"kubernetes": {},
					},
				}
				agentCfg.Spec = porterv1.NewAgentConfigSpecAdapter(agentCfg.AgentConfig.Spec)
				Expect(patchAC(&agentCfg.AgentConfig)).Should(Succeed())
				// Verify that the new job is created
				Expect(waitForPorter(ctx, &agentCfg.AgentConfig, 4, "waiting for agent config to update")).Should(Succeed())
				validateResourceConditions(agentCfg)
				Expect(len(agentCfg.Spec.Plugins.GetNames())).To(Equal(1))
				// Verify its been updated
				Log("verify it's updated")
				verifyAgentConfigPlugins(ctx, agentCfg, "revert-check-plugins-list")
				// Verify that the AgentConfig is ready
				k8sClient.Get(ctx, client.ObjectKeyFromObject(&agentCfg.AgentConfig), &agentCfg.AgentConfig)
				Expect(agentCfg.AgentConfig.Status.Ready).To(BeTrue())

			})
		})
	})
})

func verifyAgentConfigPlugins(ctx context.Context, agentCfg *porterv1.AgentConfigAdapter, actionName string) {
	jsonOut := runAgentAction(ctx, actionName, agentCfg.Namespace, agentCfg.Name, []string{"plugins", "list", "-o", "json"})
	firstName := gjson.Get(jsonOut, "0.name").String()
	numPluginsInstalled := gjson.Get(jsonOut, "#").Int()
	Expect(int64(1)).To(Equal(numPluginsInstalled))
	_, ok := agentCfg.Spec.Plugins.GetByName(firstName)
	Expect(ok).To(BeTrue())
}

// NewTestAgentCfg minimal AgentConfig CRD for tests
func NewTestAgentCfg() *porterv1.AgentConfigAdapter {
	var retryLimit int32 = 2
	ac := porterv1.AgentConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "getporter.org/v1",
			Kind:       "AgentConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "porter-test-me-",
		},
		Spec: porterv1.AgentConfigSpec{
			RetryLimit: &retryLimit,
			PluginConfigFile: &porterv1.PluginFileSpec{
				SchemaVersion: "1.0.0",
				Plugins: map[string]porterv1.Plugin{
					"kubernetes": {},
				},
			},
		},
	}
	return porterv1.NewAgentConfigAdapter(ac)
}
