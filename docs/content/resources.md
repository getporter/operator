---
title: Porter Operator Resources
---

The Porter Operator provides custom resource definitions (CRDs) that you can use to interact with Porter and control how it is executed.
Though both Porter and Kubernetes has the concept of names, namespaces and labels, the resources do not reuse those fields from the 
CRD, and instead uses the values as defined on the resource spec.
This allows you to run the operator in a Kubernetes namespace, and target a different Porter namespace because
although they both use the term namespace, there is no relation between Kubernetes namespaces and Porter namespaces.
The same goes for the name and labels fields.

* [Installation](#installation)
* [AgentConfig](#agent-config)
* [PorterConfig](#porter-config)

## Installation

The Installation CRD represents an installation of a bundle in Porter.
The Installation CRD spec is a superset of the Installation resource in Porter, so it is safe to copy/paste the output of
the `porter installation show NAME -o yaml` command into the spec field and have that be a valid installation.

In addition to the normal fields available on a [Porter Installation document](https://release-v1--porter.netlify.app/reference/file-formats/)
the following fields are supported:

| Field        | Required    | Default | Description |
| -----------  | ----------- | ------- | ----------- |
| agentConfig  | false       | See [Agent Config](#agent-config) | Reference to an AgentConfig resource in the same namespace.  |
| porterConfig | false       | See [Porter Config](#porter-config) | Reference to a PorterConfig resource in the same namespace.  |

## Agent Config

The Porter Agent is a Kubernetes job that executes the porter CLI when an [installation resource](#installation) is modified.
The agent is a Docker image with the porter CLI installed, and a custom entry point to assist with applying the Porter [configuration file].
By default, the job uses the getporter/porter-agent:latest image.
The AgentConfig CRD represents the configuration that the operator should use when executing Porter on Kubernetes, which is known as the Porter agent.

A default AgentConfig is generated for you by the **configureNamespace** custom action of the porter-operator bundle.
You can change the configuration for running Porter by creating an AgentConfig resource and overriding relevant fields.
Depending on the desired scope of that configuration either reference that AgentConfig directly on the installation or name the AgentConfig default and define it in the installation namespace or the porter-operator-system namespace.

```yaml
apiVersion: porter.sh/v1
kind: AgentConfig
metadata:
  name: customAgent
spec:
  porterRepository: ghcr.io/getporter/porter-agent
  porterVersion: canary
  serviceAccount: porter-agent
  volumeSize: 64Mi
  pullPolicy: Always
  installationServiceAccount: installation-agent
```

Configuration is hierarchical and has the following precedence:

* AgentConfig referenced on the Installation overrides everything else.
* AgentConfig defined in the Installation namespace with the name "default".
* AgentConfig defined in the Porter Operator namespace with the name "default".

Values are merged from all resolved AgentConfig resources, so that you can define a base set of defaults and selectively override them within a namespace or for a particular installation.

| Field        | Required    | Default | Description |
| -----------  | ----------- | ------- | ----------- |
| porterRepository  | false  | ghcr.io/getporter/porter-agent | The repository for the Porter Agent image. |
| porterVersion | false      | latest  | The tag for the Porter Agent image. For example, vX.Y.Z, latest, or canary.  |
| serviceAccount | true | (none) | The service account to run the Porter Agent under. Must exist in the same namespace as the installation. |
| installationServiceAccount | false | (none) | The service account to run the Kubernetes pod/job for the installation image. |
| volumeSize | false | 64Mi | The size of the persistent volume that Porter will request when running the Porter Agent. It is used to share data between the Porter Agent and the bundle invocation image. It must be large enough to store any files used by the bundle including credentials, parameters and outputs. |
| pullPolicy | false | PullAlways when the tag is canary or latest, otherwise PullIfNotPresent. | Specifies when to pull the Porter Agent image |

### Service Account

The only required configuration is the name of the service account under which Porter should run.
The configureNamespace action of the porter operator bundle creates a service account named "porter-agent" for you with the porter-operator-agent-role role binding.

## Porter Config

The PorterConfig CRD represents the porter configuration file that should be used by the Porter Agent.
On a local installation of Porter, this file can be found in PORTER_HOME/config.json|toml|yaml, usually ~/.porter/config.toml.
By default, Porter uses the mongodb server deployed with the Operator.
Since the mongodb server is not secured with a password, and is accessible to the cluster, this is not suitable for production use.

🔒 We don't yet support referencing external secrets from the configuration file, so be careful if you embed a real connection string in this file!

A default PorterConfig is generated for you by the **configureNamespace** custom action of the porter-operator bundle.
You can the configuration of the porter CLI by creating a PorterConfig resource and overriding relevant fields.
Depending on the desired scope of that configuration either reference that PorterConfig directly on the installation or name the PorterConfig default and define it in the installation namespace or the porter-operator-system namespace.

```yaml
apiVersion: porter.sh/v1
kind: PorterConfig
metadata:
  name: customPorterConfig
spec:
  debug: true
  debugPlugins: false
  defaultSecretsPlugin: kubernetes.secrets
  defaultStorage: in-cluster-mongodb
  storage:
    - name: in-cluster-mongodb
      plugin: mongodb
      config:
        url: "mongodb://mongodb.porter-operator-system.svc.cluster.local"

```

Configuration is hierarchical and has the following precedence:

* PorterConfig referenced on the Installation overrides everything else.
* PorterConfig defined in the Installation namespace with the name "default".
* PorterConfig defined in the Porter Operator namespace with the name "default".

Values are merged from all resolved PorterConfig resources, so that you can define a base set of defaults and selectively override them within a namespace or for a particular installation.

| Field        | Required    | Default | Description |
| -----------  | ----------- | ------- | ----------- |
| debug        | false       | false   | Specifies if Porter should output debug logs. |
| debugPlugins | false       | false   | Specifies if Porter should output debug logs for the plugins. |
| namespace    | false       | (empty) | The default Porter namespace. Used when a resource is defined without the namespace set in the spec. |
| experimental | false       | (empty) | Specifies which experimental features are enabled. See Porter Feature Flags for more information. |
| defaultStorage | false     | in-cluster-mongodb | The name of the storage configuration to use. |
| defaultSecrets | false     | (empty) | The name of the secrets configuration to use. |
| defaultStoragePlugin | false | (empty) | The name of the storage plugin to use when defaultStorage is unspecified. |
| defaultSecretsPlugin | false | kubernetes.secrets | The name of the storage plugin to use when defaultSecrets is unspecified. |
| storage | false | The mongodb server installed with the operator. | A list of named storage configurations. |
| secrets | false | (empty) | A list of named secrets configurations. |

[Porter Feature Flags]: https://release-v1.porter.sh/configuration/#experimental-feature-flags
[configuration file](https://release-v1.porter.sh/configuration/)
