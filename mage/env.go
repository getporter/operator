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
	Name            string
	Registry        string
	ControllerImage string
	AgentImage      string
}

func getAmbientEnvironment() Environment {
	name := os.Getenv("ENV")
	switch name {
	case "prod", "production":
		return GetProductionEnvironment()
	case "test", "":
		return GetTestEnvironment()
	default:
		mgx.Must(errors.Errorf("invalid ENV %q", name))
		return Environment{}
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
		Name:            name,
		Registry:        registry,
		ControllerImage: path.Join(registry, "porterops-controller:canary"),
		AgentImage:      path.Join(registry, "porter:kubernetes-canary"),
	}
}
