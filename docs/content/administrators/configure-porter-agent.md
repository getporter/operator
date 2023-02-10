---
title: Configuring the Porter Agent
description: How to customize Porter Agent
---

The Porter Agent is a containerized version of the Porter CLI that is optimized for running Porter commands on a Kubernetes cluster. With the AgentConfig Custom Resource Definition (CRD), you can customize how the Porter Agent is run to meet your specific needs and requirements. For example, you can specify the version of Porter to use, install additional Porter plugins, or provide a custom Porter config file.

This guide will show some ways to configure the Porter Agent through the [AgentConfig CRD](/operator/file-format/#agentconfig).

First, let's create a new file with the AgentConfig CRD:
```yaml
apiVersion: getporter.org/v1
kind: AgentConfig
metadata:
  name: customAgent
spec:
```

### Specifying a version

By default, the Porter Agent uses the latest version from the official GitHub release. However, if you want to use a different version or a custom build hosted outside the official project, you can specify the repository and the version in the CRD. Here's an example:
```yaml
apiVersion: getporter.org/v1
kind: AgentConfig
metadata:
  name: customAgent
spec:
  porterRepository: <your-own-repository>
  porterVersion: v1.2.3
```

### Configuring Access Permission

In some cases, you may want to restrict access to the private registry that contains the images you need to install. With the AgentConfig CRD, you can specify two service accounts, one for the pod that runs the Agent job and another for the pod that runs the Porter installation. Here's an example:
```yaml
apiVersion: getporter.org/v1
kind: AgentConfig
metadata:
  name: customAgent
spec:
  porterRepository: <your-own-repository>
  porterVersion: v1.2.3
  serviceAccount: <service-account-for-porter-agent>
  installationServiceAccount: <service-account-for-the-installation>
```

The porter operator ships two pre-defined ClusterRole, agentconfigs-editor-role and agentconfigs-viewer-role, for AgentConfig resources to help you to properly assign permissions to a custom service account.

For more information on working with private registry images, see [this section of the Porter Operator Quickstart Guide](/quickstart/#private-bundle-registries).



## Configuring Porter Plugins

You can also specify any required plugins necessary for your installation of Porter. For example, if you want to use the Kubernetes and Azure plugins, you can configure the AgentConfig like this:

```yaml
apiVersion: getporter.org/v1
kind: AgentConfig
metadata:
  name: customAgent
spec:
  pluginConfigFile:
    schemaVersion: 1.0.0
    plugins:
        kubernetes:
            version: v1.0.1
        azure:
            version: v1.0.1

```

The schema for the pluginConfigFile field is defined [in the Porter reference documentation](/reference/file-formats/#plugins).

🚨 WARNING: By default, the plugin version is set to `latest`. We recommend pinning to specific version of any plugins used to avoid undesired behavior caused by a stale plugin cache. Porter currently does not expire cached installations of plugins, so installing "latest" will not pick up new versions of plugins when they are released.