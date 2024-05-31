# vpa_butler
![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/sapcc/vpa_butler/ci.yaml?branch=main)
[![Coverage Status](https://coveralls.io/repos/github/sapcc/vpa_butler/badge.svg)](https://coveralls.io/github/sapcc/vpa_butler)

A Kubernetes controller designed to simplify the process of deploying and managing [VerticalPodAutoscalers](https://github.com/kubernetes/autoscaler/tree/master/vertical-pod-autoscaler) (VPAs) for deployments, statefulsets, and daemonsets.
This controller automatically creates instances of the `VerticalPodAutoscaler` CRD as payload is created in your cluster, saving developers time and effort.

## Motivation
Deploying VPA instances is a necessary step to enable automatic scaling for workloads in Kubernetes.
However, the process of manually deploying VPA instances can be tedious and time-consuming.
vpa_butler automates this process by creating `VerticalPodAutoscalers` with reasonable defaults while respecting custom VPA instances.

## Functionality

vpa_butler is a Kubernetes controller that continuously watches all deployments, statefulsets, and daemonsets within your cluster.
When it encounters a resource not currently being targeted by a VPA instance, it creates a new VPA resource with appropriate defaults.
 
The served VPA is constructed in the following way:
- The VPA is created in the same namespace as the targeted resource and named like the targeted resource adding the suffix `-deployment`, `-statefulset`, `-daemonset`.
- The update mode is set to the value of the `--default-vpa-update-mode` CLI flag.
- The `minAllowed` recommendation is set to the values of the `--default-min-allowed-cpu` and `--default-min-allowed-memory` CLI flags.
- The `maxAllowed` recommendation is set to the percentage of capacity specified by the `--capacity-percent` CLI flag of the largest viable node regarding memory. The vpa_butler determines the viable nodes by considering, where pods of the payload could be scheduled on respecting `NodeName`, `NodeAffinity`, `NodeUnscheduable` and `TaintToleration`.

Every 30 seconds the `maxAllowed` values are updated.
This feature ensures that pods stay schedulable.
To opt-out of the vpa_butler, deploy a custom VPA instance along with the payload.
The vpa_butler will clean-up the VPA instances it served.

The served VPA can be adjusted using the following annotations on the payload resource (do **not** annotate the pod template):
- `vpa-butler.cloud.sap/main-container` can be set to the name of the most resource-hungry container of eventually created pods. That container will have the `maxAllowed` recommendations increased.
- `vpa-butler.cloud.sap/update-mode` sets the update mode on the served VPA.
- `vpa-butler.cloud.sap/controlled-values` sets the controlled values on the served VPA.
