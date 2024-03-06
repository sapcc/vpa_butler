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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/vpa_butler/internal/common"
	"github.com/sapcc/vpa_butler/internal/controllers"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
)

const (
	deploymentName          string = "test-deployment"
	statefulSetName         string = "test-statefulset"
	daemonSetName           string = "test-daemonset"
	deploymentCustomVpaName string = "test-deployment-custom-vpa"
)

var (
	labels   = map[string]string{"app": "test"}
	selector = metav1.LabelSelector{
		MatchLabels: labels,
	}
	containers = []corev1.Container{
		{
			Name:  "test-container",
			Image: "nginx",
		},
	}
)

func expectVpa(name string) {
	GinkgoHelper()
	var vpaRef types.NamespacedName
	vpaRef.Name = name
	vpaRef.Namespace = metav1.NamespaceDefault
	Eventually(func() error {
		var vpa vpav1.VerticalPodAutoscaler
		err := k8sClient.Get(context.Background(), vpaRef, &vpa)
		if err != nil {
			return err
		}
		mangedBy, ok := vpa.Annotations[common.AnnotationManagedBy]
		if !ok {
			return fmt.Errorf("vpa does not have managed-by annotation")
		}
		if mangedBy != common.AnnotationVpaButler {
			return fmt.Errorf("vpa has wrong managed-by annotation")
		}
		// the min resources stuff technically belongs to vpa_controller.go
		if vpa.Spec.ResourcePolicy == nil {
			return fmt.Errorf("vpa resource policy is nil")
		}
		if len(vpa.Spec.ResourcePolicy.ContainerPolicies) != 1 {
			return fmt.Errorf("vpa has wrong amount of container policies")
		}
		minAllowed := vpa.Spec.ResourcePolicy.ContainerPolicies[0].MinAllowed
		if !minAllowed.Cpu().Equal(testMinAllowedCPU) {
			return fmt.Errorf("vpa minAllowed CPU does not match")
		}
		if !minAllowed.Memory().Equal(testMinAllowedMemory) {
			return fmt.Errorf("vpa minAllowed memory does not match")
		}
		return nil
	}).Should(Succeed())
}

func deleteVpa(name string) {
	var vpa vpav1.VerticalPodAutoscaler
	vpa.Name = name
	vpa.Namespace = metav1.NamespaceDefault
	err := k8sClient.Delete(context.Background(), &vpa)
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).To(Succeed())
}

func makeDeployment() *appsv1.Deployment {
	deployment := &appsv1.Deployment{}
	deployment.Name = deploymentName
	deployment.Namespace = metav1.NamespaceDefault
	deployment.Spec.Selector = &selector
	deployment.Spec.Template.ObjectMeta.Labels = labels
	deployment.Spec.Replicas = ptr.To[int32](1)
	deployment.Spec.Template.Spec.Containers = containers
	deployment.Spec.Template.Spec.Tolerations = []corev1.Toleration{{
		Key:      corev1.TaintNodeNotReady,
		Operator: corev1.TolerationOpExists,
		Effect:   corev1.TaintEffectNoSchedule,
	}}
	return deployment
}

func makeStatefulSet() *appsv1.StatefulSet {
	statefulset := appsv1.StatefulSet{}
	statefulset.Name = statefulSetName
	statefulset.Namespace = metav1.NamespaceDefault
	statefulset.Spec.Selector = &selector
	statefulset.Spec.Template.ObjectMeta.Labels = labels
	statefulset.Spec.Replicas = ptr.To[int32](1)
	statefulset.Spec.Template.Spec.Containers = containers
	statefulset.Spec.Template.Spec.Tolerations = []corev1.Toleration{{
		Key:      corev1.TaintNodeNotReady,
		Operator: corev1.TolerationOpExists,
		Effect:   corev1.TaintEffectNoSchedule,
	}}
	return &statefulset
}

func makeDaemonSet() *appsv1.DaemonSet {
	daemonset := &appsv1.DaemonSet{}
	daemonset.Name = daemonSetName
	daemonset.Namespace = metav1.NamespaceDefault
	daemonset.Spec.Selector = &selector
	daemonset.Spec.Template.ObjectMeta.Labels = labels
	daemonset.Spec.Template.Spec.Containers = containers
	daemonset.Spec.Template.Spec.Tolerations = []corev1.Toleration{{
		Key:      corev1.TaintNodeNotReady,
		Operator: corev1.TolerationOpExists,
		Effect:   corev1.TaintEffectNoSchedule,
	}}
	return daemonset
}

