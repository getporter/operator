package mage

import "path"

const (
	ProductionRegistry = "ghcr.io/getporter"
	TestRegistry       = "localhost:5000"
)

var (
	Env = GetTestEnvironment()
)

type Environment struct {
	Name            string
	Registry        string
	ControllerImage string
	AgentImage      string
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
