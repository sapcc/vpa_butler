// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package filter_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/sapcc/vpa_butler/internal/filter"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Schedulable", func() {

	It("removes nodes that are unschedulable", func() {
		var node corev1.Node
		node.Spec.Unschedulable = true
		Expect(filter.Schedulable([]corev1.Node{node})).To(BeEmpty())
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
		Expect(filter.NodeName(target, []corev1.Node{{}})).To(BeEmpty())
	})

	It("only keeps the node with the matching name", func() {
		var node1 corev1.Node
		node1.Name = "node1"
		var node2 corev1.Node
		node2.Name = "node2"
		nodes := []corev1.Node{node1, node2}
		target := filter.TargetedVpa{PodSpec: corev1.PodSpec{NodeName: "node2"}}
		result, err := filter.NodeName(target, nodes)
		Expect(err).To(Succeed())
		Expect(result).To(HaveLen(1))
		Expect(result[0].Name).To(Equal("node2"))
	})

})

var _ = Describe("TaintToleration", func() {

	It("keeps nodes if they have no taints", func() {
		Expect(filter.TaintToleration(filter.TargetedVpa{}, []corev1.Node{{}})).To(HaveLen(1))
	})

	It("removes nodes if the have a taint and the pod no toleration", func() {
		Expect(filter.TaintToleration(filter.TargetedVpa{}, []corev1.Node{{
			Spec: corev1.NodeSpec{
				Taints: []corev1.Taint{{
					Key:    corev1.TaintNodeNotReady,
					Effect: corev1.TaintEffectNoExecute,
				}},
			},
		}})).To(BeEmpty())
	})

	It("keeps nodes if they are tainted but pods have a matching toleration", func() {
		Expect(filter.TaintToleration(filter.TargetedVpa{
			PodSpec: corev1.PodSpec{
				Tolerations: []corev1.Toleration{{
					Key:      corev1.TaintNodeNotReady,
					Effect:   corev1.TaintEffectNoExecute,
					Operator: corev1.TolerationOpExists,
				}},
			},
		}, []corev1.Node{{
			Spec: corev1.NodeSpec{
				Taints: []corev1.Taint{{
					Key:    corev1.TaintNodeNotReady,
					Effect: corev1.TaintEffectNoExecute,
				}},
			},
		}})).To(HaveLen(1))
	})

})

var _ = Describe("NodeAffinity", func() {

	affinity := &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "required",
								Operator: corev1.NodeSelectorOpExists,
								Values:   []string{},
							},
						},
					},
				},
			},
		},
	}

	It("keeps nodes if pod has no affinities", func() {
		Expect(filter.NodeAffinity(filter.TargetedVpa{}, []corev1.Node{{}})).To(HaveLen(1))
	})

	It("filters node if an affinity does not match", func() {
		Expect(filter.NodeAffinity(filter.TargetedVpa{PodSpec: corev1.PodSpec{
			Affinity: affinity,
		}}, []corev1.Node{{}})).To(BeEmpty())
	})

	It("keeps nodes if an affinity matches", func() {
		Expect(filter.NodeAffinity(filter.TargetedVpa{PodSpec: corev1.PodSpec{
			Affinity: affinity,
		}}, []corev1.Node{{ObjectMeta: v1.ObjectMeta{Labels: map[string]string{"required": "yes"}}}})).To(HaveLen(1))
	})

})
