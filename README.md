<img align="right" src="https://porter.sh/images/porter-docs-header.svg" width="300px" />

[![Build Status](https://github.com/getporter/operator/workflows/build/badge.svg)](https://github.com/getporter/operator/actions?query=workflow:pr)

# PorterOps: Porter Operator

🚨 **This is a new project; the goals below are aspirational and not all implemented yet.**

The Porter Operator gives you a native, integrated experience for managing your
bundles from Kubernetes. It is the recommended way to automate your bundle
pipeline with support for GitOps.

* Manage bundle installations using desired state configuration.
* Installs the bundle when an installation CRD is added. 
* Upgrades the bundle when the bundle definition or values used to install the bundle change.
* Uninstalls the bundle when the installation CRD is deleted.
* Automatically deploy new versions of bundles when a new version is pushed, and update an 
  installation when changes are pushed in git, through integration with Flux.
* Isolated environments for running bundles in your organization, limiting
  access to secrets used by the bundles using namespaces and RBAC.
* Create and respond to events on your cluster to integrate bundles into your
  pipeline.

<p align="center">Learn all about Porter at <a href="https://porter.sh">porter.sh</a></p>

# Project Status

🚧 This is a proof of concept only, is currently being rewritten to work with the Porter v1 prerelease.
**It is not safe to use in production or with production secrets.**

We are planning a security review and audit before it is released.

# Install from source

Requirements:
* KinD
* Docker
* Go

```
mage deploy
```

# Install

Use explain to see what credentials and parameters you can use when installing and configuring the operator.
```
porter explain -r ghcr.io/getporter/porter-operator:canary
```

Generate a credential set for the bundle, the only required credential for the operator is a kubeconfig for the cluster that the operator is to be installed in.
```
porter credentials generate porterops -r ghcr.io/getporter/porter-operator:canary
```

Install the operator into the porter-operator-system namespace
```
porter install porterops -c porterops -r ghcr.io/getporter/porter-operator:canary
```

Create a namespace with the appropriate RBAC and configuration. This is where you will run porter.

```
porter invoke porterops --action configure-namespace --param namespace=TODO -c porterops
```

* The operator installs a mongodb server in its namespace (with no password set for root). This is only
  suitable for testing the operator.
* A PorterConfig resource named default is created in the specified namespace configuring Porter to use
  the kubernetes.secrets and mongodb plugin.

# Run a test installation

There are sample installation CRDs in config/samples that you can quickly try out with:

```
mage bump SAMPLE
```

For example, to apply config/samples/porter-hello.yaml, run command below.
If the installation does not already exist, it will be created
Otherwise, the retry annotation on the installation to force the operator to reevaluate the installation.

```
mage bump porter-hello
```

# Inspect the installation

If you have your local Porter configuration file pointed to the in-cluster mongodb server, you can use Porter
directly to check the status of an installation.

Expose the in-cluster mongodb server on the default mongo porter: 27017.
```
kubectl port-forward --namespace porter-operator-system svc/mongodb 27017:27017 &
```

Update ~/.porter/config.toml to use the in-cluster mongodb server.
The in-cluster mongodb server is running with authentication turned off
so there is no username or password required.
```toml
default-storage = "in-cluster-mongodb"

[[storage]]
  name = "in-cluster-mongodb"
  plugin = "mongodb"

  [storage.config]
    url = "mongodb://localhost:27017"
```

In the example below, the config/samples/porter-hello.yaml installation CRD is applied,
and then porter is used to view the logs.
```
mage bump porter-hello
# wait a few seconds, the next command will only return logs once the bundle finishes
porter logs hello -n operator
```

# Configure

The bundle accepts a parameter, porter-config, that should be a YAML-formatted [Porter configuration file](https://release-v1.porter.sh/configuration).

Here is an example of the default configuration used when none is specified:

```yaml
# Resolve secrets using secrets on the cluster in the current namespace.
default-secrets-plugin: "kubernetes.secrets"

# Use the mongodb server that was deployed with the operator
default-storage: "in-cluster-mongodb"
storage:
  - name: "in-cluster-mongodb"
    plugin: "mongodb"
    config:
      url: "mongodb://mongodb.porter-operator-system.svc.cluster.local"
```

You can use a different file when installing the operator like so:

```
porter install porterops --param porter-config=./myconfig.yaml  -c porterops -r ghcr.io/getporter/porter-operator:canary
```

The bundle also has parameters defined that control how the Porter agent is configured and run.

| Parameter  | Description  |
|---|---|
| installationServiceAccount  | Name of the service account to run installation with.<br/>If set, you are responsible for creating this service account and giving it required permissions.  |
| namespace  | Setup Porter in this namespace  |
| porterRepository  | Docker image repository of the Porter agent.<br/><br/>Defaults to ghcr.io/getporter/porter.  |
| porterVersion  | Version of the Porter agent, e.g. latest, canary, v0.33.0.<br/><br/>Defaults to latest.  |
| pullPolicy  | Specifies how the Porter agent image should be pulled. Does not affect how bundles are pulled.<br/><br/>Defaults to PullAlways for latest and canary, and PullIfNotPresent otherwise.  |
| serviceAccount  | Name of the service account to run the Porter agent.<br/>If set, you are responsible for creating this service account and binding it to the porter-agent ClusterRole.<br/><br/>Defaults to the porter-agent account created by the configure-namespace custom action.  |
| volumeSize  | Size of the volume shared between Porter and the bundles it executes.<br/><br/>Defaults to 64Mi.  |
                                                                                                                                                                      string                  false      configure-namespace

# Apply an installation

The operator does not explicitly run install or upgrade. Instead, the operator relies on the `porter installation apply` command
to reconcile the state of the installation against the desired state defined in the CRD.
After the bundle is installed, changing a field on the CRD and applying it will trigger an upgrade.

Here is an example installation CRD:

```yaml
apiVersion: porter.sh/v1
kind: Installation
metadata:
  name: hello-llama
spec:
  schemaVersion: 1.0.0
  targetNamespace: demo
  installationName: mellama
  bundleRepository: getporter/hello-llama
  bundleVersion: 0.1.1
  parameters:
    name: "my lovely drama llamas"
```

After you apply it with `kubectl apply -f`, the Porter Operator will run the following command, passing in the
Porter representation of the installation CRD.

```
porter installation apply
```

# Uninstall a bundle

This isn't supported yet. Once it's implemented, uninstall is triggered when a CRD is deleted.

# Retry the last operation

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
doing under the hood. If you end up having to manually configure these values,
let us know! That means the custom action in our bundle isn't working out.

## AgentConfig

The operator has a CRD, agentconfig.porter.sh, that contains settings for the
porter operator. These values are used by the job that runs porter. Only
serviceAccount is required, the rest may be omitted or set to "". This is generated
for you by the **configure-namespace** custom action of the porter-operator bundle.

```yaml
apiVersion: porter.sh/v1
kind: AgentConfig
metadata:
  name: porter
spec:
  serviceAccount: porter-agent # Required. ServiceAccount to run the Porter Agent under.
  pullPolicy: Always # Optional. Policy for pulling new versions of the Porter Agent image. Defaults to Always for latest and canary, IfNotPresent otherwise.
  porterVersion: canary # Optional. Version of the Porter Agent image. Allowed values: latest, canary, vX.Y.Z
  porterRepository: ghcr.io/getporter/porter-agent # Optional. The Porter Agent repository to use.
  volumeSize: 128Mi # Optional. The size of the shared volume used by Porter and the invocation image. Defaults to 64Mi
  installationServiceAccount: # Optional. ServiceAccount to run the installation under, service account must exist in the namespace that the installation is run in.
```

The agent configuration has a hierarchy and values are merged from all three
(empty values are ignored):

1. Referenced on the Installation (highest)
2. The default AgentConfig in the installation's namespace.
3. The default AgentConfig defined in the porter-operator-system namespace. (lowest)

## PorterConfig

The operator has a CRD, porterconfig.porter.sh, that contains a [Porter configuration file](https://release-v1.porter.sh/configuration)
embedded in the spec. 

🔒 We don't yet support referencing an external secret, so be careful if you embed a real connection string in this file!

```yaml
apiVersion: porter.sh/v1
kind: PorterConfig
metadata:
  name: default
spec:
  debug: true # Optional. Specifies if porter should output additional debug logs. Defaults to false.
  debugPlugins: true # Optional. Specifies if porter should output additional debug logs related to plugins. Defaults to false.
  default-secrets-plugin: kubernetes.secrets. # Optional. Specifies the key of the secrets plugin to use. Defaults to the Kubernetes secrets plugin.
  default-storage: in-cluster-mongodb # Optional. Specifies the name of the storage configuration to use. Defaults to the in-cluster mongodb server deployed with the operator.
  storage: # Optional. Defines a storage configuration to use instead of the in-cluster mongodb server.
    - name: in-cluster-mongodb
      plugin: mongodb
      url: "mongodb://mongodb.porter-operator-system.svc.cluster.local"
```

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
