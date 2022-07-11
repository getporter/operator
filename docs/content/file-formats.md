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
* [CredentialSet](#credentialset)
* [ParameterSet](#parameterset)
* [AgentAction](#agentaction)
* [AgentConfig](#agentconfig)
* [PorterConfig](#porterconfig)

## Installation

See the glossary for more information about the [Installation] resource.

The Installation spec is the same schema as the Installation resource in Porter.
You can copy/paste the output of the `porter installation show NAME -o yaml` command into the Installation resource spec (removing the status section).

In addition to the normal fields available on a [Porter Installation document](/reference/file-formats/#installation), the following fields are supported:

| Field        | Required | Default                             | Description                                                 |
|--------------|----------|-------------------------------------|-------------------------------------------------------------|
| agentConfig  | false    | See [Agent Config](#agentconfig)   | Reference to an AgentConfig resource in the same namespace. |
| porterConfig | false    | See [Porter Config](#porterconfig) | Reference to a PorterConfig resource in the same namespace. |

[Installation]: /operator/glossary/#installation

## CredentialSet

See the glossary for more information about the [CredentialSet] resource.

The CredentialSet spec is the same schema as the CredentialSet resource in Porter.
You can copy/paste the output of the `porter credentials show NAME -o yaml` command into the CredentialSet resource spec (removing the status section).

In addition to the normal fields available on a [Porter Credential Set document](/reference/file-formats/#credential-set), the following fields are supported:

```yaml
apiVersion: porter.sh/v1
kind: CredentialSet
metadata:
  name: credentialset-sample
spec:
  schemaVersion: 1.0.1
  namespace: operator
  name: porter-test-me
  credentials:
    - name: test-credential
      source:
        secret: test-secret
```

| Field                     | Required | Default                            | Description                                                 |
|---------------------------|----------|------------------------------------|-------------------------------------------------------------|
| agentConfig               | false    | See [Agent Config](#agentconfig)   | Reference to an AgentConfig resource in the same namespace. |
| porterConfig              | false    | See [Porter Config](#porterconfig) | Reference to a PorterConfig resource in the same namespace. |
| credentials               | true     |                                    | List of credential sources for the set |
| credentials.name          | true     |                                    | The name of the credential for the bundle |
| credentials.source        | true     |                                    | The credential type. Currently `secret` is the only supported source |
| credentials.source.secret | true     |                                    | The name of the secret |

[CredentialSet]: /operator/glossary/#credentialset

## ParameterSet

See the glossary for more information about the [ParameterSet] resource.

The ParameterSet spec is the same schema as the ParameterSet resource in Porter.
You can copy/paste the output of the `porter parameters show NAME -o yaml` command into the ParameterSet resource spec (removing the status section).

In addition to the normal fields available on a [Porter Parameter Set document](/reference/file-formats/#parameter-set), the following fields are supported:


```yaml
apiVersion: porter.sh/v1
kind: ParameterSet
metadata:
  name: parameterset-sample
spec:
  schemaVersion: 1.0.1
  namespace: operator
  name: porter-test-me
  parameters:
    - name: test-secret
      source:
        value: test-value
    - name: test-secret
      source:
        secret: test-secret
```

| Field                     | Required | Default                            | Description                                                 |
|---------------------------|----------|------------------------------------|-------------------------------------------------------------|
| agentConfig               | false    | See [Agent Config](#agentconfig)   | Reference to an AgentConfig resource in the same namespace. |
| porterConfig              | false    | See [Porter Config](#porterconfig) | Reference to a PorterConfig resource in the same namespace. |
| parameters                | true     |                                    | List of parameter sources for the set |
| parameters.name           | true     |                                    | The name of the parameter for the bundle |
| parameters.source         | true     |                                    | The parameters type. Currently `vaule` and `secret` are the only supported sources |
| **oneof** `parameters.source.secret` `parameters.source.value`   | true     |                                    | The plaintext value to use or the name of the secret that holds the parameter |

[ParameterSet]: /operator/glossary/#parameterset

## AgentAction

See the glossary for more information about the [AgentAction] resource.

```yaml
apiVersion: porter.sh/v1
kind: AgentAction
metadata:
  name: agentaction-sample
spec:
  args: ["installation", "apply", "installation.yaml"]
  files:
    # base64 encoded file contents
    installation.yaml: c2NoZW1hVmVyc2lvbjogMS4wLjAKbmFtZXNwYWNlOiBvcGVyYXRvcgpuYW1lOiBoZWxsbwpidW5kbGU6CiAgcmVwb3NpdG9yeTogZ2hjci5pby9nZXRwb3J0ZXIvdGVzdC9wb3J0ZXItaGVsbG8KICB2ZXJzaW9uOiAwLjIuMApwYXJhbWV0ZXJzOgogIG5hbWU6IGxsYW1hcyAK

```

| Field        | Required | Default                                | Description                                                                                                                           |
|--------------|----------|----------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------|
| agentConfig  | false    | See [Agent Config](#agentconfig)       | Reference to an AgentConfig resource in the same namespace.                                                                           |
| porterConfig | false    | See [Porter Config](#porterconfig)     | Reference to a PorterConfig resource in the same namespace.                                                                           |
| command      | false    | /app/.porter/agent                     | Overrides the entrypoint of the Porter Agent image.                                                                                   |
| args         | true     | None.                                  | Arguments to pass to the porter command. Do not include "porter" in the arguments. For example, use ["help"], not ["porter", "help"]. |
| files        | false    | None.                                  | Files that should be present in the working directory where the command is run.                                                       |
| env          | false    | Settings for the kubernetes driver.    | Additional environment variables that should be set.                                                                                  | 
| envFrom      | false    | None.                                  | Load environment variables from a ConfigMap or Secret.                                                                                |
| volumeMounts | false    | Porter's config and working directory. | Additional volumes that should be mounted into the Porter Agent.                                                                      |
| volumes      | false    | Porter's config and working directory. | Additional volumes that should be mounted into the Porter Agent.                                                                      |                

[AgentAction]: /operator/glossary/#agentaction

## AgentConfig

See the glossary for more information about the [AgentConfig] resource.

```yaml
apiVersion: porter.sh/v1
kind: AgentConfig
metadata:
  name: customAgent
spec:
  porterRepository: ghcr.io/getporter/porter-agent
  porterVersion: v1.0.0-beta.1
  serviceAccount: porter-agent
  volumeSize: 64Mi
  pullPolicy: Always
  installationServiceAccount: installation-agent
```

| Field        | Required    | Default | Description |
| -----------  | ----------- | ------- | ----------- |
| porterRepository  | false  | ghcr.io/getporter/porter-agent | The repository for the Porter Agent image. |
| porterVersion | false      | varies  | The tag for the Porter Agent image. For example, vX.Y.Z, latest, or canary. Defaults to the most recent version of porter that has been tested with the operator.  |
| serviceAccount | true | (none) | The service account to run the Porter Agent under. Must exist in the same namespace as the installation. |
| installationServiceAccount | false | (none) | The service account to run the Kubernetes pod/job for the installation image. |
| volumeSize | false | 64Mi | The size of the persistent volume that Porter will request when running the Porter Agent. It is used to share data between the Porter Agent and the bundle invocation image. It must be large enough to store any files used by the bundle including credentials, parameters and outputs. |
| pullPolicy | false | PullAlways when the tag is canary or latest, otherwise PullIfNotPresent. | Specifies when to pull the Porter Agent image |

[AgentConfig]: /operator/glossary/#agentconfig

### Service Account

The only required configuration is the name of the service account under which Porter should run.
The configureNamespace action of the porter operator bundle creates a service account named "porter-agent" for you with the porter-operator-agent-role role binding.

## PorterConfig

See the glossary for more information about the [PorterConfig] resource.

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

[PorterConfig]: /operator/glossary/#porterconfig

[Porter Feature Flags]: /configuration/#experimental-feature-flags
