---
title: QuickStart
description: Try out the Porter Operator!
---

In this QuickStart you will learn how to install and use the Porter Operator on a non-production cluster.

# Prerequisites

* [Install the latest Porter v1 prerelease][install-porter]
* Docker, either a local installation or a remote Docker Host.
* A Kubernetes cluster. [KinD] or Minikube works well.
* kubectl, with its kubeconfig configured to use the cluster.

# Install the Operator

The Porter Operator is installed using Porter, and requires an existing Kubernetes cluster.
First, generate a credential set that points to the location of your kubeconfig file, for example using the path $HOME/.kube/config.

```
porter credentials generate porterops -r ghcr.io/getporter/porter-operator:canary
```

Now that Porter knows which cluster to target, install the Operator with the following command:

```
porter install porterops -c porterops -r ghcr.io/getporter/porter-operator:canary
```

Before you use the operator, you need to configure a Kubernetes namespace with the necessary configuration.
The bundle includes a custom action that prepares a namespace for you:

```
porter invoke porterops --action configureNamespace --param namespace=quickstart -c porterops
```

The Porter Operator is now installed on your cluster in the porter-operator-system namespace, along with a Mongodb server.
This database is not secured with a username/password, so do not use this default installation configuration with production secrets!
The cluster has a namespace, quickstart, where we will create resources and Porter will create jobs to run Porter.

# Point porter at the operator's datastore

Let's update your local porter CLI to read the data from the operator's datastore.
This isn't necessary for the operator to work, but will allow us to see what's happening and understand how the operator works.

Run the following command to expose the operator's mongodb server to your localhost:
```
kubectl port-forward --namespace porter-operator-system svc/mongodb 27020:27017 >/dev/null &
```

Update your porter configuration file at ~/.porter/config.toml to use the in-cluster mongodb server.
Create the file if it does not exist.

```toml
namespace = "quickstart"
default-storage = "in-cluster-mongodb"

[[storage]]
  name = "in-cluster-mongodb"
  plugin = "mongodb"

  [storage.config]
    url = "mongodb://localhost:27020"
```

Run `porter list` to verify that your configuration is working.
The output should print an empty list of installations.

```
$ porter list
NAMESPACE   NAME   CREATED   MODIFIED   LAST ACTION   LAST STATUS
```

# Install a Bundle

For this QuickStart, we will use the [getporter/hello-llama] bundle. It does not allocate any resources or require credentials, and is a demo bundle that prints the specified name to the console.

The Operator uses the concept of **desired state**.
It watches for installation.porter.sh resources (which will be referred to as just installation in this Quickstart) on the cluster, and compares the desired state of the installation from that resource with Porter's records. If the installation does not already exist, the bundle is installed. If it already exists, and the desired state doesn't match Porter's records, the installation is upgraded. When the installation resource is deleted, the installation is uninstalled.

Let's create an installation resource that specifies that we want to have the getporter/hello-llama bundle installed.

1. Create a file named [llama.yaml](llama.yaml) with the following contents:
    
    ```yaml
    apiVersion: porter.sh/v1
    kind: Installation
    metadata:
      name: hello-llama
      namespace: quickstart
    spec:
      schemaVersion: 1.0.0
      namespace: operator
      name: mellama
      bundle:
        repository: getporter/hello-llama
        version: 0.1.1
      parameters:
        name: quickstart
    ```
1. Apply the installation resource to the cluster
   ```
   kubectl apply -f llama.yaml
   
   ```
1. The operator detects the installation and runs the Porter Agent (a job that runs the porter CLI). The agent will run the appropriate porter install command to install the bundle. The bundle runs in a separate job (known as the installer). You can watch the progress of these events with kubectl with `kubectl get pods -n quickstart --watch`.
   ```console
   $ kubectl get pods -n quickstart --watch
   NAME                          READY   STATUS              RESTARTS   AGE
   hello-llama-7245-hhcq4        1/1     Running             0          7s
   install-mellama-g769d-nzqsn   0/1     ContainerCreating   0          0s
   install-mellama-g769d-nzqsn   0/1     Completed           0          5s
   hello-llama-7245-hhcq4        0/1     Completed           0          13s
   ```
   
   You can tell that porter is done when the hello-llama-* pod is completed.
