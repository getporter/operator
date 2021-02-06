# Operator Lifecycle Manager (OLM) Bundle

This bundle installs [Operator Lifecycle Manager][olm]. It downloads the appropriate
manifests at runtime, so the same bundle can install any version of OLM.


## Generate credentials

Before you can run the bundle, you need to generate credentials that point to
the Kubernetes cluster where OLM should be installed.

```
porter credentials generate mycluster --reference ghcr.io/getporter/olm:v0.1.0
```

## Install the latest version of OLM
```
porter install olm --reference ghcr.io/getporter/olm:v0.1.0 --cred mycluster
```

## Install a specific version of OLM
```
porter install olm --reference ghcr.io/getporter/olm:v0.1.0 --cred mycluster --param version=v0.16.0
```

[olm]: https://github.com/operator-framework/operator-lifecycle-manager
