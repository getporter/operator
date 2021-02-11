#!/usr/bin/env bash
set -euo pipefail

configure-namespace() {
  cd manifests/namespace

  echo "Configuring porter-config secret..."
  yq eval ".data.\"config.toml\" = \"`base64 config.toml`\"" -i porter-config-secret.yaml

  echo "Configuring porter-env secret..."
  yq eval ".stringData.AZURE_STORAGE_CONNECTION_STRING = \"$AZURE_STORAGE_CONNECTION_STRING\"" -i porter-env-secret.yaml
  yq eval ".stringData.AZURE_TENANT_ID = \"$AZURE_TENANT_ID\"" -i porter-env-secret.yaml
  yq eval ".stringData.AZURE_CLIENT_ID = \"$AZURE_CLIENT_ID\"" -i porter-env-secret.yaml
  yq eval ".stringData.AZURE_CLIENT_SECRET = \"$AZURE_CLIENT_SECRET\"" -i porter-env-secret.yaml

  # If settings were specified for the porter operator, create a configmap with them included
  cfgFiles=`ls porter-config`
  for cfg in $cfgFiles; do
    contents=`cat porter-config/$cfg`
    if [[ $contents != "" ]]; then
      yq eval ".data.$cfg = \"$contents\"" -i porter-configmap.yaml
    fi
  done

  echo "Configuring porter-agent role binding..."
  yq eval ".subjects[].namespace=\"$NAMESPACE\"" -i porter-agent-binding.yaml

  echo "Setting namespace to $NAMESPACE..."
  yq eval ".metadata.name = \"$NAMESPACE\"" -i namespace.yaml
  yq eval-all ".metadata.namespace = \"$NAMESPACE\"" *.yaml > manifests.yaml

  echo "Applying manifests to cluster..."
  kubectl apply -f manifests.yaml

  echo "Namespace $NAMESPACE is ready to use with the Porter Operator"
}

# Call the requested function and pass the arguments as-is
"$@"
