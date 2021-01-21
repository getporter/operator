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

### Make Targets

Below are the most common developer tasks. Run a target with `make TARGET`, e.g.
`make build`.

* `build` builds all binaries, porter and internal mixins.
* `build-porter-client` just builds the porter client for your operating system.
  It does not build the porter-runtime binary. Useful when you just want to do a
  build and don't remember the proper way to call `go build` yourself.
* `build-porter` builds both the porter client and runtime. It does not clean up
  generated files created by packr, so you usually want to also run
  `clean-packr`.
* `install-porter` installs porter from source into your home directory **$(HOME)/.porter**.
* `install-mixins` installs the mixins from source into **$(HOME)/.porter/**.
  This is useful when you are working on the exec or kubernetes mixin.
* `install` installs porter _and_ the mixins from source into **$(HOME)/.porter/**.
* `test-unit` runs the unit tests.
* `test-integration` runs the integration tests. This requires a kubernetes
  cluster setup with credentials located at **~/.kube/config**. Expect this to
  take 20 minutes.
* `docs-preview` hosts the docs site. See [Preview
  Documentation](#preview-documentation).
* `test` runs all the tests.
* `clean-packr` removes extra packr files that were a side-effect of the build.
  Normally this is run automatically but if you run into issues with packr, 
  run this command.
* `setup-dco` installs a git commit hook that automatically signsoff your commit
  messages per the DCO requirement.

## Modify the porter agent

The "porter agent" is the Docker image used to execute Porter in a Kubernetes job.
At the moment, I am testing out changes to a few dependencies so it is a custom 
local build of getporter/porter, named carolynvs/porter:dev. Once those changes are
back in porter upstream we can use getporter/porter directly.
