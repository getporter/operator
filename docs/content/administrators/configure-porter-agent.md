---
title: Configure the Porter Agent
description: Customize how Porter runs on Kubernetes
---

The [Porter Agent] is a containerized version of the Porter CLI that is optimized for running Porter commands on a Kubernetes cluster. With the AgentConfig Custom Resource Definition (CRD), you can customize how the Porter Agent is run to meet your specific needs and requirements. For example, you can specify the version of Porter to use, install additional Porter plugins, or provide a custom Porter config file.

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

## Configuring Persistent Volume

The AgentConfig can modify the VolumeSize that it creates as well as what StorageClassName it uses to create the volume.

The VolumeSize can be specified using [CSIStorageCapacity Notation] and will result in the Persistent Volumes created by the operator having the requested capacity. When VolumeSize is not specified then a default of `64Mi` is used.

The StorageClassName must resolve to the name of a StorageClass that is defined on the cluster. That StorageClass must have the following capabilities:

- Supported AccessModes: ReadWriteOnce, ReadOnlyMany
- Allow for running `chmod`

When StorageClassName is not specified on the AgentConfig then the clusters default StorageClass is used. A custom StorageClass can be created by the cluster administrator and used by the operator if none of the clusters built-in StorageClasses fulfill the above requirements.

You can configure the VolumeSize and StorageClassName in the AgentConfig like this:

```yaml
apiVersion: getporter.org/v1
kind: AgentConfig
metadata:
  name: customAgent
spec:
  storageClassName: customStorageClassName
  volumeSize: 128Mi
```

[csistoragecapacity notation]: https://kubernetes.io/docs/reference/kubernetes-api/config-and-storage-resources/csi-storage-capacity-v1/

### StorageClassName Cluster Compatability Matrix

This matrix will be updated as more clusters and CSI drivers are determined to be compatible

| Cluster Type | Built In Compatible Driver | Cluster Version |
| ------------ | -------------------------- | --------------- |
| AKS          | azureblob-nfs-premium      | v1.25.4         |
| KinD         | default                    | v1.23.4         |

[Porter Agent]: /operator/glossary/#porter-agent
