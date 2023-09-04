package filter_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/vpa_butler/internal/filter"

	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("Schedulable", func() {

	It("removes nodes that are unschedulable", func() {
		var node corev1.Node
		node.Spec.Unschedulable = true
		Expect(filter.Schedulable([]corev1.Node{node})).To(HaveLen(0))
	})

	It("keeps nodes that are schedulable", func() {
		var node corev1.Node
		node.Spec.Unschedulable = false
		Expect(filter.Schedulable([]corev1.Node{node})).To(HaveLen(1))
	})

})

var _ = Describe("NodeName", func() {

	It("keeps all nodes if no name is specified", func() {
		Expect(filter.NodeName(filter.TargetedVpa{}, []corev1.Node{{}})).To(HaveLen(1))
	})

	It("returns zero nodes if no name matches", func() {
		target := filter.TargetedVpa{PodSpec: corev1.PodSpec{NodeName: "brr"}}
		Expect(filter.NodeName(target, []corev1.Node{{}})).To(HaveLen(0))
	})

	It("only keeps the node with the matching name", func() {
		var node1 corev1.Node
		node1.Name = "node1"
		var node2 corev1.Node
		node2.Name = "node2"
		nodes := []corev1.Node{node1, node2}
		target := filter.TargetedVpa{PodSpec: corev1.PodSpec{NodeName: "node2"}}
		result := filter.NodeName(target, nodes)
		Expect(result).To(HaveLen(1))
		Expect(result[0].Name).To(Equal("node2"))
	})

})
