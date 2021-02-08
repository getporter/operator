// +build mage

// This is a magefile, and is a "makefile for go".
// See https://magefile.org/
package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/template"

	. "get.porter.sh/operator/mage"
	"github.com/carolynvs/magex/pkg"
	"github.com/carolynvs/magex/shx"
	"github.com/magefile/mage/mg"
	"github.com/pkg/errors"
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

// Ensure mage is installed.
func EnsureMage() error {
	addGopathBinOnGithubActions()
	return pkg.EnsureMage("v1.11.0")
}

// Add GOPATH/bin to the path on the GitHub Actions agent
func addGopathBinOnGithubActions() error {
	githubPath := os.Getenv("GITHUB_PATH")
	if githubPath == "" {
		return nil
	}

	log.Println("Adding GOPATH/bin to the PATH for the GitHub Actions Agent")
	gopathBin := pkg.GetGopathBin()
	return ioutil.WriteFile(githubPath, []byte(gopathBin), 0644)
}

func Generate() error {
	return shx.RunV("controller-gen", `object:headerFile="hack/boilerplate.go.txt"`, `paths="./..."`)
}

func Fmt() error {
	return shx.RunV("go", "fmt", "./...")
}

func Vet() error {
	return shx.RunV("go", "vet", "./...")
}

// Compile the operator.
func Build() error {
	mg.Deps(Fmt, Vet)

	LoadMetadatda()

	return shx.RunV("go", "build", "-o", "bin/manager", "main.go")
}

func Bundle() error {
	mg.SerialDeps(UseProductionEnvironment, BuildManifests)

	err := shx.Copy("manifests.yaml", "installer/")
	if err != nil {
		return err
	}

	// TODO: set --version
	return shx.Command("porter", "publish", "--debug").In("installer").RunV()
}

func BuildManifests() error {
	mg.Deps(EnsureKustomize, EnsureControllerGen)

	fmt.Println("Using environment", Env.Name)
	err := kustomize("edit", "set", "image", "manager="+Env.ControllerImage).In("config/manager").Run()
	if err != nil {
		return err
	}

	if err := os.Remove("manifests.yaml"); err != nil && !os.IsNotExist(err) {
		return errors.Wrap(err, "could not remove generated manifests directory")
	}

	// Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
	crdOpts := "crd:trivialVersions=true,preserveUnknownFields=false"
	err = shx.RunV("controller-gen", crdOpts, "rbac:roleName=manager-role", "webhook", `paths="./..."`, "output:crd:artifacts:config=config/crd/bases")
	if err != nil {
		return err
	}

	return kustomize("build", "config/default", "-o", "manifests.yaml").RunV()
}

// Run all tests
func Test() error {
	mg.Deps(TestUnit, TestIntegration)
	return nil
}

// Run unit tests.
func TestUnit() error {
	return shx.RunV("go", "test", "./...", "-coverprofile", "coverage-unit.out")
}

// Run integration tests against the test cluster.
func TestIntegration() error {
	mg.Deps(UseTestEnvironment, CleanTests, EnsureGinkgo)

	if !isDeployed() {
		mg.Deps(Deploy)
	}

	return shx.Run("go", "test", "-tags=integration", "./...", "-coverprofile=coverage-integration.out")
}

// Build the operator and deploy it to the test cluster.
func Deploy() error {
	mg.Deps(UseTestEnvironment, EnsureCluster, StartDockerRegistry, Build)

	err := BuildManifests()
	if err != nil {
		return err
	}

	err = kubectl("apply", "-f", "manifests.yaml").Run()
	if err != nil {
		return err
	}

	err = PublishController()
	if err != nil {
		return err
	}

	return kubectl("rollout", "restart", "deployment/porter-operator-controller-manager", "--namespace", operatorNamespace).RunV()
}

func isDeployed() bool {
	if ok, _ := useCluster(); ok {
		err := kubectl("rollout", "status", "deployment", "porter-operator-controller-manager", "--namespace", operatorNamespace).RunS()
		return err == nil
	}
	return false
}

func PublishImages() error {
	mg.Deps(PublishAgent, PublishController)
	return nil
}

// Publish the Porter agent image to the local docker registry.
func PublishAgent() error {
	err := shx.Command("docker", "build", "-t", Env.AgentImage, "images/porter").
		Env("DOCKER_BUILDKIT=1").RunV()
	if err != nil {
		return err
	}

	return shx.RunV("docker", "push", Env.AgentImage)
}

func PublishController() error {
	err := shx.Command("docker", "build", "-t", Env.ControllerImage, ".").
		Env("DOCKER_BUILDKIT=1").RunV()
	if err != nil {
		return err
	}

	return shx.RunV("docker", "push", Env.ControllerImage)
}

