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
	AnnotationManagedBy        = "managedBy"
	AnnotationVPAButler        = "vpa_butler"
	AnnotationVPAButlerVersion = "cloud.sap/vpa-butler-version"
)

var (
	VPAUpdateMode       = vpav1.UpdateModeOff
	VPAControlledValues = vpav1.ContainerControlledValuesRequestsOnly
)

func IsHandleVPA(vpa *vpav1.VerticalPodAutoscaler) bool {
	if vpa.Annotations == nil {
		return false
	}
	v, ok := vpa.Annotations[AnnotationManagedBy]
	return ok && v == AnnotationVPAButler
}

func mutateVPA(scheme *runtime.Scheme, vpaOwner client.Object, vpa *vpav1.VerticalPodAutoscaler) error {
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
	vpaSpec.ResourcePolicy = &vpav1.PodResourcePolicy{
		ContainerPolicies: []vpav1.ContainerResourcePolicy{
			{
				ContainerName:       "*",
				ControlledResources: &resourceList,
				ControlledValues:    &VPAControlledValues,
			},
		},
	}
	if vpa.Annotations == nil {
		vpa.Annotations = make(map[string]string, 0)
	}
	vpa.Annotations[AnnotationManagedBy] = AnnotationVPAButler

	return controllerutil.SetOwnerReference(vpaOwner, vpa, scheme)
}
