// Copyright 2024 SAP SE
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
