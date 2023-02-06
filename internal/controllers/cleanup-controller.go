package controllers

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/sapcc/vpa_butler/internal/common"
	"k8s.io/apimachinery/pkg/runtime"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
)

type VPACleanupController struct {
	client.Client
	log    logr.Logger
	scheme *runtime.Scheme
}

func (v *VPACleanupController) SetupWithManager(mgr ctrl.Manager) error {
	name := "cleanup-controller"
	v.Client = mgr.GetClient()
	v.log = mgr.GetLogger().WithName(name)
	v.scheme = mgr.GetScheme()
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&vpav1.VerticalPodAutoscaler{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1, Log: v.log}).
		Complete(v)
}

func (v *VPACleanupController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var vpa = new(vpav1.VerticalPodAutoscaler)
	if err := v.Get(ctx, req.NamespacedName, vpa); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !common.IsNewNamingSchema(vpa.GetName()) && common.IsHandleVPA(vpa) {
		err := v.Delete(ctx, vpa)
		if err != nil {
			return ctrl.Result{}, err
		}
		v.log.Info("Cleanup old VPA successful", "namespace", vpa.GetNamespace(), "name", vpa.GetName())
	}

	return ctrl.Result{}, nil
}
