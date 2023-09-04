package filter

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
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
	Vpa      *vpav1.VerticalPodAutoscaler
	PodSpec  corev1.PodSpec
	Selector metav1.LabelSelector
}

type NodeFilter func(target TargetedVpa, nodes []corev1.Node) []corev1.Node

func NodeName(target TargetedVpa, nodes []corev1.Node) []corev1.Node {
	if target.PodSpec.NodeName == "" {
		return nodes
	}
	for _, node := range nodes {
		if node.Name == target.PodSpec.NodeName {
			return []corev1.Node{node}
		}
	}
	return []corev1.Node{}
}

func Evaluate(target TargetedVpa, nodes []corev1.Node) []corev1.Node {
	filters := []NodeFilter{NodeName}
	next := nodes
	for _, filter := range filters {
		next = filter(target, next)
	}
	return next
}
