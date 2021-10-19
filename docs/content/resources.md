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

The AgentConfig CRD represents the configuration that the operator should use when executing Porter on Kubernetes, which is known as the Porter agent.

Configuration is hierarchical and has the following precedence:

* AgentConfig referenced on the Installation overrides everything else
* AgentConfig defined in the Installation namespace
* AgentConfig defined in the Porter Operator namespace is the default

| Field        | Required    | Default | Description |
| -----------  | ----------- | ------- | ----------- |
| porterRepository  | false  | ghcr.io/getporter/porter-agent | The repository for the Porter Agent image. |
| porterVersion | false      | latest  | The tag for the Porter Agent image. |
| serviceAccount | false | (none) | The service account to run the Porter Agent under. |
| installationServiceAccount | false | (none) | The service account to run the Kubernetes pod/job for the installation image. |
| volumeSize | false | 64Mi | The size of the persistent volume that Porter will request when running the Porter Agent. It is used to share data between the Porter Agent and the bundle invocation image. It must be large enough to store any files used by the bundle including credentials, parameters and outputs. |
| pullPolicy | false | PullAlways when the tag is canary or latest, otherwise PullIfNotPresent. | Specifies when to pull the Porter Agent image |

## Porter Config

The PorterConfig CRD represents the porter configuration file found in PORTER_HOME/config.json|toml|yaml, usually ~/.porter/config.toml.
By default, Porter uses the mongodb server deployed with the Operator. Since the mongodb server is not secured with a password, and is accessible to the cluster, this is not suitable for production use.

Configuration is hierarchical and has the following precedence:

* PorterConfig referenced on the Installation overrides everything else
* PorterConfig defined in the Installation namespace
* PorterConfig defined in the Porter Operator namespace is the default

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