// Reapply the file in config/samples, usage: mage bump porter-hello.
func Bump(sample string) error {
	mg.Deps(EnsureTestNamespace, EnsureYq)

	sampleFile := fmt.Sprintf("config/samples/%s.yaml", sample)
	dataB, err := ioutil.ReadFile(sampleFile)
	if err != nil {
		return errors.Errorf("error reading installation definition %s", sampleFile)
	}

	retryCountField := ".metadata.annotations.retryCount"
	cmd := shx.Command("yq", "eval", retryCountField, "-")
	cmd.Cmd.Stdin = bytes.NewReader(dataB)
	retryCount, err := cmd.OutputE()
	if err != nil {
		return err
	}

	x, err := strconv.Atoi(retryCount)
	if err != nil {
		x = 0
	}
	retryCount = strconv.Itoa(x + 1)

	cmd = shx.Command("yq", "eval", fmt.Sprintf("%s = %q", retryCountField, retryCount), "-")
	cmd.Cmd.Stdin = bytes.NewReader(dataB)
	crd, err := cmd.OutputE()
	if err != nil {
		return err
	}

	log.Println(crd)
	cmd = kubectl("apply", "-f", "-")
	cmd.Cmd.Stdin = strings.NewReader(crd)
	return cmd.RunV()
}

// Ensures that a namespace named "test" exists.
func EnsureTestNamespace() error {
	if namespaceExists(testNamespace) {
		return nil
	}

	return setupTestNamespace()
}

func setupTestNamespace() error {
	return SetupNamespace(testNamespace)
}

func namespaceExists(name string) bool {
	err := kubectl("get", "namespace", name).RunS()
	return err == nil
}

// Create a namespace, usage: mage SetupNamespace demo.
// Configures the namespace for use with the operator.
func SetupNamespace(name string) error {
	mg.Deps(EnsureCluster)

	if namespaceExists(name) {
		err := kubectl("delete", "ns", name, "--wait=true").RunS()
		if err != nil {
			return errors.Wrapf(err, "could not delete namespace %s", name)
		}
	}

	err := kubectl("create", "namespace", name).RunE()
	if err != nil {
		return errors.Wrapf(err, "could not create namespace %s", name)
	}

	err = kubectl("label", "namespace", name, "porter-test=true").RunE()
	if err != nil {
		return errors.Wrapf(err, "could not label namespace %s", name)
	}

	err = kubectl("create", "configmap", "porter", "--namespace", name,
		"--from-literal=porterVersion=canary",
		"--from-literal=serviceAccount=porter-agent",
		"--from-literal=outputsVolumeSize=64Mi").RunE()
	if err != nil {
		return errors.Wrap(err, "could not create porter configmap")
	}

	err = kubectl("create", "secret", "generic", "porter-config", "--namespace", name,
		"--from-file=config.toml=hack/porter-config.toml").RunE()
	if err != nil {
		return errors.Wrap(err, "could not create porter-config secret")
	}

	err = kubectl("create", "secret", "generic", "porter-env", "--namespace", name,
		"--from-literal=AZURE_STORAGE_CONNECTION_STRING="+os.Getenv("PORTER_TEST_AZURE_STORAGE_CONNECTION_STRING"),
		"--from-literal=AZURE_CLIENT_SECRET="+os.Getenv("PORTER_AZURE_CLIENT_SECRET"),
		"--from-literal=AZURE_CLIENT_ID="+os.Getenv("PORTER_AZURE_CLIENT_ID"),
		"--from-literal=AZURE_TENANT_ID="+os.Getenv("PORTER_AZURE_TENANT_ID")).RunE()
	if err != nil {
		return errors.Wrap(err, "could not create porter-env secret")
	}

	err = kubectl("create", "serviceaccount", "porter-agent", "--namespace", name).RunE()
	if err != nil {
		return errors.Wrapf(err, "could not create porter-agent service account in %s", name)
	}

	err = kubectl("create", "rolebinding", "porter-agent",
		"--clusterrole", "porter-operator-agent-role",
		"--serviceaccount", name+":porter-agent",
		"--namespace", name).RunE()
	if err != nil {
		return errors.Wrapf(err, "could not create porter-agent service account in %s", name)
	}

	return setClusterNamespace(name)
}

// Delete operator data from the test cluster.
func Clean() error {
	mg.Deps(CleanManual, CleanTests)
	return nil
}

// Remove data created by running the test suite
func CleanTests() error {
	if ok, _ := useCluster(); ok {
		return kubectl("delete", "ns", "-l", "porter-test=true").RunV()
	}
	return nil
}

