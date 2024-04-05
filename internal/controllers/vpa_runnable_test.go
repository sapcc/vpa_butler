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
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sapcc/vpa_butler/internal/common"
	"github.com/sapcc/vpa_butler/internal/controllers"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

const (
	deployVpaName = deploymentName + "-deployment"
)

func expectMaxResources(name, cpu, mem string) {
	GinkgoHelper()
	Eventually(func() error {
		var vpaRef types.NamespacedName
		vpaRef.Name = name
		vpaRef.Namespace = metav1.NamespaceDefault

		var vpa vpav1.VerticalPodAutoscaler
		err := k8sClient.Get(context.Background(), vpaRef, &vpa)
		if err != nil {
			return err
		}
		mangedBy, ok := vpa.Annotations[common.AnnotationManagedBy]
		if !ok {
			return errors.New("vpa does not have managed-by annotation")
		}
		if mangedBy != common.AnnotationVpaButler {
			return errors.New("vpa has wrong managed-by annotation")
		}
		if vpa.Spec.ResourcePolicy == nil {
			return errors.New("vpa resource policy is nil")
		}
		if len(vpa.Spec.ResourcePolicy.ContainerPolicies) != 1 {
			return errors.New("vpa has wrong amount of container policies")
		}
		maxAllowed := vpa.Spec.ResourcePolicy.ContainerPolicies[0].MaxAllowed
		if !maxAllowed.Cpu().Equal(resource.MustParse(cpu)) {
			return errors.New("vpa maxAllowed CPU does not match")
		}
		if !maxAllowed.Memory().Equal(resource.MustParse(mem)) {
			return errors.New("vpa maxAllowed memory does not match")
		}
		return nil
	}).Should(Succeed())
}

