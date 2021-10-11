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

## Agent Config

The AgentConfig CRD represents the configuration that the operator should use when executing Porter on Kubernetes, which is known as the Porter agent.

Configuration is hierarchical and has the following precedence:

* AgentConfig referenced on the Installation overrides everything else
* AgentConfig defined in the Installation namespace
* AgentConfig defined in the Porter Operator namespace is the default

## Porter Config

The PorterConfig CRD represents the porter configuration file found in PORTER_HOME/config.json|toml|yaml, usually ~/.porter/config.toml.

Configuration is hierarchical and has the following precedence:

* PorterConfig referenced on the Installation overrides everything else
* PorterConfig defined in the Installation namespace
* PorterConfig defined in the Porter Operator namespace is the default