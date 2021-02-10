<img align="right" src="https://porter.sh/images/porter-docs-header.svg" width="300px" />

[![Build Status](https://github.com/getporter/operator/workflows/build/badge.svg)](https://github.com/getporter/operator/actions?query=workflow:pr)

# PorterOps: Porter Operator

🚨 This is a new project and the goals below are aspirational and not all implemented yet.

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

# Install

Use explain to see what credentials and parameters you can use when installing and configuring the operator.
```
porter explain -r ghcr.io/getporter/porter-operator:canary
```

Generate a credential set for the bundle
```
porter credentials generate porterops -r ghcr.io/getporter/porter-operator:canary
```

Install the operator into the porter-operator-system namespace
```
porter install porterops -c porterops -r ghcr.io/getporter/porter-operator:canary
```

Create a namespace with the appropriate rbac, secrets and configmaps. This is where you will run porter.

```
porter invoke porterops --action configure-namespace --param namespace=TODO -c porterops
```

# Install a bundle

Here is an example installation CRD:

```yaml
apiVersion: porter.sh/v1
kind: Installation
metadata:
  name: porter-hello
spec:
  reference: "getporter/porter-hello:v0.1.1"
  action: "install"
```

After you apply it with `kubectl apply -f`, the Porter Operator will run the following command:

```
porter install porter-hello getporter/porter-hello:v0.1.1
```

# Upgrade a bundle

Edit the installation CRD and change the action to "upgrade":

```yaml
apiVersion: porter.sh/v1
kind: Installation
metadata:
  name: porter-hello
spec:
  reference: "getporter/porter-hello:v0.1.1"
  action: "upgrade"
```

# Retry the last operator

If your bundle operation failed, you can run it again by changing an annotation on the installation CRD:

```
apiVersion: porter.sh/v1
kind: Installation
metadata:
  name: porter-hello
  annotations:
    retry: 1
spec:
  reference: "getporter/porter-hello:v0.1.1"
  action: "upgrade"
```

Each time you need to repeat the operation, change the annotation value again.
There is nothing special about the key used for the annotation. I chose retry,
however you could use "favorite-color: blue", changing the value each time, and
it would still trigger Porter to retry it. 

# Configure the Operator
This section breaks down what the configure-namespace action of the bundle is
doing under the hood.

## Define Secrets

The operator uses secrets defined in the namespace of the CRD being managed to populate
Porter's config file and environment variables. For example the azure connection
string, or service principal environment variables.

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
  --from-literal=AZURE_STORAGE_CONNECTION_STRING=$AZURE_STORAGE_CONNECTION_STRING \
  --from-literal=AZURE_CLIENT_SECRET=$AZURE_CLIENT_SECRET \
  --from-literal=AZURE_CLIENT_ID=$AZURE_CLIENT_ID \
  --from-literal=AZURE_TENANT_ID=$AZURE_TENANT_ID
``` 

Right now the bundle only works with azure plugin.

## Define Configuration

### porter
These are configuration settings for the Porter Operator.

```
kubectl create configmap porter \
  --from-literal=serviceAccount=porter-agent \
  --from-literal=porterVersion=canary \
  --from-literal=porterRepository=ghcr.io/getporter/porter-operator \
  --from-literal=volumeSize=64Mi \
  --from-literal=pullPolicy=Always
```

Only serviceAccount is required, the rest can be omitted.

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