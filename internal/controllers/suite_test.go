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
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/vpa_butler/internal/controllers"
	"github.com/sapcc/vpa_butler/internal/metrics"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controllers Suite")
}

var (
	testEnv        *envtest.Environment
	k8sManager     ctrl.Manager
	k8sClient      client.Client
	stopController context.CancelFunc

	testMinAllowedCPU    = resource.MustParse("100m")
	testMinAllowedMemory = resource.MustParse("128Mi")
)

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	metrics.RegisterMetrics()

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{"../../test/crds"},
	}

	cfg, err := testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = vpav1.AddToScheme(testEnv.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = corev1.AddToScheme(testEnv.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = appsv1.AddToScheme(testEnv.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sManager, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: testEnv.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	err = (&controllers.VpaController{
		Client:           k8sManager.GetClient(),
		Log:              GinkgoLogr.WithName("vpa-controller"),
		Scheme:           k8sManager.GetScheme(),
		Version:          "test",
		MinAllowedCPU:    testMinAllowedCPU,
		MinAllowedMemory: testMinAllowedMemory,
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	Expect(controllers.SetupForAppsV1(k8sManager)).To(Succeed())

	Expect(k8sManager.Add(&controllers.VpaRunnable{
		Client:          k8sManager.GetClient(),
		Period:          100 * time.Millisecond,
		JitterFactor:    1,
		CapacityPercent: 90,
		Log:             GinkgoLogr.WithName("vpa-runnable"),
	})).To(Succeed())

	go func() {
		stopCtx, cancel := context.WithCancel(ctrl.SetupSignalHandler())
		stopController = cancel
		Expect(k8sManager.Start(stopCtx)).To(Succeed())
	}()

	k8sClient, err = client.New(cfg, client.Options{Scheme: testEnv.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClient).ToNot(BeNil())

	SetDefaultEventuallyTimeout(3 * time.Second)
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	stopController()
	Expect(testEnv.Stop()).To(Succeed())
})
