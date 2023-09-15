# vpa_butler
![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/sapcc/vpa_butler/test.yaml?branch=main)
[![Coverage Status](https://coveralls.io/repos/github/sapcc/vpa_butler/badge.svg)](https://coveralls.io/github/sapcc/vpa_butler)

The vpa_butler is a Kubernetes controller that serves instances of the `VerticalPodAutoscaler` CRD for deployments, statefulsets and daemonsets, so you don't have to create them.

## Concept
The butler regularly reconciles all deployments, statefulsets and daemonsets in a cluster.
If it comes across one that is not targeted by a `VerticalPodAutoscaler` resource, it will create a VPA resource to get predictions for CPU and memory.
To opt-out of the vpa_butler, deploy a hand-crafted VPA instance along with the payload.
The vpa_butler will clean-up served VPA instances.

The served VPA is constructed in the following way:

- The VPA is created in the same namespace as the targeted resource and named like the targeted resource adding the suffix `-deployment`, `-statefulset`, `-daemonset`.
- The update mode is set to the value of the `--default-vpa-update-mode` CLI flag.
- The `minAllowed` recommendation is set to the values of the `--default-min-allowed-cpu` and `--default-min-allowed-memory` CLI flags.
- The `maxAllowed` recommendation is set to the percentage of capacity specified by the `--capacity-percent` CLI flag of the largest viable node regarding memory. The vpa_butler determines the viable nodes by considering, where pods of the payload could be scheduled repsecting `NodeName`, `NodeAffinity`, `NodeUnscheduable` and `TaintToleration`. These values are updated every 30 seconds.
