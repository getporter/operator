// +build mage

// This is a magefile, and is a "makefile for go".
// See https://magefile.org/
package main

import (
	"errors"
	"fmt"
	"runtime"

	"github.com/carolynvs/magex/pkg"
	"github.com/magefile/mage/sh"
)

// Default target to run when none is specified
// If not set, running mage will list available targets
// var Default = Build

const (
	registryContainer = "registry"
	mixinsURL         = "https://cdn.porter.sh/mixins/"
)

// Ensure Mage is installed and on the PATH.
func EnsureMage() error {
	return pkg.EnsureMage("")
}

func Deploy() error {
	err := sh.RunV("make", "docker-build", "docker-push")
	if err != nil {
		return err
	}

	err = sh.RunV("make", "deploy")
	if err != nil {
		return err
	}

	return sh.RunV("kubectl", "rollout", "restart", "deployment/porter-operator-controller-manager", "--namespace=porter-operator-system")
}

func Logs() error {
	return sh.RunV("kubectl", "logs", "-f", "deployment/porter-operator-controller-manager", "-c=manager", "--namespace=porter-operator-system")
}

func EnsureOperatorSDK() error {
	const version = "v1.3.0"

	if runtime.GOOS == "windows" {
		return errors.New("Sorry, OperatorSDK does not support Windows. In order to contribute to this repository, you will need to use WSL.")
	}

	url := fmt.Sprintf("https://github.com/operator-framework/operator-sdk/releases/%s/download/operator-sdk_%s_%s", version, runtime.GOOS, runtime.GOARCH)
	return pkg.DownloadToGopathBin(url, "operator-sdk")
}
