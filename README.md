<img align="right" src="https://porter.sh/images/porter-docs-header.svg" width="300px" />

[![Build Status](https://github.com/getporter/operator/workflows/build/badge.svg)](https://github.com/getporter/operator/actions?query=workflow:pr)

# PorterOps: Porter Operator

PorterOps not only gives you a native, integrated experience for managing your
bundles with Kubernetes but is the recommended way to automate your bundle
pipeline with support for GitOps.

* Automate building and publishing bundles, when the bundle definition changes
  or new versions of images used by the bundle are available.
* Automatically deploy new versions of bundles.
* Isolated environments for running bundles in your organization, limiting
  access to secrets used by the bundles using namespaces and RBAC.
* Create and respond to events on your cluster to integrate bundles into your
  pipeline.

<p align="center">Learn all about Porter at <a href="https://porter.sh">porter.sh</a></p>

## Define Secrets

The operator uses secrets defined in namespace of the CRD being managed to populate
Porter's config file and environment variables. For example the azure connection
string, or service principal environment variables.

TODO: Have porter help do these parts

### porter-config
These secrets are copied into the pod as files to tell Porter where to save its data
and resolve secrets.

```
kubectl create secret generic porter-config \
  --from-file=config.toml=/Users/carolynvs/.porter/k8s.config.toml
```

### porter-env
These secrets are copied into the pod as environment variables. Porter will use them to 
for the plugins so that they can connect to remote services such storage or vault.

```
kubectl create secret generic porter-env \
  --from-literal=AZURE_STORAGE_CONNECTION_STRING=$PORTER_TEST_AZURE_STORAGE_CONNECTION_STRING \
  --from-literal=AZURE_CLIENT_SECRET=$PORTER_AZURE_CLIENT_SECRET \
  --from-literal=AZURE_CLIENT_ID=$PORTER_AZURE_CLIENT_ID \
  --from-literal=AZURE_TENANT_ID=$PORTER_AZURE_TENANT_ID
``` 

## Define Configuration

### porter
These are configuration settings for the Porter Operator.

```
kubectl create configmap porter \
  --from-literal=porterVersion=canary \
  --from-literal=serviceAccount=porter-agent
```

See [Modify the porter agent](/CONTRIBUTING.md#modify-the-porter-agent) for details on 
how this is created.

# Contact

* [Mailing List] - Great for following the project at a high level because it
  is low traffic, mostly release notes and blog posts on new features.
* [Slack] - Discuss #porter or #cnab with other users and the maintainers.
* [Open an Issue] - If you have a bug, feature request or question about Porter,
  ask on GitHub so that we can prioritize it and make sure you get an answer.
  If you ask on Slack, we will probably turn around and make an issue anyway. 😉

[Mailing List]: https://porter.sh/mailing-list
[Slack]: https://porter.sh/community/#slack
[Open an Issue]: https://github.com/getporter/operator/issues/new

---

# Looking for Contributors

Want to work on Porter with us? 💖 We are actively seeking out new contributors
with the hopes of building up both casual contributors and enticing some of you
into becoming reviewers and maintainers.

<p align="center">Start with our <a href="https://porter.sh/contribute/">New Contributors Guide</a>

Porter wouldn't be possible without our [contributors][contributors], carrying
the load and making it better every day! 🙇‍♀️

[contributors]: https://porter.sh/src/CONTRIBUTORS.md

---

# Roadmap

Porter is an open-source project and things get done as quickly as we have
motivated contributors working on features that interest them. 😉

We use a single [project board][board] across all of our repositories to track
open issues and pull requests.

The roadmap represents what the core maintainers have said that they are
currently working on and plan to work on over the next few months. We use the
"on-hold" bucket to communicate items of interest that doesn't have a core
maintainer who will be working on it.

<p align="center">Check out our <a href="https://porter.sh/roadmap">roadmap</a></p>

[board]: https://porter.sh/board