// Remove any porter data in the cluster
func CleanManual() error {
	if ok, _ := useCluster(); ok {
		err := kubectl("delete", "jobs", "-l", "porter=true").RunV()
		if err != nil {
			return err
		}

		return kubectl("delete", "secrets", "-l", "porter-test=true").RunV()
	}
	return nil
}

// Follow the logs for the operator.
func Logs() error {
	mg.Deps(EnsureKubectl)

	return kubectl("logs", "-f", "deployment/porter-operator-controller-manager", "-c=manager", "--namespace", operatorNamespace).RunV()
}

// Ensure operator-sdk is installed.
func EnsureOperatorSDK() error {
	const version = "v1.3.0"

	if runtime.GOOS == "windows" {
		return errors.New("Sorry, OperatorSDK does not support Windows. In order to contribute to this repository, you will need to use WSL.")
	}

	url := "https://github.com/operator-framework/operator-sdk/releases/{{.VERSION}}/download/operator-sdk_{{.GOOS}}_{{.GOARCH}}"
	return pkg.DownloadToGopathBin(url, "operator-sdk", version)
}

// Ensure that the test KIND cluster is up.
func EnsureCluster() error {
	mg.Deps(EnsureKubectl)

	ok, err := useCluster()
	if err != nil {
		return err
	}

	if !ok {
		return CreateKindCluster()
	}
	return nil
}

// get the config of the current kind cluster, if available
func getClusterConfig() (kubeconfig string, ok bool) {
	contents, err := shx.OutputE("kind", "get", "kubeconfig", "--name", kindClusterName)
	return contents, err == nil
}

// setup environment to use the current kind cluster, if availabe
func useCluster() (bool, error) {
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
		if err != nil {
			errors.Wrapf(err, "error writing %s", kubeconfig)
		}

		err = setClusterNamespace(operatorNamespace)
		if err != nil {
			return false, err
		}

		return true, nil
	}

	return false, nil
}

func setClusterNamespace(name string) error {
	return shx.RunE("kubectl", "config", "set-context", "--current", "--namespace", name)
}

// Create a KIND cluster named porter.
func CreateKindCluster() error {
	mg.Deps(EnsureKind, StartDockerRegistry)

	// Determine host ip to populate kind config api server details
	// https://kind.sigs.k8s.io/docs/user/configuration/#api-server
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return errors.Wrap(err, "could not get a list of network interfaces")
	}

	var ipAddress string
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				fmt.Println("Current IP address : ", ipnet.IP.String())
				ipAddress = ipnet.IP.String()
				break
			}
		}
	}

	os.Setenv("KUBECONFIG", filepath.Join(pwd(), kubeconfig))
	kindCfg, err := ioutil.ReadFile("hack/kind.config.yaml")
	if err != nil {
		return errors.Wrap(err, "error reading hack/kind.config.yaml")
	}
	kindCfgTmpl, err := template.New("kind.config.yaml").Parse(string(kindCfg))
	if err != nil {
		return errors.Wrap(err, "error parsing Kind config template hack/kind.config.yaml")
	}
	var kindCfgContents bytes.Buffer
	kindCfgData := struct {
		Address string
	}{
		Address: ipAddress,
	}
	err = kindCfgTmpl.Execute(&kindCfgContents, kindCfgData)
	err = ioutil.WriteFile("kind.config.yaml", kindCfgContents.Bytes(), 0644)
	if err != nil {
		return errors.Wrap(err, "could not write kind config file")
	}
	defer os.Remove("kind.config.yaml")

	err = shx.Run("kind", "create", "cluster", "--name", kindClusterName, "--config", "kind.config.yaml")
	if err != nil {
		errors.Wrap(err, "could not create KIND cluster")
	}

	// Connect the kind and registry containers on the same network
	err = shx.Run("docker", "network", "connect", "kind", registryContainer)
	if err != nil {
		return errors.Wrap(err, "could not connect the test kind cluster to local docker registry")
	}

	// Document the local registry
	err = kubectl("apply", "-f", "hack/local-registry.yaml").Run()
	if err != nil {
		return errors.Wrap(err, "could not apply hack/local-registry.yaml")
	}

	return setClusterNamespace(operatorNamespace)
}

// Delete the KIND cluster named porter.
func DeleteKindCluster() error {
	mg.Deps(EnsureKind)

	err := shx.RunE("kind", "delete", "cluster", "--name", kindClusterName)
	if err != nil {
		return errors.Wrap(err, "could not delete KIND cluster")
	}

	if isOnDockerNetwork(registryContainer, "kind") {
		err = shx.RunE("docker", "network", "disconnect", "kind", registryContainer)
		return errors.Wrap(err, "could not disconnect the registry container from the docker network: kind")
	}

	return nil
}

