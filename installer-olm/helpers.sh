#!/usr/bin/env bash
set -euo pipefail

VERISON_FILE="/cnab/app/version.txt"
DOWNLOAD_URL="https://github.com/operator-framework/operator-lifecycle-manager/releases/download"

install() {
  setVersion
  downloadManifests
  applyManifests
}

upgrade() {
  setVersion
  downloadManifests
  applyManifests
}

uninstall() {
  setVersion
  downloadManifests
  deleteManifests
}

applyManifests() {
  echo "Running manifests for OLM ${OLM_VERSION}"

  echo "Apply CRDs..."
  kubectl apply -f crds.yaml
  echo "Waiting for CRDs to register..."
  kubectl wait --for condition="established" -f crds.yaml

  echo "Deploying OLM..."
  kubectl apply -f olm.yaml

  echo "Waiting for the OLM deployment to complete..."
  kubectl rollout status deployment/olm-operator --namespace olm
}

deleteManifests() {
  echo "Removing manifests for OLM ${OLM_VERSION}"
  kubectl delete -f olm.yaml --ignore-not-found=true --wait
  kubectl delete -f crds.yaml --ignore-not-found=true --wait
}

downloadManifests() {
  echo "Downloading OLM manifests"
  download ${DOWNLOAD_URL}/${OLM_VERSION}/crds.yaml
  download ${DOWNLOAD_URL}/${OLM_VERSION}/olm.yaml
}

download() {
  MANIFEST_URL=$1
  echo ${MANIFEST_URL}
  curl -sfLO ${MANIFEST_URL}
  if [[ "${PORTER_DEBUG}" == "true" ]]; then
    MANIFEST_FILE=$(basename -- "${MANIFEST_URL}")
    cat ${MANIFEST_FILE}
  fi
}

setVersion() {
  OLM_VERSION=`cat ${VERISON_FILE}`

  if [[ -z ${OLM_VERSION} || ${OLM_VERSION} == "latest" ]]; then
    echo "Determining the latest version of OLM..."
    OLM_VERSION=`curl -sf https://api.github.com/repos/operator-framework/operator-lifecycle-manager/releases/latest | jq -r .tag_name`
    echo "The latest OLM version is ${OLM_VERSION}"
    echo ${OLM_VERSION} > ${VERISON_FILE}
  fi
}

printContext() {
  echo "Kubernetes Version:"
  kubectl version
  NAME=`kubectl config current-context`
  echo "Using kubeconfig context ${NAME}"
  kubectl config get-contexts ${NAME}
}

# Call the requested function and pass the arguments as-is
printContext
"$@"
