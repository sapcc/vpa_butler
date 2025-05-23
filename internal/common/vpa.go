// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package common

import (
	autoscaling "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
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
	SupportedUpdatedModes = []string{
		string(vpav1.UpdateModeOff),
		string(vpav1.UpdateModeInitial),
		string(vpav1.UpdateModeRecreate),
		string(vpav1.UpdateModeAuto),
	}
	SupportedControlledValues = []string{
		string(vpav1.ContainerControlledValuesRequestsOnly),
		string(vpav1.ContainerControlledValuesRequestsAndLimits),
	}
)

type NamedResourceList struct {
	ContainerName string
	Resources     corev1.ResourceList
}

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
		APIVersion: owner.GetObjectKind().GroupVersionKind().GroupVersion().String(),
	}
	vpa.Spec.UpdatePolicy = &vpav1.PodUpdatePolicy{
		UpdateMode: &updateMode,
	}
	if vpa.Annotations == nil {
		vpa.Annotations = make(map[string]string, 0)
	}
	vpa.Annotations[AnnotationManagedBy] = AnnotationVpaButler
}
