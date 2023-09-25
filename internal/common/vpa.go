package common

import (
	autoscaling "k8s.io/api/autoscaling/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	AnnotationManagedBy = "managedBy"
	AnnotationVpaButler = "vpa_butler"
)

var (
	VpaUpdateMode         = vpav1.UpdateModeOff
	VpaControlledValues   = vpav1.ContainerControlledValuesRequestsOnly
	SupportedUpdatedModes = []string{"Off", "Initial", "Recreate"}
)

func ManagedByButler(vpa *vpav1.VerticalPodAutoscaler) bool {
	if vpa.Annotations == nil {
		return false
	}
	v, ok := vpa.Annotations[AnnotationManagedBy]
	return ok && v == AnnotationVpaButler
}

func ConfigureVpaBaseline(vpa *vpav1.VerticalPodAutoscaler, owner client.Object, updateMode vpav1.UpdateMode) {
	vpa.Spec.TargetRef = &autoscaling.CrossVersionObjectReference{
		Kind:       owner.GetObjectKind().GroupVersionKind().Kind,
		Name:       owner.GetName(),
		APIVersion: owner.GetObjectKind().GroupVersionKind().Version,
	}
	vpa.Spec.UpdatePolicy = &vpav1.PodUpdatePolicy{
		UpdateMode: &updateMode,
	}
	if vpa.Annotations == nil {
		vpa.Annotations = make(map[string]string, 0)
	}
	vpa.Annotations[AnnotationManagedBy] = AnnotationVpaButler
}
