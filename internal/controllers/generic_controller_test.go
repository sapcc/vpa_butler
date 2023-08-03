package controllers_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/vpa_butler/internal/common"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("GenericControllers", func() {

	labels := map[string]string{"app": "test"}
	selector := metav1.LabelSelector{
		MatchLabels: labels,
	}
	containers := []corev1.Container{
		{
			Name:  "test-container",
			Image: "nginx",
		},
	}

	expectVpa := func(name string) {
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
			return nil
		}).Should(Succeed())
	}

	deleteVpa := func(name string) {
		var vpaRef types.NamespacedName
		vpaRef.Name = name
		vpaRef.Namespace = metav1.NamespaceDefault
		var vpa vpav1.VerticalPodAutoscaler
		err := k8sClient.Get(context.Background(), vpaRef, &vpa)
		if errors.IsNotFound(err) {
			return
		}
		Expect(err).To(Succeed())
		Expect(k8sClient.Delete(context.Background(), &vpa)).To(Succeed())
	}

	Context("when creating a deployment", func() {
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			deployment = &appsv1.Deployment{}
			deployment.Name = "test-deployment"
			deployment.Namespace = metav1.NamespaceDefault
			deployment.Spec.Selector = &selector
			deployment.Spec.Template.ObjectMeta.Labels = labels
			deployment.Spec.Replicas = ptr.To[int32](1)
			deployment.Spec.Template.Spec.Containers = containers
			Expect(k8sClient.Create(context.Background(), deployment)).To(Succeed())
		})

		AfterEach(func() {
			deleteVpa("test-deployment-deployment")
			Expect(k8sClient.Delete(context.Background(), deployment)).To(Succeed())
		})

		It("should create a vpa", func() {
			expectVpa("test-deployment-deployment")
		})

		It("should delete its vpa if one is hand-crafted", func() {
			var vpa vpav1.VerticalPodAutoscaler
			vpa.Name = "test-deployment-custom-vpa"
			vpa.Namespace = metav1.NamespaceDefault
			vpa.Spec.TargetRef = &autoscalingv1.CrossVersionObjectReference{
				Name:       "test-deployment",
				Kind:       "Deployment",
				APIVersion: "v1",
			}
			Expect(k8sClient.Create(context.Background(), &vpa)).To(Succeed())

			Eventually(func() error {
				var vpa vpav1.VerticalPodAutoscaler
				err := k8sClient.Get(context.Background(), types.NamespacedName{
					Name:      "test-deployment-deployment",
					Namespace: metav1.NamespaceDefault,
				}, &vpa)
				return err
			}).ShouldNot(Succeed())
			deleteVpa("test-deployment-custom-vpa")
		})
	})

	Context("when creating a statefulset", func() {
		var statefulset appsv1.StatefulSet

		BeforeEach(func() {
			statefulset.Name = "test-statefulset"
			statefulset.Namespace = metav1.NamespaceDefault
			statefulset.Spec.Selector = &selector
			statefulset.Spec.Template.ObjectMeta.Labels = labels
			statefulset.Spec.Replicas = ptr.To[int32](1)
			statefulset.Spec.Template.Spec.Containers = containers
			Expect(k8sClient.Create(context.Background(), &statefulset)).To(Succeed())
		})

		AfterEach(func() {
			deleteVpa("test-statefulset-statefulset")
			Expect(k8sClient.Delete(context.Background(), &statefulset)).To(Succeed())
		})

		It("should create a vpa", func() {
			expectVpa("test-statefulset-statefulset")
		})
	})

	Context("when creating a daemonset", func() {
		var daemonset appsv1.DaemonSet

		BeforeEach(func() {
			daemonset.Name = "test-daemonset"
			daemonset.Namespace = metav1.NamespaceDefault
			daemonset.Spec.Selector = &selector
			daemonset.Spec.Template.ObjectMeta.Labels = labels
			daemonset.Spec.Template.Spec.Containers = containers
			Expect(k8sClient.Create(context.Background(), &daemonset)).To(Succeed())
		})

		AfterEach(func() {
			deleteVpa("test-daemonset-daemonset")
			Expect(k8sClient.Delete(context.Background(), &daemonset)).To(Succeed())
		})

		It("should create a vpa", func() {
			expectVpa("test-daemonset-daemonset")
		})
	})

})
