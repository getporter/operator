//go:build mage
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
	"get.porter.sh/operator/mage/docker"
	. "get.porter.sh/porter/mage/docker"
	"get.porter.sh/porter/mage/releases"
	"github.com/carolynvs/magex/mgx"
	"github.com/carolynvs/magex/pkg"
	"github.com/carolynvs/magex/pkg/archive"
	"github.com/carolynvs/magex/pkg/downloads"
	"github.com/carolynvs/magex/pkg/gopath"
	"github.com/carolynvs/magex/shx"
	"github.com/magefile/mage/mg"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"

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

func Fmt() {
	must.RunV("go", "fmt", "./...")
}

func Vet() {
	must.RunV("go", "vet", "./...")
}

// Build the controller and bundle.
func Build() {
	mg.SerialDeps(BuildController, BuildBundle)
	Fmt()
	Vet()
}

// Compile the operator and its API types
func BuildController() {
	mg.SerialDeps(EnsureControllerGen, GenerateController)

	releases.LoadMetadata()

	must.RunV("go", "build", "-o", "bin/manager", "main.go")
}

func GenerateController() error {
	return shx.RunV("controller-gen", `object:headerFile="hack/boilerplate.go.txt"`, `paths="./..."`)
}

// Build the porter-operator bundle.
func BuildBundle() {
	mg.SerialDeps(getMixins, StartDockerRegistry, PublishImages)

	buildManifests()

	meta := releases.LoadMetadata()
	version := strings.TrimPrefix(meta.Version, "v")
	porter("build", "--version", version, "-f=vanilla.porter.yaml").In("installer").Must().RunV()
}

// Build the controller image
func BuildImages() {
	mg.Deps(BuildController)
	meta := releases.LoadMetadata()
	img := Env.ControllerImagePrefix + meta.Version
	imgPermalink := Env.ControllerImagePrefix + meta.Permalink

	log.Println("Building", img)
	must.Command("docker", "build", "-t", img, ".").
		Env("DOCKER_BUILDKIT=1").RunV()

	log.Println("Tagging as", imgPermalink)
	must.RunV("docker", "tag", img, imgPermalink)
}

func getMixins() error {
	// TODO: move this to a shared target in porter

	mixins := []struct {
		name    string
		feed    string
		version string
	}{
		{name: "helm3", feed: "https://mchorfa.github.io/porter-helm3/atom.xml", version: "v0.1.14"},
		{name: "kubernetes", feed: "https://cdn.porter.sh/mixins/atom.xml", version: "latest"},
	}
	var errG errgroup.Group
	for _, mixin := range mixins {
		mixin := mixin
		mixinDir := filepath.Join("bin/mixins/", mixin.name)
		if _, err := os.Stat(mixinDir); err == nil {
			log.Println("Mixin already installed into bin:", mixin.name)
			continue
		}

		errG.Go(func() error {
			log.Println("Installing mixin:", mixin.name)
			if mixin.version == "" {
				mixin.version = "latest"
			}
			return porter("mixin", "install", mixin.name, "--version", mixin.version, "--feed-url", mixin.feed).Run()
		})
	}

	return errG.Wait()
}

// Publish the operator and its bundle.
func Publish() {
	mg.Deps(PublishImages, PublishBundle)
}

// Push the porter-operator bundle to a registry. Defaults to the local test registry.
func PublishBundle() {
	mg.SerialDeps(PublishImages, BuildBundle)
	porter("publish", "--registry", Env.Registry, "-f=vanilla.porter.yaml").In("installer").Must().RunV()

	meta := releases.LoadMetadata()
	porter("publish", "--registry", Env.Registry, "-f=vanilla.porter.yaml", "--tag", meta.Permalink).In("installer").Must().RunV()
}

