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

	"github.com/carolynvs/magex/pkg"
	"github.com/carolynvs/magex/shx"
	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
	"github.com/pkg/errors"
)

// Default target to run when none is specified
// If not set, running mage will list available targets
// var Default = Build

const (
	kindVersion     = "v0.9.0"
	kindClusterName = "porter"
	namespace       = "porter-operator-system"
	kubeconfig      = "kind.config"
)

// Install mage if necessary.
func EnsureMage() error {
	return pkg.EnsureMage("v1.11.0")
}

// Build the controller and deploy it to the active cluster.
func Deploy() error {
	mg.Deps(EnsureCluster)

	err := runMake("manager", "docker-build", "docker-push", "deploy")
	if err != nil {
		return err
	}

	return kubectl("rollout", "restart", "deployment/porter-operator-controller-manager", "--namespace", namespace)
}

func Bump(sample string) error {
	mg.Deps(EnsureCluster, EnsureYq)

	data, err := kubectlCmd("get", "bundleinstallation", sample, "-o", "yaml").OutputE()
	dataB := []byte(data)
	if err != nil {
		dataB, err = ioutil.ReadFile(fmt.Sprintf("config/samples/%s.yaml", sample))
		if err != nil {
			return errors.New("cannot find the definition for porter-hello")
		}
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
	log.Println("retry count =", retryCount)
	retryCount = strconv.Itoa(x + 1)

	cmd = shx.Command("yq", "eval", fmt.Sprintf("%s = %q", retryCountField, retryCount), "-")
	cmd.Cmd.Stdin = bytes.NewReader(dataB)
	crd, err := cmd.OutputE()
	if err != nil {
		return err
	}

	log.Println(crd)
	cmd = kubectlCmd("apply", "-f", "-")
	cmd.Cmd.Stdin = strings.NewReader(crd)
	return cmd.RunV()
}

func CleanupJobs() error {
	return kubectl("delete", "jobs", "-l", "installation=porter-hello")
}

// Publish the docker image used to run the Porter jobs.
func PublishAgent() error {
	img := "ghcr.io/getporter/porter:kubernetes-canary"
	err := shx.RunV("docker", "build", "-t", img, "images/porter")
	if err != nil {
		return err
	}

	return shx.RunV("docker", "push", img)
}

// Follow the logs for the controller.
func Logs() error {
	mg.Deps(EnsureKubectl)

	return kubectl("logs", "-f", "deployment/porter-operator-controller-manager", "-c=manager", "--namespace", namespace)
}

// Install the operator-sdk if necessary
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

	contents, err := shx.OutputE("kind", "get", "kubeconfig", "--name", kindClusterName)
	if err != nil {
		return CreateKindCluster()
	}

	log.Println("Reusing existing kind cluster")
	pwd, _ := os.Getwd()
	userKubeConfig, _ := filepath.Abs(os.Getenv("KUBECONFIG"))
	currentKubeConfig := filepath.Join(pwd, kubeconfig)
	if userKubeConfig != currentKubeConfig {
		fmt.Printf("ATTENTION! You should set your KUBECONFIG to match the cluster used by this project\n\n\texport KUBECONFIG=%s\n\n", currentKubeConfig)
	}
	os.Setenv("KUBECONFIG", currentKubeConfig)
	err = ioutil.WriteFile(kubeconfig, []byte(contents), 0644)
	if err != nil {
		errors.Wrapf(err, "error writing %s", kubeconfig)
	}
	return setClusterNamespace()
}

func setClusterNamespace() error {
	return shx.RunV("kubectl", "config", "set-context", "--current", "--namespace", "porter-operator-system")
}

// Create a KIND cluster named porter.
func CreateKindCluster() error {
	mg.Deps(EnsureKind)

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

	pwd, _ := os.Getwd()
	os.Setenv("KUBECONFIG", filepath.Join(pwd, kubeconfig))
	kindCfg := "kind.config.yaml"
	contents := fmt.Sprintf(`kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  apiServerAddress: %s
`, ipAddress)
	err = ioutil.WriteFile(kindCfg, []byte(contents), 0644)
	if err != nil {
		return errors.Wrap(err, "could not write kind config file")
	}
	defer os.Remove(kindCfg)

	err = shx.RunE("kind", "create", "cluster", "--name", kindClusterName)
	if err != nil {
		errors.Wrap(err, "could not create KIND cluster")
	}

	return setClusterNamespace()
}

// Delete the KIND cluster named porter.
func DeleteKindCluster() error {
	mg.Deps(EnsureKind)

	err := shx.RunE("kind", "delete", "cluster", "--name", kindClusterName)
	if err != nil {
		errors.Wrap(err, "could not delete KIND cluster")
	}

	return nil
}

// Install kind if necessary.
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

// Install kubectl if necessary.
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

func runMake(args ...string) error {
	// Can't call this function make because it redefines the make keyword
	env := map[string]string{
		"KUBECONFIG": os.Getenv("KUBECONFIG"),
	}

	return sh.RunWithV(env, "make", args...)
}

func kubectl(args ...string) error {
	return kubectlCmd(args...).RunV()
}

func kubectlCmd(args ...string) shx.PreparedCommand {
	kubeconfig := fmt.Sprintf("KUBECONFIG=%s", os.Getenv("KUBECONFIG"))
	return shx.Command("kubectl", args...).Env(kubeconfig)
}

func EnsureYq() error {
	return pkg.EnsurePackage("github.com/mikefarah/yq/v4", "", "")
}
