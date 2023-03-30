#!/usr/bin/env bash
set -euo pipefail

OPNAMESPACE="porter-operator-system"

setCustomNamespaceForOperator() {
  if [ -z "$1" ]; then
    echo "No namespace specified, using default $OPNAMESPACE"
  else
    OPNAMESPACE=$1
    echo "Using custom namespace $OPNAMESPACE"
  fi

  # Replace the namespace in the operator.yaml
  echo "Setting namespace to $OPNAMESPACE"
  cd manifests
  kustomize edit set namespace $OPNAMESPACE
  kustomize build -o operator.yaml
}

setControllerImage() {
  # Replace the manager image with the image packaged with the bundle
  echo "Setting manager image to $1"
  cd manifests
  kustomize edit set image manager=$1
  kustomize build -o operator.yaml
}

configureNamespace() {
  cd manifests/namespace

  spec="/cnab/app/porter-config-spec.yaml"
  if [ -s $spec ]; then
    echo "Applying porter configuration..."
  else
    echo "Using the default porter configuration"
    cp defaults/porter-config-spec.yaml $spec
  fi
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

waitForDeployment() {
  if [ ! -z "$1" ]; then
    OPNAMESPACE=$1
  fi

  set +e # allow this next command to fail
  kubectl rollout status deploy/porter-operator-controller-manager --namespace $OPNAMESPACE --timeout 30s
  if [[ $? != 0 ]]; then
    echo "Deployment failed, retrieving logs to troubleshoot"
    kubectl logs deploy/porter-operator-controller-manager --namespace $OPNAMESPACE -c manager
  fi
}

removeData() {
  filter="getporter.org/generator=porter-operator-bundle"
  # This should get anything made by the bundle
  kubectl delete namespace -l $filter --wait
  # Look for any stray data that wasn't in a porter managed namespace, or were missing labels
  kubectl delete jobs,pods,secrets,pvc,pv --all-namespaces $filter --wait
  kubectl delete installations.getporter.org,agentconfigs.getporter.org,porterconfigs.getporter.org --all-namespaces --wait
}

# Call the requested function and pass the arguments as-is
"$@"
