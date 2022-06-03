//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	porterv1 "get.porter.sh/operator/api/v1"
	"github.com/carolynvs/magex/shx"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"github.com/tidwall/pretty"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cl "k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type porterStatus interface {
	GetConditions() *[]metav1.Condition
	GetObservedGeneration() int64
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

func Log(value string, args ...interface{}) {
	GinkgoWriter.Write([]byte(fmt.Sprintf(value+"\n", args...)))
}

func LogJson(value string) {
	GinkgoWriter.Write(pretty.Pretty([]byte(value + "\n")))
}

func waitForPorter(ctx context.Context, resource client.Object, status porterStatus, namespace, name, msg string) error {
	Log("%s: %s/%s", msg, namespace, name)
	key := client.ObjectKey{Namespace: namespace, Name: name}
	ctx, cancel := context.WithTimeout(ctx, getWaitTimeout())
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			Fail(errors.Wrapf(ctx.Err(), "timeout %s", msg).Error())
		default:
			err := k8sClient.Get(ctx, key, resource)
			if err != nil {
				// There is lag between creating and being able to retrieve, I don't understand why
				if apierrors.IsNotFound(err) {
					time.Sleep(time.Second)
					continue
				}
				return err
			}
			conditions := status.GetConditions()
			// Check if the latest change has been processed
			if resource.GetGeneration() == status.GetObservedGeneration() {
				if apimeta.IsStatusConditionTrue(*conditions, string(porterv1.ConditionComplete)) {
					time.Sleep(time.Second)
					return nil
				}

				if apimeta.IsStatusConditionTrue(*conditions, string(porterv1.ConditionFailed)) {
					// Grab some extra info to help with debugging
					debugFailedResource(ctx, name, namespace)
					return errors.New("porter did not run successfully")
				}
			}

			time.Sleep(time.Second)
			continue
		}
	}
}

func waitForResourceDeleted(ctx context.Context, resource client.Object, namespace, name string) error {
	Log("Waiting for resource to finish deleting: %s/%s", namespace, name)
	key := client.ObjectKey{Namespace: namespace, Name: name}
	waitCtx, cancel := context.WithTimeout(ctx, getWaitTimeout())
	defer cancel()
	for {
		select {
		case <-waitCtx.Done():
			Fail(errors.Wrap(waitCtx.Err(), "timeout waiting for CredentialSet to delete").Error())
		default:
			err := k8sClient.Get(ctx, key, resource)
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

func debugFailedResource(ctx context.Context, namespace, name string) {

	Log("DEBUG: ----------------------------------------------------")
	actionKey := client.ObjectKey{Name: name, Namespace: namespace}
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

func validateResourceConditions(conditions []metav1.Condition) {
	// Checks that all expected conditions are set
	Expect(apimeta.IsStatusConditionTrue(conditions, string(porterv1.ConditionScheduled)))
	Expect(apimeta.IsStatusConditionTrue(conditions, string(porterv1.ConditionStarted)))
	Expect(apimeta.IsStatusConditionTrue(conditions, string(porterv1.ConditionComplete)))
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
	Expect(waitForPorter(ctx, aa, &aa.Status, aa.Namespace, aa.Name, fmt.Sprintf("waiting for action %s to run", actionName))).Should(Succeed())
	aaOut, err := getAgentActionJobOutput(ctx, aa.Name, namespace)
	Expect(err).Error().ShouldNot(HaveOccurred())
	return getAgentActionCmdOut(aa, aaOut)
}
