//go:build mage

// This is a magefile, and is a "makefile for go".
// See https://magefile.org/
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	. "get.porter.sh/magefiles/docker"
	"get.porter.sh/magefiles/porter"
	"get.porter.sh/magefiles/releases"
	. "get.porter.sh/magefiles/tests"
	"get.porter.sh/magefiles/tools"
	. "get.porter.sh/operator/mage"
	"get.porter.sh/operator/mage/docs"
	"get.porter.sh/porter/pkg/cnab"
	"github.com/carolynvs/magex/ci"
	"github.com/carolynvs/magex/mgx"
	"github.com/carolynvs/magex/pkg"
	"github.com/carolynvs/magex/pkg/archive"
	"github.com/carolynvs/magex/pkg/downloads"
	"github.com/carolynvs/magex/pkg/gopath"
	"github.com/carolynvs/magex/shx"
	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/target"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

const (
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

	// Porter home for running commands
	porterVersion = "v1.0.0-alpha.20"
)

var srcDirs = []string{"api", "config", "controllers", "installer", "installer-olm"}
var binDir = "bin"

// Porter agent that has k8s plugin included
var porterAgentImgRepository = "ghcr.io/getporter/dev/porter-agent-kubernetes"
var porterAgentImgVersion = "v1.0.0-alpha.20"

// Local porter agent image name to use for local testing
var localAgentImgName = "localhost:5000/porter-agent:canary-dev"

// Build a command that stops the build on if the command fails
var must = shx.CommandBuilder{StopOnError: true}

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

// Ensure EnsureMage is installed and on the PATH.
func EnsureMage() error {
	return tools.EnsureMage()
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
	mg.SerialDeps(getPlugins, getMixins, StartDockerRegistry, PublishImages)

	buildManifests()

	meta := releases.LoadMetadata()
	version := strings.TrimPrefix(meta.Version, "v")
	verbose := ""
	if mg.Verbose() {
		verbose = "--verbose"
	}
	buildPorterCmd("build", "--version", version, "-f=porter.yaml", verbose).
		CollapseArgs().Env("PORTER_EXPERIMENTAL=build-drivers", "PORTER_BUILD_DRIVER=buildkit").
		In("installer").Must().RunV()
}

// Build the controller image
func BuildImages() {
	mg.Deps(BuildController)
	meta := releases.LoadMetadata()
	img := Env.ManagerImagePrefix + meta.Version
	imgPermalink := Env.ManagerImagePrefix + meta.Permalink

	log.Println("Building", img)
	must.Command("docker", "build", "-t", img, ".").
		Env("DOCKER_BUILDKIT=1").RunV()

	log.Println("Tagging as", imgPermalink)
	must.RunV("docker", "tag", img, imgPermalink)

	// Make the full image name available as an environment variable
	p, _ := ci.DetectBuildProvider()
	mgx.Must(p.SetEnv("MANAGER_IMAGE", img))
}

func getPlugins() error {
	// TODO: move this to a shared target in porter

	plugins := []struct {
		name    string
		url     string
		feed    string
		version string
	}{
		{name: "kubernetes", feed: "https://cdn.porter.sh/plugins/atom.xml", version: "latest"},
	}
	var errG errgroup.Group
	for _, plugin := range plugins {
		plugin := plugin
		pluginDir := filepath.Join("bin/plugins/", plugin.name)
		if _, err := os.Stat(pluginDir); err == nil {
			log.Println("Plugin already installed into bin:", plugin.name)
			continue
		}

		errG.Go(func() error {
			log.Println("Installing plugin:", plugin.name)
			if plugin.version == "" {
				plugin.version = "latest"
			}
			var source string
			if plugin.feed != "" {
				source = "--feed-url=" + plugin.feed
			} else {
				source = "--url=" + plugin.url
			}
			return buildPorterCmd("plugin", "install", plugin.name, "--version", plugin.version, source).Run()
		})
	}

	return errG.Wait()
}

