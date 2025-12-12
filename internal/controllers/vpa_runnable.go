// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sapcc/vpa_butler/internal/common"
	"github.com/sapcc/vpa_butler/internal/filter"
)

const scaleDivisor int64 = 100

// VpaRunnable is responsible for setting the maximum allowed resources
// of a served Vpa. As all served Vpas have to evaluated against all nodes
// we fetch the Vpas, their target and the nodes only once.
type VpaRunnable struct {
	client.Client
	Period          time.Duration
	JitterFactor    float64
	CapacityPercent int64
	Log             logr.Logger
}

func (v *VpaRunnable) Start(ctx context.Context) error {
	wait.JitterUntilWithContext(ctx, v.reconcile, v.Period, v.JitterFactor, false)
	return nil
}

func (v *VpaRunnable) reconcile(ctx context.Context) {
	var nodes corev1.NodeList
	err := v.List(ctx, &nodes)
	if err != nil {
		v.Log.Error(err, "failed to list nodes to determine maximum allowed resources")
		return
	}
	var vpas vpav1.VerticalPodAutoscalerList
	err = v.List(ctx, &vpas)
	if err != nil {
		v.Log.Error(err, "failed to list vpas to determine maximum allowed resources")
		return
	}
	targetedVpas := make([]filter.TargetedVpa, 0)
	for i := range vpas.Items {
		current := vpas.Items[i]
		if common.ManagedByButler(&current) {
			targeted, err := v.extractTarget(ctx, &current)
			if err != nil {
				v.Log.Error(err, "failed to extract target")
				continue
			}
			targetedVpas = append(targetedVpas, targeted)
		}
	}
	schedulable := filter.Schedulable(nodes.Items)
	for _, target := range targetedVpas {
		v.reconcileMaxResource(ctx, target, schedulable)
	}
}

func (v *VpaRunnable) extractTarget(ctx context.Context, vpa *vpav1.VerticalPodAutoscaler) (filter.TargetedVpa, error) {
	if vpa.Spec.TargetRef == nil {
		return filter.TargetedVpa{}, fmt.Errorf("vpa %s/%s has nil target ref", vpa.Namespace, vpa.Name)
	}
	ref := *vpa.Spec.TargetRef
	switch ref.Kind {
	case DeploymentStr:
		var deployment appsv1.Deployment
		err := v.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: vpa.Namespace}, &deployment)
		if err != nil {
			return filter.TargetedVpa{}, fmt.Errorf("failed to fetch target %s/%s of kind %s for vpa",
				vpa.Namespace, ref.Name, ref.Kind)
		}
		return filter.TargetedVpa{
			Type:       filter.TargetDeployment,
			Vpa:        vpa,
			PodSpec:    deployment.Spec.Template.Spec,
			Selector:   *deployment.Spec.Selector,
			ObjectMeta: deployment.ObjectMeta,
		}, nil
	case StatefulSetStr:
		var sts appsv1.StatefulSet
		err := v.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: vpa.Namespace}, &sts)
		if err != nil {
			return filter.TargetedVpa{}, fmt.Errorf("failed to fetch target %s/%s of kind %s for vpa",
				vpa.Namespace, ref.Name, ref.Kind)
		}
		return filter.TargetedVpa{
			Type:       filter.TargetStatefulSet,
			Vpa:        vpa,
			PodSpec:    sts.Spec.Template.Spec,
			Selector:   *sts.Spec.Selector,
			ObjectMeta: sts.ObjectMeta,
		}, nil
	case DaemonSetStr:
		var ds appsv1.DaemonSet
		err := v.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: vpa.Namespace}, &ds)
		if err != nil {
			return filter.TargetedVpa{}, fmt.Errorf("failed to fetch target %s/%s of kind %s for vpa",
				vpa.Namespace, ref.Name, ref.Kind)
		}
		return filter.TargetedVpa{
			Type:       filter.TargetDaemonSet,
			Vpa:        vpa,
			PodSpec:    ds.Spec.Template.Spec,
			Selector:   *ds.Spec.Selector,
			ObjectMeta: ds.ObjectMeta,
		}, nil
	}
	return filter.TargetedVpa{}, fmt.Errorf("unknown target kind %s for vpa %s/%s encountered",
		ref.Kind, vpa.Namespace, vpa.Name)
}

func (v *VpaRunnable) reconcileMaxResource(ctx context.Context, target filter.TargetedVpa, schedulable []corev1.Node) {
	viable, err := filter.Evaluate(target, schedulable)
	if err != nil {
		v.Log.Error(err, "failed to determine valid nodes", "namespace", target.Vpa.Namespace, "name", target.Vpa.Name)
		return
	}
	if len(viable) == 0 {
		v.Log.Error(err, "no valid nodes for vpa target found", "namespace", target.Vpa.Namespace, "name", target.Vpa.Name)
		return
	}
	distributionFunc := uniformDistribution
	if target.ObjectMeta.Annotations != nil && len(target.PodSpec.Containers) > 1 {
		if mainContainer, ok := target.ObjectMeta.Annotations[MainContainerAnnotationKey]; ok {
			distributionFunc = asymmetricDistribution(mainContainer)
		}
	}
	var largest corev1.Node
	// DaemonSets needs to fit onto all nodes their pods can be placed on.
	// Therefore the smallest of them is used to derive an upper recommendation
	// bound. Other payloads usually create less pods.
	if target.Type == filter.TargetDaemonSet {
		largest = minByMemory(viable)
	} else {
		largest = maxByMemory(viable)
	}
	err = v.patchMaxResources(ctx, patchParams{
		vpa: target.Vpa,
		namedResources: distributionFunc(resourceDistributionParams{
			target:          target,
			largest:         &largest,
			capacityPercent: v.CapacityPercent,
		}),
	})
	if err != nil {
		v.Log.Error(err, "failed to set maximum allowed resources for vpa",
			"name", target.Vpa.Name, "namespace", target.Vpa.Namespace)
		return
	}
}

