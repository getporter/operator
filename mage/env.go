package mage

import (
	"os"
	"path"

	"github.com/carolynvs/magex/mgx"
	"github.com/pkg/errors"
)

const (
	ProductionRegistry = "ghcr.io/getporter"
	TestRegistry       = "localhost:5000"
)

var (
	Env = getAmbientEnvironment()
)

type Environment struct {
	Name               string
	Registry           string
	ManagerImagePrefix string
	BundlePrefix       string
	HelmChartPrefix    string
}

func getAmbientEnvironment() Environment {
	name := os.Getenv("PORTER_ENV")
	switch name {
	case "prod", "production":
		return GetProductionEnvironment()
	case "test", "":
		return GetTestEnvironment()
	default:
		registry := os.Getenv("PORTER_OPERATOR_REGISTRY")
		if registry == "" {
			mgx.Must(errors.New("environment variable PORTER_OPERATOR_REGISTRY must be set to push to a custom registry"))
		}
		return buildEnvironment(name, registry)
	}
}

func UseTestEnvironment() {
	Env = GetTestEnvironment()
}

func UseProductionEnvironment() {
	Env = GetProductionEnvironment()
}

func GetTestEnvironment() Environment {
	return buildEnvironment("test", TestRegistry)
}

func GetProductionEnvironment() Environment {
	return buildEnvironment("production", ProductionRegistry)
}

func buildEnvironment(name string, registry string) Environment {
	return Environment{
		Name:               name,
		Registry:           registry,
		ManagerImagePrefix: path.Join(registry, "porter-operator-manager:"),
		BundlePrefix:       path.Join(registry, "porter-operator:"),
		HelmChartPrefix:    path.Join(registry, "charts"),
	}
}
