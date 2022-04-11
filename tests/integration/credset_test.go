//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"get.porter.sh/operator/controllers"
	"github.com/carolynvs/magex/shx"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	"github.com/pkg/errors"
	"github.com/tidwall/gjson"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	cl "k8s.io/client-go/kubernetes"
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
				Expect(waitForPorterCS(ctx, cs, "waiting for credential set to apply")).Should(Succeed())
				validateCredSetConditions(cs)

				Log("install porter-test-me bundle with new credset")
				inst := NewTestInstallation("cs-with-secret")
				inst.ObjectMeta.Namespace = ns
				inst.Spec.Namespace = ns
				inst.Spec.CredentialSets = append(inst.Spec.CredentialSets, name)
				inst.Spec.SchemaVersion = "1.0.0"
				Expect(k8sClient.Create(ctx, inst)).Should(Succeed())
				Expect(waitForPorter(ctx, inst, "waiting for porter-test-me to install")).Should(Succeed())
				validateInstallationConditions(inst)

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
				Expect(waitForPorterCS(ctx, cs, "waiting for credential set to apply")).Should(Succeed())
				validateCredSetConditions(cs)

			})
			By("failing the installation install", func() {
				Log("install porter-test-me bundle with new credset")
				inst := NewTestInstallation("cs-no-secret")
				inst.ObjectMeta.Namespace = ns
				inst.Spec.Namespace = ns
				inst.Spec.CredentialSets = append(inst.Spec.CredentialSets, name)
				inst.Spec.SchemaVersion = "1.0.0"
				Expect(k8sClient.Create(ctx, inst)).Should(Succeed())
				err := waitForPorter(ctx, inst, "waiting for porter-test-me to install")
				Expect(err).Should(HaveOccurred())
				validateInstallationConditions(inst)
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
				Expect(waitForPorterCS(ctx, cs, "waiting for credential set to apply")).Should(Succeed())
				validateCredSetConditions(cs)

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
				Expect(waitForPorterCS(ctx, cs, "waiting for credential update to apply")).Should(Succeed())
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
				Expect(waitForPorterCS(ctx, cs, "waiting for credential set to apply")).Should(Succeed())
				validateCredSetConditions(cs)

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
				Expect(waitForCredSetDeleted(ctx, cs)).Should(Succeed())

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
			SchemaVersion: schemaVersion,
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
			SchemaVersion: schemaVersion,
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

func waitForAgentAction(ctx context.Context, aa *porterv1.AgentAction, msg string) error {
	Log("%s: %s/%s", msg, aa.Namespace, aa.Name)
	key := client.ObjectKey{Namespace: aa.Namespace, Name: aa.Name}
	ctx, cancel := context.WithTimeout(ctx, getWaitTimeout())
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			Fail(errors.Wrapf(ctx.Err(), "timeout %s", msg).Error())
		default:
			err := k8sClient.Get(ctx, key, aa)
			if err != nil {
				// There is lag between creating and being able to retrieve, I don't understand why
				if apierrors.IsNotFound(err) {
					time.Sleep(time.Second)
					continue
				}
				return err
			}

			// Check if the latest change has been processed
			if aa.Generation == aa.Status.ObservedGeneration {
				if apimeta.IsStatusConditionTrue(aa.Status.Conditions, string(porterv1.ConditionComplete)) {
					return nil
				}

				if apimeta.IsStatusConditionTrue(aa.Status.Conditions, string(porterv1.ConditionFailed)) {
					// Grab some extra info to help with debugging
					//debugFailedCSCreate(ctx, aa)
					return errors.New("porter did not run successfully")
				}
			}

			time.Sleep(time.Second)
			continue
		}
	}

}

func waitForPorterCS(ctx context.Context, cs *porterv1.CredentialSet, msg string) error {
	Log("%s: %s/%s", msg, cs.Namespace, cs.Name)
	key := client.ObjectKey{Namespace: cs.Namespace, Name: cs.Name}
	ctx, cancel := context.WithTimeout(ctx, getWaitTimeout())
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			Fail(errors.Wrapf(ctx.Err(), "timeout %s", msg).Error())
		default:
			err := k8sClient.Get(ctx, key, cs)
			if err != nil {
				// There is lag between creating and being able to retrieve, I don't understand why
				if apierrors.IsNotFound(err) {
					time.Sleep(time.Second)
					continue
				}
				return err
			}

			// Check if the latest change has been processed
			if cs.Generation == cs.Status.ObservedGeneration {
				if apimeta.IsStatusConditionTrue(cs.Status.Conditions, string(porterv1.ConditionComplete)) {
					time.Sleep(time.Second)
					return nil
				}

				if apimeta.IsStatusConditionTrue(cs.Status.Conditions, string(porterv1.ConditionFailed)) {
					// Grab some extra info to help with debugging
					debugFailedCSCreate(ctx, cs)
					return errors.New("porter did not run successfully")
				}
			}

			time.Sleep(time.Second)
			continue
		}
	}
}

