package filter

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	v1helper "k8s.io/component-helpers/scheduling/corev1"
	"k8s.io/component-helpers/scheduling/corev1/nodeaffinity"
)

func Schedulable(nodes []corev1.Node) []corev1.Node {
	schedulable := make([]corev1.Node, 0)
	for _, node := range nodes {
		if !node.Spec.Unschedulable {
			schedulable = append(schedulable, node)
		}
	}
	return schedulable
}

type TargetedVpa struct {
	Vpa        *vpav1.VerticalPodAutoscaler
	PodSpec    corev1.PodSpec
	Selector   metav1.LabelSelector
	ObjectMeta metav1.ObjectMeta
}

type NodeFilter func(target TargetedVpa, nodes []corev1.Node) ([]corev1.Node, error)

func NodeName(target TargetedVpa, nodes []corev1.Node) ([]corev1.Node, error) {
	if target.PodSpec.NodeName == "" {
		return nodes, nil
	}
	for _, node := range nodes {
		if node.Name == target.PodSpec.NodeName {
			return []corev1.Node{node}, nil
		}
	}
	return []corev1.Node{}, nil
}

func TaintToleration(target TargetedVpa, nodes []corev1.Node) ([]corev1.Node, error) {
	doNotScheduleTaintsFilterFunc := func(t *corev1.Taint) bool {
		// PodToleratesNodeTaints is only interested in NoSchedule and NoExecute taints.
		return t.Effect == corev1.TaintEffectNoSchedule || t.Effect == corev1.TaintEffectNoExecute
	}
	tolerated := make([]corev1.Node, 0)
	for _, node := range nodes {
		_, untolerated := v1helper.FindMatchingUntoleratedTaint(
			node.Spec.Taints,
			target.PodSpec.Tolerations,
			doNotScheduleTaintsFilterFunc,
		)
		if !untolerated {
			tolerated = append(tolerated, node)
		}
	}
	return tolerated, nil
}

func NodeAffinity(target TargetedVpa, nodes []corev1.Node) ([]corev1.Node, error) {
	required := nodeaffinity.GetRequiredNodeAffinity(&corev1.Pod{Spec: target.PodSpec})
	matched := make([]corev1.Node, 0)
	for i := range nodes {
		current := nodes[i]
		matches, err := required.Match(&current)
		if err != nil {
			return nil, err
		}
		if matches {
			matched = append(matched, current)
		}
	}
	return matched, nil
}

func Evaluate(target TargetedVpa, nodes []corev1.Node) ([]corev1.Node, error) {
	filters := []NodeFilter{NodeName, TaintToleration, NodeAffinity}
	next := nodes
	for _, filter := range filters {
		var err error
		next, err = filter(target, next)
		if err != nil {
			return nil, err
		}
	}
	return next, nil
}