var _ = Describe("VpaRunnable", func() {

	var node *corev1.Node

	BeforeEach(func() {
		node = &corev1.Node{}
		node.Name = "the-node"
		node.Status.Allocatable = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: resource.MustParse("2000"),
		}
		Expect(k8sClient.Create(context.Background(), node)).To(Succeed())
	})

	When("a deployment is created", func() {
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			deployment = makeDeployment()
			Expect(k8sClient.Create(context.Background(), deployment)).To(Succeed())
		})

		It("sets maximum allocatable resources", func() {
			expectMaxResources(deployVpaName, "900m", "1800")
		})

		AfterEach(func() {
			deleteVpa(deployVpaName)
			Expect(k8sClient.Delete(context.Background(), deployment)).To(Succeed())
		})

	})

	When("a statefulset is created", func() {
		var statefulset *appsv1.StatefulSet

		BeforeEach(func() {
			statefulset = makeStatefulSet()
			Expect(k8sClient.Create(context.Background(), statefulset)).To(Succeed())
		})

		It("sets the maximum allocatable resources", func() {
			expectMaxResources(statefulSetName+"-statefulset", "900m", "1800")
		})

		AfterEach(func() {
			deleteVpa(statefulSetName + "-statefulset")
			Expect(k8sClient.Delete(context.Background(), statefulset)).To(Succeed())
		})
	})

	When("a daemonset is created", func() {
		var daemonset *appsv1.DaemonSet

		BeforeEach(func() {
			daemonset = makeDaemonSet()
			Expect(k8sClient.Create(context.Background(), daemonset)).To(Succeed())
		})

		It("sets the maximum allocatable resources", func() {
			expectMaxResources(daemonSetName+"-daemonset", "900m", "1800")
		})

		AfterEach(func() {
			deleteVpa(daemonSetName + "-daemonset")
			Expect(k8sClient.Delete(context.Background(), daemonset)).To(Succeed())
		})

	})

	When("creating a hand-crafted vpa and a deployment afterwards", func() {
		var vpa *vpav1.VerticalPodAutoscaler
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			vpa = &vpav1.VerticalPodAutoscaler{}
			vpa.Name = deploymentCustomVpaName
			vpa.Namespace = metav1.NamespaceDefault
			vpa.Spec.TargetRef = &autoscalingv1.CrossVersionObjectReference{
				Name:       deploymentName,
				Kind:       controllers.DeploymentStr,
				APIVersion: "apps/v1",
			}
			vpa.Spec.ResourcePolicy = &vpav1.PodResourcePolicy{
				ContainerPolicies: []vpav1.ContainerResourcePolicy{
					{
						ContainerName: "*",
						MaxAllowed: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("20"),
							corev1.ResourceMemory: resource.MustParse("30"),
						},
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), vpa)).To(Succeed())
			deployment = makeDeployment()
			Expect(k8sClient.Create(context.Background(), deployment)).To(Succeed())
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(context.Background(), deployment)).To(Succeed())
			deleteVpa(deploymentCustomVpaName)
			deleteVpa("test-deployment-deployment")
		})

		It("does not change the hand-crafted vpa", func() {
			Consistently(func(g Gomega) bool {
				var vpa vpav1.VerticalPodAutoscaler
				g.Expect(k8sClient.Get(context.Background(), types.NamespacedName{
					Name:      deploymentCustomVpaName,
					Namespace: metav1.NamespaceDefault,
				}, &vpa)).To(Succeed())
				maxAllowed := vpa.Spec.ResourcePolicy.ContainerPolicies[0].MaxAllowed
				if !maxAllowed.Cpu().Equal(resource.MustParse("20")) {
					return false
				}
				return maxAllowed.Memory().Equal(resource.MustParse("30"))
			}).Should(BeTrue())
		})

	})

	When("having a second differently sized node", func() {
		var secondNode *corev1.Node
		var deployment *appsv1.Deployment
		var daemonSet *appsv1.DaemonSet

		BeforeEach(func() {
			secondNode = &corev1.Node{}
			secondNode.Name = "second-node"
			secondNode.Status.Allocatable = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4000m"),
				corev1.ResourceMemory: resource.MustParse("500"),
			}
			Expect(k8sClient.Create(context.Background(), secondNode)).To(Succeed())
			deployment = makeDeployment()
			Expect(k8sClient.Create(context.Background(), deployment)).To(Succeed())
			daemonSet = makeDaemonSet()
			Expect(k8sClient.Create(context.Background(), daemonSet)).To(Succeed())
		})

		AfterEach(func() {
			deleteVpa("test-daemonset-daemonset")
			deleteVpa(deployVpaName)
			Expect(k8sClient.Delete(context.Background(), daemonSet)).To(Succeed())
			Expect(k8sClient.Delete(context.Background(), deployment)).To(Succeed())
			Expect(k8sClient.Delete(context.Background(), secondNode)).To(Succeed())
		})

		It("prefers the node with the most memory for setting maximum allowed resources for non-daemonsets", func() {
			expectMaxResources(deployVpaName, "900m", "1800")
		})

		It("prefers the node with the least memory for setting maximum allowed resources for daemonsets", func() {
			expectMaxResources("test-daemonset-daemonset", "3600m", "450")
		})
	})

	When("using a deployment with two containers", func() {
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			deployment = makeDeployment()
			containers := deployment.Spec.Template.Spec.Containers
			next := containers[0].DeepCopy()
			next.Name = "next"
			deployment.Spec.Template.Spec.Containers = append(containers, *next)
			Expect(k8sClient.Create(context.Background(), deployment)).To(Succeed())
		})

		It("distributes maximum allocatable resources evenly", func() {
			expectMaxResources(deployVpaName, "450m", "900")
		})

		It("distributes resources asymmetrical if a main container is annotated", func() {
			unmodified := deployment.DeepCopy()
			deployment.Annotations = map[string]string{controllers.MainContainerAnnotationKey: "next"}
			Expect(k8sClient.Patch(context.Background(), deployment, client.MergeFrom(unmodified))).To(Succeed())
			var vpaRef types.NamespacedName
			vpaRef.Name = deployVpaName
			vpaRef.Namespace = metav1.NamespaceDefault

			var vpa vpav1.VerticalPodAutoscaler
			var policies []vpav1.ContainerResourcePolicy
			Eventually(func(g Gomega) []vpav1.ContainerResourcePolicy {
				g.Expect(k8sClient.Get(context.Background(), vpaRef, &vpa)).To(Succeed())
				if vpa.Spec.ResourcePolicy == nil {
					return nil
				}
				policies = vpa.Spec.ResourcePolicy.ContainerPolicies
				return policies
			}).Should(HaveLen(2))
			Expect(policies[0].ContainerName).To(Equal("next"))
			Expect(policies[0].MaxAllowed.Cpu().MilliValue()).To(BeEquivalentTo(670))
			Expect(policies[0].MaxAllowed.Memory().Value()).To(BeEquivalentTo(1340))
			Expect(policies[1].ContainerName).To(Equal("*"))
			Expect(policies[1].MaxAllowed.Cpu().MilliValue()).To(BeEquivalentTo(220))
			Expect(policies[1].MaxAllowed.Memory().Value()).To(BeEquivalentTo(440))
		})

		AfterEach(func() {
			deleteVpa(deployVpaName)
			Expect(k8sClient.Delete(context.Background(), deployment)).To(Succeed())
		})
	})

	AfterEach(func() {
		Expect(k8sClient.Delete(context.Background(), node)).To(Succeed())
	})

})
