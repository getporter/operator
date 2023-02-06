---
title: Configuring the Porter Agent
description: How to customize Porter Agent
---

The Porter Agent is a critical component in executing Porter commands in a controlled and reliable manner. With the AgentConfig Custom Resource Definition (CRD), you can customize the Porter Agent to meet your specific needs and requirements.

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

For more information on working with private registry images, see [this section of the Porter Operator Quickstart Guide](/quickstart/#private-bundle-registries).



## Configuring Porter Plugins

In addition to specifying the version of Porter, you can also specify the plugins you need to install before bundle executions. For example, if you want to use the Kubernetes and Azure plugins, you can configure the AgentConfig like this:
```yaml
apiVersion: getporter.org/v1
kind: AgentConfig
metadata:
  name: customAgent
spec:
  porterRepository: <your-own-repository>
  porterVersion: v1.2.3
  pluginConfigFile:
    schemaVersion: 1.0.0
    plugins:
        kubernetes:
            version: v1.0.1
        azure:
            version: v1.0.1

```

The schema for the pluginConfigFile field is defined [in the Porter reference documentation](/reference/file-formats/#plugins).

🚨 WARNING: By default, the plugin version is set to `latest`. However, it is recommended to specify the plugin version to avoid any unexpected behavior that could result from an outdated plugin.


