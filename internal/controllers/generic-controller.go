package controllers

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/sapcc/vpa_butler/internal/common"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
)

type GenericController[T client.Object] struct {
	client.Client
	typeName string
	log      logr.Logger
	scheme   *runtime.Scheme
}

func (v *GenericController[T]) SetupWithManager(mgr ctrl.Manager) error {
	var instance T
	v.typeName = reflect.TypeOf(instance).Name()
	name := fmt.Sprintf("%s-controller", v.typeName)
	v.Client = mgr.GetClient()
	v.log = mgr.GetLogger().WithName(name)
	v.scheme = mgr.GetScheme()
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(instance).
		WithOptions(controller.Options{MaxConcurrentReconciles: 10, Log: v.log}).
		Complete(v)
}

func (v *GenericController[T]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var instance T
	if err := v.Get(ctx, req.NamespacedName, instance); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	result, err := common.ReconcileVPA(ctx, v.Client, v.scheme, instance, v.log)
	if err != nil {
		return ctrl.Result{}, err
	}
	switch result {
	case common.OperationResultCreated, common.OperationResultUpdated:
		v.log.Info(fmt.Sprintf("VPA for %s was %s", v.typeName, result), "namespace", req.Namespace, "name", req.Name)
	}

	return ctrl.Result{}, nil
}