func getMixins() error {
	// TODO: move this to a shared target in porter

	mixins := []struct {
		name    string
		url     string
		feed    string
		version string
	}{
		{name: "helm3", url: "https://github.com/carolynvs/porter-helm3/releases/download", version: "v0.1.15-8-g864f450"},
		{name: "kubernetes", feed: "https://cdn.porter.sh/mixins/atom.xml", version: "latest"},
		{name: "exec", feed: "https://cdn.porter.sh/mixins/atom.xml", version: "latest"},
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
			var source string
			if mixin.feed != "" {
				source = "--feed-url=" + mixin.feed
			} else {
				source = "--url=" + mixin.url
			}
			return buildPorterCmd("mixin", "install", mixin.name, "--version", mixin.version, source).Run()
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
	meta := releases.LoadMetadata()
	buildPorterCmd("publish", "--registry", Env.Registry, "-f=porter.yaml", "--tag", meta.Version).In("installer").Must().RunV()

	buildPorterCmd("publish", "--registry", Env.Registry, "-f=porter.yaml", "--tag", meta.Permalink).In("installer").Must().RunV()
}

// Generate k8s manifests for the operator.
func buildManifests() {
	mg.Deps(EnsureKustomize, EnsureControllerGen, EnsureYq)

	// Set the image reference in porter.yaml so that the manager image is packaged with the bundle
	managerRef := resolveManagerImage()
	mgx.Must(shx.Copy("installer/vanilla.porter.yaml", "installer/porter.yaml"))
	setRepo := fmt.Sprintf(`.images.manager.repository = "%s"`, managerRef.Repository())
	must.Command("yq", "eval", setRepo, "-i", "installer/porter.yaml").RunV()
	setDigest := fmt.Sprintf(`.images.manager.digest = "%s"`, managerRef.Digest())
	must.Command("yq", "eval", setDigest, "-i", "installer/porter.yaml").RunV()

	fmt.Printf("Using porter-operator-manager %s@%s", managerRef.Repository(), managerRef.Digest())
	if err := os.Remove("manifests.yaml"); err != nil && !os.IsNotExist(err) {
		mgx.Must(errors.Wrap(err, "could not remove generated manifests directory"))
	}

	must.RunV("controller-gen", "rbac:roleName=manager-role", "crd", "webhook", `paths="./..."`, "output:crd:artifacts:config=config/crd/bases")
	kustomize("build", "config/default", "-o", "installer/manifests/operator.yaml").RunV()
}

func resolveManagerImage() cnab.OCIReference {
	fmt.Println("Using environment", Env.Name)
	meta := releases.LoadMetadata()
	img := Env.ManagerImagePrefix + meta.Version

	imgDef, err := must.Output("docker", "image", "inspect", img)
	if err != nil {
		panic("the manager image has not been built yet")
	}
	imgWithDigest, err := ExtractRepoDigest(imgDef)
	if err != nil {
		panic("could not resolve the repository digest of the manager image")
	}
	ref, err := cnab.ParseOCIReference(imgWithDigest)
	mgx.Must(err)

	return ref
}

// Run all tests
func Test() {
	mg.SerialDeps(TestUnit, TestIntegration)
}

// Run unit tests.
func TestUnit() {
	must.RunV("go", "test", "./...", "-coverprofile", "coverage-unit.out")
	generatedCodeFilter("coverage-unit.out")
}

// Update golden test files to match the new test outputs
func UpdateTestfiles() {
	must.Command("go", "test", "./...").Env("PORTER_UPDATE_TEST_FILES=true").RunV()
	TestUnit()
}

func TestOutline() {
	must.Command("ginkgo", "-v", "-dryRun", "-tags=integration", "./tests/integration/...").
		CollapseArgs().Env("ACK_GINKGO_DEPRECATIONS=1.16.5").RunV()
}

// Run integration tests against the test cluster.
func TestIntegration() {
	mg.SerialDeps(UseTestEnvironment, CleanTestdata)
	mg.Deps(EnsureGinkgo, EnsureDeployed)

	// TODO: we need to run these tests either isolated against EnvTest, or
	// against a cluster that doesn't have the operator deployed. Otherwise
	// both the controller running in the test, and the controller on the cluster
	// are responding to the same events.
	// For now, it's up to the caller to use a fresh cluster with CRDs installed until we can fix it.

	kubectl("delete", "deployment", "porter-operator-controller-manager", "-n=porter-operator-system").RunV()

	if os.Getenv("PORTER_AGENT_REPOSITORY") != "" && os.Getenv("PORTER_AGENT_VERSION") != "" {
		porterAgentImgRepository = os.Getenv("PORTER_AGENT_REPOSITORY")
		porterAgentImgVersion = os.Getenv("PORTER_AGENT_VERSION")
	}
	//"-p", "-nodes", "4",
	must.Command("ginkgo").Args("-v", "-tags=integration", "./tests/integration/...", "-coverprofile=coverage-integration.out").
		Env(fmt.Sprintf("PORTER_AGENT_REPOSITORY=%s", porterAgentImgRepository),
			fmt.Sprintf("PORTER_AGENT_VERSION=%s", porterAgentImgVersion),
			"ACK_GINKGO_DEPRECATIONS=1.16.5",
			"ACK_GINKGO_RC=true",
			fmt.Sprintf("KUBECONFIG=%s/kind.config", pwd())).RunV()
}

// Check if the operator is deployed to the test cluster.
func EnsureDeployed() {
	if !isDeployed() {
		Deploy()
	}
}

// Deploy the website
func DocsDeploy() error {
	return docs.DeployWebsite()
}

// Preview the website
func DocsPreview() error {
	return docs.Preview()
}

// Deploy a preview of the website
func DocsDeployPreview() error {
	return docs.DeployWebsitePreview()
}

// Build the operator and deploy it to the test cluster using
func Deploy() {
	mg.Deps(UseTestEnvironment, EnsureTestCluster)
	rebuild, err := target.Dir(binDir, srcDirs...)
	if err != nil {
		mgx.Must(fmt.Errorf("error inspecting source dirs %s: %w", srcDirs, err))
	}
	meta := releases.LoadMetadata()
	if rebuild {
		//PublishLocalPorterAgent()
		PublishBundle()
		buildPorterCmd("credentials", "apply", "hack/creds.yaml", "-n=operator").Must().RunV()
	}
	bundleRef := Env.BundlePrefix + meta.Version
	buildPorterCmd("install", "operator", "-r", bundleRef, "-c=kind", "--force", "-n=operator").Must().RunV()
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
	img := Env.ManagerImagePrefix + meta.Version
	imgPermalink := Env.ManagerImagePrefix + meta.Permalink

	log.Println("Pushing", img)
	must.RunV("docker", "push", img)

	log.Println("Pushing", imgPermalink)
	must.RunV("docker", "push", imgPermalink)
}

func PublishLocalPorterAgent() {
	// Check if we have a local porter build
	// TODO: let's move some of these helpers into Porter
	mg.SerialDeps(BuildLocalPorterAgent, EnsureTestCluster)
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
	if os.Getenv("PORTER_AGENT_REPOSITORY") != "" && os.Getenv("PORTER_AGENT_VERSION") != "" {
		localAgentImgName = fmt.Sprintf("%s:%s", os.Getenv("PORTER_AGENT_REPOSITORY"), os.Getenv("PORTER_AGENT_VERSION"))
	}

	if ok, _ := imageExists(localAgentImgName); ok {
		err := pushImage(localAgentImgName)
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

func SetupNamespace(name string) {
	mg.Deps(EnsureTestCluster)
	porterConfigFile := "./tests/integration/testdata/operator_porter_config.yaml"

	// Only specify the parameter set we have the env vars set
	// It would be neat if Porter could handle this for us
	//PublishLocalPorterAgent()
	buildPorterCmd("parameters", "apply", "./hack/params.yaml", "-n=operator").RunV()
	ps := ""
	if os.Getenv("PORTER_AGENT_REPOSITORY") != "" && os.Getenv("PORTER_AGENT_VERSION") != "" {
		ps = "-p=dev-build"
	}

	buildPorterCmd("invoke", "operator", "--action=configureNamespace", ps, "--param", "pullPolicy=Always", "--param", "namespace="+name, "--param", "porterConfig="+porterConfigFile, "-c", "kind", "-n=operator").
		CollapseArgs().Must().RunV()
	kubectl("label", "namespace", name, "--overwrite=true", "porter.sh/devenv=true").Must().RunV()

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
		// find all test namespaces
		output, _ := kubectl("get", "ns", "-l", "porter.sh/testdata=true", `--template={{range .items}}{{.metadata.name}},{{end}}`).
			OutputE()
		namespaces := strings.Split(output, ",")

		// Remove the finalizers from any testdata in that namespace
		// Otherwise they will block when you delete the namespace
		for _, namespace := range namespaces {
			if namespace == "" {
				continue
			}

			output, _ = kubectl("get", "installation,credentialset,parameterset,agentaction", "-n", namespace, `--template={{range .items}}{{.kind}}/{{.metadata.name}},{{end}}`).
				Output()
			resources := strings.Split(output, ",")
			for _, resource := range resources {
				if resource == "" {
					continue
				}

				removeFinalizers(namespace, resource)
			}

			// Okay, now it's safe to delete the namespace
			kubectl("delete", "ns", namespace).Run()
		}
	}
}

// Remove all finalizers from the specified resource
// name should be in the format: kind/name
func removeFinalizers(namespace, name string) {
	mg.Deps(EnsureYq)

	// Get the resource definition
	kubectl("patch", "-n", namespace, name, "-p", `[{"op": "remove", "path": "/metadata/finalizers"}]`, "--type=json").Must(false).RunS()
	time.Sleep(time.Second * 6)
}

// Remove any porter data in the cluster
func CleanAllData() {
	if useCluster() {
		buildPorterCmd("invoke", "operator", "--action=removeData", "-c", "kind", "-n=operator").Must().RunV()
	}
}

// Follow the logs for the operator.
func Logs() {
	mg.Deps(EnsureKubectl)

	kubectl("logs", "-f", "deployment/porter-operator-controller-manager", "-c=manager", "--namespace", operatorNamespace).RunV()
}

// Ensure operator-sdk is installed.
func EnsureOperatorSDK() {
	const version = "v1.19.0"

	if runtime.GOOS == "windows" {
		mgx.Must(errors.New("Sorry, OperatorSDK does not support Windows. In order to contribute to this repository, you will need to use WSL."))
	}

	url := "https://github.com/operator-framework/operator-sdk/releases/download/{{.VERSION}}/operator-sdk_{{.GOOS}}_{{.GOARCH}}"
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
	mgx.Must(pkg.EnsurePackage("github.com/onsi/ginkgo/ginkgo", "1.16.5", "version"))
}

// Ensure kustomize is installed.
func EnsureKustomize() {
	opts := archive.DownloadArchiveOptions{
		DownloadOptions: downloads.DownloadOptions{
			UrlTemplate: "https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2F{{.VERSION}}/kustomize_{{.VERSION}}_{{.GOOS}}_{{.GOARCH}}.tar.gz",
			Name:        "kustomize",
			Version:     "v4.5.4",
		},
		ArchiveExtensions:  map[string]string{"darwin": ".tar.gz", "linux": ".tar.gz", "windows": ".tar.gz"},
		TargetFileTemplate: "kustomize{{.EXT}}",
	}
	mgx.Must(archive.DownloadToGopathBin(opts))
}

// Ensure controller-gen is installed.
func EnsureControllerGen() {
	mgx.Must(pkg.EnsurePackage("sigs.k8s.io/controller-tools/cmd/controller-gen", "v0.8.0", "--version"))
}

func pwd() string {
	wd, _ := os.Getwd()
	return wd
}

// Run porter using the local storage, not the in-cluster storage
func buildPorterCmd(args ...string) shx.PreparedCommand {
	mg.SerialDeps(porter.UseBinForPorterHome)
	porter.EnsurePorterAt(porterVersion)
	return must.Command(filepath.Join(pwd(), "bin/porter")).Args(args...).
		Env("PORTER_DEFAULT_STORAGE=",
			"PORTER_DEFAULT_STORAGE_PLUGIN=mongodb-docker",
			fmt.Sprintf("PORTER_HOME=%s", filepath.Join(pwd(), "bin")))
}

func BuildLocalPorterAgent() {
	mg.SerialDeps(porter.UseBinForPorterHome)
	porter.EnsurePorterAt(porterVersion)
	mg.SerialDeps(getPlugins, getMixins)
	porterRegistry := "ghcr.io/getporter"
	buildImage := func(img string) error {
		_, err := shx.Output("docker", "build", "-t", img,
			"--build-arg", fmt.Sprintf("PORTER_VERSION=%s", porterVersion),
			"--build-arg", fmt.Sprintf("REGISTRY=%s", porterRegistry),
			"-f", "tests/integration/testdata/Dockerfile.k8s-plugin-agent", ".")
		if err != nil {
			return err
		}
		return nil
	}
	if os.Getenv("PORTER_AGENT_REPOSITORY") != "" && os.Getenv("PORTER_AGENT_VERSION") != "" {
		localAgentImgName = fmt.Sprintf("%s:%s", os.Getenv("PORTER_AGENT_REPOSITORY"), os.Getenv("PORTER_AGENT_VERSION"))
	}
	err := buildImage(localAgentImgName)
	mgx.Must(err)
}

//generatedCodeFilter remove generated code files from coverage report
func generatedCodeFilter(filename string) error {
	fd, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}
	fs := string(fd)
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(fs))
	for sc.Scan() {
		if !strings.Contains(sc.Text(), "zz_generated") {
			lines = append(lines, sc.Text())
		}
	}

	fd = []byte(strings.Join(lines, "\n"))
	err = ioutil.WriteFile(filename, fd, 0600)
	if err != nil {
		return err
	}
	return nil
}
