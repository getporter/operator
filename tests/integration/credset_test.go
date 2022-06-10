//go:build integration

package integration_test

import (
	"context"
	"fmt"

	"get.porter.sh/operator/controllers"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	"github.com/tidwall/gjson"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	porterv1 "get.porter.sh/operator/api/v1"
	. "github.com/onsi/gomega"
)

var _ = Describe("CredentialSet create", func() {
	Context("when a new CredentialSet resource is created with secret source", func() {
		It("should run porter", func() {
			By("creating an agent action", func() {
				ctx := context.Background()
				ns := createTestNamespace(ctx)
				name := "test-cs-" + ns
				testSecret := "foo"
				createTestSecret(ctx, name, testSecret, ns)
				Log(fmt.Sprintf("create credential set '%s' for credset", name))
				cs := NewTestCredSet(name)
				cs.ObjectMeta.Namespace = ns
				cred := porterv1.Credential{
					Name: "insecureValue",
					Source: porterv1.CredentialSource{
						Secret: name,
					},
				}
				cs.Spec.Credentials = append(cs.Spec.Credentials, cred)
				cs.Spec.Namespace = ns

				Expect(k8sClient.Create(ctx, cs)).Should(Succeed())
				Expect(waitForPorter(ctx, cs, 1, "waiting for credential set to apply")).Should(Succeed())
				validateResourceConditions(cs)

				Log("install porter-test-me bundle with new credset")
				inst := NewTestInstallation("cs-with-secret")
				inst.ObjectMeta.Namespace = ns
				inst.Spec.Namespace = ns
				inst.Spec.CredentialSets = append(inst.Spec.CredentialSets, name)
				inst.Spec.SchemaVersion = "1.0.1"
				Expect(k8sClient.Create(ctx, inst)).Should(Succeed())
				Expect(waitForPorter(ctx, inst, 1, "waiting for porter-test-me to install")).Should(Succeed())
				validateResourceConditions(inst)

				// Validate that the correct credential set was used by the installation
				jsonOut := runAgentAction(ctx, "show-outputs", ns, []string{"installation", "outputs", "list", "-n", ns, "-i", inst.Spec.Name, "-o", "json"})
				credsValue := gjson.Get(jsonOut, `#(name=="outInsecureValue").value`).String()
				Expect(credsValue).To(Equal(testSecret))
			})
		})
	})
})
var _ = Describe("CredentialSet secret does not exist", func() {
	Context("when an installation is created with a CredentialSet resource that references a secret that does not exist", func() {
		It("should fail the installation install", func() {
			ctx := context.Background()
			ns := createTestNamespace(ctx)
			name := "test-cs-" + ns
			By("successfully creating the credential set", func() {
				Log(fmt.Sprintf("create credential set '%s' for credset", name))
				cs := NewTestCredSet(name)
				cs.ObjectMeta.Namespace = ns
				cred := porterv1.Credential{
					Name: "insecureValue",
					Source: porterv1.CredentialSource{
						Secret: name,
					},
				}
				cs.Spec.Credentials = append(cs.Spec.Credentials, cred)
				cs.Spec.Namespace = ns

				Expect(k8sClient.Create(ctx, cs)).Should(Succeed())
				Expect(waitForPorter(ctx, cs, 1, "waiting for credential set to apply")).Should(Succeed())
				validateResourceConditions(cs)

			})
			By("failing the installation install", func() {
				Log("install porter-test-me bundle with new credset")
				inst := NewTestInstallation("cs-no-secret")
				inst.ObjectMeta.Namespace = ns
				inst.Spec.Namespace = ns
				inst.Spec.CredentialSets = append(inst.Spec.CredentialSets, name)
				inst.Spec.SchemaVersion = "1.0.1"
				Expect(k8sClient.Create(ctx, inst)).Should(Succeed())
				err := waitForPorter(ctx, inst, 1, "waiting for porter-test-me to install")
				Expect(err).Should(HaveOccurred())
				validateResourceConditions(inst)
				Expect(inst.Status.Phase).To(Equal(porterv1.PhaseFailed))
			})
		})
	})
})

