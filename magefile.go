// +build mage

// This is a magefile, and is a "makefile for go".
// See https://magefile.org/
package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	. "get.porter.sh/operator/mage"
	"github.com/carolynvs/magex/mgx"
	"github.com/carolynvs/magex/pkg"
	"github.com/carolynvs/magex/pkg/gopath"
	"github.com/carolynvs/magex/shx"
	"github.com/magefile/mage/mg"
	"github.com/pkg/errors"

	// mage:import
	. "get.porter.sh/porter/mage/tests"
)

// Default target to run when none is specified
// If not set, running mage will list available targets
// var Default = Build

const (
	// Version of KIND to install if not already present
	kindVersion = "v0.10.0"

	// Name of the KIND cluster used for testing
	kindClusterName = "porter"

	// Namespace where you can do manual testing
	testNamespace = "test"

	// Relative location of the KUBECONFIG for the test cluster
	kubeconfig = "kind.config"

	// Namespace of the porter operator
	operatorNamespace = "porter-operator-system"

	// Container name of the local registry
	registryContainer = "registry"
)

// Build a command that stops the build on if the command fails
var must = shx.CommandBuilder{StopOnError: true}

// Ensure mage is installed.
func EnsureMage() error {
	addGopathBinOnGithubActions()
	return pkg.EnsureMage("v1.11.0")
}

// Add GOPATH/bin to the path on the GitHub Actions agent
// TODO: Add to magex
func addGopathBinOnGithubActions() error {
	githubPath := os.Getenv("GITHUB_PATH")
	if githubPath == "" {
		return nil
	}

	log.Println("Adding GOPATH/bin to the PATH for the GitHub Actions Agent")
	gopathBin := gopath.GetGopathBin()
	return ioutil.WriteFile(githubPath, []byte(gopathBin), 0644)
}

func Generate() {
	must.RunV("controller-gen", `object:headerFile="hack/boilerplate.go.txt"`, `paths="./..."`)
}

func Fmt() {
	must.RunV("go", "fmt", "./...")
}

func Vet() {
	must.RunV("go", "vet", "./...")
}

// Compile the operator.
func Build() {
	mg.Deps(Fmt, Vet)

	LoadMetadatda()

	must.RunV("go", "build", "-o", "bin/manager", "main.go")
}

// Build the porter-operator bundle.
func BuildBundle() {
	mg.SerialDeps(BuildManifests)

	mgx.Must(shx.Copy("manifests.yaml", "installer/manifests/operator.yaml"))

	meta := LoadMetadatda()
	must.Command("porter", "build", "--version", strings.TrimPrefix(meta.Version, "v")).In("installer").RunV()
}

func Publish() {
	mg.Deps(PublishController, PublishBundle)
}

// Push the porter-operator bundle to a registry. Defaults to the local test registry.
func PublishBundle() {
	mg.Deps(BuildBundle)
	must.Command("porter", "publish", "--registry", Env.Registry).In("installer").RunV()

	meta := LoadMetadatda()
	must.Command("porter", "publish", "--registry", Env.Registry, "--tag", meta.Permalink).In("installer").RunV()
}

func BuildManifests() {
	mg.Deps(EnsureKustomize, EnsureControllerGen)

	fmt.Println("Using environment", Env.Name)
	meta := LoadMetadatda()
	img := Env.ControllerImagePrefix + meta.Version
	kustomize("edit", "set", "image", "manager="+img).In("config/manager").Run()

	if err := os.Remove("manifests.yaml"); err != nil && !os.IsNotExist(err) {
		mgx.Must(errors.Wrap(err, "could not remove generated manifests directory"))
	}

	// Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
	crdOpts := "crd:trivialVersions=true,preserveUnknownFields=false"

	must.RunV("controller-gen", crdOpts, "rbac:roleName=manager-role", "webhook", `paths="./..."`, "output:crd:artifacts:config=config/crd/bases")
	kustomize("build", "config/default", "-o", "manifests.yaml").RunV()
}

// Run all tests
func Test() {
	mg.Deps(TestUnit, TestIntegration)
}

// Run unit tests.
func TestUnit() {
	must.RunV("go", "test", "./...", "-coverprofile", "coverage-unit.out")
}

// Run integration tests against the test cluster.
func TestIntegration() {
	mg.Deps(UseTestEnvironment, CleanTests, EnsureGinkgo, EnsureDeployed)

	must.RunV("go", "test", "-tags=integration", "./...", "-coverprofile=coverage-integration.out")
}

