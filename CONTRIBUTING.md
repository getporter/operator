# Contributing Guide

---
* [New Contributor Guide](#new-contributor-guide)
* [Developer Tasks](#developer-tasks)
  * [Initial setup](#initial-setup)
  * [Makefile explained](#makefile-explained)
  * [Run a test installation](#run-a-test-installation)  
  * [Modify the porter agent](#modify-the-porter-agent)
---

# New Contributor Guide

The [Porter New Contributor Guide](https://porter.sh/src/CONTRIBUTING.md) has information on how to find issues, what
type of help we are looking for, what to expect in your first pull request and
more.

# Developer Tasks

## Initial setup

We have a [tutorial] that walks you through how to setup your developer
environment for Porter, make a change and test it.

We recommend that you start there so that you understand how to use Porter.

You will need a Porter, KinD, Go, and Docker to work on the operator.

[tutorial]: https://porter.sh/contribute/tutorial/

```
# Install mage
go run mage.go EnsureMage

# Build and deploy the opeartor to a local KinD cluster
mage deploy

# Use the test cluster
export KUBECONFIG=kind.config
```

## Magefile explained

We use [mage](https://magefile.org) instead of make. If you don't have mage installed already,
you can install it with `go run mage.go EnsureMage`.

[mage]: https://magefile.org

Mage targets are not case-sensitive, but in our docs we use camel case to make
it easier to read. You can run either `mage Deploy` or `mage deploy` for
example. Run `mage` without any arguments to see a list of the available targets.

* **Deploy** builds the controller and deploys it to a local KinD cluster.
* **Bump**, e.g. `mage bump porter-hello` applies one of the sample Installations in config/samples to the test cluster.
* **Logs** follows the logs for the controller.

### Utility Targets
These are targets that you won't usually run directly, other targets use them as dependencies.

* **EnsureOperatorSDK** installs the operator-sdk CLI if it is not already installed.
* **EnsureTestCluster** starts a KIND cluster if it's not already running.
* **CreateTestCluster** creates a new KIND cluster named porter.
* **DeleteTestCluster** deletes the KIND cluster named porter.
* **Clean** deletes all data from the test cluster.
* **CleanManual** removes all 
* **CleanTests** removes any namespaces created by the test suite (where porter-test=true).

## Modify the porter agent

If you need to make changes to the Porter CLI used by the operator, known as the Porter Agent,
you can check out Porter's source code, and then run

```
mage XBuildAll LocalPorterAgentBuild
```

This will build an agent for you named localhost:5000/porter-agent:canary-dev and push
it to the local registry running on the test cluster you have set up for the operator.

Then set the PORTER_AGENT_REPOSITORY and PORTER_AGENT_VERSION environment variables and
then use the SetupNamespace target to configure your Kubernetes namespace with a custom
AgentConfig that uses your local build:

```
export PORTER_AGENT_REPOSITORY=localhost:5000/porter-agent
export PORTER_AGENT_VERSION=canary-dev
mage SetupNamespace test
```


## Run a test installation

There are sample installation CRDs in [config/samples](/config/samples) that you can quickly try out with:

```
mage bump SAMPLE
```

This mage target handles running `porter installation apply` for you and sets an annotation to force the installation to be reconciled.
You can do this manually by following the instructions at [Retry the last operation](https://release-v1.porter.sh/operator/quickstart/#retry-the-last-operation).

For example, to apply [config/samples/porter-hello.yaml](/config/samples]/porter-hello.yaml), run command below.
If the installation does not already exist, it will be created
Otherwise, the retry annotation on the installation to force the operator to reevaluate the installation.

```
mage bump porter-hello
```


## Connect to the in-cluster mongo database

When you install the operator from source, or install the operator bundle without specifying the Porter configuration, the operator will run a Mongodb server in the same namespace as the operator.
It runs on the default Mongodb port (27017) and authentication is not required to connect to it.

With your local Porter configuration file pointed to the in-cluster mongodb server, you can use Porter to query and interact with installations created by the operator.

Expose the in-cluster mongodb server on the default mongo porter: 27017.
```
kubectl port-forward --namespace porter-operator-system svc/mongodb 27017:27017 >/dev/null &
```

Update ~/.porter/config.toml to use the in-cluster mongodb server.
The in-cluster Mongodb server is running with authentication turned off so there is no username or password required.

```toml
default-storage = "in-cluster-mongodb"

[[storage]]
  name = "in-cluster-mongodb"
  plugin = "mongodb"

  [storage.config]
    url = "mongodb://localhost:27017"
```

In the example below, the [config/samples/porter-hello.yaml](/config/samples/porter-hello.yaml) installation CRD is applied,
and then porter is used to view the logs.

```
# Apply the CRD
mage bump porter-hello

# Wait for the operator to run the installation
kubectl get pods -n test --wait 

# Now you can see the result in porter!
porter logs hello -n operator
```
