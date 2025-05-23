// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package common_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/sapcc/vpa_butler/internal/common"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

var _ = Describe("MangedByButler", func() {

	It("succeeds if a Vpa has a correct annotation", func() {
		var vpa vpav1.VerticalPodAutoscaler
		vpa.Annotations = map[string]string{common.AnnotationManagedBy: common.AnnotationVpaButler}
		Expect(common.ManagedByButler(&vpa)).To(BeTrue())
	})

	It("fails if a Vpa has a mangedBy annotation with a wrong value", func() {
		var vpa vpav1.VerticalPodAutoscaler
		vpa.Annotations = map[string]string{common.AnnotationManagedBy: "me"}
		Expect(common.ManagedByButler(&vpa)).To(BeFalse())
	})

	It("fails if a Vpa does not have annotations", func() {
		var vpa vpav1.VerticalPodAutoscaler
		Expect(common.ManagedByButler(&vpa)).To(BeFalse())
	})

})
