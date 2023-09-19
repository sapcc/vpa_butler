package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/sapcc/vpa_butler/internal/common"
	"github.com/sapcc/vpa_butler/internal/filter"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	err := v.Client.List(ctx, &nodes)
	if err != nil {
		v.Log.Error(err, "failed to list nodes to determine maximum allowed resources")
		return
	}
	var vpas vpav1.VerticalPodAutoscalerList
	err = v.Client.List(ctx, &vpas)
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
		viable, err := filter.Evaluate(target, schedulable)
		if err != nil {
			v.Log.Error(err, "failed to determine valid nodes", "namespace", target.Vpa.Namespace, "name", target.Vpa.Name)
		}
		if len(viable) == 0 {
			v.Log.Error(err, "no valid nodes found", "namespace", target.Vpa.Namespace, "name", target.Vpa.Name)
			return
		}
		largest := maxByMemory(viable)
		containers := int64(len(target.PodSpec.Containers))
		// distribute a fraction of maximum capacity evenly across containers
		cpuScaled := scaleQuantityMilli(largest.Status.Allocatable.Cpu(), v.CapacityPercent/containers)
		memScaled := scaleQuantity(largest.Status.Allocatable.Memory(), v.CapacityPercent/containers)
		err = v.patchMaxRessources(ctx, patchParams{
			vpa:    target.Vpa,
			cpu:    *cpuScaled,
			memory: *memScaled,
		})
		if err != nil {
			v.Log.Error(err, "failed to set maximum allowed resources for vpa",
				"name", target.Vpa.Name, "namespace", target.Vpa.Namespace)
			continue
		}
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
		err := v.Client.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: vpa.Namespace}, &deployment)
		if err != nil {
			return filter.TargetedVpa{}, fmt.Errorf("failed to fetch target %s/%s of kind %s for vpa",
				vpa.Namespace, ref.Name, ref.Kind)
		}
		return filter.TargetedVpa{
			Vpa:      vpa,
			PodSpec:  deployment.Spec.Template.Spec,
			Selector: *deployment.Spec.Selector,
		}, nil
	case StatefulSetStr:
		var sts appsv1.StatefulSet
		err := v.Client.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: vpa.Namespace}, &sts)
		if err != nil {
			return filter.TargetedVpa{}, fmt.Errorf("failed to fetch target %s/%s of kind %s for vpa",
				vpa.Namespace, ref.Name, ref.Kind)
		}
		return filter.TargetedVpa{
			Vpa:      vpa,
			PodSpec:  sts.Spec.Template.Spec,
			Selector: *sts.Spec.Selector,
		}, nil
	case DaemonSetStr:
		var ds appsv1.DaemonSet
		err := v.Client.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: vpa.Namespace}, &ds)
		if err != nil {
			return filter.TargetedVpa{}, fmt.Errorf("failed to fetch target %s/%s of kind %s for vpa",
				vpa.Namespace, ref.Name, ref.Kind)
		}
		return filter.TargetedVpa{
			Vpa:      vpa,
			PodSpec:  ds.Spec.Template.Spec,
			Selector: *ds.Spec.Selector,
		}, nil
	}
	return filter.TargetedVpa{}, fmt.Errorf("unknown target kind %s for vpa %s/%s encountered",
		ref.Kind, vpa.Namespace, vpa.Name)
}

type patchParams struct {
	vpa    *vpav1.VerticalPodAutoscaler
	cpu    resource.Quantity
	memory resource.Quantity
}

func (v *VpaRunnable) patchMaxRessources(ctx context.Context, params patchParams) error {
	vpa := params.vpa
	if vpa.Spec.ResourcePolicy == nil {
		return fmt.Errorf("resource policy of vpa %s/%s is empty", vpa.Namespace, vpa.Name)
	}
	if len(vpa.Spec.ResourcePolicy.ContainerPolicies) != 1 {
		return fmt.Errorf("vpa %s/%s does not have a sole container policy", vpa.Namespace, vpa.Name)
	}
	unmodified := vpa.DeepCopy()
	vpa.Spec.ResourcePolicy.ContainerPolicies[0].MaxAllowed = corev1.ResourceList{
		corev1.ResourceCPU:    params.cpu,
		corev1.ResourceMemory: params.memory,
	}
	return v.Client.Patch(ctx, vpa, client.MergeFrom(unmodified))
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

func scaleQuantityMilli(q *resource.Quantity, percent int64) *resource.Quantity {
	return resource.NewMilliQuantity(q.MilliValue()*percent/scaleDivisor, q.Format)
}

func scaleQuantity(q *resource.Quantity, percent int64) *resource.Quantity {
	return resource.NewQuantity(q.Value()*percent/scaleDivisor, q.Format)
}
