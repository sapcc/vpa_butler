package common

import (
	autoscaling "k8s.io/api/autoscaling/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	annotationManagedBy = "managedBy"
	annotationVPAButler = "vpa_butler"
)

var (
	VPAUpdateMode       = vpav1.UpdateModeOff
	VPAControlledValues = vpav1.ContainerControlledValuesRequestsOnly
)

func isHandleVPA(vpa *vpav1.VerticalPodAutoscaler) bool {
	if vpa.Annotations == nil {
		return false
	}
	v, ok := vpa.Annotations[annotationManagedBy]
	return ok && v == annotationVPAButler
}

func mutateVPA(scheme *runtime.Scheme, vpaOwner client.Object, vpa *vpav1.VerticalPodAutoscaler, containers []v1.Container) error {
	vpaSpec := &vpa.Spec
	vpaSpec.TargetRef = &autoscaling.CrossVersionObjectReference{
		Kind:       vpaOwner.GetObjectKind().GroupVersionKind().Kind,
		Name:       vpaOwner.GetName(),
		APIVersion: vpaOwner.GetObjectKind().GroupVersionKind().Version,
	}
	vpaSpec.UpdatePolicy = &vpav1.PodUpdatePolicy{
		UpdateMode: &VPAUpdateMode,
	}

	resourceList := []v1.ResourceName{v1.ResourceCPU, v1.ResourceMemory}
	var cp []vpav1.ContainerResourcePolicy
	for _, container := range containers {
		if len(container.Resources.Requests) != 0 {
			crp := vpav1.ContainerResourcePolicy{
				ContainerName: container.Name,
				ControlledResources: &resourceList,
				ControlledValues: &VPAControlledValues,
				MinAllowed: container.Resources.Requests,
			}
			cp = append(cp, crp)
		} else {
			crp := vpav1.ContainerResourcePolicy{
				ContainerName: container.Name,
				ControlledResources: &resourceList,
				ControlledValues: &VPAControlledValues,
			}
			cp = append(cp, crp)
		}
	}
	vpaSpec.ResourcePolicy = &vpav1.PodResourcePolicy{
		ContainerPolicies: cp,
	}
	if vpa.Annotations == nil {
		vpa.Annotations = make(map[string]string, 0)
	}
	vpa.Annotations[annotationManagedBy] = annotationVPAButler

	return controllerutil.SetOwnerReference(vpaOwner, vpa, scheme)
}
