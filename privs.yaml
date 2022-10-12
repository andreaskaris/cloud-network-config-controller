#!/bin/bash

oc label ns openshift-cloud-network-config-controller security.openshift.io/scc.podSecurityLabelSync="false" --overwrite=true
oc label ns openshift-cloud-network-config-controller pod-security.kubernetes.io/enforce=privileged --overwrite=true
oc label ns openshift-cloud-network-config-controller pod-security.kubernetes.io/warn=privileged --overwrite=true
oc label ns openshift-cloud-network-config-controller pod-security.kubernetes.io/audit=privileged --overwrite=true

oc project openshift-cloud-network-config-controller
oc adm policy add-scc-to-user privileged -z cloud-network-config-controller