func isOnDockerNetwork(container string, network string) bool {
	networkId, _ := shx.OutputE("docker", "network", "inspect", network, "-f", "{{.Id}}")
	networks, _ := shx.OutputE("docker", "inspect", container, "-f", "{{json .NetworkSettings.Networks}}")
	return strings.Contains(networks, networkId)
}

// Ensure kind is installed.
func EnsureKind() error {
	if ok, _ := pkg.IsCommandAvailable("kind", ""); ok {
		return nil
	}

	kindURL := "https://github.com/kubernetes-sigs/kind/releases/download/{{.VERSION}}/kind-{{.GOOS}}-{{.GOARCH}}"
	err := pkg.DownloadToGopathBin(kindURL, "kind", kindVersion)
	if err != nil {
		return errors.Wrap(err, "could not download kind")
	}

	return nil
}

// Ensure kubectl is installed.
func EnsureKubectl() error {
	if ok, _ := pkg.IsCommandAvailable("kubectl", ""); ok {
		return nil
	}

	versionURL := "https://storage.googleapis.com/kubernetes-release/release/stable.txt"
	versionResp, err := http.Get(versionURL)
	if err != nil {
		return errors.Wrapf(err, "unable to determine the latest version of kubectl")
	}
	if versionResp.StatusCode > 299 {
		return errors.Errorf("GET %s (%s): %s", versionURL, versionResp.StatusCode, versionResp.Status)
	}
	defer versionResp.Body.Close()
	kubectlVersion, err := ioutil.ReadAll(versionResp.Body)
	if err != nil {
		return errors.Wrapf(err, "error reading response from %s", versionURL)
	}

	kindURL := "https://storage.googleapis.com/kubernetes-release/release/{{.VERSION}}/bin/{{.GOOS}}/{{.GOARCH}}/kubectl{{.EXT}}"
	err = pkg.DownloadToGopathBin(kindURL, "kubectl", string(kubectlVersion))
	if err != nil {
		return errors.Wrap(err, "could not download kubectl")
	}

	return nil
}

// Run a makefile target
func makefile(args ...string) shx.PreparedCommand {
	cmd := shx.Command("make", args...)
	cmd.Env("KUBECONFIG=" + os.Getenv("KUBECONFIG"))

	return cmd
}

func kubectl(args ...string) shx.PreparedCommand {
	kubeconfig := fmt.Sprintf("KUBECONFIG=%s", os.Getenv("KUBECONFIG"))
	return shx.Command("kubectl", args...).Env(kubeconfig)
}

func kustomize(args ...string) shx.PreparedCommand {
	cmd := filepath.Join(pwd(), "bin/kustomize")
	return shx.Command(cmd, args...)
}

// Ensure yq is installed.
func EnsureYq() error {
	return pkg.EnsurePackage("github.com/mikefarah/yq/v4", "", "")
}

// Ensure ginkgo is installed.
func EnsureGinkgo() error {
	return pkg.EnsurePackage("github.com/onsi/ginkgo/ginkgo", "", "")
}

// Ensure kustomize is installed.
func EnsureKustomize() error {
	// TODO: implement installing from a URL that is tgz
	return makefile("kustomize").Run()
}

// Ensure controller-gen is installed.
func EnsureControllerGen() error {
	return pkg.EnsurePackage("sigs.k8s.io/controller-tools/cmd/controller-gen", "v0.4.1", "--version")
}

// Ensure that a local docker registry is running.
func StartDockerRegistry() error {
	if isContainerRunning(registryContainer) {
		return nil
	}

	err := StopDockerRegistry()
	if err != nil {
		return err
	}

	fmt.Println("Starting local docker registry")
	return shx.RunE("docker", "run", "-d", "-p", "5000:5000", "--name", registryContainer, "registry:2")
}

// Stops the local docker registry.
func StopDockerRegistry() error {
	if containerExists(registryContainer) {
		fmt.Println("Stopping local docker registry")
		return removeContainer(registryContainer)
	}
	return nil
}

func isContainerRunning(name string) bool {
	out, _ := shx.OutputS("docker", "container", "inspect", "-f", "{{.State.Running}}", name)
	running, _ := strconv.ParseBool(out)
	return running
}

func containerExists(name string) bool {
	err := shx.RunS("docker", "inspect", name)
	return err == nil
}

func removeContainer(name string) error {
	stderr, err := shx.OutputE("docker", "rm", "-f", name)
	// Gracefully handle the container already being gone
	if err != nil && !strings.Contains(stderr, "No such container") {
		return err
	}
	return nil
}

func pwd() string {
	wd, _ := os.Getwd()
	return wd
}
