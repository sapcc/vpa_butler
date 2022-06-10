package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/sapcc/vpa_butler/internal/common"
)

type VPAStatefulSetController struct {
	client.Client
	log    logr.Logger
	scheme *runtime.Scheme
}

func (v *VPAStatefulSetController) SetupWithManager(mgr ctrl.Manager) error {
	name := "statefulset-controller"
	v.Client = mgr.GetClient()
	v.log = mgr.GetLogger().WithName(name)
	v.scheme = mgr.GetScheme()
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&appsv1.StatefulSet{}).
		Watches(&source.Kind{Type: &vpav1.VerticalPodAutoscaler{}}, &handler.EnqueueRequestForObject{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 10, Log: v.log}).
		Complete(v)
}

func (v *VPAStatefulSetController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var statefulset = new(appsv1.StatefulSet)
	if err := v.Get(ctx, req.NamespacedName, statefulset); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	result, err := common.ReconcileVPA(ctx, v.Client, v.scheme, statefulset, statefulset.Spec.Template.Spec.Containers)
	if err != nil {
		return ctrl.Result{}, err
	}
	switch result {
	case common.OperationResultCreated, common.OperationResultUpdated:
		v.log.Info(fmt.Sprintf("VPA for StatefulSet was %s", result), "namespace", statefulset.Namespace, "name", statefulset.Name)
	}

	return ctrl.Result{}, nil
}
