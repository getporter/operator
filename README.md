<img align="right" src="https://getporter.org/images/porter-docs-header.svg" width="300px" />

[![Build Status](https://github.com/getporter/operator/workflows/build/badge.svg)](https://github.com/getporter/operator/actions?query=workflow:pr)

# Porter Operator

🚨 **This is a new project; the goals below are aspirational and not all implemented yet.**

The Porter Operator gives you a native, integrated experience for managing your
bundles from Kubernetes. It is the recommended way to automate your bundle
pipeline with support for GitOps.

* Manage bundle installations using desired state configuration.
  * Installs the bundle when an installation CRD is added. 
  * Upgrades the bundle when the bundle definition or values used to install the bundle change.
  * Uninstalls the bundle when the installation CRD is deleted or when spec.uninstalled is set to true.
* Automatically deploy new versions of bundles when a new version is pushed, and update an 
  installation when changes are pushed in git, through integration with Flux.
* Isolated environments for running bundles in your organization, limiting
  access to secrets used by the bundles using namespaces and RBAC.
* Create and respond to events on your cluster to integrate bundles into your
  pipeline.

<p align="center">Learn all about Porter at <a href="https://getporter.org/operator/">getporter.org/operator/</a></p>

# Project Status

🚧 This is a proof of concept only, is currently being rewritten to work with the Porter v1 prerelease.
**It is not safe to use in production or with production secrets.**

We are planning a security review and audit before it is released.

# Try it out

Follow our [QuickStart] to install the Porter Operator on an existing Kubernetes cluster.
If you want to build from source, instructions are in the [Contributor's Guide].

# Contact

* [Mailing List] - Great for following the project at a high level because it is low traffic, mostly release notes and blog posts on new features.
* [Slack] - Discuss #porter or #cnab with other users and the maintainers.
* [Open an Issue] - If you have a bug, feature request or question about Porter, ask on GitHub so that we can prioritize it and make sure you get an answer.
  If you ask on Slack, we will probably turn around and make an issue anyway. 😉

[Mailing List]: https://getporter.org/mailing-list
[Slack]: https://getporter.org/community/#slack
[Open an Issue]: https://github.com/getporter/operator/issues/new

---

# Looking for Contributors

Want to work on Porter with us? 💖 We are actively seeking out new contributors
with the hopes of building up both casual contributors and enticing some of you
into becoming reviewers and maintainers.

<p align="center">Start with our <a href="https://getporter.org/contribute/">New Contributors Guide</a>

Porter wouldn't be possible without our [contributors][contributors], carrying
the load and making it better every day! 🙇‍♀️

[contributors]: https://getporter.org/src/CONTRIBUTORS.md

---

# Roadmap

Porter is an open-source project and things get done as quickly as we have motivated contributors working on features that interest them. 😉

We use a single [project board][board] across all of our repositories to track open issues and pull requests.

The roadmap represents what the core maintainers have said that they are currently working on and plan to work on over the next few months. We use the
"on-hold" bucket to communicate items of interest that doesn't have a core maintainer who will be working on it.

<p align="center">Check out our <a href="https://getporter.org/roadmap">roadmap</a></p>

[board]: https://getporter.org/board
[Contributor's Guide]: CONTRIBUTING.md
[connect]: CONTRIBUTING.md#connect-to-the-in-cluster-mongo-database
[QuickStart]: /docs/content/quickstart/_index.md