// Generate k8s manifests for the operator.
func buildManifests() {
	mg.Deps(EnsureKustomize, EnsureControllerGen)

	img := resolveControllerImage()
	kustomize("edit", "set", "image", "manager="+img).In("config/manager").Run()

	if err := os.Remove("manifests.yaml"); err != nil && !os.IsNotExist(err) {
		mgx.Must(errors.Wrap(err, "could not remove generated manifests directory"))
	}

	// Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
	crdOpts := "crd:trivialVersions=true,preserveUnknownFields=false"

	must.RunV("controller-gen", crdOpts, "rbac:roleName=manager-role", "webhook", `paths="./..."`, "output:crd:artifacts:config=config/crd/bases")
	kustomize("build", "config/default", "-o", "installer/manifests/operator.yaml").RunV()
}

func resolveControllerImage() string {
	fmt.Println("Using environment", Env.Name)
	meta := releases.LoadMetadata()
	img := Env.ControllerImagePrefix + meta.Version

	imgDef, err := must.Output("docker", "image", "inspect", img)
	if err != nil {
		panic("the controller image has not been built yet")
	}
	imgWithDigest, err := docker.ExtractRepoDigest(imgDef)
	if err != nil {
		panic("could not resolve the repository digest of the controller image")
	}
	return imgWithDigest
}

// Run all tests
func Test() {
	mg.Deps(TestUnit, TestIntegration)
}

// Run unit tests.
func TestUnit() {
	must.RunV("go", "test", "./...", "-coverprofile", "coverage-unit.out")
}

// Update golden test files to match the new test outputs
func UpdateTestfiles() {
	must.Command("go", "test", "./...").Env("PORTER_UPDATE_TEST_FILES=true").RunV()
	TestUnit()
}

// Run integration tests against the test cluster.
func TestIntegration() {
	mg.Deps(UseTestEnvironment, CleanTestdata, EnsureGinkgo, EnsureDeployed)

	must.RunV("go", "test", "-tags=integration", "./...", "-coverprofile=coverage-integration.out")
}

// Check if the operator is deployed to the test cluster.
func EnsureDeployed() {
	if !isDeployed() {
		Deploy()
	}
}

// Build the operator and deploy it to the test cluster using
func Deploy() {
	mg.Deps(UseTestEnvironment, EnsureTestCluster)

	meta := releases.LoadMetadata()
	PublishLocalPorterAgent()
	PublishBundle()

	porter("credentials", "apply", "hack/creds.yaml", "-n=operator", "--debug", "--debug-plugins").Must().RunV()
	bundleRef := Env.BundlePrefix + meta.Version
	porter("install", "operator", "-r", bundleRef, "-c=kind", "--force", "-n=operator").Must().RunV()
}

func isDeployed() bool {
	if useCluster() {
		if err := kubectl("rollout", "status", "deployment", "porter-operator-controller-manager", "--namespace", operatorNamespace).Must(false).Run(); err != nil {
			log.Println("the operator is not installed")
			return false
		}
		if err := kubectl("rollout", "status", "deployment", "mongodb", "--namespace", operatorNamespace).Must(false).Run(); err != nil {
			log.Println("the database is not installed")
			return false
		}
		log.Println("the operator is installed and ready to use")
		return true
	}
	log.Println("could not connect to the test cluster")
	return false
}

// Push the operator image.
func PublishImages() {
	mg.Deps(BuildImages)
	meta := releases.LoadMetadata()
	img := Env.ControllerImagePrefix + meta.Version
	imgPermalink := Env.ControllerImagePrefix + meta.Permalink

	log.Println("Pushing", img)
	must.RunV("docker", "push", img)

	log.Println("Pushing", imgPermalink)
	must.RunV("docker", "push", imgPermalink)
}

