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

package controllers_test

import (
	"context"
	"io"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/vpa_butler/internal/common"
	"github.com/sapcc/vpa_butler/internal/controllers"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("VpaController", func() {

	var node *corev1.Node

	BeforeEach(func() {
		node = &corev1.Node{}
		node.Name = "the-node"
		node.Status.Capacity = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("10"),
			corev1.ResourceMemory: resource.MustParse("2048"),
		}
		Expect(k8sClient.Create(context.Background(), node)).To(Succeed())
	})

	AfterEach(func() {
		Expect(k8sClient.Delete(context.Background(), node)).To(Succeed())
	})

	When("creating a deployment and a hand-crafted vpa afterwards", func() {
		var deployment *appsv1.Deployment
		var vpa *vpav1.VerticalPodAutoscaler

		BeforeEach(func() {
			deployment = makeDeployment()
			Expect(k8sClient.Create(context.Background(), deployment)).To(Succeed())
		})

		AfterEach(func() {
			deleteVpa("test-deployment-custom-vpa")
			deleteVpa("test-deployment-deployment")
			Expect(k8sClient.Delete(context.Background(), deployment)).To(Succeed())
		})

		It("should delete the served vpa for apps/v1", func() {
			vpa = &vpav1.VerticalPodAutoscaler{}
			vpa.Name = "test-deployment-custom-vpa"
			vpa.Namespace = metav1.NamespaceDefault
			vpa.Spec.TargetRef = &autoscalingv1.CrossVersionObjectReference{
				Name:       deploymentName,
				Kind:       controllers.DeploymentStr,
				APIVersion: "apps/v1",
			}
			Expect(k8sClient.Create(context.Background(), vpa)).To(Succeed())
			Eventually(func() error {
				var vpa vpav1.VerticalPodAutoscaler
				err := k8sClient.Get(context.Background(), types.NamespacedName{
					Name:      "test-deployment-deployment",
					Namespace: metav1.NamespaceDefault,
				}, &vpa)
				return err
			}).ShouldNot(Succeed())
		})

		It("should delete the served vpa for v1", func() {
			vpa = &vpav1.VerticalPodAutoscaler{}
			vpa.Name = "test-deployment-custom-vpa"
			vpa.Namespace = metav1.NamespaceDefault
			vpa.Spec.TargetRef = &autoscalingv1.CrossVersionObjectReference{
				Name:       deploymentName,
				Kind:       controllers.DeploymentStr,
				APIVersion: "v1",
			}
			Expect(k8sClient.Create(context.Background(), vpa)).To(Succeed())
			Eventually(func() error {
				var vpa vpav1.VerticalPodAutoscaler
				err := k8sClient.Get(context.Background(), types.NamespacedName{
					Name:      "test-deployment-deployment",
					Namespace: metav1.NamespaceDefault,
				}, &vpa)
				return err
			}).ShouldNot(Succeed())
		})

	})

	When("creating a deployment", func() {

		var deployment *appsv1.Deployment
		var defaultUpdateMode vpav1.UpdateMode

		BeforeEach(func() {
			defaultUpdateMode = common.VpaUpdateMode
			deployment = makeDeployment()
			Expect(k8sClient.Create(context.Background(), deployment)).To(Succeed())
		})

		AfterEach(func() {
			// failsafe: there is a deletion in the tests, so we drop the error here
			_ = k8sClient.Delete(context.Background(), deployment)
			deleteVpa("test-deployment-deployment")
			common.VpaUpdateMode = defaultUpdateMode
		})

		It("deletes Vpas with an orphaned target on reconciliation", func() {
			var vpa vpav1.VerticalPodAutoscaler
			name := types.NamespacedName{Namespace: metav1.NamespaceDefault, Name: "test-deployment-deployment"}

			// await vpa creation
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(context.Background(), name, &vpa)
				return err
			}).Should(Succeed())

			Expect(k8sClient.Delete(context.Background(), deployment)).To(Succeed())

			// force reconciliation
			Expect(k8sClient.Get(context.Background(), name, &vpa)).To(Succeed())
			unmodified := vpa.DeepCopy()
			vpa.Labels = map[string]string{"cloud.sap/reconcile": "please"}
			Expect(k8sClient.Patch(context.Background(), &vpa, client.MergeFrom(unmodified))).To(
				Satisfy(func(err error) bool { return err == nil || errors.IsNotFound(err) }),
			)

			Eventually(func(g Gomega) bool {
				err := k8sClient.Get(context.Background(), name, &vpa)
				return errors.IsNotFound(err)
			}).Should(BeTrue())
		})

		It("patches the served vpa", func() {
			var unmodified vpav1.VerticalPodAutoscaler
			Eventually(func() error {
				return k8sClient.Get(context.Background(), types.NamespacedName{
					Namespace: metav1.NamespaceDefault,
					Name:      "test-deployment-deployment",
				}, &unmodified)
			}).Should(Succeed())
			// need to ensure that a vpa is created before the update
			// to this global variable
			common.VpaUpdateMode = vpav1.UpdateModeAuto
			changed := unmodified.DeepCopy()
			changed.Labels = map[string]string{"changed": "true"}
			Expect(k8sClient.Patch(context.Background(), changed, client.MergeFrom(&unmodified))).To(Succeed())
			Eventually(func() vpav1.UpdateMode {
				var vpa vpav1.VerticalPodAutoscaler
				err := k8sClient.Get(context.Background(), types.NamespacedName{
					Name:      "test-deployment-deployment",
					Namespace: metav1.NamespaceDefault,
				}, &vpa)
				if err != nil {
					return vpav1.UpdateMode("")
				}
				return *vpa.Spec.UpdatePolicy.UpdateMode
			}).Should(Equal(common.VpaUpdateMode))
		})

		It("updates the update mode based on the annotation", func() {
			unmodified := deployment.DeepCopy()
			deployment.Annotations = map[string]string{controllers.UpdateModeAnnotationKey: string(vpav1.UpdateModeRecreate)}
			Expect(k8sClient.Patch(context.Background(), deployment, client.MergeFrom(unmodified))).To(Succeed())
			Eventually(func() vpav1.UpdateMode {
				var vpa vpav1.VerticalPodAutoscaler
				err := k8sClient.Get(context.Background(), types.NamespacedName{
					Name:      "test-deployment-deployment",
					Namespace: metav1.NamespaceDefault,
				}, &vpa)
				if err != nil {
					return vpav1.UpdateMode("")
				}
				return *vpa.Spec.UpdatePolicy.UpdateMode
			}).Should(Equal(vpav1.UpdateModeRecreate))
		})

		It("updates the controlled values based on the annotation", func() {
			unmodified := deployment.DeepCopy()
			deployment.Annotations = map[string]string{
				controllers.ControlledValuesAnnotationKey: string(vpav1.ContainerControlledValuesRequestsAndLimits),
			}
			Expect(k8sClient.Patch(context.Background(), deployment, client.MergeFrom(unmodified))).To(Succeed())
			Eventually(func() vpav1.ContainerControlledValues {
				var vpa vpav1.VerticalPodAutoscaler
				err := k8sClient.Get(context.Background(), types.NamespacedName{
					Name:      "test-deployment-deployment",
					Namespace: metav1.NamespaceDefault,
				}, &vpa)
				if err != nil {
					return vpav1.ContainerControlledValues("")
				}
				if vpa.Spec.ResourcePolicy == nil || len(vpa.Spec.ResourcePolicy.ContainerPolicies) == 0 {
					return vpav1.ContainerControlledValues("")
				}
				return *vpa.Spec.ResourcePolicy.ContainerPolicies[0].ControlledValues
			}).Should(Equal(vpav1.ContainerControlledValuesRequestsAndLimits))
		})

	})

	When("reconciling a vpa", func() {
		var vpa *vpav1.VerticalPodAutoscaler

		BeforeEach(func() {
			vpa = &vpav1.VerticalPodAutoscaler{}
			vpa.Name = "metrics"
			vpa.Namespace = metav1.NamespaceDefault
			vpa.Spec.ResourcePolicy = &vpav1.PodResourcePolicy{
				ContainerPolicies: []vpav1.ContainerResourcePolicy{
					{
						ContainerName: "*",
						MaxAllowed: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1"),
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				},
			}
			vpa.Status.Recommendation = &vpav1.RecommendedPodResources{
				ContainerRecommendations: []vpav1.RecommendedContainerResources{
					{
						ContainerName: "the-container",
						UncappedTarget: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("3Gi"),
						},
						Target: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1"),
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				},
			}
			vpa.Spec.TargetRef = &autoscalingv1.CrossVersionObjectReference{
				Kind:       controllers.StatefulSetStr,
				Name:       "whatever",
				APIVersion: "apps/v1",
			}
			Expect(k8sClient.Create(context.Background(), vpa)).To(Succeed())
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(context.Background(), vpa)).To(Succeed())
		})

		It("creates recommendation excess metrics", func() {
			Eventually(func(g Gomega) []string {
				res, err := http.Get("http://127.0.0.1:8080/metrics")
				g.Expect(err).To(Succeed())
				defer res.Body.Close()
				data, err := io.ReadAll(res.Body)
				Expect(err).To(Succeed())
				lines := strings.Split(string(data), "\n")
				excess := slices.Filter(nil, lines, func(s string) bool {
					return strings.Contains(s, "vpa_butler_vpa_container_recommendation_excess{")
				})
				return excess
			}).Should(SatisfyAll(
				ContainElement("vpa_butler_vpa_container_recommendation_excess{container=\"the-container\",namespace=\"default\",resource=\"cpu\",unit=\"core\",verticalpodautoscaler=\"metrics\"} -0.5"),               //nolint:lll
				ContainElement("vpa_butler_vpa_container_recommendation_excess{container=\"the-container\",namespace=\"default\",resource=\"memory\",unit=\"byte\",verticalpodautoscaler=\"metrics\"} 2.147483648e+09"), //nolint:lll
			))
		})

	})

})
