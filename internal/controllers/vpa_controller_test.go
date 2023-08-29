package controllers_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("VpaController", func() {

	Context("when creating a deployment and a hand-crafted vpa afterwards", func() {
		var deployment *appsv1.Deployment
		var vpa *vpav1.VerticalPodAutoscaler

		BeforeEach(func() {
			deployment = makeDeployment()
			Expect(k8sClient.Create(context.Background(), deployment)).To(Succeed())
			vpa = &vpav1.VerticalPodAutoscaler{}
			vpa.Name = "test-deployment-custom-vpa"
			vpa.Namespace = metav1.NamespaceDefault
			vpa.Spec.TargetRef = &autoscalingv1.CrossVersionObjectReference{
				Name:       deploymentName,
				Kind:       "Deployment",
				APIVersion: "v1",
			}
			Expect(k8sClient.Create(context.Background(), vpa)).To(Succeed())
		})

		AfterEach(func() {
			deleteVpa("test-deployment-custom-vpa")
			deleteVpa("test-deployment-deployment")
			Expect(k8sClient.Delete(context.Background(), deployment)).To(Succeed())
		})

		It("should delete the served vpa", func() {
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

		BeforeEach(func() {
			deployment = makeDeployment()
			Expect(k8sClient.Create(context.Background(), deployment)).To(Succeed())
		})

		AfterEach(func() {
			deleteVpa("test-deployment-deployment")
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
			Expect(k8sClient.Patch(context.Background(), &vpa, client.MergeFrom(unmodified))).To(Succeed())

			Eventually(func(g Gomega) bool {
				err := k8sClient.Get(context.Background(), name, &vpa)
				return errors.IsNotFound(err)
			}).Should(BeTrue())
		})

	})
})
