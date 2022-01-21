//go:build integration
// +build integration

package controllers_test

import (
	"context"
	"fmt"
	"time"

	apiv1 "get.porter.sh/operator/api/v1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	gomegatypes "github.com/onsi/gomega/types"
	"github.com/pkg/errors"
	"github.com/tidwall/pretty"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Installation controller", func() {

	// Define utility constants for object names and testing timeouts/durations and intervals.
	const (
		InstallationName        = "porter-hello"
		AffinityMatchLabelValue = "porter.sh/resourceKind=Installation porter.sh/resourceName=" + InstallationName + " porter.sh/resourceGeneration=1"
	)

	Context("When working with Porter", func() {
		It("Should execute Porter", func() {
			By("By creating a new Installation")
			ctx := context.Background()

			inst := &apiv1.Installation{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "porter.sh/v1",
					Kind:       "Installation",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      InstallationName,
					Namespace: testNamespace,
				},
				Spec: apiv1.InstallationSpec{
					SchemaVersion: "1.0.0",
					Name:          "hello",
					Namespace:     "operator-tests",
					Bundle: apiv1.OCIReferenceParts{
						Repository: "getporter/porter-hello",
						Version:    "0.1.1",
					},
				},
			}
			Expect(k8sClient.Create(ctx, inst)).Should(Succeed())

			// Wait for the job to be created
			jobs := waitForJobStarted(ctx)
			job := jobs.Items[0]

			// Validate that the job succeeded
			job = waitForJobFinished(ctx, job)

			// If the job failed, print some debug info
			if job.Status.Succeeded == 0 {
				Log("+++JOB (%s)+++", job.Name)
				LogJson(job.Status.String())

				Log("+++POD+++")
				pods := &corev1.PodList{}
				k8sClient.List(ctx, pods, client.HasLabels{"job-name=" + job.Name})
				if len(pods.Items) > 0 {
					LogJson(pods.Items[0].String())
				}
				Fail("The job was not successful")
			}
		})
	})
})

func waitForJobStarted(ctx context.Context) batchv1.JobList {
	jobs := batchv1.JobList{}
	inNamespace := client.InNamespace(testNamespace)
	waitCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	for {
		select {
		case <-waitCtx.Done():
			Fail(errors.Wrap(waitCtx.Err(), "timeout waiting for job to be created").Error())
		default:
			err := k8sClient.List(ctx, &jobs, inNamespace)
			Expect(err).Should(Succeed())
			if len(jobs.Items) > 0 {
				return jobs
			}

			time.Sleep(time.Second)
			continue
		}
	}
}

func waitForJobFinished(ctx context.Context, job batchv1.Job) batchv1.Job {
	waitCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	for {
		select {
		case <-waitCtx.Done():
			fmt.Println(job.String())
			Fail(errors.Wrapf(waitCtx.Err(), "timeout waiting for job %s/%s to complete", job.Namespace, job.Name).Error())
		default:
			jobName := types.NamespacedName{Name: job.Name, Namespace: job.Namespace}
			Expect(k8sClient.Get(waitCtx, jobName, &job)).To(Succeed())

			if IsJobDone(job.Status) {
				return job
			}

			time.Sleep(500 * time.Millisecond)
		}
	}
}

func IsVolume(name string) gomegatypes.GomegaMatcher {
	return WithTransform(func(v corev1.Volume) string { return v.Name }, Equal(name))
}

func IsVolumeMount(name string) gomegatypes.GomegaMatcher {
	return WithTransform(func(v corev1.VolumeMount) string { return v.Name }, Equal(name))
}

func IsJobDone(status batchv1.JobStatus) bool {
	for _, c := range status.Conditions {
		if c.Type == batchv1.JobFailed || c.Type == batchv1.JobComplete {
			return true
		}
	}

	return false
}

func Log(value string, args ...interface{}) {
	GinkgoWriter.Write([]byte(fmt.Sprintf(value, args...)))
}

func LogJson(value string) {
	GinkgoWriter.Write(pretty.Pretty([]byte(value)))
}
