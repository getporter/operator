# Contributing Guide

---
* [New Contributor Guide](#new-contributor-guide)
* [Developer Tasks](#developer-tasks)
  * [Initial setup](#initial-setup)
  * [Makefile explained](#makefile-explained)
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

You will need a Porter, Go, and Docker to work on the operator.

[tutorial]: https://porter.sh/contribute/tutorial/

```
# Install mage
go run mage.go EnsureMage

# Build and deploy the opeartor to a local KinD cluster
mage deploy
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

At the moment, the scripts all assume that you have done this and try to configure the operator
to use a local agent build. If you are not running a local build, change hack/params and specify
the agent image you want to use.