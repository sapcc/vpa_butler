package controllers_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/pointer"
)

var _ = Describe("GenericControllers", func() {

	labels := map[string]string{"app": "test-deployment"}
	selector := metav1.LabelSelector{
		MatchLabels: labels,
	}
	containers := []corev1.Container{
		{
			Name:  "test-container",
			Image: "nginx",
		},
	}

	expectVPA := func(name string) {
		var vpaRef types.NamespacedName
		vpaRef.Name = name
		vpaRef.Namespace = metav1.NamespaceDefault
		Eventually(func() error {
			var vpa vpav1.VerticalPodAutoscaler
			return k8sClient.Get(context.Background(), vpaRef, &vpa)
		}).Should(Succeed())
	}

	deleteVPA := func(name string) {
		var vpaRef types.NamespacedName
		vpaRef.Name = name
		vpaRef.Namespace = metav1.NamespaceDefault
		var vpa vpav1.VerticalPodAutoscaler
		Expect(k8sClient.Get(context.Background(), vpaRef, &vpa)).To(Succeed())
		Expect(k8sClient.Delete(context.Background(), &vpa)).To(Succeed())
	}

	Context("when creating a deployment", func() {
		var deployment appsv1.Deployment

		BeforeEach(func() {
			deployment.Name = "test-deployment"
			deployment.Namespace = metav1.NamespaceDefault
			deployment.Spec.Selector = &selector
			deployment.Spec.Template.ObjectMeta.Labels = labels
			deployment.Spec.Replicas = pointer.Int32(1)
			deployment.Spec.Template.Spec.Containers = containers
			Expect(k8sClient.Create(context.Background(), &deployment)).To(Succeed())
		})

		It("should create a VPA", func() {
			expectVPA("test-deployment-deployment")
		})

		AfterEach(func() {
			deleteVPA("test-deployment-deployment")
			Expect(k8sClient.Delete(context.Background(), &deployment)).To(Succeed())
		})

	})

	Context("when creating a statefulset", func() {
		var statefulset appsv1.StatefulSet

		BeforeEach(func() {
			statefulset.Name = "test-statefulset"
			statefulset.Namespace = metav1.NamespaceDefault
			statefulset.Spec.Selector = &selector
			statefulset.Spec.Template.ObjectMeta.Labels = labels
			statefulset.Spec.Replicas = pointer.Int32(1)
			statefulset.Spec.Template.Spec.Containers = containers
			Expect(k8sClient.Create(context.Background(), &statefulset)).To(Succeed())
		})

		It("should create a VPA", func() {
			expectVPA("test-statefulset-statefulset")
		})

		AfterEach(func() {
			deleteVPA("test-statefulset-statefulset")
			Expect(k8sClient.Delete(context.Background(), &statefulset)).To(Succeed())
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

		It("should create a VPA", func() {
			expectVPA("test-daemonset-daemonset")
		})

		AfterEach(func() {
			deleteVPA("test-daemonset-daemonset")
			Expect(k8sClient.Delete(context.Background(), &daemonset)).To(Succeed())
		})

	})

})