1. Use `porter list` to see the status of the installation:
   ```console
   $ porter list
   NAMESPACE    NAME    CREATED          MODIFIED         LAST ACTION   LAST STATUS
   quickstart   llama   34 seconds ago   34 seconds ago   install       succeeded
   ```
1. You can see the logs from the installation with `porter logs --installation llama`.
   ```console
   $ porter logs --installation llama
   Could not stream logs for pod install-llama-bc4sl-7dw6f. Retrying...: container "invocation" in pod "install-llama-bc4sl-7dw6f" is waiting to start: ContainerCreating
   Could not stream logs for pod install-llama-bc4sl-7dw6f. Retrying...: container "invocation" in pod "install-llama-bc4sl-7dw6f" is waiting to start: ContainerCreating
   executing install action from hello-llama (installation: llama)
   Hello, quickstart
   execution completed successfully!
   ```

# Upgrade an installation

Now that our bundle is installed, let's make some changes to trigger an upgrade.

1. Edit llama.yaml and change the name parameter to a different value:
    ```yaml
    apiVersion: porter.sh/v1
    kind: Installation
    metadata:
      name: hello-llama
      namespace: quickstart
    spec:
      schemaVersion: 1.0.0
      namespace: operator
      name: mellama
      bundle:
        repository: getporter/hello-llama
        version: 0.1.1
      parameters:
        name: Grogu
    ```
1. Apply the updated installation resource:
   ```
   kubectl apply -f llama.yaml
   ```
1. The operator will detect that the parameter has changed, and run porter upgrade.
   Again, use `kubectl get pods -n quickstart --watch` to wait for Porter Agent to finish executing the upgrade-llama-* job.
   ```console
   $ kubectl get pods -n quickstart --watch
   NAME                          READY   STATUS      RESTARTS   AGE
   hello-llama-7245-hhcq4        0/1     Completed   0          13m
   hello-llama-9550-sms74        0/1     Completed   0          8s
   install-llama-g769d-nzqsn     0/1     Completed   0          13m
   upgrade-llama-wl8xp-knng5     0/1     Completed   0          3s
   ```
1. Let's run `porter show llama` to see more details about the installation.
   Note that the installation is using the updated value "Grogu" for the name parameter.
   ```console
   $ porter show llama
   Name: llama
   Namespace: quickstart
   Created: 12 minutes ago
   Modified: 6 seconds ago
   
   Parameters:
   -----------------------
   Name  Type    Value
   -----------------------
   name  string  Grogu
   
   Status:
     Reference: getporter/hello-llama:v0.1.1
     Version: 0.1.1
     Last Action: upgrade
     Status: succeeded
     Digest: sha256:22cdfad0756c9ce1a8f4694b0411440dfab99fa2e07125ff78efe555dd63d73e
   ```

# Uninstall a bundle

This isn't supported yet. Once it's implemented, uninstall is triggered when the CRD is deleted.

# Retry the last operation

If your bundle operation failed, you can run it again by changing an annotation on the installation CRD:

```yaml
apiVersion: porter.sh/v1
kind: Installation
metadata:
  name: porter-hello
  annotations:
    retry: "1"
```

Each time you need to repeat the operation, change the annotation value again.
There is nothing special about the key used for the annotation.
I chose retry, however you could use "favorite-color: blue", changing the value each time, and it would still trigger Porter to retry it.

# Next Steps

You now know how to install and configure the Porter Operator. The project is still incomplete, so watch this repository for updates!

* [Porter Operator Custom Resources](/docs/content/resources.md)

[install-porter]: https://github.com/getporter/porter/releases/tag/v1.0.0-alpha.4
[KinD]: https://kind.sigs.k8s.io/docs/user/quick-start/#installation
[Minikube]: https://minikube.sigs.k8s.io/docs/start/
[getporter/hello-llama]: https://hub.docker.com/r/getporter/hello-llama
