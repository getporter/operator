#!/usr/bin/env bash
set -xeuo pipefail

configure-namespace() {
  cd manifests/namespace

  spec="/cnab/app/porter-config-spec.yaml"
  if [ -s $spec ]; then
    echo "Applying porter-config..."
  else
    echo "Using the default porter-config"
    cp defaults/porter-config-spec.yaml $spec
  fi
  sed -i 's/debug-plugins/debugPlugins/g' $spec
  sed -i 's/default-storage-plugin/defaultStoragePlugin/g' $spec
  sed -i 's/default-storage/defaultStorage/g' $spec
  sed -i 's/default-secrets-plugin/defaultSecretsPlugin/g' $spec
  sed -i 's/default-secrets/defaultSecrets/g' $spec
  yq eval-all 'select(fileIndex==0).spec = select(fileIndex==1) | select(fileIndex==0)' -i porter-config.yaml $spec

  # If settings were specified for the porter operator, create a AgentConfig with them included
  cfgFiles=`ls agentconfig`
  for cfg in $cfgFiles; do
    contents=`cat agentconfig/$cfg`
    if [[ $contents != "" ]]; then
      echo "Applying agent-config $cfg"
      yq eval ".spec.$cfg = \"$contents\"" -i porter-agentconfig.yaml
    fi
  done

  echo "Configuring porter-agent role binding..."
  yq eval ".subjects[].namespace=\"$NAMESPACE\"" -i porter-agent-binding.yaml

  echo "Setting namespace to $NAMESPACE..."
  yq eval ".metadata.name = \"$NAMESPACE\"" -i namespace.yaml
  yq eval-all ".metadata.namespace = \"$NAMESPACE\"" *.yaml > manifests.yaml

  echo "Applying manifests to cluster..."
  cat manifests.yaml
  kubectl apply -f manifests.yaml

  echo "Namespace $NAMESPACE is ready to use with the Porter Operator"
}

remove-data() {
  filter="porter.sh/generator=porter-operator-bundle"
  # This should get anything made by the bundle
  kubectl delete namespace -l $filter --wait
  # Look for any stray data that wasn't in a porter managed namespace, or were missing labels
  kubectl delete jobs,pods,secrets,pvc,pv --all-namespaces $filter --wait
  kubectl delete installations.porter.sh,agentconfigs.porter.sh,porterconfigs.porter.sh --all-namespaces --wait
}

# Call the requested function and pass the arguments as-is
"$@"