type patchParams struct {
	vpa            *vpav1.VerticalPodAutoscaler
	namedResources []common.NamedResourceList
}

func (v *VpaRunnable) patchMaxResources(ctx context.Context, params patchParams) error {
	vpa := params.vpa
	if vpa.Spec.ResourcePolicy == nil || len(vpa.Spec.ResourcePolicy.ContainerPolicies) == 0 {
		return fmt.Errorf("resource policy of vpa %s/%s is empty", vpa.Namespace, vpa.Name)
	}
	unmodified := vpa.DeepCopy()
	controlledResources := vpa.Spec.ResourcePolicy.ContainerPolicies[0].ControlledResources
	controlledValues := vpa.Spec.ResourcePolicy.ContainerPolicies[0].ControlledValues
	minAllowed := vpa.Spec.ResourcePolicy.ContainerPolicies[0].MinAllowed
	mode := vpa.Spec.ResourcePolicy.ContainerPolicies[0].Mode
	policies := make([]vpav1.ContainerResourcePolicy, len(params.namedResources))
	for i, namedResources := range params.namedResources {
		policies[i] = vpav1.ContainerResourcePolicy{
			ContainerName:       namedResources.ContainerName,
			Mode:                mode,
			MinAllowed:          minAllowed,
			MaxAllowed:          namedResources.Resources,
			ControlledResources: controlledResources,
			ControlledValues:    controlledValues,
		}
	}
	vpa.Spec.ResourcePolicy.ContainerPolicies = policies
	return v.Patch(ctx, vpa, client.MergeFrom(unmodified))
}

func maxByMemory(nodes []corev1.Node) corev1.Node {
	var maxNode corev1.Node
	for _, node := range nodes {
		if node.Status.Allocatable.Memory().Cmp(*maxNode.Status.Allocatable.Memory()) == 1 {
			maxNode = node
		}
	}
	return maxNode
}

func minByMemory(nodes []corev1.Node) corev1.Node {
	var minNode corev1.Node
	minNode.Status.Allocatable = corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Ti")}
	for _, node := range nodes {
		if node.Status.Allocatable.Memory().Cmp(*minNode.Status.Allocatable.Memory()) == -1 {
			minNode = node
		}
	}
	return minNode
}

func scaleQuantityMilli(q *resource.Quantity, percent int64) *resource.Quantity {
	return resource.NewMilliQuantity(q.MilliValue()*percent/scaleDivisor, q.Format)
}

func scaleQuantity(q *resource.Quantity, percent int64) *resource.Quantity {
	return resource.NewQuantity(q.Value()*percent/scaleDivisor, q.Format)
}

type resourceDistributionParams struct {
	target          filter.TargetedVpa
	largest         *corev1.Node
	capacityPercent int64
}

type maxResourceDistributionFunc func(params resourceDistributionParams) []common.NamedResourceList

func uniformDistribution(params resourceDistributionParams) []common.NamedResourceList {
	containers := int64(len(params.target.PodSpec.Containers))
	// distribute a fraction of maximum capacity evenly across containers
	cpuScaled := scaleQuantityMilli(params.largest.Status.Allocatable.Cpu(), params.capacityPercent/containers)
	memScaled := scaleQuantity(params.largest.Status.Allocatable.Memory(), params.capacityPercent/containers)
	return []common.NamedResourceList{
		{
			ContainerName: "*",
			Resources: corev1.ResourceList{
				corev1.ResourceCPU:    *cpuScaled,
				corev1.ResourceMemory: *memScaled,
			},
		},
	}
}

func asymmetricDistribution(mainContainer string) maxResourceDistributionFunc {
	return func(params resourceDistributionParams) []common.NamedResourceList {
		totalFraction, mainFraction := 4, 3
		containers := params.target.PodSpec.Containers
		totalWeight := int64(totalFraction * (len(containers) - 1))
		mainWeight := int64(mainFraction * (len(containers) - 1))
		cpuMain := scaleQuantityMilli(params.largest.Status.Allocatable.Cpu(), params.capacityPercent*mainWeight/totalWeight)
		memMain := scaleQuantity(params.largest.Status.Allocatable.Memory(), params.capacityPercent*mainWeight/totalWeight)
		cpuOther := scaleQuantityMilli(params.largest.Status.Allocatable.Cpu(), params.capacityPercent/totalWeight)
		memOther := scaleQuantity(params.largest.Status.Allocatable.Memory(), params.capacityPercent/totalWeight)
		return []common.NamedResourceList{
			{
				ContainerName: mainContainer,
				Resources: corev1.ResourceList{
					corev1.ResourceCPU:    *cpuMain,
					corev1.ResourceMemory: *memMain,
				},
			},
			{
				ContainerName: "*",
				Resources: corev1.ResourceList{
					corev1.ResourceCPU:    *cpuOther,
					corev1.ResourceMemory: *memOther,
				},
			},
		}
	}
}