func EnsureDeployed() {
	if !isDeployed() {
		Deploy()
	}
}

// Build the operator and deploy it to the test cluster.
func Deploy() {
	mg.Deps(UseTestEnvironment, EnsureTestCluster, StartDockerRegistry, Build)

	BuildManifests()
	kubectl("apply", "-f", "manifests.yaml").Run()
	PublishController()
	kubectl("rollout", "restart", "deployment/porter-operator-controller-manager", "--namespace", operatorNamespace).RunV()
}

func isDeployed() bool {
	if useCluster() {
		err := kubectl("rollout", "status", "deployment", "porter-operator-controller-manager", "--namespace", operatorNamespace).Must(false).RunS()
		return err == nil
	}
	return false
}

func PublishImages() {
	mg.Deps(PublishController)
}

func PublishController() {
	meta := LoadMetadatda()
	img := Env.ControllerImagePrefix + meta.Version
	log.Println("Building", img)
	must.Command("docker", "build", "-t", img, ".").
		Env("DOCKER_BUILDKIT=1").RunV()

	imgPermalink := Env.ControllerImagePrefix + meta.Permalink
	log.Println("Tagging as", imgPermalink)
	must.RunV("docker", "tag", img, imgPermalink)

	log.Println("Pushing", img)
	must.RunV("docker", "push", img)

	log.Println("Pushing", imgPermalink)
	must.RunV("docker", "push", imgPermalink)
}

// Reapply the file in config/samples, usage: mage bump porter-hello.
func Bump(sample string) {
	mg.Deps(EnsureTestNamespace, EnsureYq)

	sampleFile := fmt.Sprintf("config/samples/%s.yaml", sample)

	dataB, err := ioutil.ReadFile(sampleFile)
	mgx.Must(errors.Wrapf(err, "error reading installation definition %s", sampleFile))

	retryCountField := ".metadata.annotations.retry"

	crd, _ := must.Command("yq", "eval", fmt.Sprintf("%s = %q", retryCountField, time.Now().String()), "-").
		Stdin(bytes.NewReader(dataB)).OutputE()

	log.Println(crd)
	kubectl("apply", "--namespace", testNamespace, "-f", "-").Stdin(strings.NewReader(crd)).RunV()
}

// Ensures that a namespace named "test" exists.
func EnsureTestNamespace() {
	mg.Deps(EnsureDeployed)
	if !namespaceExists(testNamespace) {
		setupTestNamespace()
	}
}

func setupTestNamespace() {
	SetupNamespace(testNamespace)
}

func namespaceExists(name string) bool {
	err := kubectl("get", "namespace", name).Must(false).RunS()
	return err == nil
}

// Create a namespace, usage: mage SetupNamespace demo.
// Configures the namespace for use with the operator.
func SetupNamespace(name string) {
	mg.Deps(EnsureTestCluster)

	// TODO: Use a bundle to install porter in a local dev env and invoke configure-namespace

	if namespaceExists(name) {
		kubectl("delete", "ns", name, "--wait=true").RunS()
	}

	kubectl("create", "namespace", name).RunE()
	kubectl("label", "namespace", name, "porter-test=true").RunE()

	agentCfg := `apiVersion: porter.sh/v1
kind: AgentConfig
metadata:
  name: porter
  labels:
    sh.porter/managed: "true"
spec:
  porterRepository: localhost:5000/porter-agent
  porterVersion: canary-dev
  serviceAccount: porter-agent
`
	kubectl("apply", "--namespace", name, "-f", "-").
		Stdin(strings.NewReader(agentCfg)).RunV()

	kubectl("create", "secret", "generic", "porter-config", "--namespace", name,
		"--from-file=config.toml=hack/porter-config.toml").RunE()

	kubectl("create", "secret", "generic", "porter-env", "--namespace", name,
		"--from-literal=AZURE_STORAGE_CONNECTION_STRING="+os.Getenv("PORTER_TEST_AZURE_STORAGE_CONNECTION_STRING"),
		"--from-literal=AZURE_CLIENT_SECRET="+os.Getenv("PORTER_AZURE_CLIENT_SECRET"),
		"--from-literal=AZURE_CLIENT_ID="+os.Getenv("PORTER_AZURE_CLIENT_ID"),
		"--from-literal=AZURE_TENANT_ID="+os.Getenv("PORTER_AZURE_TENANT_ID")).RunE()

	kubectl("create", "serviceaccount", "porter-agent", "--namespace", name).RunE()

	kubectl("create", "rolebinding", "porter-agent",
		"--clusterrole", "porter-operator-agent-role",
		"--serviceaccount", name+":porter-agent",
		"--namespace", name).RunE()

	kubectl("create", "serviceaccount", "installation-service-account", "--namespace", name).RunE()

	setClusterNamespace(name)
}

