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

Porter isn't required on your local machine to work on the Porter Operator
but we recomend that you start there so that you understand how to use Porter.

You will need a Go, Docker, and make to work on the operator.

[tutorial]: https://porter.sh/contribute/tutorial/

## Makefile explained

🚧 We are in the process of transitioning from make to [mage](https://magefile.org).

[mage]: https://magefile.org

### Mage Targets

Below are the targets that have been migrated to mage. Our new contributor
tutorial explains how to [install mage](https://porter.sh/contribute/tutorial/#install-mage).

Mage targets are not case-sensitive, but in our docs we use camel case to make
it easier to read. You can run either `mage Deploy` or `mage deploy` for
example.

* **Deploy** builds the controller and deploys it to the active cluster.
* **Logs** follows the logs for the controller.

### Utility Targets
These are targets that you won't usually run directly, other targets use them as dependencies.

* **EnsureOperatorSDK** installs the operator-sdk CLI if it is not already installed.
* **EnsureCluster** starts a KIND cluster if it's not already running.
* **CreateKINDCluster** creates a new KIND cluster named porter.
* **DeleteKINDCluster** deletes the KIND cluster named porter.

## Modify the porter agent

The "porter agent" is the Docker image used to execute Porter in a Kubernetes job.
At the moment, I am testing out changes to a few dependencies so it is a custom 
local build of getporter/porter, named carolynvs/porter:dev. Once those changes are
back in porter upstream we can use getporter/porter directly.