// TODO: Add failed installation due to missing secret test
var _ = Describe("CredentialSet update", func() {
	Context("when an existing CredentialSet resource is updated", func() {
		It("should run porter apply", func() {
			By("creating an agent action", func() {
				ctx := context.Background()
				ns := createTestNamespace(ctx)
				name := "cs-update-" + ns

				Log(fmt.Sprintf("create credential set '%s' for credset", name))
				cs := NewTestCredSet(name)
				cs.Spec.Name = "cs-update-test"
				cs.ObjectMeta.Namespace = ns
				cred := porterv1.Credential{
					Name: "test-credential",
					Source: porterv1.CredentialSource{
						Secret: name,
					},
				}
				cs.Spec.Credentials = append(cs.Spec.Credentials, cred)
				cs.Spec.Namespace = ns

				Expect(k8sClient.Create(ctx, cs)).Should(Succeed())
				Expect(waitForPorter(ctx, cs, 1, "waiting for credential set to apply")).Should(Succeed())
				validateResourceConditions(cs)

				Log("verify it's created")
				jsonOut := runAgentAction(ctx, "create-check-credentials-list", ns, []string{"credentials", "list", "-n", ns, "-o", "json"})
				firstName := gjson.Get(jsonOut, "0.name").String()
				numCredSets := gjson.Get(jsonOut, "#").Int()
				numCreds := gjson.Get(jsonOut, "0.credentials.#").Int()
				firstCredName := gjson.Get(jsonOut, "0.credentials.0.name").String()
				Expect(int64(1)).To(Equal(numCredSets))
				Expect(int64(1)).To(Equal(numCreds))
				Expect(cs.Spec.Name).To(Equal(firstName))
				Expect("test-credential").To(Equal(firstCredName))

				Log("update a credential set")
				newSecret := "update-credset"
				newCred := porterv1.Credential{
					Name: "new-credential",
					Source: porterv1.CredentialSource{
						Secret: newSecret,
					},
				}
				cs.Spec.Credentials = append(cs.Spec.Credentials, newCred)
				patchCS := func(cs *porterv1.CredentialSet) {
					controllers.PatchObjectWithRetry(ctx, logr.Discard(), k8sClient, k8sClient.Patch, cs, func() client.Object {
						return &porterv1.CredentialSet{}
					})
				}
				patchCS(cs)
				Expect(waitForPorter(ctx, cs, 2, "waiting for credential update to apply")).Should(Succeed())
				Log("verify it's updated")
				jsonOut = runAgentAction(ctx, "update-check-credentials-list", ns, []string{"credentials", "list", "-n", ns, "-o", "json"})
				updatedFirstName := gjson.Get(jsonOut, "0.name").String()
				updatedNumCredSets := gjson.Get(jsonOut, "#").Int()
				updatedNumCreds := gjson.Get(jsonOut, "0.credentials.#").Int()
				updatedFirstCredName := gjson.Get(jsonOut, "0.credentials.0.name").String()
				updatedSecondCredName := gjson.Get(jsonOut, "0.credentials.1.name").String()
				Expect(int64(1)).To(Equal(updatedNumCredSets))
				Expect(int64(2)).To(Equal(updatedNumCreds))
				Expect(cs.Spec.Name).To(Equal(updatedFirstName))
				Expect("test-credential").To(Equal(updatedFirstCredName))
				Expect("new-credential").To(Equal(updatedSecondCredName))
			})
		})
	})
})

