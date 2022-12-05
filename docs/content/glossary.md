---
title: Porter Operator Glossary
description: Definitions for common terms used in the Porter Operator
---

This glossary defines terms that are unique to the Porter Operator.
See the Porter docs for general Porter terminology.

* [Concepts](#concepts)
    * [Operator](#operator)
    * [PorterAgent](#porteragent)
* [Resources](#resources)
  * [Installation](#installation)
  * [CredentialSet](#credentialset)
  * [AgentAction](#agentaction)
  * [AgentConfig](#agentconfig)
  * [PorterConfig](#porterconfig)

## Concepts

### Operator

The Porter Operator is a service that runs on a Kubernetes cluster.
It uses Kubernetes Custom Resource Definitions (CRD) to define [Installation](#installation) and [AgentAction](#agentaction) resources.
The operator watches these resources and actions are triggered when they are modified.
For example, applying an Installation resource to the cluster causes the associated bundle to be installed.
Modifying that Installation resource causes the Installation to be upgraded.
The Installation is uninstalled when the resource is deleted or the uninstalled field on the installation spec is set to true.

### PorterAgent

The PorterAgent is a Kubernetes job that executes a Porter command via the [AgentAction](#agentaction) resource.
The agent is a Docker image with the porter CLI installed, and a custom entry point to assist with applying the Porter [configuration file].

## Resources

### Installation

The [Installation] custom resource represents an installation of a bundle in Porter.

[Installation]: /operator/file-formats/#installation

### CredentialSet

The [CredentialSet] custom resource represents a credential set in Porter.

[CredentialSet]: /operator/file-formats/#credentialset

A CredentialSet supports a credential source of `secret` for Porter secrets 
plugins. Secrets source keys may vary depending on which [secret plugin](/plugins/)
you have configured. The [host secrets plugin](/plugins/host/) is not a
good fit for use with the Porter Operator because environment variables or
files are not a recommended way to manage secrets on a cluster.
The [kubernetes.secrets plugin](https://release-v1.porter.sh/plugins/kubernetes/#secrets) 
can retrieve secrets from native Kubernetes secrets, and otherwise we 
recommend that an external secret store such as [Azure KeyVault](/plugins/azure/#secrets)
or [Hashicorp Vault](/plugins/hashicorp/) are configured instead.

The operator creates a corresponding AgentAction to create, update or delete Porter credentials.
Once created the credential set is available to an Installation resource via its spec file.

### ParameterSet

The [ParameterSet] custom resource represents a parameter set in Porter.

[ParameterSet]: /operator/file-formats/#parameterset

A ParameterSet supports a parameter source of `secret` for Porter secrets 
plugins and `value` for plaintext values.

Secrets source keys may vary depending on which [secret plugin](/plugins/) you have configured.
The [host secrets plugin](/plugins/host/) is not a good fit for use with the Porter Operator because environment variables or
files are not a recommended way to manage secrets on a cluster.
The [kubernetes.secrets plugin](https://release-v1.porter.sh/plugins/kubernetes/#secrets) 
can retrieve secrets from native Kubernetes secrets, and otherwise we 
recommend that an external secret store such as [Azure KeyVault](/plugins/azure/#secrets)
or [Hashicorp Vault](/plugins/hashicorp/) are configured instead.

Value sources are stored in plaintext in the resource.

The operator creates a corresponding AgentAction to create, update or delete Porter parameters.
Once created the parameter set is available to an Installation resource via its spec file.

### AgentAction

The [AgentAction] custom resource represents a Porter command that is run in the [PorterAgent](#porteragent).
The command can be any arbitrary command that the Porter CLI supports.

The Operator creates a corresponding AgentAction to apply changes to [Installation](#installation) resources.
The core bundle commands: install, upgrade, and uninstall are all managed by the Operator through the Installation resource.
The invoke command, which is used to run custom commands defined by the bundle, can only be run with an AgentAction.

[AgentAction]: /operator/file-formats/#agentaction

### AgentConfig

The [AgentConfig] custom resource represents the configuration used by the [PorterAgent](#porteragent).
A default AgentConfig in the specified namespace is generated for you by the **configureNamespace** custom action of the porter-operator bundle.
You can change the configuration for running Porter by creating an AgentConfig resource and overriding relevant fields.

When the PorterAgent runs, it resolves the Agent configuration in a hierarchical manner.
Any matching AgentConfig resources are _merged_ together with the following precedence.
Values are merged from all resolved AgentConfig resources, so that you can define a base set of defaults and selectively override them within a namespace or for a particular resource.

* First, using the AgentConfig defined directly on the resource.
* Using the AgentConfig with the name "default" defined in the resource namespace.
* Using the AgentConfig with the name "default" defined in the operator namespace.
* By default, using a reasonable set of defaults for the default installation of the Operator, assuming that the default RBAC roles exist in the cluster.

Currently, only "kubernetes" plugin can be installed through the plugins configuration.

[AgentConfig]: /operator/file-formats/#agentconfig

### PorterConfig

The [PorterConfig] custom resource represents the Porter configuration file that should be used by the [PorterAgent](#porteragent).
Your Porter [configuration] can be found in PORTER_HOME/config.json|toml|yaml, usually ~/.porter/config.toml, of a local Porter installation.
By default, the PorterAgent connects to the mongodb server deployed with the Operator.
Since the mongodb server is not secured with a password, and is accessible to the cluster, this is not suitable for production use.

Do not hard-code sensitive data in a Porter configuration file.
Secure sensitive data in a secret store and reference the secrets using the template syntax, `${secret.NAME}`.
For more details, see the [configuration file] documentation.

A default PorterConfig is generated for you by the **configureNamespace** custom action of the porter-operator bundle.
You can change the configuration of the porter CLI by creating a PorterConfig resource and overriding relevant fields.

When the PorterAgent runs, it resolves Porter's configuration in a hierarchical manner.
Any matching PorterConfig resources are _merged_ together with the following precedence.
Values are merged from all resolved PorterConfig resources, so that you can define a base set of defaults and selectively override them within a namespace or for a particular resource.

* First, using the PorterConfig defined directly on the resource.
* Using the PorterConfig with the name "default" defined in the resource namespace.
* Using the PorterConfig with the name "default" defined in the operator namespace.
* By default, Porter is configured to connect to the in-cluster mongo database, and use the Kubernetes secret plugin.

[PorterConfig]: /operator/file-formats/#porterconfig
[configuration file]: /configuration/#config-file
[Desired State QuickStart]: /quickstart/desired-state/


## Next Steps

* [Porter Operator File Formats](/operator/file-formats/)
