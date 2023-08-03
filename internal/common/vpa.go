package common

import (
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

const (
	AnnotationManagedBy = "managedBy"
	AnnotationVpaButler = "vpa_butler"
)

var (
	VpaUpdateMode       = vpav1.UpdateModeOff
	VpaControlledValues = vpav1.ContainerControlledValuesRequestsOnly
)

func ManagedByButler(vpa *vpav1.VerticalPodAutoscaler) bool {
	if vpa.Annotations == nil {
		return false
	}
	v, ok := vpa.Annotations[AnnotationManagedBy]
	return ok && v == AnnotationVpaButler
}