func PublishLocalPorterAgent() {
	// Check if we have a local porter build
	// TODO: let's move some of these helpers into Porter
	imageExists := func(img string) (bool, error) {
		out, err := shx.Output("docker", "image", "inspect", img)
		if err != nil {
			if strings.Contains(out, "No such image") {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}

	pushImage := func(img string) error {
		return shx.Run("docker", "push", img)
	}

	agentImg := "localhost:5000/porter-agent:canary-dev"
	if ok, _ := imageExists(agentImg); ok {
		err := pushImage(agentImg)
		mgx.Must(err)
	}
}

// Reapply a file in config/samples, usage: mage bump porter-hello.
func Bump(sample string) {
	mg.Deps(EnsureTestNamespace, EnsureYq)

	sampleFile := fmt.Sprintf("config/samples/%s.yaml", sample)

	dataB, err := ioutil.ReadFile(sampleFile)
	mgx.Must(errors.Wrapf(err, "error reading installation definition %s", sampleFile))

	updateRetry := fmt.Sprintf(`.metadata.annotations."porter.sh/retry" = "%s"`, time.Now().Format(time.RFC3339))
	crd, _ := must.Command("yq", "eval", updateRetry, "-").
		Stdin(bytes.NewReader(dataB)).OutputE()

	log.Println(crd)
	kubectl("apply", "--namespace", testNamespace, "-f", "-").Stdin(strings.NewReader(crd)).RunV()

	setClusterNamespace(testNamespace)
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

	// Only specify the parameter set we have the env vars set
	// It would be neat if Porter could handle this for us
	ps := ""
	if os.Getenv("PORTER_AGENT_REPOSITORY") != "" && os.Getenv("PORTER_AGENT_VERSION") != "" {
		ps = "-p=./hack/params.yaml"
	}

	porter("invoke", "operator", "--action=configureNamespace", ps, "--param", "namespace="+name, "-c", "kind", "-n=operator").
		CollapseArgs().Must().RunV()
	kubectl("label", "namespace", name, "-l", "porter.sh/testdata=true")
	setClusterNamespace(name)
}

// Remove the test cluster and registry.
func Clean() {
	mg.Deps(DeleteTestCluster, StopDockerRegistry)
	os.RemoveAll("bin")
}

// Remove data created by running the test suite
func CleanTestdata() {
	if useCluster() {
		kubectl("delete", "ns", "-l", "porter.sh/testdata=true").RunV()
	}
}

// Remove any porter data in the cluster
func CleanAllData() {
	if useCluster() {
		porter("invoke", "operator", "--action=removeData", "-c", "kind", "-n=operator").Must().RunV()
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
	shx.RunE("kubectl", "config", "set-context", "--current", "--namespace", name)
}

func kubectl(args ...string) shx.PreparedCommand {
	kubeconfig := fmt.Sprintf("KUBECONFIG=%s", os.Getenv("KUBECONFIG"))
	return must.Command("kubectl", args...).Env(kubeconfig)
}

func kustomize(args ...string) shx.PreparedCommand {
	return must.Command("kustomize", args...)
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
	opts := archive.DownloadArchiveOptions{
		DownloadOptions: downloads.DownloadOptions{
			UrlTemplate: "https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2F{{.VERSION}}/kustomize_{{.VERSION}}_{{.GOOS}}_{{.GOARCH}}.tar.gz",
			Name:        "kustomize",
			Version:     "v3.8.7",
		},
		ArchiveExtensions:  map[string]string{"darwin": ".tar.gz", "linux": ".tar.gz", "windows": ".tar.gz"},
		TargetFileTemplate: "kustomize{{.EXT}}",
	}
	mgx.Must(archive.DownloadToGopathBin(opts))
}

// Ensure controller-gen is installed.
func EnsureControllerGen() {
	mgx.Must(pkg.EnsurePackage("sigs.k8s.io/controller-tools/cmd/controller-gen", "v0.4.1", "--version"))
}

func pwd() string {
	wd, _ := os.Getwd()
	return wd
}

// Run porter using the local storage, not the in-cluster storage
func porter(args ...string) shx.PreparedCommand {
	return shx.Command("porter").Args(args...).
		Env("PORTER_DEFAULT_STORAGE=", "PORTER_DEFAULT_STORAGE_PLUGIN=mongodb-docker")
}