func waitForCredSetDeleted(ctx context.Context, cs *porterv1.CredentialSet) error {
	Log("Waiting for CredentialSet to finish deleting: %s/%s", cs.Namespace, cs.Name)
	key := client.ObjectKey{Namespace: cs.Namespace, Name: cs.Name}
	waitCtx, cancel := context.WithTimeout(ctx, getWaitTimeout())
	defer cancel()
	for {
		select {
		case <-waitCtx.Done():
			Fail(errors.Wrap(waitCtx.Err(), "timeout waiting for CredentialSet to delete").Error())
		default:
			err := k8sClient.Get(ctx, key, cs)
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

func debugFailedCSCreate(ctx context.Context, cs *porterv1.CredentialSet) {
	Log("DEBUG: ----------------------------------------------------")
	actionKey := client.ObjectKey{Name: cs.Status.Action.Name, Namespace: cs.Namespace}
	action := &porterv1.AgentAction{}
	if err := k8sClient.Get(ctx, actionKey, action); err != nil {
		Log(errors.Wrap(err, "could not retrieve the CredentialSet's AgentAction to troubleshoot").Error())
		return
	}

	jobKey := client.ObjectKey{Name: action.Status.Job.Name, Namespace: action.Namespace}
	job := &batchv1.Job{}
	if err := k8sClient.Get(ctx, jobKey, job); err != nil {
		Log(errors.Wrap(err, "could not retrieve the CredentialSet's Job to troubleshoot").Error())
		return
	}

	shx.Command("kubectl", "logs", "-n="+job.Namespace, "job/"+job.Name).
		Env("KUBECONFIG=" + "../../kind.config").RunV()
	Log("DEBUG: ----------------------------------------------------")
}

func validateCredSetConditions(cs *porterv1.CredentialSet) {
	// Checks that all expected conditions are set
	Expect(apimeta.IsStatusConditionTrue(cs.Status.Conditions, string(porterv1.ConditionScheduled)))
	Expect(apimeta.IsStatusConditionTrue(cs.Status.Conditions, string(porterv1.ConditionStarted)))
	Expect(apimeta.IsStatusConditionTrue(cs.Status.Conditions, string(porterv1.ConditionComplete)))
}

// Get the pod logs associated to the job created by the agent action
func getAgentActionJobOutput(ctx context.Context, agentActionName string, namespace string) (string, error) {
	actionKey := client.ObjectKey{Name: agentActionName, Namespace: namespace}
	action := &porterv1.AgentAction{}
	if err := k8sClient.Get(ctx, actionKey, action); err != nil {
		Log(errors.Wrap(err, "could not retrieve the CredentialSet's AgentAction to troubleshoot").Error())
		return "", err
	}
	// Find the job associated with the agent action
	jobKey := client.ObjectKey{Name: action.Status.Job.Name, Namespace: action.Namespace}
	job := &batchv1.Job{}
	if err := k8sClient.Get(ctx, jobKey, job); err != nil {
		Log(errors.Wrap(err, "could not retrieve the Job to troubleshoot").Error())
		return "", err
	}
	// Create a new k8s client that's use for fetching pod logs. This is not implemented on the controller-runtime client
	c, err := cl.NewForConfig(testEnv.Config)
	if err != nil {
		Log(err.Error())
		return "", err
	}
	selector, err := metav1.LabelSelectorAsSelector(job.Spec.Selector)
	if err != nil {
		Log(errors.Wrap(err, "could not retrieve label selector for job").Error())
		return "", err
	}
	// Get the pod associated with the job. There should only be 1
	pods, err := c.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		Log(errors.Wrap(err, "could not retrive pod list for job").Error())
		return "", err
	}
	if len(pods.Items) != 1 {
		Log(fmt.Sprintf("too many pods associated to agent action job. Expected 1 found %v", len(pods.Items)))
		return "", err
	}
	podLogOpts := corev1.PodLogOptions{}
	// Fetch the pod logs
	req := c.CoreV1().Pods(namespace).GetLogs(pods.Items[0].Name, &podLogOpts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		Log(errors.Wrap(err, "could not stream pod logs").Error())
		return "", err
	}
	defer podLogs.Close()
	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		Log(errors.Wrap(err, "could not copy pod logs to byte buffer").Error())
		return "", err
	}
	outputLog := buf.String()
	return outputLog, nil
}

func getAgentActionCmdOut(action *porterv1.AgentAction, aaOut string) string {
	return strings.SplitAfterN(strings.Replace(aaOut, "\n", "", -1), strings.Join(action.Spec.Args, " "), 2)[1]
}

/* Fully execute an agent action and return the associated result of the command executed. For example an agent action
that does "porter credentials list" will return just the result of the porter command from the job logs. This can be
used to run porter commands inside the cluster to validate porter state
*/
func runAgentAction(ctx context.Context, actionName string, namespace string, cmd []string) string {
	aa := newAgentAction(namespace, actionName, cmd)
	Expect(k8sClient.Create(ctx, aa)).Should(Succeed())
	Expect(waitForAgentAction(ctx, aa, fmt.Sprintf("waiting for action %s to run", actionName))).Should(Succeed())
	aaOut, err := getAgentActionJobOutput(ctx, aa.Name, namespace)
	Expect(err).Error().ShouldNot(HaveOccurred())
	return getAgentActionCmdOut(aa, aaOut)
}
