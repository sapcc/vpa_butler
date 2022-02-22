package common

import (
	autoscaling "k8s.io/api/autoscaling/v1"
	v1 "k8s.io/api/core/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

func BuildVPA(name string, namespace string, kind string, apiVersion string, controller string) *vpav1.VerticalPodAutoscaler {
	vpaSpec := new(vpav1.VerticalPodAutoscalerSpec)
	crossRef := autoscaling.CrossVersionObjectReference{
		Kind:       kind,
		Name:       name,
		APIVersion: apiVersion,
	}
	updateMode := vpav1.UpdateModeOff
	vpaSpec.TargetRef = &crossRef
	vpaSpec.UpdatePolicy = &vpav1.PodUpdatePolicy{
		UpdateMode: &updateMode,
	}
	resourceList := []v1.ResourceName{"cpu", "memory"}
	containerPolicy := vpav1.ContainerResourcePolicy{
		ContainerName:       "*",
		ControlledResources: &resourceList,
	}
	vpaSpec.ResourcePolicy = &vpav1.PodResourcePolicy{ContainerPolicies: []vpav1.ContainerResourcePolicy{containerPolicy}}
	vpa := new(vpav1.VerticalPodAutoscaler)
	vpa.Namespace = namespace
	vpa.Name = name
	vpa.Spec = *vpaSpec
	vpa.Annotations = map[string]string{"managedBy": "vpa_butler", "vpa_controller": controller}
	return vpa
}