var _ = Describe("GenericController", func() {

	Context("when creating a deployment", func() {
		var deployment *appsv1.Deployment
		var defaultUpdateMode vpav1.UpdateMode

		BeforeEach(func() {
			defaultUpdateMode = common.VpaUpdateMode
			deployment = makeDeployment()
			Expect(k8sClient.Create(context.Background(), deployment)).To(Succeed())
		})

		AfterEach(func() {
			deleteVpa("test-deployment-deployment")
			Expect(k8sClient.Delete(context.Background(), deployment)).To(Succeed())
			common.VpaUpdateMode = defaultUpdateMode
		})

		It("should create a vpa", func() {
			expectVpa("test-deployment-deployment")
		})
	})

	Context("when creating a statefulset", func() {
		var statefulset *appsv1.StatefulSet

		BeforeEach(func() {
			statefulset = makeStatefulSet()
			Expect(k8sClient.Create(context.Background(), statefulset)).To(Succeed())
		})

		AfterEach(func() {
			deleteVpa("test-statefulset-statefulset")
			Expect(k8sClient.Delete(context.Background(), statefulset)).To(Succeed())
		})

		It("should create a vpa", func() {
			expectVpa("test-statefulset-statefulset")
		})
	})

	Context("when creating a daemonset", func() {
		var daemonset *appsv1.DaemonSet

		BeforeEach(func() {
			daemonset = makeDaemonSet()
			Expect(k8sClient.Create(context.Background(), daemonset)).To(Succeed())
		})

		AfterEach(func() {
			deleteVpa("test-daemonset-daemonset")
			Expect(k8sClient.Delete(context.Background(), daemonset)).To(Succeed())
		})

		It("should create a vpa", func() {
			expectVpa("test-daemonset-daemonset")
		})
	})

	Context("when creating a hand-crafted vpa and a deployment afterwards", func() {
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
			Expect(k8sClient.Create(context.Background(), vpa)).To(Succeed())
			deployment = makeDeployment()
			Expect(k8sClient.Create(context.Background(), deployment)).To(Succeed())
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(context.Background(), deployment)).To(Succeed())
			deleteVpa(deploymentCustomVpaName)
			deleteVpa("test-deployment-deployment")
		})

		It("does not serve a vpa", func() {
			var vpa vpav1.VerticalPodAutoscaler
			Consistently(func(g Gomega) error {
				defer GinkgoRecover()
				err := k8sClient.Get(context.Background(), types.NamespacedName{
					Name:      "test-deployment-deployment",
					Namespace: metav1.NamespaceDefault,
				}, &vpa)
				g.Expect(err).To(HaveOccurred())
				return err
			}).Should(Satisfy(errors.IsNotFound))
		})

	})

	Context("when creating a vpa targeting the owner of a deployment", func() {
		var vpa *vpav1.VerticalPodAutoscaler
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			vpa = &vpav1.VerticalPodAutoscaler{}
			vpa.Name = deploymentCustomVpaName
			vpa.Namespace = metav1.NamespaceDefault
			vpa.Spec.TargetRef = &autoscalingv1.CrossVersionObjectReference{
				Name:       deploymentName,
				Kind:       controllers.DeploymentStr + "Owner",
				APIVersion: "apps/v1",
			}
			Expect(k8sClient.Create(context.Background(), vpa)).To(Succeed())
			deployment = makeDeployment()
			deployment.OwnerReferences = []metav1.OwnerReference{
				{
					APIVersion: vpa.Spec.TargetRef.APIVersion,
					Kind:       vpa.Spec.TargetRef.Kind,
					Name:       vpa.Spec.TargetRef.Name,
					UID:        vpa.UID, // makes no sense, but passes validation
				},
			}
			Expect(k8sClient.Create(context.Background(), deployment)).To(Succeed())
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(context.Background(), deployment)).To(Succeed())
			deleteVpa(deploymentCustomVpaName)
			deleteVpa("test-deployment-deployment")
		})

		It("does not serve a vpa", func() {
			var vpa vpav1.VerticalPodAutoscaler
			Consistently(func(g Gomega) error {
				defer GinkgoRecover()
				err := k8sClient.Get(context.Background(), types.NamespacedName{
					Name:      "test-deployment-deployment",
					Namespace: metav1.NamespaceDefault,
				}, &vpa)
				g.Expect(err).To(HaveOccurred())
				return err
			}).Should(Satisfy(errors.IsNotFound))
		})
	})

})