var _ = Describe("CredentialSet delete", func() {
	Context("when an existing CredentialSet is delete", func() {
		It("should run porter credentials delete", func() {
			By("creating an agent action", func() {
				ctx := context.Background()
				ns := createTestNamespace(ctx)
				name := "cs-delete-" + ns
				testSecret := "secret-value"

				Log(fmt.Sprintf("create k8s secret '%s' for credset", name))
				csSecret := NewTestSecret(name, testSecret)
				csSecret.ObjectMeta.Namespace = ns
				Expect(k8sClient.Create(ctx, csSecret)).Should(Succeed())

				Log(fmt.Sprintf("create credential set '%s' for credset", name))
				cs := NewTestCredSet(name)
				cs.Spec.Name = "cs-delete-test"
				cs.ObjectMeta.Namespace = ns
				cred := porterv1.Credential{
					Name: "test-credential",
					Source: porterv1.CredentialSource{
						Secret: name,
					},
				}
				cs.Spec.Credentials = append(cs.Spec.Credentials, cred)
				cs.Spec.Namespace = ns

				Expect(k8sClient.Create(ctx, cs)).Should(Succeed())
				Expect(waitForPorter(ctx, cs, 1, "waiting for credential set to apply")).Should(Succeed())
				validateResourceConditions(cs)

				Log("verify it's created")
				jsonOut := runAgentAction(ctx, "create-check-credentials-list", ns, []string{"credentials", "list", "-n", ns, "-o", "json"})
				firstName := gjson.Get(jsonOut, "0.name").String()
				numCreds := gjson.Get(jsonOut, "#").Int()
				firstCredName := gjson.Get(jsonOut, "0.credentials.0.name").String()
				Expect(int64(1)).To(Equal(numCreds))
				Expect(cs.Spec.Name).To(Equal(firstName))
				Expect("test-credential").To(Equal(firstCredName))

				Log("delete a credential set")
				Expect(k8sClient.Delete(ctx, cs)).Should(Succeed())
				Expect(waitForResourceDeleted(ctx, cs)).Should(Succeed())

				Log("verify credential set is gone from porter data store")
				delJsonOut := runAgentAction(ctx, "delete-check-credentials-list", ns, []string{"credentials", "list", "-n", ns, "-o", "json"})
				delNumCreds := gjson.Get(delJsonOut, "#").Int()
				Expect(int64(0)).To(Equal(delNumCreds))

				Log("verify credential set CRD is gone from k8s cluster")
				nsName := apitypes.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}
				getCS := &porterv1.CredentialSet{}
				Expect(k8sClient.Get(ctx, nsName, getCS)).ShouldNot(Succeed())
			})
		})
	})
})

//NewTestCredSet minimal CredentialSet CRD for tests
func NewTestCredSet(csName string) *porterv1.CredentialSet {
	cs := &porterv1.CredentialSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "porter.sh/v1",
			Kind:       "CredentialSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "porter-test-me-",
		},
		Spec: porterv1.CredentialSetSpec{
			//TODO: get schema version from porter version?
			// https://github.com/getporter/porter/pull/2052
			SchemaVersion: "1.0.1",
			Name:          csName,
		},
	}
	return cs
}

func createTestSecret(ctx context.Context, name, value, namespace string) {
	Log(fmt.Sprintf("create k8s secret '%s' for credset", name))
	csSecret := NewTestSecret(name, value)
	csSecret.ObjectMeta.Namespace = namespace
	Expect(k8sClient.Create(ctx, csSecret)).Should(Succeed())
}

func NewTestSecret(name, value string) *corev1.Secret {
	csSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: map[string]string{"value": value},
	}
	return csSecret
}

func NewTestInstallation(iName string) *porterv1.Installation {
	inst := &porterv1.Installation{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "porter.sh/v1",
			Kind:       "Installation",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "porter-test-me-",
		},
		Spec: porterv1.InstallationSpec{
			SchemaVersion: "1.0.1",
			Name:          iName,
			Bundle: porterv1.OCIReferenceParts{
				Repository: "ghcr.io/bdegeeter/porter-test-me",
				Version:    "0.3.0",
			},
		},
	}
	return inst
}

func newAgentAction(namespace string, name string, cmd []string) *porterv1.AgentAction {
	return &porterv1.AgentAction{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: porterv1.AgentActionSpec{
			Args: cmd,
		},
	}
}
