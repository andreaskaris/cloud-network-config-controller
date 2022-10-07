#!/bin/bash

set -eo pipefail

DIR=$(dirname "${BASH_SOURCE[0]}")

CNCC_REPOSITORY=${CNCC_REPOSITORY:-""}
if [ -z "${CNCC_REPOSITORY}" ]; then
	echo "Please set environment variable CNCC_REPOSITORY and point it to your container repository"
	exit 1
fi

CONTAINER_ENGINE="${CONTAINER_ENGINE:-podman}"
PODMAN_BIN="podman"
PODMAN_BUILD_CMD="podman build"
PODMAN_CMD="podman"
if [ "$CONTAINER_ENGINE" == "docker" ]; then
	PODMAN_BIN="docker"
	PODMAN_BUILD_CMD="docker build"
	PODMAN_CMD="docker"
fi

for command in mktemp awk oc uuidgen $PODMAN_BIN; do
	if ! command -v $command &> /dev/null
	then
		echo "$command not found"
		exit 1
	fi
done

UUID=$(uuidgen)
export CONTROLLER_IMAGE="${CNCC_REPOSITORY}:${UUID}"

pushd $DIR/../
	$PODMAN_BUILD_CMD -t ${CONTROLLER_IMAGE} .
	$PODMAN_CMD push ${CONTROLLER_IMAGE}
popd

for cmd in go jq oc; do
   if ! command -v "$cmd" &> /dev/null; then
      echo "$cmd is not available"
      exit 1
   fi
done

# This script expects you to have KUBECONFIG exported in your env

HERE=$(dirname "$(readlink --canonicalize "$BASH_SOURCE")")
ROOT=$(readlink --canonicalize "$HERE/..")
SECRET_LOCATION=$ROOT/tmp-secret-location/
mkdir -p $SECRET_LOCATION
CONFIG_LOCATION=$ROOT/tmp-config-location/
mkdir -p $CONFIG_LOCATION

platformtype=$(oc get infrastructures.config.openshift.io cluster  -o jsonpath='{.status.platform}')
export CONTROLLER_NAMESPACE="${CONTROLLER_NAMESPACE:-openshift-cloud-network-config-controller}"
export CONTROLLER_NAME="${CONTROLLER_NAME:-tmp-local-controller}"

# This won't work on platforms != AWS, but we don't care. 
# The command won't fail and `cloudregion` is only used on AWS
platformregion=$(oc get infrastructures.config.openshift.io cluster  -o jsonpath='{.status.platformStatus.aws.region}')

json=$(oc get secret cloud-credentials -n ${CONTROLLER_NAMESPACE} -o jsonpath='{.data}')
for key in $(echo $json | jq -r 'keys[]'); do
    value=$(echo $json | jq -r ".[\"$key\"]" | base64 -d)
    echo -n "$value">$SECRET_LOCATION/$key
done
json=$(oc get configmap kube-cloud-config -n ${CONTROLLER_NAMESPACE} -o jsonpath='{.data}' || echo "{}")
for key in $(echo $json | jq -r 'keys[]'); do
    value=$(echo $json | jq -r ".[\"$key\"]")
    echo -n "$value">$CONFIG_LOCATION/$key
done

oc scale deployment network-operator -n openshift-network-operator --replicas 0
oc scale deployment cloud-network-config-controller -n openshift-cloud-network-config-controller --replicas 0 || true

go run $ROOT/cmd/cloud-network-config-controller/main.go \
	-kubeconfig $KUBECONFIG \
	-platform-type $platformtype \
	-secret-name "cloud-credentials" \
	-secret-override "$SECRET_LOCATION" \
	-config-name "kube-cloud-config" \
	-config-override "$CONFIG_LOCATION" \
	-platform-region "$platformregion"