// Delete operator data from the test cluster.
func Clean() {
	mg.Deps(CleanManual, CleanTests)
}

// Remove data created by running the test suite
func CleanTests() {
	if useCluster() {
		kubectl("delete", "ns", "-l", "porter-test=true").RunV()
	}
}

// Remove any porter data in the cluster
func CleanManual() {
	if useCluster() {
		kubectl("delete", "jobs", "-l", "porter=true").RunV()
		kubectl("delete", "secrets", "-l", "porter-test=true").RunV()
	}
}

// Follow the logs for the operator.
func Logs() {
	mg.Deps(EnsureKubectl)

	kubectl("logs", "-f", "deployment/porter-operator-controller-manager", "-c=manager", "--namespace", operatorNamespace).RunV()
}

// Ensure operator-sdk is installed.
func EnsureOperatorSDK() {
	const version = "v1.3.0"

	if runtime.GOOS == "windows" {
		mgx.Must(errors.New("Sorry, OperatorSDK does not support Windows. In order to contribute to this repository, you will need to use WSL."))
	}

	url := "https://github.com/operator-framework/operator-sdk/releases/{{.VERSION}}/download/operator-sdk_{{.GOOS}}_{{.GOARCH}}"
	mgx.Must(pkg.DownloadToGopathBin(url, "operator-sdk", version))
}

// get the config of the current kind cluster, if available
func getClusterConfig() (kubeconfig string, ok bool) {
	contents, err := shx.OutputE("kind", "get", "kubeconfig", "--name", kindClusterName)
	return contents, err == nil
}

// setup environment to use the current kind cluster, if available
func useCluster() bool {
	contents, ok := getClusterConfig()
	if ok {
		log.Println("Reusing existing kind cluster")

		userKubeConfig, _ := filepath.Abs(os.Getenv("KUBECONFIG"))
		currentKubeConfig := filepath.Join(pwd(), kubeconfig)
		if userKubeConfig != currentKubeConfig {
			fmt.Printf("ATTENTION! You should set your KUBECONFIG to match the cluster used by this project\n\n\texport KUBECONFIG=%s\n\n", currentKubeConfig)
		}
		os.Setenv("KUBECONFIG", currentKubeConfig)

		err := ioutil.WriteFile(kubeconfig, []byte(contents), 0644)
		mgx.Must(errors.Wrapf(err, "error writing %s", kubeconfig))

		setClusterNamespace(operatorNamespace)
		return true
	}

	return false
}

func setClusterNamespace(name string) {
	must.RunE("kubectl", "config", "set-context", "--current", "--namespace", name)
}

// Run a makefile target
func makefile(args ...string) shx.PreparedCommand {
	cmd := must.Command("make", args...)
	cmd.Env("KUBECONFIG=" + os.Getenv("KUBECONFIG"))

	return cmd
}

func kubectl(args ...string) shx.PreparedCommand {
	kubeconfig := fmt.Sprintf("KUBECONFIG=%s", os.Getenv("KUBECONFIG"))
	return must.Command("kubectl", args...).Env(kubeconfig)
}

func kustomize(args ...string) shx.PreparedCommand {
	cmd := filepath.Join(pwd(), "bin/kustomize")
	return must.Command(cmd, args...)
}

// Ensure yq is installed.
func EnsureYq() {
	mgx.Must(pkg.EnsurePackage("github.com/mikefarah/yq/v4", "", ""))
}

// Ensure ginkgo is installed.
func EnsureGinkgo() {
	mgx.Must(pkg.EnsurePackage("github.com/onsi/ginkgo/ginkgo", "", ""))
}

// Ensure kustomize is installed.
func EnsureKustomize() {
	// TODO: implement installing from a URL that is tgz
	makefile("kustomize").Run()
}

// Ensure controller-gen is installed.
func EnsureControllerGen() {
	mgx.Must(pkg.EnsurePackage("sigs.k8s.io/controller-tools/cmd/controller-gen", "v0.4.1", "--version"))
}

func pwd() string {
	wd, _ := os.Getwd()
	return wd
}
