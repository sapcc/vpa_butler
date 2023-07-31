package common

import (
	"context"
	"fmt"

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

func ManagedByButler(vpa *vpav1.VerticalPodAutoscaler) bool {
	if vpa.Annotations == nil {
		return false
	}
	v, ok := vpa.Annotations[AnnotationManagedBy]
	return ok && v == AnnotationVPAButler
}

func ShouldHandleVPA(ctx context.Context, params VPAReconcileParams) (bool, error) {
	var vpas vpav1.VerticalPodAutoscalerList
	err := params.Client.List(ctx, &vpas, client.InNamespace(params.VpaOwner.GetNamespace()))
	if err != nil {
		return false, fmt.Errorf("failed to list vpas: %w", err)
	}
	for i := range vpas.Items {
		vpa := vpas.Items[i]
		if vpa.Spec.TargetRef == nil {
			continue
		}
		// vpa matches the vpa owner
		if vpa.Spec.TargetRef.Name == params.VpaOwner.GetName() &&
			vpa.Spec.TargetRef.Kind == params.VpaOwner.GetObjectKind().GroupVersionKind().Kind &&
			vpa.Spec.TargetRef.APIVersion == params.VpaOwner.GetObjectKind().GroupVersionKind().Version {
			managed := ManagedByButler(&vpa)
			if !managed {
				// there is a hand-crafted VPA targeting a resource the butler cares about
				// so the served VPA needs to be deleted
				return false, nil
			}
		}
	}
	// no vpa found or vpa managed by butler, so handle it
	return true, nil
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
